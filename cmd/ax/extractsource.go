package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/fetch"
	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/latexml"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/source"
)

// extractSource reads a cached e-print and writes the paper into the content
// plane.
//
// The same command as ax extract render with three steps in front of it: unpack
// the submission, find the file the document starts from, and run LaTeXML over
// it. What comes out is the same markup arXiv's rendering is, so everything
// after the conversion is the code the render path already uses, down to the
// reject rule and the front matter.
//
// This is the path for most of arXiv. arXiv began rendering submissions in
// December 2023 and does not backfill, so every paper announced before then
// arrives here, and so does every paper whose rendering was rejected.
func extractSource(args []string) error {
	fs := flag.NewFlagSet("ax extract source", flag.ContinueOnError)
	dry := fs.Bool("n", false, "convert, report what it holds, and write nothing")
	outline := fs.Bool("outline", false, "print every heading and block, and not just the counts")
	force := fs.Bool("force", false, "overwrite a file somebody has corrected by hand")
	lang := fs.String("lang", "en", "the language directory to write into")
	main := fs.String("main", "", "the file the document starts from, for a submission that holds more than one")
	budget := fs.Duration("timeout", latexml.Timeout, "how long one conversion gets before it is stopped")
	again := fs.Bool("again", false, "convert again even if this version has been converted already")
	// Two versions of LaTeXML convert the same paper differently, so which one
	// is on the PATH is a thing somebody eventually needs to be able to choose
	// without editing their PATH to do it.
	binary := fs.String("latexmlc", latexml.Binary, "the LaTeXML to run, for a machine with more than one or with it somewhere unusual")
	post := fs.String("latexmlpost", latexml.Post, "the LaTeXML post processor to run, which ships with the same install")
	if err := fs.Parse(args); err != nil {
		return err
	}
	refs := fs.Args()
	if len(refs) == 0 {
		return errors.New("usage: ax extract source [-n] <id>v<n> [...]")
	}
	if len(refs) > 1 && *main != "" {
		return errors.New("-main names a file inside one submission, so it takes one paper at a time")
	}
	// Every reference is read before anything is converted, so that a typo in
	// the tenth of ten papers is a refusal now rather than in twenty minutes.
	ids := make([]axid.ID, len(refs))
	for i, ref := range refs {
		id, err := axid.Parse(ref)
		if err != nil {
			return err
		}
		ids[i] = id
	}

	root := corpusRoot()
	plane := metadata.Plane{Root: root}
	manifest, err := fetch.Load(corpus.SourcesPath(root))
	if err != nil {
		return err
	}
	conv := &latexml.Converter{Binary: *binary, Post: *post, Timeout: *budget}
	if err := conv.Available(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if v, err := conv.Version(ctx); err == nil {
		fmt.Fprintln(os.Stderr, v)
	}

	rejected, native := 0, 0
	for i, id := range ids {
		p, entry, res, err := readSubmission(ctx, conv, root, manifest, id, refs[i], *main, *again)
		// A submission that is a PDF is a fact about the paper and not a failure
		// of the command, the same way a version arXiv never rendered is on the
		// fetch side, so a batch of a hundred papers steps over it rather than
		// stopping on the first one.
		var pdf *source.PDFOnly
		if errors.As(err, &pdf) {
			native++
			fmt.Fprintf(os.Stderr, "%v\n", err)
			continue
		}
		if err != nil {
			return err
		}
		printPaper(p, *outline)
		if r, bad := refuse(p, res); bad {
			rejected++
			fmt.Fprintf(os.Stderr, "%sv%d: %v\n", p.ID, p.Version, r)
			for _, f := range r.Faults {
				fmt.Fprintf(os.Stderr, "  at %s: %s\n", f.Where, f.Text)
			}
			continue
		}
		if *dry {
			continue
		}
		if err := writePaper(plane, id, p, entry, *lang, *force); err != nil {
			return err
		}
	}
	if native > 0 {
		fmt.Fprintf(os.Stderr, "%d of %d submissions are a PDF and not TeX, so those papers are on the native path\n", native, len(refs))
	}
	if rejected > 0 {
		// The render path can say a rejected paper falls through to here. This
		// path has nowhere further down that is written yet, so it says what it
		// is rather than promising a path that does not exist.
		return fmt.Errorf("%d of %d conversions are too broken to use, and those papers need the native path or a person", rejected, len(refs))
	}
	return nil
}

// readSubmission unpacks a cached e-print, converts it and parses the result.
//
// The manifest and not the disk says where the bytes are, for the same reason
// the render path reads it that way: an e-print nobody recorded is an e-print
// nobody knows the licence of.
func readSubmission(ctx context.Context, conv *latexml.Converter, root string, m fetch.Manifest, id axid.ID, ref, named string, again bool) (*extract.Paper, fetch.Source, latexml.Result, error) {
	var res latexml.Result
	if id.Version < 1 {
		return nil, fetch.Source{}, res, fmt.Errorf("%s names no version, and a submission is of one version", ref)
	}
	entry, ok := m.Find(id.Canonical, id.Version, fetch.RouteSource)
	if !ok {
		return nil, entry, res, fmt.Errorf("the manifest has no e-print of %sv%d, so run ax fetch source %sv%d first", id.Canonical, id.Version, id.Canonical, id.Version)
	}
	bundle, err := source.Read(filepath.Join(root, filepath.FromSlash(entry.Path)))
	if err != nil {
		return nil, entry, res, fmt.Errorf("%s: %w", entry.Ref(), err)
	}

	start := named
	if start == "" {
		start, err = bundle.Main()
		if err != nil {
			return nil, entry, res, fmt.Errorf("%s: %w, which -main does", entry.Ref(), err)
		}
	} else if _, ok := bundle.Find(start); !ok {
		return nil, entry, res, fmt.Errorf("%s holds no %s, and it holds %v", entry.Ref(), start, bundle.Names())
	}

	dir := corpus.EPrintDir(root, id, id.Version)
	// Unpacked every time rather than only when the directory is missing. The
	// bytes are one hash in the manifest and the files under work/ are not, so
	// a submission somebody has edited by hand would otherwise be converted as
	// though it were what arXiv served.
	if err := os.RemoveAll(dir); err != nil {
		return nil, entry, res, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, entry, res, err
	}
	if err := bundle.Write(dir); err != nil {
		return nil, entry, res, err
	}

	dest := corpus.ConvertedPath(root, id, id.Version)
	res, fresh, err := converted(ctx, conv, dir, start, dest, entry.Ref(), again)
	if err != nil {
		return nil, entry, res, err
	}
	p, err := parse(res, id, entry)
	if err != nil {
		return nil, entry, res, err
	}
	if _, bad := refuse(p, res); bad && fresh && !conv.IncludeStyles {
		p, res, err = styles(ctx, conv, dir, start, dest, id, entry, p, res)
		if err != nil {
			return nil, entry, res, err
		}
	}
	if fresh {
		// The log holds the line explaining why a macro was ignored and nothing
		// else holds it, so it is kept beside the document rather than printed.
		if err := os.WriteFile(dest+".log", res.Log, 0o644); err != nil {
			return nil, entry, res, err
		}
	}
	return p, entry, res, nil
}

// refuse decides whether this conversion can be used.
//
// The reject rule and one thing the reject rule cannot see. It counts the
// errors LaTeXML left in the document, which works for a conversion that ran to
// the end, and a conversion that stopped partway through has not left them
// there: LaTeXML gives up after a hundred errors and writes what it had, so a
// paper that lost its packages in the preamble arrives as a title with nothing
// under it and no faults in it at all. That document reads as a clean paper of
// no sections, and status 3 is the only thing that says otherwise.
func refuse(p *extract.Paper, res latexml.Result) (extract.Rejection, bool) {
	if r, bad := p.Reject(); bad {
		return r, true
	}
	if res.Status == latexml.StatusFatal {
		return extract.Rejection{Why: "LaTeXML stopped partway through, and what it wrote is the front of the paper rather than the paper"}, true
	}
	return extract.Rejection{}, false
}

// styles converts a second time, with the submission's own style files.
//
// The first pass is LaTeXML with the packages it models and nothing else, which
// is what arXiv runs and is the safer of the two. A paper the reject rule threw
// out has usually lost a macro rather than a sentence, and most of the time that
// macro is one the author defined in a .sty they shipped in the tarball, which
// the first pass skipped. Reading it is the obvious second try.
//
// It is a second try and not the default because a .sty is a program. LaTeXML
// reading one is LaTeXML running low level TeX it may have no answer for, so
// the pass that reads them can come out worse than the pass that did not, and
// when it does the first document is put back.
func styles(ctx context.Context, conv *latexml.Converter, dir, main, dest string, id axid.ID, entry fetch.Source, first *extract.Paper, res latexml.Result) (*extract.Paper, latexml.Result, error) {
	why, _ := refuse(first, res)
	fmt.Fprintf(os.Stderr, "%s was rejected: %s\n", entry.Ref(), why.Why)
	fmt.Fprintf(os.Stderr, "converting it again with the style files the submission ships, in case what it lost is something the author defined\n")
	with := *conv
	with.IncludeStyles = true
	second, _, err := converted(ctx, &with, dir, main, dest, entry.Ref(), true)
	if err != nil {
		return nil, res, err
	}
	p, err := parse(second, id, entry)
	if err != nil {
		return nil, res, err
	}
	if _, bad := refuse(p, second); !bad {
		fmt.Fprintf(os.Stderr, "%s: reading the style files fixed it\n", entry.Ref())
		return p, second, nil
	}
	// Better and still not good enough is worth keeping, because the errors it
	// has left are the ones somebody looking at this paper has to read, and a
	// hundred fewer of them is a shorter afternoon. A pass that stopped partway
	// through is never the better of the two, however few faults are in what it
	// wrote, because most of the paper is not in it to have faults.
	if second.Status != latexml.StatusFatal && holes(p) < holes(first) {
		fmt.Fprintf(os.Stderr, "%s: reading the style files took the errors inside the body from %d to %d, and the paper is still rejected\n", entry.Ref(), holes(first), holes(p))
		return p, second, nil
	}
	// The first document is written back rather than converted again, because
	// the bytes are still here and a third conversion would cost minutes to
	// arrive at something already in hand. Both files of it: a page from one
	// pass beside the XML of the other is a set of labels that point at anchors
	// which are not there.
	fmt.Fprintf(os.Stderr, "%s: reading the style files did not help, so the first conversion is the one kept\n", entry.Ref())
	if err := os.WriteFile(dest, res.HTML, 0o644); err != nil {
		return nil, res, err
	}
	if len(res.XML) > 0 {
		if err := os.WriteFile(latexml.XMLPath(dest), res.XML, 0o644); err != nil {
			return nil, res, err
		}
	}
	return first, res, nil
}

// holes is how many conversion errors landed inside the body of the paper.
//
// The count the reject rule cares about. One in a bibliography entry is a
// reference that prints badly and one in a section body is a sentence of the
// paper that is gone, so comparing two conversions by their total would call a
// pass that lost a paragraph and fixed nine citations an improvement.
func holes(p *extract.Paper) int {
	n := 0
	for _, f := range p.Faults {
		if f.Where != "bibliography" {
			n++
		}
	}
	return n
}

// parse reads the document, and puts the author's labels back on it.
//
// Two files and not one, because the name the author gave a theorem is in
// neither the page LaTeXML writes nor the one arXiv serves. The post processor
// turns every \label into the number the paper prints and the name does not
// reach the page, so it is read out of the XML the post processor was given and
// matched back on by the anchor, which is the same string in both.
func parse(res latexml.Result, id axid.ID, entry fetch.Source) (*extract.Paper, error) {
	p, err := extract.Parse(res.HTML, id.Canonical, id.Version)
	if err != nil {
		return nil, fmt.Errorf("%s converted and the result does not read as a LaTeXML document: %w", entry.Ref(), err)
	}
	if len(res.XML) == 0 {
		return p, nil
	}
	byID, err := extract.Labels(res.XML)
	if err != nil {
		// Not a rejection. A paper with no labels on it is a paper ax tags diff
		// follows by its second pass instead, which is what every paper off the
		// render path is, so a document that will not parse as XML costs the
		// stronger pass and not the extraction.
		fmt.Fprintf(os.Stderr, "%s: the conversion's XML does not read, so this paper carries no labels: %v\n", entry.Ref(), err)
		return p, nil
	}
	p.Label(byID)
	return p, nil
}

// converted runs the conversion, or reads the one that is already there.
//
// A conversion of a long paper is minutes, and a person looking at the same
// paper twice in a row is the ordinary way this command is used, so the
// document is kept under work/ and reused. It is this project's own output and
// not arXiv's bytes, so nothing is lost by throwing it away, which is what
// -again does.
func converted(ctx context.Context, conv *latexml.Converter, dir, main, dest, ref string, again bool) (latexml.Result, bool, error) {
	if !again {
		if body, err := os.ReadFile(dest); err == nil {
			fmt.Fprintf(os.Stderr, "%s was converted already, at %s, and -again converts it afresh\n", ref, dest)
			// The log is read back with it, because the status is in the log and
			// nowhere else, and a document reused without its status is one this
			// cannot tell a clean conversion from a conversion that stopped. The
			// XML for the same reason: the author's labels are in it and in
			// nothing else that was kept.
			log, _ := os.ReadFile(dest + ".log")
			xml, _ := os.ReadFile(latexml.XMLPath(dest))
			return latexml.Result{
				HTML:    body,
				XML:     xml,
				Dest:    dest,
				Status:  latexml.Status(log),
				Log:     log,
				Missing: latexml.Missing(log),
			}, false, nil
		}
	}
	fmt.Fprintf(os.Stderr, "converting %s from %s\n", ref, main)
	// A long paper is minutes of silence otherwise, so the lines that say
	// something went wrong are passed through as they arrive. Only those lines,
	// because LaTeXML says several hundred about packages it loaded, and only
	// the first few of them, because a paper that lost a package says the same
	// thing three hundred times and the whole of it is in the log.
	shown := 0
	conv.Log = func(line string) {
		if !strings.HasPrefix(line, "Error:") && !strings.HasPrefix(line, "Fatal:") {
			return
		}
		shown++
		switch {
		case shown <= errorLines:
			fmt.Fprintf(os.Stderr, "  %s\n", line)
		case shown == errorLines+1:
			fmt.Fprintf(os.Stderr, "  and more, all of which are in %s\n", dest+".log")
		}
	}
	res, err := conv.Convert(ctx, dir, main, dest)
	if err != nil {
		return res, true, fmt.Errorf("%s: %w", ref, err)
	}
	note := map[int]string{
		latexml.StatusClean:    "no obvious problems",
		latexml.StatusWarnings: "warnings, which LaTeXML worked around",
		latexml.StatusErrors:   "errors, and whether they matter is the reject rule's question",
		latexml.StatusFatal:    "a document LaTeXML gave up partway through",
	}[res.Status]
	if note == "" {
		note = "a status this does not recognise"
	}
	fmt.Fprintf(os.Stderr, "converted %s in %s with %s\n", ref, res.Took.Round(time.Millisecond), note)
	unread(res)
	return res, true, nil
}

// errorLines is how many of LaTeXML's error lines are worth printing as they
// arrive.
const errorLines = 12

// unread says which packages LaTeXML could not read, and what that means.
//
// A paper with three hundred undefined macros in it has one cause and the
// errors are all symptoms, so the useful sentence is the one naming the
// packages rather than the three hundred naming the commands those packages
// would have defined.
//
// The two cases are different problems with different answers. A package
// LaTeXML has no binding for is usually one the author wrote and shipped in the
// tarball, and the second pass reads it. A package it does have a binding for
// loads the real .sty out of a TeX tree, and on a machine with no TeX
// installed there is nothing to read, which is one install to fix and nothing
// this project can do about it.
func unread(res latexml.Result) {
	if len(res.Missing) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "  LaTeXML could not read %s: %s\n",
		prose.Count(len(res.Missing), "package"), strings.Join(res.Missing, ", "))
	if !latexml.TeX() {
		fmt.Fprintf(os.Stderr, "  and this machine has no TeX installation, so the packages LaTeXML models by reading the real .sty, which is TikZ among others, could not load at all\n")
	}
}

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
	conv := &latexml.Converter{Binary: *binary, Timeout: *budget}
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
		p, entry, err := readSubmission(ctx, conv, root, manifest, id, refs[i], *main, *again)
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
		report(p, *outline)
		if r, bad := p.Reject(); bad {
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
func readSubmission(ctx context.Context, conv *latexml.Converter, root string, m fetch.Manifest, id axid.ID, ref, named string, again bool) (*extract.Paper, fetch.Source, error) {
	if id.Version < 1 {
		return nil, fetch.Source{}, fmt.Errorf("%s names no version, and a submission is of one version", ref)
	}
	entry, ok := m.Find(id.Canonical, id.Version, fetch.RouteSource)
	if !ok {
		return nil, entry, fmt.Errorf("the manifest has no e-print of %sv%d, so run ax fetch source %sv%d first", id.Canonical, id.Version, id.Canonical, id.Version)
	}
	bundle, err := source.Read(filepath.Join(root, filepath.FromSlash(entry.Path)))
	if err != nil {
		return nil, entry, fmt.Errorf("%s: %w", entry.Ref(), err)
	}

	start := named
	if start == "" {
		start, err = bundle.Main()
		if err != nil {
			return nil, entry, fmt.Errorf("%s: %w, which -main does", entry.Ref(), err)
		}
	} else if _, ok := bundle.Find(start); !ok {
		return nil, entry, fmt.Errorf("%s holds no %s, and it holds %v", entry.Ref(), start, bundle.Names())
	}

	dir := corpus.EPrintDir(root, id, id.Version)
	// Unpacked every time rather than only when the directory is missing. The
	// bytes are one hash in the manifest and the files under work/ are not, so
	// a submission somebody has edited by hand would otherwise be converted as
	// though it were what arXiv served.
	if err := os.RemoveAll(dir); err != nil {
		return nil, entry, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, entry, err
	}
	if err := bundle.Write(dir); err != nil {
		return nil, entry, err
	}

	dest := corpus.ConvertedPath(root, id, id.Version)
	res, err := converted(ctx, conv, dir, start, dest, entry.Ref(), again)
	if err != nil {
		return nil, entry, err
	}
	p, err := extract.Parse(res.HTML, id.Canonical, id.Version)
	if err != nil {
		return nil, entry, fmt.Errorf("%s converted and the result does not read as a LaTeXML document: %w", entry.Ref(), err)
	}
	return p, entry, nil
}

// converted runs the conversion, or reads the one that is already there.
//
// A conversion of a long paper is minutes, and a person looking at the same
// paper twice in a row is the ordinary way this command is used, so the
// document is kept under work/ and reused. It is this project's own output and
// not arXiv's bytes, so nothing is lost by throwing it away, which is what
// -again does.
func converted(ctx context.Context, conv *latexml.Converter, dir, main, dest, ref string, again bool) (latexml.Result, error) {
	if !again {
		if body, err := os.ReadFile(dest); err == nil {
			fmt.Fprintf(os.Stderr, "%s was converted already, at %s, and -again converts it afresh\n", ref, dest)
			return latexml.Result{HTML: body, Dest: dest}, nil
		}
	}
	fmt.Fprintf(os.Stderr, "converting %s from %s\n", ref, main)
	// A long paper is minutes of silence otherwise. Only the lines that say
	// something went wrong, because LaTeXML says several hundred about packages
	// it loaded and a person watching wants the four that matter.
	conv.Log = func(line string) {
		if strings.HasPrefix(line, "Error:") || strings.HasPrefix(line, "Fatal:") {
			fmt.Fprintf(os.Stderr, "  %s\n", line)
		}
	}
	res, err := conv.Convert(ctx, dir, main, dest)
	if err != nil {
		return res, fmt.Errorf("%s: %w", ref, err)
	}
	note := map[int]string{
		latexml.StatusClean:    "no obvious problems",
		latexml.StatusWarnings: "warnings, which LaTeXML worked around",
		latexml.StatusErrors:   "errors, and whether they matter is the reject rule's question",
	}[res.Status]
	if note == "" {
		note = "a status this does not recognise"
	}
	fmt.Fprintf(os.Stderr, "converted %s in %s with %s\n", ref, res.Took.Round(time.Millisecond), note)
	// The log holds the line explaining why a macro was ignored and nothing
	// else holds it, so it is kept beside the document rather than printed.
	if err := os.WriteFile(dest+".log", res.Log, 0o644); err != nil {
		return res, err
	}
	return res, nil
}

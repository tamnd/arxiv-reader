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
	"text/tabwriter"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/fetch"
	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/pdftext"
	"github.com/tamnd/arxiv-reader/selection"
	"github.com/tamnd/arxiv-reader/source"
)

// runPath decides which of the four ways each selected paper gets read.
//
// It is the other half of the selection: the reasons say why a paper is in the
// content plane and the path says what reading it will cost. Three of the four
// paths cost nothing but local work, the fourth costs a model call a page, and the
// difference between a corpus of a thousand papers and a bill is which papers end
// up on it.
func runPath(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ax path decide|list")
	}
	switch args[0] {
	case "decide":
		return pathDecide(args[1:])
	case "list":
		return pathList(args[1:])
	default:
		return fmt.Errorf("unknown path subcommand %q, which is decide or list", args[0])
	}
}

// pathDecide works out the path for the papers in the selection and writes it down.
//
// Everything it decides on is either already on disk or one HEAD away, and the
// order it asks in is the order of the decision tree rather than the order of the
// facts: a paper whose rendering is already fetched is decided without opening a
// file, because the rendering existing is arXiv saying the submission held TeX.
func pathDecide(args []string) error {
	fs := flag.NewFlagSet("ax path decide", flag.ContinueOnError)
	shard := fs.String("shard", "", "only the papers announced in one month, as YYMM")
	probe := fs.Bool("probe", false, "ask arXiv whether it renders a version nothing on disk answers for, at one request per paper")
	again := fs.Bool("again", false, "decide the papers that already have a path as well")
	binary := fs.String("pdftotext", "", "the pdftotext to run, for reading whether a PDF holds text")
	pace := fs.Duration("pace", fetch.Pace, "the gap to leave between probes")
	dry := fs.Bool("n", false, "decide and print, and write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *pace < fetch.Pace {
		return fmt.Errorf("a pace of %s is faster than the fifteen seconds arXiv asks for on the website, and a probe reads the website", *pace)
	}

	root := corpusRoot()
	path := corpus.SelectedPath(root)
	m, err := selection.Load(path)
	if err != nil {
		return err
	}
	sources, err := fetch.Load(corpus.SourcesPath(root))
	if err != nil {
		return err
	}

	want, err := pathWanted(m, fs.Args(), *shard, *again)
	if err != nil {
		return err
	}
	// Nothing to do is a success and not a failure, because this is a command that
	// gets run over a month at a time and the second run of it should cost nothing
	// and say so. A paper that already has a path keeps it.
	if len(want) == 0 {
		fmt.Fprintln(os.Stderr, "nothing in the selection needs a path, and a paper that has one keeps it unless -again says otherwise")
		return nil
	}

	// The reader is built once for the run and not once per paper, so a machine
	// with no poppler hears about it once. It is only wanted for the papers that
	// reach the text layer question, which is the PDF only ones.
	text := &pdftext.Reader{Binary: *binary}
	if err := text.Available(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, "the papers with TeX are still decided, and the PDF only ones are the ones that need this")
		text = nil
	}
	f := &fetch.Fetcher{
		UserAgent: userAgent(),
		Pace:      *pace,
		Log:       func(ref string) { fmt.Fprintf(os.Stderr, "asking arXiv about %s\n", ref) },
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var decided, held, probes int
	out := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, e := range want {
		facts, asked, err := pathFacts(ctx, root, sources, text, f, e, *probe)
		probes += asked
		if err != nil {
			return err
		}
		chosen, why, err := selection.Decide(facts)
		var missing *selection.Missing
		if errors.As(err, &missing) {
			// A paper nobody has gathered the facts for is not a paper for the
			// vision path. It is a paper that needs one more cheap thing done to
			// it, and saying which is the whole use of this run.
			held++
			fmt.Fprintf(os.Stderr, "%s: %v\n", e.Ref(), err)
			continue
		}
		if err != nil {
			return err
		}
		e.Path, e.PathWhy = chosen, why
		m.Put(e)
		decided++
		fmt.Fprintf(out, "%s\t%s\t%s\n", e.Ref(), chosen, why)
	}
	out.Flush()

	if !*dry && decided > 0 {
		if err := m.Save(path); err != nil {
			return err
		}
	}
	counts := m.ByPath()
	tw := tabwriter.NewWriter(os.Stderr, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "  papers\t%d, %d decided now, %d nothing can decide yet\n", len(want), decided, held)
	if probes > 0 {
		fmt.Fprintf(tw, "  probes\t%d\n", probes)
	}
	for _, p := range selection.Paths {
		fmt.Fprintf(tw, "  %s\t%d\n", p, counts[p])
	}
	fmt.Fprintf(tw, "  undecided\t%d\n", counts[""])
	tw.Flush()
	if *dry {
		fmt.Fprintf(os.Stderr, "nothing written, because this was a dry run\n")
	}
	if held == 1 {
		return errors.New("1 paper has no path yet, and the line above it says what would settle that")
	}
	if held > 1 {
		return fmt.Errorf("%d papers have no path yet, and the line above each one says what would settle that", held)
	}
	return nil
}

// pathWanted is the papers this run is about.
//
// A paper that already has a path is left alone unless -again says otherwise, which
// is what makes running this over a month twice cost nothing. Naming papers on the
// command line is the exception: somebody asking about one paper by name means that
// one, whatever it already says.
func pathWanted(m selection.Manifest, refs []string, shard string, again bool) ([]selection.Entry, error) {
	if len(refs) > 0 {
		out := make([]selection.Entry, 0, len(refs))
		for _, ref := range refs {
			id, err := axid.Parse(ref)
			if err != nil {
				return nil, err
			}
			e, ok := m.Find(id.Canonical)
			if !ok {
				return nil, fmt.Errorf("%s is not in the selection, so run ax select add first", id.Canonical)
			}
			out = append(out, e)
		}
		return out, nil
	}
	out := make([]selection.Entry, 0, len(m.Selected))
	for _, e := range m.Selected {
		if e.Path != "" && !again {
			continue
		}
		if shard != "" {
			id, err := axid.Parse(e.ID)
			if err != nil {
				return nil, err
			}
			if corpus.Shard(id) != shard {
				continue
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// pathFacts gathers what the decision needs, in the order the tree asks for it.
//
// In that order on purpose. Every fact costs something, even if most of them only
// cost opening a file, and a paper whose rendering is already fetched needs neither
// the e-print opened nor a request made. It returns how many requests it spent so
// the run can report them, because a probe is the one thing here that is paid for
// at fifteen seconds each.
func pathFacts(ctx context.Context, root string, sources fetch.Manifest, text *pdftext.Reader, f *fetch.Fetcher, e selection.Entry, probe bool) (selection.Facts, int, error) {
	var facts selection.Facts
	var probes int

	// A rendering on disk settles both questions at once. arXiv only renders
	// submissions that held TeX, so a rendering existing is arXiv saying so, and
	// asking anything else about the paper would be asking a question that is
	// already answered.
	if _, ok := sources.Find(e.ID, e.Version, fetch.RouteRender); ok {
		return selection.Facts{TeX: selection.Yes, Rendering: selection.Yes}, 0, nil
	}

	if held, ok := sources.Find(e.ID, e.Version, fetch.RouteSource); ok {
		// What the e-print held is in the manifest when the fetch that took it
		// wrote it down, and the manifest is preferred over the file because the
		// file is under work/, which is gitignored: a corpus cloned fresh has the
		// manifest and none of the bytes.
		switch held.Holds {
		case fetch.HoldsPDF:
			facts.TeX = selection.No
		case fetch.HoldsTeX:
			facts.TeX = selection.Yes
		default:
			file := filepath.Join(root, filepath.FromSlash(held.Path))
			_, err := source.Read(file)
			var pdfOnly *source.PDFOnly
			switch {
			case errors.As(err, &pdfOnly):
				facts.TeX = selection.No
			case err != nil:
				// The manifest says the e-print was fetched, it does not say what
				// was in it, and the file is not readable now. That is a fact about
				// this corpus rather than about the paper, so it is said out loud
				// and the paper is left undecided rather than guessed at.
				fmt.Fprintf(os.Stderr, "%s: %v\n", e.Ref(), err)
			default:
				facts.TeX = selection.Yes
			}
		}
	}

	if facts.TeX == selection.Yes {
		if probe {
			rendered, err := f.Rendered(ctx, e.Ref())
			probes++
			if err != nil {
				return facts, probes, err
			}
			facts.Rendering = selection.Yes
			if !rendered {
				facts.Rendering = selection.No
			}
		}
		return facts, probes, nil
	}

	if facts.TeX == selection.No {
		held, ok := sources.Find(e.ID, e.Version, fetch.RouteNative)
		switch {
		case !ok:
		case held.Text == fetch.Yes:
			facts.TextLayer = selection.Yes
		case held.Text == fetch.No:
			facts.TextLayer = selection.No
		case text == nil:
			// The PDF is here, nothing recorded what is in it, and this machine has
			// no poppler to look. That is worth saying rather than routing the paper
			// to the path that costs money by default.
			fmt.Fprintf(os.Stderr, "%s: the manifest does not say whether this PDF holds text and there is no pdftotext here to ask\n", e.Ref())
		default:
			doc, err := text.Read(ctx, filepath.Join(root, filepath.FromSlash(held.Path)))
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", e.Ref(), err)
				return facts, probes, nil
			}
			facts.TextLayer = selection.No
			if doc.Born() {
				facts.TextLayer = selection.Yes
			}
		}
	}
	return facts, probes, nil
}

// pathList prints what the selection says about the four paths.
//
// The number worth reading is the last one. A corpus with a thousand papers chosen
// and four hundred paths decided has six hundred papers whose cost nobody knows,
// and that is the number that decides whether the next stage can be planned.
func pathList(args []string) error {
	fs := flag.NewFlagSet("ax path list", flag.ContinueOnError)
	only := fs.String("path", "", "only the papers on one path, which is "+selection.PathNames())
	quiet := fs.Bool("q", false, "print the counts and not the papers")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("ax path list takes no arguments, so drop %s", fs.Args()[0])
	}
	var want selection.Path
	if *only != "" {
		p, err := selection.ParsePath(*only)
		if err != nil {
			return err
		}
		want = p
	}
	m, err := selection.Load(corpus.SelectedPath(corpusRoot()))
	if err != nil {
		return err
	}
	if !*quiet {
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		shown := 0
		for _, e := range m.Selected {
			if want != "" && e.Path != want {
				continue
			}
			where := string(e.Path)
			if where == "" {
				where = "undecided"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", e.Ref(), where, e.PathWhy)
			shown++
		}
		tw.Flush()
		if shown > 0 {
			fmt.Println()
		}
	}
	counts := m.ByPath()
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, p := range selection.Paths {
		fmt.Fprintf(tw, "%s\t%d\t%s\n", p, counts[p], p.Says())
	}
	fmt.Fprintf(tw, "undecided\t%d\t%s\n", counts[""], "nobody has worked out how these get read")
	tw.Flush()
	parts := make([]string, 0, len(selection.Paths))
	for _, p := range selection.Paths {
		if counts[p] > 0 {
			parts = append(parts, fmt.Sprintf("%d on %s", counts[p], p))
		}
	}
	fmt.Printf("\n%s in the content plane", prose.Count(len(m.Selected), "paper"))
	if len(parts) > 0 {
		fmt.Printf(": %s", strings.Join(parts, ", "))
	}
	fmt.Println()
	return nil
}

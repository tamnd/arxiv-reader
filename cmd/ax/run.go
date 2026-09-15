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
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/pdftext"
	"github.com/tamnd/arxiv-reader/selection"
)

// runRun takes the selection down its paths, a rung at a time.
//
// Everything this does can be done a paper at a time by hand, and for one paper
// that is what to do. This is for a thousand of them, where the run takes hours,
// something will go wrong in the middle of it, and the question that matters is
// what the next run costs. The answer here is that it costs only the work nobody
// has done: the rung each paper has reached is in the selection, the bytes each
// paper needed are in the fetch manifest, and between them there is nothing left
// for this command to remember.
//
// It does not stop on a paper that fails. A batch that gave up on the third paper
// of a thousand because one submission is a PostScript file from 1997 would be a
// batch nobody could leave running, so a failure is printed, counted, and the run
// carries on. The exit code says how it went.
func runRun(args []string) error {
	fs := flag.NewFlagSet("ax run", flag.ContinueOnError)
	shard := fs.String("shard", "", "only the papers announced in one month, as YYMM")
	only := fs.String("path", "", "only the papers on one path, which is "+selection.PathNames())
	to := fs.String("to", string(selection.StatusExtracted), "the rung to take every paper to, which is fetched, extracted or tagged")
	limit := fs.Int("limit", 0, "stop after this many papers that needed work, for a first run somebody is watching")
	dry := fs.Bool("n", false, "say what each paper would cost and do none of it")
	pace := fs.Duration("pace", fetch.Pace, "the gap to leave between requests")
	lang := fs.String("lang", "en", "the language directory to write into")
	latexmlc := fs.String("latexmlc", "", "the LaTeXML to run for the papers on the source path")
	latexmlpost := fs.String("latexmlpost", "", "the LaTeXML post processor to run with it")
	pdftotextBinary := fs.String("pdftotext", "", "the pdftotext to run for the papers on the native path")
	pdftoppmBinary := fs.String("pdftoppm", "", "the pdftoppm to run for the papers on the vision path")
	reader := fs.String("reader", "", "the program that reads a page, without which the papers on the vision path are left alone")
	model := fs.String("model", "", "the model the reader is told to use, recorded in every file it produced")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *pace < fetch.Pace {
		return fmt.Errorf("a pace of %s is faster than the fifteen seconds arXiv asks for on the website, and this reads the website", *pace)
	}
	target, err := selection.ParseStatus(*to)
	if err != nil {
		return err
	}
	// The three rungs this drives are the three something here can do. Naming one
	// of the others is not a mistake worth guessing about, so it says which part
	// of the pipeline has not been written rather than stopping one rung short and
	// reporting success.
	if selection.Rung(target) > selection.Rung(selection.StatusTagged) {
		return fmt.Errorf("nothing here translates or publishes yet, so -to %s has nothing to run, and the rungs this takes a paper to are fetched, extracted and tagged", target)
	}

	root := corpusRoot()
	selPath := corpus.SelectedPath(root)
	sel, err := selection.Load(selPath)
	if err != nil {
		return err
	}
	sources, err := fetch.Load(corpus.SourcesPath(root))
	if err != nil {
		return err
	}
	want, err := runWanted(sel, fs.Args(), *shard, *only)
	if err != nil {
		return err
	}
	if len(want) == 0 {
		fmt.Fprintln(os.Stderr, "nothing in the selection matches this run, so there is nothing to take anywhere")
		return nil
	}

	r := &runner{
		root:    root,
		plane:   metadata.Plane{Root: root},
		sources: sources,
		spath:   corpus.SourcesPath(root),
		sel:     sel,
		selpath: selPath,
		target:  target,
		lang:    *lang,
		dry:     *dry,
		tools: tools{
			latexmlc:    *latexmlc,
			latexmlpost: *latexmlpost,
			pdftotext:   *pdftotextBinary,
			pdftoppm:    *pdftoppmBinary,
			reader:      *reader,
			model:       *model,
		},
		f: &fetch.Fetcher{
			UserAgent: userAgent(),
			Pace:      *pace,
			Log:       func(ref string) { fmt.Fprintf(os.Stderr, "fetching %s\n", ref) },
		},
	}
	// Asked once for the run and not once per paper, the same way the fetch asks,
	// so a machine without poppler hears about it once. It is only wanted by the
	// papers that read a PDF.
	text := &pdftext.Reader{Binary: *pdftotextBinary}
	if err := text.Available(); err == nil {
		r.text = text
	} else if wantsPDF(want) {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, "the downloads carry on, and nothing will say which of these PDFs are scans")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var counts struct{ advanced, there, held, failed, demoted, worked int }
	// The number of papers this run is about, taken before the loop, because a paper
	// that gets sent round again is the same paper and not another one.
	papers := len(want)
	out := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	// A demoted paper goes round once more and no further. The render path is the
	// only path that can be wrong about itself, and it can only be wrong in one
	// direction, so a second pass is enough and a third would be a loop.
	for i := 0; i < len(want); i++ {
		if ctx.Err() != nil {
			fmt.Fprintf(os.Stderr, "stopped after %s, and everything done so far is written down\n", prose.Count(i, "paper"))
			break
		}
		if *limit > 0 && counts.worked >= *limit {
			fmt.Fprintf(os.Stderr, "stopping at %s, because that is the limit this run was given\n", prose.Count(*limit, "paper"))
			break
		}
		e := want[i]
		if r.dry {
			plan := r.plan(e)
			if plan == "" {
				counts.there++
				continue
			}
			counts.worked++
			fmt.Fprintf(out, "%s\t%s\t%s\n", e.Ref(), pathOrNothing(e), plan)
			continue
		}
		after, did, result := r.paper(ctx, e)
		switch result {
		case outThere:
			counts.there++
			continue
		case outHeld:
			counts.held++
		case outFailed:
			counts.failed++
		case outDemoted:
			counts.demoted++
			want = append(want, after)
		case outAdvanced:
			counts.advanced++
		}
		counts.worked++
		if len(did) > 0 {
			// The path printed is the one the work was done on, which for a demoted
			// paper is the path it was on when it was read and not the one it is on now.
			fmt.Fprintf(out, "%s\t%s\t%s\n", e.Ref(), pathOrNothing(e), strings.Join(did, ", "))
		}
		if err := r.record(after, e); err != nil {
			out.Flush()
			return err
		}
	}
	out.Flush()

	status := r.sel.ByStatus()
	tw := tabwriter.NewWriter(os.Stderr, 0, 4, 2, ' ', 0)
	if r.dry {
		// A dry run has not moved anything and saying it moved nothing would read as
		// a run that could not, so it counts the work instead of the outcome.
		fmt.Fprintf(tw, "  papers\t%d, %d with something to do, %d already at %s\n",
			papers, counts.worked, counts.there, r.target)
	} else {
		fmt.Fprintf(tw, "  papers\t%d, %d moved, %d already at %s, %d nothing can move yet, %d failed\n",
			papers, counts.advanced, counts.there, r.target, counts.held, counts.failed)
	}
	if r.requests > 0 {
		fmt.Fprintf(tw, "  requests\t%d\n", r.requests)
	}
	if counts.demoted > 0 {
		fmt.Fprintf(tw, "  demoted\t%d, from the render path to the source path\n", counts.demoted)
	}
	for _, s := range selection.Statuses {
		if status[s] > 0 {
			fmt.Fprintf(tw, "  %s\t%d\n", s, status[s])
		}
	}
	tw.Flush()
	if r.dry {
		fmt.Fprintln(os.Stderr, "nothing written, because this was a dry run")
		return nil
	}
	if counts.failed > 0 {
		return fmt.Errorf("%s failed, and the line above each one says what happened to it", prose.Count(counts.failed, "paper"))
	}
	if counts.held > 0 {
		return fmt.Errorf("%s cannot move yet, and the line above each one says what would settle that", prose.Count(counts.held, "paper"))
	}
	return nil
}

// tools are the programs the four paths run, named here so that one batch can
// pass them all through without anybody editing their PATH.
type tools struct {
	latexmlc, latexmlpost string
	pdftotext, pdftoppm   string
	reader, model         string
}

// runner is one batch: what it is working on, what it has spent, and where it
// writes what it learns.
type runner struct {
	root    string
	plane   metadata.Plane
	sources fetch.Manifest
	spath   string
	sel     selection.Manifest
	selpath string
	f       *fetch.Fetcher
	text    *pdftext.Reader
	target  selection.Status
	lang    string
	dry     bool
	tools   tools
	// requests is what this run cost arXiv, which at fifteen seconds each is also
	// most of what it cost in time.
	requests int
}

// outcome is what happened to one paper.
type outcome int

const (
	// outAdvanced is a paper that got further up the ladder than it was.
	outAdvanced outcome = iota
	// outThere is a paper that was already at the rung this run is taking papers
	// to, which costs nothing and is the ordinary state of a second run.
	outThere
	// outHeld is a paper that needs something done that this run will not decide
	// on its own, which is a path, or a reader for the path that costs money.
	outHeld
	// outFailed is a paper whose fetch or whose reading did not work.
	outFailed
	// outDemoted is a paper whose rendering the reject rule would not accept, so
	// it is on the source path now and goes round once more.
	outDemoted
)

// paper takes one paper as far up the ladder as this run is going.
//
// It returns the entry as it should now be recorded, what it did in words, and how
// it went. Nothing is written here: the caller writes, so that a paper that failed
// halfway still records the rung it did reach.
func (r *runner) paper(ctx context.Context, e selection.Entry) (selection.Entry, []string, outcome) {
	if selection.Reached(e.Status, r.target) {
		return e, nil, outThere
	}
	if e.Path == "" {
		fmt.Fprintf(os.Stderr, "%s: nothing has decided how this paper gets read, so run ax path decide\n", e.Ref())
		return e, nil, outHeld
	}
	// The vision path is the one that costs money a page, so a batch does not start
	// it because a manifest said so. Somebody has to name the reader, and naming it
	// is the sentence that says they meant to spend that.
	if e.Path == selection.PathVision && r.tools.reader == "" {
		fmt.Fprintf(os.Stderr, "%s: the vision path reads every page with a model, so name the reader with -reader and the model with -model before a run spends that\n", e.Ref())
		return e, nil, outHeld
	}

	var did []string
	if !selection.Reached(e.Status, selection.StatusFetched) {
		got, err := r.bytes(ctx, e)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", e.Ref(), err)
			return e, did, outFailed
		}
		e.Status = selection.StatusFetched
		if got {
			did = append(did, "fetched the "+artefact(routeFor(e.Path)))
		} else {
			did = append(did, "the "+artefact(routeFor(e.Path))+" was already here")
		}
	}
	if selection.Reached(r.target, selection.StatusExtracted) && !selection.Reached(e.Status, selection.StatusExtracted) {
		// Asked again even though the rung says fetched, because work/ is not
		// committed and the machine doing the reading is often not the machine that
		// did the fetching. A paper whose bytes are already here costs nothing here.
		got, err := r.bytes(ctx, e)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", e.Ref(), err)
			return e, did, outFailed
		}
		if got {
			did = append(did, "fetched the "+artefact(routeFor(e.Path))+" again, because the manifest had it and the disk did not")
		}
		err = r.read(e)
		var broken *tooBroken
		if errors.As(err, &broken) {
			// The one thing about a path that cannot be known before the reading. A
			// rendering LaTeXML could not finish is not a paper that fails, it is a
			// paper on the source path, and this is the only place that ever finds out.
			e.Path = selection.PathSource
			e.PathWhy = "the rendering holds errors the reject rule will not accept, so the TeX is compiled here"
			return e, append(did, "the rendering was rejected, so this paper is on the source path now"), outDemoted
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", e.Ref(), err)
			return e, did, outFailed
		}
		e.Status = selection.StatusExtracted
		did = append(did, "extracted")
	}
	if selection.Reached(r.target, selection.StatusTagged) && !selection.Reached(e.Status, selection.StatusTagged) {
		if err := tagsAssign([]string{e.ID}); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", e.Ref(), err)
			return e, did, outFailed
		}
		e.Status = selection.StatusTagged
		did = append(did, "tagged")
	}
	return e, did, outAdvanced
}

// bytes makes sure the artefact this paper's path reads is on the disk.
//
// The manifest and the disk are both asked, because either one alone is a wrong
// answer. A manifest entry with no file is a fresh clone, and a file with no
// manifest entry is a file nobody knows the licence of.
func (r *runner) bytes(ctx context.Context, e selection.Entry) (bool, error) {
	route := routeFor(e.Path)
	if held, ok := r.sources.Find(e.ID, e.Version, route); ok {
		if _, err := os.Stat(filepath.Join(r.root, filepath.FromSlash(held.Path))); err == nil {
			return false, nil
		}
	}
	var text *pdftext.Reader
	if route == fetch.RouteNative {
		text = r.text
	}
	t, err := fetchEach(ctx, r.f, route, r.plane, &r.sources, []string{e.Ref()}, false, text, fetch.Order{})
	r.requests += t.fetched
	if t.fetched > 0 || t.learnt > 0 {
		if werr := r.sources.Save(r.spath); werr != nil {
			return t.fetched > 0, werr
		}
	}
	if err != nil {
		return t.fetched > 0, err
	}
	if len(t.absent) > 0 {
		return false, fmt.Errorf("arXiv serves no %s for this version, so this is not the path this paper can go down, and ax path decide -again over it would say so", route)
	}
	return t.fetched > 0, nil
}

// artefact is what the file a route brings back is called, which is not the name
// of the route in any of the three cases.
func artefact(route fetch.Route) string {
	switch route {
	case fetch.RouteRender:
		return "rendering"
	case fetch.RouteSource:
		return "e-print"
	default:
		return "PDF"
	}
}

// read runs the path this paper is on over the bytes that are now on the disk.
//
// One command each, the same four commands a person would run by hand, called with
// the programs this run was given. A batch that reimplemented the four paths would
// be a second set of four paths to keep true.
func (r *runner) read(e selection.Entry) error {
	args := []string{"-lang", r.lang}
	switch e.Path {
	case selection.PathRender:
		return extractRender(append(args, e.Ref()))
	case selection.PathSource:
		if r.tools.latexmlc != "" {
			args = append(args, "-latexmlc", r.tools.latexmlc)
		}
		if r.tools.latexmlpost != "" {
			args = append(args, "-latexmlpost", r.tools.latexmlpost)
		}
		return extractSource(append(args, e.Ref()))
	case selection.PathNative:
		if r.tools.pdftotext != "" {
			args = append(args, "-pdftotext", r.tools.pdftotext)
		}
		return extractNative(append(args, e.Ref()))
	default:
		args = append(args, "-reader", r.tools.reader)
		if r.tools.model != "" {
			args = append(args, "-model", r.tools.model)
		}
		if r.tools.pdftotext != "" {
			args = append(args, "-pdftotext", r.tools.pdftotext)
		}
		if r.tools.pdftoppm != "" {
			args = append(args, "-pdftoppm", r.tools.pdftoppm)
		}
		return extractVision(append(args, e.Ref()))
	}
}

// record writes the entry back if this paper moved.
//
// Written per paper and not once at the end. A run of a thousand papers takes
// hours, something will interrupt it, and a selection file written only at the end
// would make the next run redo every paper the last one got through.
func (r *runner) record(after, before selection.Entry) error {
	if after.Status == before.Status && after.Path == before.Path {
		return nil
	}
	r.sel.Put(after)
	return r.sel.Save(r.selpath)
}

// plan is what this paper would cost, for a run that is only being asked.
func (r *runner) plan(e selection.Entry) string {
	if selection.Reached(e.Status, r.target) {
		return ""
	}
	if e.Path == "" {
		return "nothing, until ax path decide says how this paper gets read"
	}
	if e.Path == selection.PathVision && r.tools.reader == "" {
		return "nothing, until a reader is named for the path that costs money a page"
	}
	var parts []string
	route := routeFor(e.Path)
	if held, ok := r.sources.Find(e.ID, e.Version, route); !ok {
		parts = append(parts, "fetch the "+artefact(route))
	} else if _, err := os.Stat(filepath.Join(r.root, filepath.FromSlash(held.Path))); err != nil {
		parts = append(parts, "fetch the "+artefact(route)+" again, because the manifest has it and the disk does not")
	}
	if selection.Reached(r.target, selection.StatusExtracted) && !selection.Reached(e.Status, selection.StatusExtracted) {
		parts = append(parts, "read it on the "+string(e.Path)+" path")
	}
	if selection.Reached(r.target, selection.StatusTagged) && !selection.Reached(e.Status, selection.StatusTagged) {
		parts = append(parts, "assign its tags")
	}
	if len(parts) == 0 {
		return "nothing but the rung, which says " + string(e.Status) + " and should say " + string(r.target)
	}
	return strings.Join(parts, ", then ")
}

// runWanted is the papers this run is about.
//
// Everything in the selection, or one month of it, or one path of it, or the papers
// somebody named. A paper already at the rung this run is going to is kept in the
// list rather than filtered out here, because a run that says nothing about the
// papers it skipped is a run nobody can check.
func runWanted(m selection.Manifest, refs []string, shard, only string) ([]selection.Entry, error) {
	var want selection.Path
	if only != "" {
		p, err := selection.ParsePath(only)
		if err != nil {
			return nil, err
		}
		want = p
	}
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
		if want != "" && e.Path != want {
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

// routeFor is the surface a path reads.
//
// The vision path and the native path read the same file, because they are two
// ways of reading a PDF and not two downloads.
func routeFor(p selection.Path) fetch.Route {
	switch p {
	case selection.PathRender:
		return fetch.RouteRender
	case selection.PathSource:
		return fetch.RouteSource
	default:
		return fetch.RouteNative
	}
}

// wantsPDF says whether any of these papers reads a PDF, which is what decides
// whether a machine without poppler is worth a line about.
func wantsPDF(want []selection.Entry) bool {
	for _, e := range want {
		if e.Path == selection.PathNative || e.Path == selection.PathVision {
			return true
		}
	}
	return false
}

// pathOrNothing is what to print for a paper nobody has decided a path for.
func pathOrNothing(e selection.Entry) string {
	if e.Path == "" {
		return "undecided"
	}
	return string(e.Path)
}

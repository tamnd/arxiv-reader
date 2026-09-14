package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"text/tabwriter"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/fetch"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/pdftext"
	"github.com/tamnd/arxiv-reader/source"
)

func runFetch(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: ax fetch render <id>v<n> [...], ax fetch source <id>v<n> [...], ax fetch native <id>v<n> [...], or ax fetch verify")
	}
	switch args[0] {
	case "render":
		return fetchDownload(fetch.RouteRender, args[1:])
	case "source":
		return fetchDownload(fetch.RouteSource, args[1:])
	case "native":
		return fetchDownload(fetch.RouteNative, args[1:])
	case "verify":
		return fetchVerify(args[1:])
	default:
		return fmt.Errorf("unknown fetch subcommand %q, which is render, source, native or verify", args[0])
	}
}

// fetchDownload downloads one surface of the versions it is given.
//
// One request per version at fifteen seconds, so a run of twenty papers is five
// minutes and a run of a thousand is four hours. It is built to be left alone:
// every version already on disk and matching the manifest is skipped without a
// request, so re-running after an interruption costs only the versions that
// were never reached.
//
// The three routes are one command because everything except the URL is the same
// between them, down to the sentence printed at the end. What differs is what a
// 404 means, and that is a fact about the route rather than about the run.
func fetchDownload(route fetch.Route, args []string) error {
	fs := flag.NewFlagSet("ax fetch "+string(route), flag.ContinueOnError)
	all := fs.Bool("all", false, "fetch every version of each paper, and not just the one named")
	accept := fs.Bool("accept", false, "record new bytes for a source whose hash no longer matches the manifest")
	anyway := fs.Bool("anyway", false, "fetch past the licence gate, for reading a paper this corpus may never publish")
	pace := fs.Duration("pace", fetch.Pace, "the gap to leave between requests")
	if err := fs.Parse(args); err != nil {
		return err
	}
	refs := fs.Args()
	if len(refs) == 0 {
		return fmt.Errorf("usage: ax fetch %s <id>v<n> [...], or -all with a bare id", route)
	}
	if *pace < fetch.Pace {
		return fmt.Errorf("a pace of %s is faster than the fifteen seconds arXiv asks for on the website, and this reads the website", *pace)
	}

	plane := metadata.Plane{Root: corpusRoot()}
	path := corpus.SourcesPath(plane.Root)
	manifest, err := fetch.Load(path)
	if err != nil {
		return err
	}
	f := &fetch.Fetcher{
		UserAgent: userAgent(),
		Pace:      *pace,
		Log:       func(ref string) { fmt.Fprintf(os.Stderr, "fetching %s\n", ref) },
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Asked once for the whole run rather than once per paper, so a machine
	// without poppler gets one line about it instead of one per PDF. A missing
	// program costs the report and not the download, because the bytes are worth
	// having either way and this is a thing said about them.
	var text *pdftext.Reader
	if route == fetch.RouteNative {
		text = &pdftext.Reader{}
		if err := text.Available(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			fmt.Fprintln(os.Stderr, "nothing here will say which of these PDFs are scans, and the downloads carry on")
			text = nil
		}
	}

	// The manifest is written once at the end, and also on the way out of a
	// failure, because a run that stopped halfway has still spent the requests
	// and a manifest that forgot them would spend them again. A run that
	// fetched nothing writes nothing, so a refused fetch leaves no trace in the
	// corpus at all.
	tally, err := fetchEach(ctx, f, route, plane, &manifest, refs, *all, text, fetch.Order{Accept: *accept, Anyway: *anyway})
	if tally.fetched > 0 {
		if werr := manifest.Save(path); werr != nil {
			return werr
		}
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%d fetched, %d cached, %d arXiv does not serve, %d sources in %s\n",
		tally.fetched, tally.cached, len(tally.absent), len(manifest.Sources), path)
	if tally.fetched == 0 && tally.cached == 0 {
		return fmt.Errorf("arXiv serves no %s for any of the %d versions asked for", route, len(tally.absent))
	}
	return nil
}

// tally is what a run of fetches came to.
type tally struct {
	fetched, cached int
	// absent is the versions arXiv does not serve this route for, which is a
	// fact about those papers rather than a fault in the run.
	absent []string
}

func fetchEach(ctx context.Context, f *fetch.Fetcher, route fetch.Route, plane metadata.Plane, m *fetch.Manifest, refs []string, all bool, text *pdftext.Reader, o fetch.Order) (tally, error) {
	var t tally
	for _, ref := range refs {
		id, err := axid.Parse(ref)
		if err != nil {
			return t, err
		}
		rec, err := record(plane, id)
		if err != nil {
			return t, err
		}
		versions := []int{id.Version}
		switch {
		case all:
			versions = versions[:0]
			for _, v := range rec.Versions {
				versions = append(versions, v.Version)
			}
		case id.Version == 0:
			return t, fmt.Errorf("%s names no version, so either name one or pass -all", ref)
		}
		for _, n := range versions {
			order := o
			order.Record, order.Version = rec, n
			var res fetch.Result
			switch route {
			case fetch.RouteSource:
				res, err = f.EPrint(ctx, plane.Root, m, order)
			case fetch.RouteNative:
				res, err = f.PDF(ctx, plane.Root, m, order)
			default:
				res, err = f.Render(ctx, plane.Root, m, order)
			}

			// A version arXiv does not serve this route for is reported and
			// stepped over. arXiv began rendering in December 2023 and does
			// not backfill, so most papers have a rendering of their latest
			// version and none of their first, and a batch that stopped at
			// the first one would never reach the papers it can do.
			//
			// A PDF that is still being compiled is stepped over with them. It
			// is not an absence, because the paper has a PDF and it will be
			// there in a few minutes, but it is the same thing to do about it:
			// say so, and get on with the rest of the run.
			var unrendered *fetch.NotRendered
			var noEPrint *fetch.NoEPrint
			var noPDF *fetch.NoPDF
			var building *fetch.NotBuilt
			if errors.As(err, &unrendered) || errors.As(err, &noEPrint) || errors.As(err, &noPDF) || errors.As(err, &building) {
				ref := fmt.Sprintf("%sv%d", rec.ID, n)
				t.absent = append(t.absent, ref)
				fmt.Fprintln(os.Stderr, err)
				continue
			}
			if err != nil {
				return t, err
			}
			if res.Outcome == fetch.OutcomeCached {
				t.cached++
			} else {
				t.fetched++
			}
			fmt.Printf("%-22s %-8s %-12s %9d  %s\n", res.Entry.Ref(), res.Outcome, res.Entry.Licence, res.Entry.Bytes, res.Entry.Path)
			if route == fetch.RouteSource {
				reportMain(plane.Root, res.Entry.Path)
			}
			if text != nil {
				reportText(ctx, text, plane.Root, res.Entry.Path)
			}
		}
	}
	return t, nil
}

// reportMain says what the submission turned out to be.
//
// Printed here rather than left for ax extract because it is the one thing
// about an e-print that cannot be seen from the outside, and a person fetching
// a hundred papers wants to know which of them are PDF only before they start
// the extraction rather than after. Nothing here fails the run: a submission
// this tool cannot read is a paper for another path and not a broken download.
func reportMain(root, rel string) {
	bundle, err := source.Read(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %v\n", err)
		return
	}
	main, err := bundle.Main()
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %v\n", err)
		return
	}
	fmt.Printf("%-22s %s of %d files\n", "", main, len(bundle.Files))
}

// reportText says whether the PDF that just arrived has a text layer.
//
// Here for the same reason reportMain is. Whether a PDF was typeset or scanned
// cannot be seen from its size, its licence or its name, it decides which of two
// paths the paper is on, and a person fetching a thousand of them wants to know
// the split before they start extracting rather than after. It is a reading and
// not a decision: nothing is written down, and the paper is routed by ax extract.
//
// Nothing here fails the run. A PDF pdftotext will not open is a fact about that
// paper, the bytes are on disk either way, and a download that threw itself away
// over a report would be the wrong trade at fifteen seconds a request.
func reportText(ctx context.Context, r *pdftext.Reader, root, rel string) {
	doc, err := r.Read(ctx, filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %v\n", err)
		return
	}
	fmt.Printf("%-22s %s\n", "", doc.Why())
}

// record reads one paper out of the metadata plane.
//
// A paper the plane has never heard of is an error and not a reason to fetch
// anyway. The licence lives on the record, so a fetch without one is a fetch
// with no gate on it.
func record(plane metadata.Plane, id axid.ID) (metadata.Record, error) {
	stored, err := plane.Read(corpus.Shard(id))
	if err != nil {
		return metadata.Record{}, err
	}
	for _, s := range stored {
		if s.ID == id.Canonical {
			return s, nil
		}
	}
	return metadata.Record{}, fmt.Errorf("%s is not in the plane at %s, so there is nothing saying what its licence is", id.Canonical, plane.Dir())
}

// fetchVerify checks every file the manifest names against its hash.
//
// Missing is the ordinary state and not a failure, because work/ is not
// committed and a fresh clone has none of it. Changed is the failure, and it is
// the whole reason the manifest holds hashes.
func fetchVerify(args []string) error {
	fs := flag.NewFlagSet("ax fetch verify", flag.ContinueOnError)
	quiet := fs.Bool("q", false, "print only the entries that are not ok")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("ax fetch verify takes no arguments, so drop %s", fs.Args()[0])
	}

	root := corpusRoot()
	manifest, err := fetch.Load(corpus.SourcesPath(root))
	if err != nil {
		return err
	}
	checked, err := manifest.Verify(root)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, v := range checked {
		if *quiet && v.State == fetch.StateOK {
			continue
		}
		note := fetch.Short(v.Source.SHA256)
		if v.State == fetch.StateChanged {
			note = fmt.Sprintf("%s on disk, %s in the manifest", fetch.Short(v.Got), fetch.Short(v.Source.SHA256))
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", v.Source.Ref(), v.Source.Route, v.State, note)
	}
	tw.Flush()

	counts := fetch.Count(checked)
	fmt.Fprintf(os.Stderr, "%d sources, %d ok, %d missing, %d changed\n",
		len(checked), counts[fetch.StateOK], counts[fetch.StateMissing], counts[fetch.StateChanged])
	if counts[fetch.StateChanged] > 0 {
		return fmt.Errorf("%d sources no longer hash to what the manifest says", counts[fetch.StateChanged])
	}
	return nil
}

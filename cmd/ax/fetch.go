package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"text/tabwriter"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/fetch"
	"github.com/tamnd/arxiv-reader/metadata"
)

func runFetch(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: ax fetch render <id>v<n> [...], or ax fetch verify")
	}
	switch args[0] {
	case "render":
		return fetchRender(args[1:])
	case "verify":
		return fetchVerify(args[1:])
	default:
		return fmt.Errorf("unknown fetch subcommand %q, which is render or verify", args[0])
	}
}

// fetchRender downloads arXiv's own rendering of the versions it is given.
//
// One request per version at fifteen seconds, so a run of twenty papers is five
// minutes and a run of a thousand is four hours. It is built to be left alone:
// every version already on disk and matching the manifest is skipped without a
// request, so re-running after an interruption costs only the versions that
// were never reached.
func fetchRender(args []string) error {
	fs := flag.NewFlagSet("ax fetch render", flag.ContinueOnError)
	all := fs.Bool("all", false, "fetch every version of each paper, and not just the one named")
	accept := fs.Bool("accept", false, "record new bytes for a source whose hash no longer matches the manifest")
	anyway := fs.Bool("anyway", false, "fetch past the licence gate, for reading a paper this corpus may never publish")
	pace := fs.Duration("pace", fetch.Pace, "the gap to leave between requests")
	if err := fs.Parse(args); err != nil {
		return err
	}
	refs := fs.Args()
	if len(refs) == 0 {
		return errors.New("usage: ax fetch render <id>v<n> [...], or -all with a bare id")
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

	// The manifest is written once at the end, and also on the way out of a
	// failure, because a run that stopped halfway has still spent the requests
	// and a manifest that forgot them would spend them again. A run that
	// fetched nothing writes nothing, so a refused fetch leaves no trace in the
	// corpus at all.
	tally, err := fetchEach(ctx, f, plane, &manifest, refs, *all, fetch.Order{Accept: *accept, Anyway: *anyway})
	if tally.fetched > 0 {
		if werr := manifest.Save(path); werr != nil {
			return werr
		}
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%d fetched, %d cached, %d with no rendering, %d sources in %s\n",
		tally.fetched, tally.cached, len(tally.unrendered), len(manifest.Sources), path)
	if tally.fetched == 0 && tally.cached == 0 {
		return fmt.Errorf("none of the %d versions asked for has an HTML rendering, so they are all on the source path", len(tally.unrendered))
	}
	return nil
}

// tally is what a run of fetches came to.
type tally struct {
	fetched, cached int
	// unrendered is the versions arXiv has no rendering of, which is a fact
	// about those papers rather than a fault in the run.
	unrendered []string
}

func fetchEach(ctx context.Context, f *fetch.Fetcher, plane metadata.Plane, m *fetch.Manifest, refs []string, all bool, o fetch.Order) (tally, error) {
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
			res, err := f.Render(ctx, plane.Root, m, order)

			// A version with no rendering is reported and stepped over.
			// arXiv began rendering in December 2023 and does not backfill,
			// so most papers have a rendering of their latest version and
			// none of their first, and a batch that stopped at the first one
			// would never reach the papers it can do.
			var missing *fetch.NotRendered
			if errors.As(err, &missing) {
				t.unrendered = append(t.unrendered, missing.Ref)
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
		}
	}
	return t, nil
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

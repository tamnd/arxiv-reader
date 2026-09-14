package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/tamnd/arxiv-reader/harvest"
	"github.com/tamnd/arxiv-reader/metadata"
)

// corpusRoot is the directory ax reads and writes.
//
// ARXIV_CORPUS, or the working directory when that is unset. One environment
// variable and no config file, because the only thing a config file would hold
// is this path and a path that can be in two places is a path that will be.
func corpusRoot() string {
	if root := os.Getenv("ARXIV_CORPUS"); root != "" {
		return root
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func runHarvest(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ax harvest <kaggle|hf|oai|status>")
	}
	switch args[0] {
	case "status":
		return harvestStatus(args[1:])
	case "oai":
		return harvestOAI(args[1:])
	case "kaggle", "hf":
		return fmt.Errorf("harvest %s is part of milestone M1 and is not written yet", args[0])
	default:
		return fmt.Errorf("unknown harvest subcommand %q", args[0])
	}
}

// harvestOAI reads arXiv's own endpoint into the metadata plane.
//
// This is the catch up rather than the bootstrap. OAI-PMH will serve the whole
// archive, but at three seconds a page and about a thousand records a page that
// is most of a week of continuous requests for the first pass. The Kaggle
// snapshot does the same job in an afternoon, and then this command keeps the
// plane current from the day the snapshot was cut.
func harvestOAI(args []string) error {
	fs := flag.NewFlagSet("ax harvest oai", flag.ContinueOnError)
	from := fs.String("from", "", "earliest datestamp to ask for, as YYYY-MM-DD")
	until := fs.String("until", "", "latest datestamp to ask for, as YYYY-MM-DD")
	format := fs.String("format", harvest.FormatRaw, "arXivRaw for the record, arXiv for an authors pass over it")
	set := fs.String("set", "", "restrict to one OAI set, so math or cs")
	limit := fs.Int("limit", 0, "stop after about this many records, rounded up to the end of a page")
	dry := fs.Bool("n", false, "fetch and report, but write nothing")
	quiet := fs.Bool("q", false, "no per page progress")
	if err := fs.Parse(args); err != nil {
		return err
	}

	query := harvest.Query{
		Format: *format,
		From:   *from,
		Until:  *until,
		Set:    *set,
		Limit:  *limit,
	}
	if err := query.Validate(); err != nil {
		return err
	}
	// The datestamp is when arXiv last touched the record and not when the
	// paper was submitted, so an unbounded run asks for the whole archive. That
	// is a week of requests and it should be something a person typed on
	// purpose.
	if query.From == "" && query.Until == "" && query.Limit == 0 {
		return errors.New("a harvest with no -from, no -until and no -limit asks for the whole archive, which is about a week of requests, so say -from 1991-01-01 if that is what you meant")
	}

	client := &harvest.OAI{
		// arXiv blocks anonymous bulk readers and they are right to. The
		// address is here so that someone at Cornell who wants this to stop has
		// somebody to write to before they reach for a block.
		UserAgent: "arxiv-reader/" + Version + " (+https://github.com/tamnd/arxiv-reader; tamnd87@gmail.com)",
	}
	if !*quiet {
		client.Log = func(page, records int, token string) {
			more := ""
			if token != "" {
				more = ", more to come"
			}
			fmt.Fprintf(os.Stderr, "page %d, %d records%s\n", page, records, more)
		}
	}

	plane := metadata.Plane{Root: corpusRoot()}
	var records []metadata.Record
	err := client.List(context.Background(), query, func(r metadata.Record) error {
		records = append(records, r)
		return nil
	})
	if err != nil {
		return err
	}

	// An id this tool cannot place is a record with nowhere to go. Reported
	// rather than fatal, because one bad id in a page of a thousand should not
	// throw the other nine hundred and ninety nine away.
	byShard, unplaceable := metadata.Group(records)
	shards := make([]string, 0, len(byShard))
	for shard := range byShard {
		shards = append(shards, shard)
	}
	sort.Strings(shards)

	if *dry {
		fmt.Printf("%d records over %d months, nothing written\n", len(records), len(shards))
		return reportUnplaceable(unplaceable)
	}

	var stats metadata.Stats
	for _, shard := range shards {
		recs := byShard[shard]
		// The arXiv format has no title, no abstract and no version history, so
		// a record it is the first to mention cannot be written: there is not
		// enough of it to be a record. It updates papers the plane already has
		// and leaves the rest for a raw pass, which is what makes it an authors
		// pass rather than a harvest in its own right.
		if query.Format == harvest.FormatArXiv {
			var err error
			recs, err = onlyKnown(plane, shard, recs)
			if err != nil {
				return fmt.Errorf("%s: %w", shard, err)
			}
		}
		s, err := plane.Merge(shard, recs)
		if err != nil {
			return fmt.Errorf("%s: %w", shard, err)
		}
		stats = stats.Add(s)
	}
	fmt.Printf("%s over %d months\n", stats, len(shards))
	return reportUnplaceable(unplaceable)
}

// onlyKnown drops the records for papers this month does not already hold.
func onlyKnown(plane metadata.Plane, shard string, recs []metadata.Record) ([]metadata.Record, error) {
	existing, err := plane.Read(shard)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(existing))
	for _, r := range existing {
		known[r.ID] = true
	}
	out := recs[:0]
	for _, r := range recs {
		if known[r.ID] {
			out = append(out, r)
		}
	}
	return out, nil
}

func reportUnplaceable(recs []metadata.Record) error {
	if len(recs) == 0 {
		return nil
	}
	ids := make([]string, len(recs))
	for i, r := range recs {
		ids[i] = r.ID
	}
	// A warning and not an error. The run did what it could and the operator
	// needs to know which ids to look at by hand.
	fmt.Fprintf(os.Stderr, "%d records had an id this tool cannot place: %s\n", len(ids), strings.Join(ids, " "))
	return nil
}

// harvestStatus says what the metadata plane holds, a month at a time.
//
// The first thing worth having, because every later question about the plane is
// a question about a gap in this table. A month with no file is a month nobody
// harvested, and the whole of M1 is the work of making that list empty.
func harvestStatus(args []string) error {
	fs := flag.NewFlagSet("ax harvest status", flag.ContinueOnError)
	total := fs.Bool("total", false, "print the record count and nothing else")
	if err := fs.Parse(args); err != nil {
		return err
	}
	plane := metadata.Plane{Root: corpusRoot()}
	shards, err := plane.Shards()
	if err != nil {
		return err
	}

	counts := make([]int, len(shards))
	sum := 0
	for i, shard := range shards {
		n, err := plane.Count(shard)
		if err != nil {
			return err
		}
		counts[i] = n
		sum += n
	}
	if *total {
		fmt.Println(sum)
		return nil
	}
	if len(shards) == 0 {
		fmt.Printf("the metadata plane at %s is empty\n", plane.Dir())
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', tabwriter.AlignRight)
	for i, shard := range shards {
		fmt.Fprintf(tw, "%s\t%d\t\n", shard, counts[i])
	}
	fmt.Fprintf(tw, "%d months\t%d\t\n", len(shards), sum)
	return tw.Flush()
}

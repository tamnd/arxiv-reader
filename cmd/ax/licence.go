package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/harvest"
	"github.com/tamnd/arxiv-reader/licence"
	"github.com/tamnd/arxiv-reader/metadata"
)

// licenceCensus counts the metadata plane and writes reports/licence.md.
//
// It reads nothing but the plane, so it runs offline and it runs in the time it
// takes to read three million lines. Everything it counts is a latest version
// licence, because that is the only thing any bulk surface carries, and the
// report it writes says so before it says anything else.
func licenceCensus(args []string) error {
	fs := flag.NewFlagSet("ax licence census", flag.ContinueOnError)
	shard := fs.String("shard", "", "one month, so 2106")
	out := fs.String("o", "", "where to write the report, and the default is reports/licence.md in the corpus")
	quiet := fs.Bool("q", false, "write the file and print nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("ax licence census takes no arguments, so drop %s", fs.Args()[0])
	}

	plane := metadata.Plane{Root: corpusRoot()}
	shards, err := plane.Shards()
	if err != nil {
		return err
	}
	if *shard != "" {
		if !metadata.ValidShard(*shard) {
			return fmt.Errorf("%q is not a month, want four digits like 2106", *shard)
		}
		shards = keepShard(shards, *shard)
		if len(shards) == 0 {
			return fmt.Errorf("the plane at %s has no %s", plane.Dir(), *shard)
		}
	}

	census, err := licence.Take(plane, shards)
	if err != nil {
		return err
	}
	census.Generated = time.Now()

	path := *out
	if path == "" {
		path = filepath.Join(plane.Root, "reports", "licence.md")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(census.Markdown()), 0o644); err != nil {
		return err
	}
	if !*quiet {
		fmt.Print(census.Text())
		fmt.Fprintf(os.Stderr, "written to %s\n", path)
	}
	return nil
}

// licenceResolve reads a version's licence off its abs page.
//
// One page per version at arXiv's pace for the website, which is fifteen
// seconds. That is why this takes a list of papers and not a corpus: resolving
// all of arXiv this way is over five million pages and half a year, so this is
// what runs when a paper is selected.
func licenceResolve(args []string) error {
	fs := flag.NewFlagSet("ax licence resolve", flag.ContinueOnError)
	all := fs.Bool("all", false, "read every version of each paper, and not just the one named")
	write := fs.Bool("write", false, "write what was read back onto the record in the metadata plane")
	pace := fs.Duration("pace", licence.Pace, "the gap to leave between requests")
	if err := fs.Parse(args); err != nil {
		return err
	}
	refs := fs.Args()
	if len(refs) == 0 {
		return errors.New("usage: ax licence resolve <id>v<n> [...], or -all with a bare id")
	}
	if *pace < licence.Pace {
		return fmt.Errorf("a pace of %s is faster than the fifteen seconds arXiv asks for on the website, and this reads the website", *pace)
	}

	plane := metadata.Plane{Root: corpusRoot()}
	r := &licence.Resolver{
		UserAgent: userAgent(),
		Pace:      *pace,
		Log:       func(ref string) { fmt.Fprintf(os.Stderr, "reading %s\n", ref) },
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	for _, ref := range refs {
		if err := resolveOne(ctx, r, plane, ref, *all, *write); err != nil {
			return err
		}
	}
	return nil
}

// licenceCrosscheck compares the metadata plane against a third party reading
// of the same question, and writes reports/crosscheck.md.
//
// There is one third party wired up and it is the Common Pile, so -against
// takes one value. It is a flag rather than a constant because the point of the
// report is that a second reading exists, and a second one would be read the
// same way.
func licenceCrosscheck(args []string) error {
	fs := flag.NewFlagSet("ax licence crosscheck", flag.ContinueOnError)
	against := fs.String("against", "common-pile", "whose reading to compare against")
	limit := fs.Int("limit", 0, "how many of their rows to read, and there is no default because the whole collection is about six hours")
	offset := fs.Int("offset", 0, "the row to start at, for picking up a walk that stopped")
	pace := fs.Duration("pace", harvest.RowsPace, "the gap to leave between requests")
	out := fs.String("o", "", "where to write the report, and the default is reports/crosscheck.md in the corpus")
	quiet := fs.Bool("q", false, "write the file and print nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("ax licence crosscheck takes no arguments, so drop %s", fs.Args()[0])
	}
	if *against != "common-pile" {
		return fmt.Errorf("%q is not a reading this knows how to read, and the one it knows is common-pile", *against)
	}
	if *limit < 1 {
		return errors.New("pass -limit, because their rows carry the full text of the paper and the whole collection is about six hours at the pace the server asks for")
	}
	if *pace < harvest.RowsPace {
		return fmt.Errorf("a pace of %s is faster than the three seconds this server was measured to tolerate, and it answers a burst with a 429", *pace)
	}

	plane := metadata.Plane{Root: corpusRoot()}
	reader := &licence.Reader{
		Rows: &harvest.Rows{
			Dataset:   licence.Pile,
			UserAgent: userAgent(),
			Pace:      *pace,
		},
		Log: func(page, claims, total int) {
			fmt.Fprintf(os.Stderr, "page %d, %d claims, of %d rows\n", page, claims, total)
		},
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	check, err := licence.Check(ctx, reader, plane, *offset, *limit)
	if err != nil {
		return err
	}
	check.Generated = time.Now()

	path := *out
	if path == "" {
		path = filepath.Join(plane.Root, "reports", "crosscheck.md")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(check.Markdown()), 0o644); err != nil {
		return err
	}
	if !*quiet {
		fmt.Print(check.Text())
		fmt.Fprintf(os.Stderr, "written to %s\n", path)
	}
	return nil
}

// userAgent names the project and a way to get hold of a person, which is what
// every surface this tool reads asks for and arXiv enforces.
func userAgent() string {
	return "arxiv-reader/" + Version + " (+https://github.com/tamnd/arxiv-reader; tamnd87@gmail.com)"
}

func resolveOne(ctx context.Context, r *licence.Resolver, plane metadata.Plane, ref string, all, write bool) error {
	id, err := axid.Parse(ref)
	if err != nil {
		return err
	}

	// The record is read first whether or not anything is written back, because
	// the version count comes from it and because a resolution the plane has
	// never heard of is worth saying so about rather than printing on its own.
	shard := corpus.Shard(id)
	stored, err := plane.Read(shard)
	if err != nil {
		return err
	}
	var rec metadata.Record
	found := false
	for _, s := range stored {
		if s.ID == id.Canonical {
			rec, found = s, true
			break
		}
	}

	var got []licence.Resolution
	switch {
	case all && !found:
		return fmt.Errorf("%s is not in the plane at %s, so there is nothing to say how many versions it has", id.Canonical, plane.Dir())
	case all:
		got, err = r.ResolveAll(ctx, id.Canonical, len(rec.Versions))
	case id.Version == 0:
		return fmt.Errorf("%s names no version, so either name one or pass -all", ref)
	default:
		var one licence.Resolution
		one, err = r.Resolve(ctx, ref)
		got = []licence.Resolution{one}
	}
	if err != nil {
		return err
	}

	for _, res := range got {
		note := ""
		if res.Withdrawn {
			note = "  withdrawn, so arXiv states no licence"
		}
		fmt.Printf("%-22s %-12s %s%s\n", res.Ref(), res.Licence, corpus.AccessFor(res.Licence), note)
	}

	if !write {
		return nil
	}
	if !found {
		return fmt.Errorf("%s is not in the plane at %s, so there is nothing to write to", id.Canonical, plane.Dir())
	}
	updated, changed, err := licence.Apply(rec, got)
	if err != nil {
		return err
	}
	for _, c := range changed {
		fmt.Fprintf(os.Stderr, "%s changed to %s\n", c.Ref(), c.Licence)
	}
	stats, err := plane.MergeWith(shard, []metadata.Record{updated}, metadata.Replace)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s: %s\n", plane.Path(shard), stats)
	return nil
}

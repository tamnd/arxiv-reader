package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

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

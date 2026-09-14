package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

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
	case "kaggle", "hf", "oai":
		return fmt.Errorf("harvest %s is part of milestone M1 and is not written yet", args[0])
	default:
		return fmt.Errorf("unknown harvest subcommand %q", args[0])
	}
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

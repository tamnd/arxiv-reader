package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tamnd/arxiv-reader/licence"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/scope"
)

// selectScope writes the list of the papers in scope from the metadata plane
// (AX2.1). The list is a view of the plane, so it is rebuilt each time and
// not merged.
func selectScope(args []string) error {
	fs := flag.NewFlagSet("ax select scope", flag.ContinueOnError)
	reason := fs.String("reason", scope.Math, "the scope reason, and only scope-math is known")
	dry := fs.Bool("n", false, "count, but write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("ax select scope takes no arguments, so drop %s", fs.Arg(0))
	}
	if *reason != scope.Math {
		return fmt.Errorf("%q is not a scope reason, want %s", *reason, scope.Math)
	}
	plane := metadata.Plane{Root: corpusRoot()}
	shards, err := plane.Shards()
	if err != nil {
		return err
	}
	if len(shards) == 0 {
		return errors.New("the metadata plane is empty, so run ax harvest first")
	}
	list := scope.List{Reason: *reason, Date: time.Now().UTC().Format("2006-01-02")}
	for _, shard := range shards {
		err := plane.Scan(shard, func(r metadata.Record) error {
			if in, cross := scope.Of(r.Categories); in {
				list.IDs = append(list.IDs, r.ID)
				if cross {
					list.Cross++
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	fmt.Printf("%d papers in %s, %d by a cross-list only, from %d months\n", len(list.IDs), *reason, list.Cross, len(shards))
	if *dry {
		return nil
	}
	path := scope.Path(plane.Root, *reason)
	if err := list.Write(path); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "written to %s\n", path)
	return nil
}

// licenceScope is ax licence census -scope math: the share of each licence
// class in the scope, by year and by category (AQ03).
func licenceScope(reason, out string, quiet bool) error {
	if reason != "math" && reason != scope.Math {
		return fmt.Errorf("%q is not a scope, want math", reason)
	}
	plane := metadata.Plane{Root: corpusRoot()}
	shards, err := plane.Shards()
	if err != nil {
		return err
	}
	c := licence.NewScopeCounter(scope.Math)
	for _, shard := range shards {
		err := plane.Scan(shard, func(r metadata.Record) error {
			_, err := c.Add(shard, r)
			return err
		})
		if err != nil {
			return err
		}
	}
	census := c.Census()
	census.Generated = time.Now()
	if out == "" {
		out = filepath.Join(plane.Root, "reports", "licence-math.md")
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(out, []byte(census.Markdown()), 0o644); err != nil {
		return err
	}
	if !quiet {
		fmt.Print(census.Text())
		fmt.Fprintf(os.Stderr, "written to %s\n", out)
	}
	return nil
}

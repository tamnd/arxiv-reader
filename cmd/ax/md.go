package main

import (
	"errors"
	"flag"
	"fmt"
	iofs "io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/mdrec"
)

// mdPlanes are the directories of the corpus whose files of records were JSON
// Lines and are Markdown.
var mdPlanes = []string{
	"metadata",
	"graph",
	"analysis/objects",
	"analysis/edges",
	"analysis/bib",
	"analysis/prose",
	"analysis/witness",
	"work/objects",
}

const mdUsage = "usage: ax md move [-dry], or ax md cat <file> [...]"

// runMD moves the files of records of the corpus to Markdown, or prints the
// records of files as JSON Lines.
func runMD(args []string) error {
	if len(args) == 0 {
		return errors.New(mdUsage)
	}
	switch args[0] {
	case "move":
		return mdMove(args[1:])
	case "cat":
		return mdCat(args[1:])
	}
	return errors.New(mdUsage)
}

// mdCat prints the records of each file as JSON Lines, one record on each
// line, for grep and jq. A file of the name with .jsonl is read when the
// Markdown file is not there.
func mdCat(args []string) error {
	if len(args) == 0 {
		return errors.New(mdUsage)
	}
	for _, f := range args {
		b, err := mdrec.ReadLines(f)
		if err != nil {
			return err
		}
		if _, err := os.Stdout.Write(b); err != nil {
			return err
		}
	}
	return nil
}

// mdMove moves each file of records of the corpus from JSON Lines to Markdown
// with a YAML header, a file for a file, and checks that each reads back with
// all of its records.
func mdMove(args []string) error {
	fs := flag.NewFlagSet("ax md move", flag.ContinueOnError)
	dry := fs.Bool("dry", false, "count the files to move and write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New(mdUsage)
	}
	root := corpusRoot()
	moved, both := 0, 0
	for _, plane := range mdPlanes {
		err := filepath.WalkDir(filepath.Join(root, plane), func(p string, d iofs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(p) != mdrec.Old {
				return err
			}
			md := strings.TrimSuffix(p, mdrec.Old) + mdrec.Ext
			if _, err := os.Stat(md); err == nil {
				// A file in both forms is a write of the new form next to
				// an old file that it did not remove, and the reader
				// reads the new one, so the old one is left for a person.
				fmt.Fprintf(os.Stderr, "%s is there in both forms, and the JSON Lines file stays\n", md)
				both++
				return nil
			}
			moved++
			if *dry {
				return nil
			}
			return moveMD(p, md)
		})
		if err != nil && !errors.Is(err, iofs.ErrNotExist) {
			return err
		}
	}
	verb := "moved"
	if *dry {
		verb = "to move"
	}
	fmt.Fprintf(os.Stderr, "%s %s to Markdown in %s\n", prose.Count(moved, "file"), verb, root)
	if both > 0 {
		return fmt.Errorf("%s are there in both forms", prose.Count(both, "file"))
	}
	return nil
}

func moveMD(old, md string) error {
	data, err := os.ReadFile(old)
	if err != nil {
		return err
	}
	recs := mdrec.Lines(data)
	out, err := mdrec.EncodeAuto(recs)
	if err != nil {
		return fmt.Errorf("%s: %w", old, err)
	}
	back, err := mdrec.Decode(out)
	if err != nil {
		return fmt.Errorf("%s: %w", old, err)
	}
	if len(back) != len(recs) {
		return fmt.Errorf("%s: %d records read back of %d", old, len(back), len(recs))
	}
	return mdrec.Put(md, out)
}

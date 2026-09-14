package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/internal/prose"
)

// runSplit reports and accepts hand corrections to the content plane.
//
// The cutting of a paper into files is done by ax extract, which is where the
// paper is, so what is left for this command is the other half of what the spec
// asks of it: the content hash, and what happens when somebody edits a file.
//
// Somebody will. A corpus of extracted papers has mistakes in it and the whole
// reason the content plane is Markdown rather than a database is that a person
// can fix one in an editor. This is what keeps that fix from being thrown away
// by the next run, and it works without their having had to know it exists.
func runSplit(args []string) error {
	fs := flag.NewFlagSet("ax split", flag.ContinueOnError)
	accept := fs.Bool("accept", false, "keep the corrections, restamping the hash and marking the files edited")
	lang := fs.String("lang", "en", "the language directory to read")
	if err := fs.Parse(args); err != nil {
		return err
	}
	refs := fs.Args()
	if len(refs) == 0 {
		return errors.New("usage: ax split [-accept] <id> [...]")
	}
	corrected := 0
	for _, ref := range refs {
		id, err := axid.Parse(ref)
		if err != nil {
			return err
		}
		dir := corpus.ContentDir(corpusRoot(), *lang, id)
		results, err := extract.Check(dir)
		if err != nil {
			return fmt.Errorf("%s has nothing in the content plane at %s, so run ax extract render first: %w", id.Canonical, dir, err)
		}
		if len(results) == 0 {
			return fmt.Errorf("%s has no files at %s, so run ax extract render first", id.Canonical, dir)
		}
		for _, r := range results {
			if r.State != extract.StateProtected {
				continue
			}
			corrected++
			fmt.Printf("%s  corrected, %s\n", r.Name, r.Why)
			if !*accept {
				continue
			}
			changed, err := extract.Accept(dir, r.Name)
			if err != nil {
				return err
			}
			if changed {
				fmt.Printf("%s  accepted\n", r.Name)
			}
		}
		fmt.Printf("%s of %s, %d corrected\n", prose.Count(len(results), "file"), id.Canonical, corrected)
	}
	if corrected > 0 && !*accept {
		fmt.Fprintln(os.Stderr, "pass -accept to keep the corrections, which restamps the hash and marks the files edited")
		return fmt.Errorf("%s been corrected since it was written", prose.Count(corrected, "file has"))
	}
	return nil
}

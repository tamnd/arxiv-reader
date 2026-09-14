package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/fetch"
	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/tags"
)

// runTags hands out the permanent names for the objects inside a paper.
//
// A tag is four characters, it is assigned once, and it never changes. Every
// link in the reading app, every edge in the graph and every translated file
// points at one, which is what lets all three survive the paper being extracted
// again next year by a better tool.
func runTags(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: ax tags assign <id> [...] or ax tags diff <id> -from v2 -to v3")
	}
	switch args[0] {
	case "assign":
		return tagsAssign(args[1:])
	case "diff":
		return tagsDiff(args[1:])
	case "report":
		return tagsReport(args[1:])
	default:
		return fmt.Errorf("unknown tags subcommand %q, the subcommands are assign, diff and report", args[0])
	}
}

// tagsAssign reads a paper's content files, gives every object a tag, and writes
// the tag back into the file it came from.
//
// The register is the record and the content files are the copy. Both are
// written because the reading app serves the files and the graph reads the
// register, and rule G05 checks that the two say the same thing.
func tagsAssign(args []string) error {
	fs := flag.NewFlagSet("ax tags assign", flag.ContinueOnError)
	dry := fs.Bool("n", false, "say what would be assigned and write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return errors.New("usage: ax tags assign <id> [...]")
	}
	plane := metadata.Plane{Root: corpusRoot()}
	for _, ref := range rest {
		id, err := axid.Parse(ref)
		if err != nil {
			return err
		}
		dir := corpus.ContentDir(plane.Root, "en", id)
		names, err := sections(dir)
		if err != nil {
			return err
		}
		docs := map[string]extract.Document{}
		var objects []tags.Object
		version := 0
		for _, name := range names {
			b, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				return err
			}
			d, err := extract.ParseDocument(b)
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			docs[name] = d
			version = versionOf(d, version)
			// A section that is a whole file is an object like any other and its
			// heading is in the front matter rather than in the body, so its
			// identifier is taken from there. The front matter file is not one:
			// it is the title, the authors and the abstract, and there is nothing
			// in it anybody cites.
			if d.Front.LocalID != "" && d.Front.Kind != "front" {
				objects = append(objects, tags.Object{File: name, Local: d.Front.LocalID, Class: d.Front.Kind})
			}
			objects = append(objects, tags.Objects(name, d.Body)...)
		}
		// The gate, at the point something is about to be written into the
		// corpus, the same as everywhere else. A tag is not content, but it is
		// written into the content files, and a run that rewrote every section of
		// a paper nobody may hold is a run that touched a paper nobody may hold.
		rec, err := record(plane, id)
		if err != nil {
			return err
		}
		if err := fetch.Gate(rec, version); err != nil {
			return err
		}
		old, err := tags.LoadRegister(corpus.TagsPath(plane.Root, id))
		if err != nil {
			return err
		}
		// A register that belongs to an older version of the paper cannot be
		// assigned from, and this is the one failure here that is silent
		// otherwise. A paper gains a table in the middle, every table after it is
		// renumbered, and every identifier in the register still names something,
		// so matching on the identifier keeps every tag, finds nothing missing
		// and has just moved every table's permanent name onto the table after
		// it. Carrying the register over first is what ax tags diff is for.
		if old.Version != 0 && version > old.Version {
			return fmt.Errorf("%s is at v%d in the content plane and its register was assigned against v%d, so carry the register over first with ax tags diff %s -from v%d -to v%d -w", id.Canonical, version, old.Version, id.Canonical, old.Version, version)
		}
		plan, err := tags.Assign(id.Canonical, objects, old)
		if err != nil {
			return err
		}
		if version > 0 {
			plan.Register.Version = version
		}
		if *dry {
			reportAssign(id, plan)
			if err := plan.Err(); err != nil {
				return err
			}
			continue
		}
		if err := plan.Err(); err != nil {
			return err
		}
		written, err := writeTags(dir, docs, plan)
		if err != nil {
			return err
		}
		changed, err := plan.Register.Save(corpus.TagsPath(plane.Root, id))
		if err != nil {
			return err
		}
		if plan.Run != nil {
			runs, err := tags.LoadRuns(corpus.RunsPath(plane.Root, id))
			if err != nil {
				return err
			}
			if _, err := tags.SaveRuns(corpus.RunsPath(plane.Root, id), append(runs, *plan.Run)); err != nil {
				return err
			}
		}
		fmt.Fprintf(os.Stderr, "%s on %s, %d new and %d kept, %s rewritten%s\n",
			prose.Count(len(plan.Assigned), "tag"), id.Canonical, len(plan.Added), plan.Kept,
			prose.Count(written, "file"), unchanged(changed || written > 0))
	}
	return nil
}

// writeTags puts each object's tag into the file it was found in.
//
// A paper with a hand correction in it stops the run, the same as ax split and
// ax extract render stop. Writing a tag changes the body and restamps the
// content hash, which is the one thing that says a correction happened, so
// going ahead here would make somebody's edit invisible. The way past it is the
// same as everywhere else, which is to accept the correction first.
//
// It stops the whole paper and not the one file, because a register that names a
// tag no content file carries is a register rule G05 fails, and half a paper is
// exactly that.
func writeTags(dir string, docs map[string]extract.Document, plan tags.Plan) (int, error) {
	for name, d := range docs {
		if d.Corrected() {
			return 0, fmt.Errorf("%s was corrected by hand and tagging it would restamp the hash that says so, run ax split -accept first", name)
		}
	}
	n := 0
	for name, d := range docs {
		if t, ok := plan.Assigned[d.Front.LocalID]; ok {
			d.Front.Tag = string(t)
		}
		d.Body, _ = tags.Retag(d.Body, plan.Assigned)
		b, err := d.Bytes()
		if err != nil {
			return n, err
		}
		path := filepath.Join(dir, name)
		old, err := os.ReadFile(path)
		if err != nil {
			return n, err
		}
		if string(old) == string(b) {
			continue
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// versionOf is the version the content files were extracted from.
//
// Read off the files rather than taken from the command line, because the gate
// is about the version that is actually in the corpus and not about the one the
// caller had in mind. Front matter writes it as v2 and the gate wants 2.
func versionOf(d extract.Document, have int) int {
	var n int
	if _, err := fmt.Sscanf(d.Front.Version, "v%d", &n); err == nil && n > have {
		return n
	}
	return have
}

// reportAssign says what a run would do, and names what it would not.
//
// The new tags are printed in full. There are a few dozen per paper on a first
// assignment and a handful on any run after that, and the handful is the whole
// point of looking: a new tag on a second run means an object the last run did
// not see, which is either a real addition to the paper or a bug in the mapping.
func reportAssign(id axid.ID, plan tags.Plan) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "%s\t%s\n", id.Canonical, prose.Count(len(plan.Assigned), "object"))
	fmt.Fprintf(tw, "  new\t%d\n", len(plan.Added))
	fmt.Fprintf(tw, "  kept\t%d\n", plan.Kept)
	if plan.Run != nil {
		fmt.Fprintf(tw, "  run\t%s to %s\n", plan.Run.First, plan.Run.Last)
	}
	tw.Flush()
	for _, local := range plan.Added {
		fmt.Printf("  %s  %s\n", plan.Assigned[local], local)
	}
	// Named and not counted, because each one is an object that has a tag
	// somewhere in the corpus and no longer has anything to point at, and
	// deciding what happened to it is a person's job.
	for _, local := range plan.Missing {
		fmt.Printf("  gone  %s  in the register and not in the paper\n", local)
	}
}

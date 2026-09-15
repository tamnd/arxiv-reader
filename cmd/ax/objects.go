package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/objects"
)

// runObjects reads a paper back out of the content plane as objects.
//
// 2166-06 says a paper here is not a document with sections but a set of
// objects, each with a kind, a tag, a position, a body and edges. This is the
// verb that produces that set, and the graph, the four emitters and the reading
// app all join on what it writes.
//
// It derives. The Markdown is the truth, the record under work/objects is a
// build artefact, and rebuilding it costs a second, so nothing here is worth
// keeping if the content files and the record disagree: the files win and the
// record gets built again.
func runObjects(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: ax objects build <id> [...], ax objects list <id>, or ax objects count <id> [...]")
	}
	switch args[0] {
	case "build":
		return objectsBuild(args[1:])
	case "list":
		return objectsList(args[1:])
	case "count":
		return objectsCount(args[1:])
	default:
		return fmt.Errorf("unknown objects subcommand %q, the subcommands are build, list and count", args[0])
	}
}

// objectsBuild writes one paper's object record.
func objectsBuild(args []string) error {
	fs := flag.NewFlagSet("ax objects build", flag.ContinueOnError)
	lang := fs.String("lang", "en", "the language directory to read")
	dry := fs.Bool("n", false, "say what was found and write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return errors.New("usage: ax objects build <id> [...]")
	}
	root := corpusRoot()
	for _, ref := range rest {
		id, err := axid.Parse(ref)
		if err != nil {
			return err
		}
		p, err := objects.Build(root, *lang, id)
		if err != nil {
			return err
		}
		if *dry {
			counts(p)
			continue
		}
		path := corpus.ObjectsPath(root, id)
		changed, err := written(p, path)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "%s in %s%s\n", prose.Count(len(p.Objects), "object"), path, unchanged(changed))
	}
	return nil
}

// written saves the record and says whether the file on disk changed.
//
// A rebuild of a paper nobody has touched leaves the file alone, which is what
// keeps a rebuild of the whole corpus out of the working tree and out of every
// timestamp that depends on it.
func written(p objects.Paper, path string) (bool, error) {
	want, err := p.Bytes()
	if err != nil {
		return false, err
	}
	if have, err := os.ReadFile(path); err == nil && string(have) == string(want) {
		return false, nil
	}
	return true, p.Save(path)
}

// objectsList prints one paper's objects in reading order.
//
// The columns are what somebody looking at a record that seems wrong wants: the
// order, the kind, the local identifier and the file to go and fix it in. The
// body is clipped rather than printed, because the body is in the file.
func objectsList(args []string) error {
	fs := flag.NewFlagSet("ax objects list", flag.ContinueOnError)
	lang := fs.String("lang", "en", "the language directory to read")
	kind := fs.String("kind", "", "print only objects of this kind")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return errors.New("usage: ax objects list <id>")
	}
	id, err := axid.Parse(rest[0])
	if err != nil {
		return err
	}
	p, err := objects.Build(corpusRoot(), *lang, id)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, o := range p.Objects {
		if *kind != "" && o.Kind != *kind {
			continue
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n",
			o.Order, kindOf(o), o.Local, o.Tag, prose.Clip(heading(o), 48), prose.Clip(oneLine(o.BodyMD), 60))
	}
	return tw.Flush()
}

// kindOf is the kind and, when the paper called it something more particular,
// what the paper called it.
func kindOf(o objects.Record) string {
	if o.Subkind == "" {
		return o.Kind
	}
	return o.Kind + "/" + o.Subkind
}

// heading is what the paper printed in front of an object, which is its number
// and its title when it has both and whichever it has when it has one.
func heading(o objects.Record) string {
	return strings.TrimSpace(o.Number + " " + o.Title)
}

// oneLine folds a body onto one line so that a table of them stays a table.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// objectsCount prints how many of each kind a paper has.
//
// In the spec's order and not alphabetically, and every kind printed even when
// the count is nought, because what this is read for is whether a paper came
// out of extraction looking like a paper, and a paper with no equations in it
// is easier to see in a column of noughts than in a shorter list.
func objectsCount(args []string) error {
	fs := flag.NewFlagSet("ax objects count", flag.ContinueOnError)
	lang := fs.String("lang", "en", "the language directory to read")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return errors.New("usage: ax objects count <id> [...]")
	}
	for _, ref := range rest {
		id, err := axid.Parse(ref)
		if err != nil {
			return err
		}
		p, err := objects.Build(corpusRoot(), *lang, id)
		if err != nil {
			return err
		}
		counts(p)
	}
	return nil
}

func counts(p objects.Paper) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "%s %s\t%s\t%s\n", p.Paper, p.Version,
		prose.Count(len(p.Objects), "object"), prose.Count(len(p.Files), "file"))
	have := p.Counts()
	for _, k := range objects.Kinds {
		fmt.Fprintf(tw, "  %s\t%d\n", k, have[k])
	}
	tw.Flush()
}

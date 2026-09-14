package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/fetch"
	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/refs"
)

// runRefs parses a paper's bibliography into structured entries.
//
// The bibliography is the most regular prose in a paper. A style file laid it
// out and the same style file laid out every other entry in the same list, so
// this is done by pattern and never by a model: a model asked to read a hundred
// entries per paper across three million papers would cost more than everything
// else in this pipeline put together, and would be wrong in prose, which is the
// hardest kind of wrong to find later.
//
// An entry that resolves to nothing is still published as a bibliography line.
// It just does not become an edge, and a reference to a textbook resolving to
// nothing is the right answer rather than a failure.
func runRefs(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: ax refs build <id>v<n> [...]")
	}
	switch args[0] {
	case "build":
		return refsBuild(args[1:])
	case "resolve":
		return errors.New("refs resolve arrives with the metadata plane lookup and is not written yet, run ax refs build")
	default:
		return fmt.Errorf("unknown refs subcommand %q, the subcommand is build", args[0])
	}
}

func refsBuild(args []string) error {
	fs := flag.NewFlagSet("ax refs build", flag.ContinueOnError)
	dry := fs.Bool("n", false, "say what was parsed and write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return errors.New("usage: ax refs build <id>v<n> [...]")
	}
	plane := metadata.Plane{Root: corpusRoot()}
	sources, err := fetch.Load(corpus.SourcesPath(plane.Root))
	if err != nil {
		return err
	}
	for _, ref := range rest {
		id, err := axid.Parse(ref)
		if err != nil {
			return err
		}
		p, _, err := readRendering(plane.Root, sources, id, ref)
		if err != nil {
			return err
		}
		// The gate runs here too. A bibliography entry is a line of the paper
		// and the manifest holds the whole of every one of them, so this file
		// is content and is checked at the point it is about to be written.
		rec, err := record(plane, id)
		if err != nil {
			return err
		}
		if err := fetch.Gate(rec, p.Version); err != nil {
			return err
		}
		m := refs.Manifest{Paper: id.Canonical, Version: p.Version, Entries: refs.Read(p.Bibliography)}
		if len(m.Entries) == 0 {
			fmt.Fprintf(os.Stderr, "%sv%d has no bibliography in its rendering\n", id.Canonical, p.Version)
			continue
		}
		if *dry {
			reportRefs(m)
			continue
		}
		path := corpus.RefsPath(plane.Root, id)
		changed, err := m.Save(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "%s in %s%s\n", prose.Count(len(m.Entries), "reference"), path, unchanged(changed))
	}
	return nil
}

func unchanged(changed bool) string {
	if changed {
		return ""
	}
	return ", unchanged"
}

// reportRefs says what came out of the bibliography and how much of it is
// usable.
//
// The counts are the point of the dry run. A field is a reading of a style and
// any of them can come back empty, so the way to know whether the parse worked
// is the proportion that carries a title and a year, and the way to know what
// will resolve is the proportion that names an arXiv id or a DOI outright.
func reportRefs(m refs.Manifest) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "%sv%d\t%s\n", m.Paper, m.Version, prose.Count(len(m.Entries), "reference"))
	var thin []refs.Entry
	counts := map[string]int{}
	for _, e := range m.Entries {
		if e.Title != "" {
			counts["a title"]++
		}
		if e.Year != 0 {
			counts["a year"]++
		}
		if len(e.Authors) > 0 {
			counts["an author"]++
		}
		if e.ArXiv != "" {
			counts["an arXiv id"]++
		}
		if e.DOI != "" {
			counts["a DOI"]++
		}
		if e.Title == "" {
			thin = append(thin, e)
		}
	}
	for _, k := range sorted(counts) {
		fmt.Fprintf(tw, "  %s\t%d of %d\n", k, counts[k], len(m.Entries))
	}
	tw.Flush()
	// Named rather than counted, because an entry with no title is one the
	// resolver cannot match on anything but an id, and three of them is a
	// style this parser does not read while thirty is a paper to look at.
	for _, e := range thin {
		fmt.Printf("  no title  %s  %s\n", e.ID, prose.Clip(e.Text, 80))
	}
}

func sorted(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

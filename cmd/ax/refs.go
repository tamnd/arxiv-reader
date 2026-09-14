package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
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
		return errors.New("usage: ax refs build <id>v<n> [...], or ax refs resolve <id> [...]")
	}
	switch args[0] {
	case "build":
		return refsBuild(args[1:])
	case "resolve":
		return refsResolve(args[1:])
	default:
		return fmt.Errorf("unknown refs subcommand %q, the subcommands are build and resolve", args[0])
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

// refsResolve matches the entries of a bibliography against the metadata plane.
//
// The ladder is in 05-extract.md section 9 and it stops at the first hit: an
// arXiv id printed in the entry, then a DOI, then the title with the year
// within one and at least one author in common, then nothing. Nothing is a fine
// outcome. A reference to a textbook resolves to nothing and the entry is still
// published as a bibliography line, it just does not become an edge.
//
// Every paper named in one run is resolved by one pass over the plane, because
// the pass is what the run costs. The plane holds three million records and a
// bibliography holds a hundred entries, so the entries are what goes into a map
// and the plane is streamed past them.
func refsResolve(args []string) error {
	fs := flag.NewFlagSet("ax refs resolve", flag.ContinueOnError)
	dry := fs.Bool("n", false, "say what resolved and write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return errors.New("usage: ax refs resolve <id> [...]")
	}
	plane := metadata.Plane{Root: corpusRoot()}
	type paper struct {
		id   axid.ID
		path string
		m    refs.Manifest
	}
	var papers []paper
	r := refs.NewResolver()
	for _, ref := range rest {
		id, err := axid.Parse(ref)
		if err != nil {
			return err
		}
		path := corpus.RefsPath(plane.Root, id)
		m, err := refs.Load(path)
		if err != nil {
			return err
		}
		if len(m.Entries) == 0 {
			return fmt.Errorf("nobody has read the bibliography of %s yet, run ax refs build %sv<n>", id.Canonical, id.Canonical)
		}
		// The same gate as everywhere else, at the point the file is about to
		// be written rather than at the point the command started.
		rec, err := record(plane, id)
		if err != nil {
			return err
		}
		if err := fetch.Gate(rec, m.Version); err != nil {
			return err
		}
		// Cleared and resolved again from scratch every run. Resolution is a
		// reading of the plane and the plane grows, so an entry that resolved
		// to nothing last month is a question worth asking again, and an entry
		// that resolved to something the plane no longer holds should stop
		// saying so.
		for i := range m.Entries {
			m.Entries[i].Resolved, m.Entries[i].Via = "", ""
			r.Want(entryKey(id.Canonical, m.Entries[i].ID), m.Entries[i])
		}
		papers = append(papers, paper{id: id, path: path, m: m})
	}
	if err := plane.ScanAll(func(_ string, rec metadata.Record) error {
		r.Offer(rec)
		return nil
	}); err != nil {
		return err
	}
	found := r.Matches()
	for _, p := range papers {
		for i, e := range p.m.Entries {
			if m, ok := found[entryKey(p.id.Canonical, e.ID)]; ok {
				p.m.Entries[i].Resolved, p.m.Entries[i].Via = m.Paper, m.Via
			}
		}
		if *dry {
			reportResolve(p.m, r.Misses())
			continue
		}
		changed, err := p.m.Save(p.path)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "%s of %s resolved in %s%s\n", prose.Count(resolved(p.m), "reference"), p.m.Paper, p.path, unchanged(changed))
	}
	return nil
}

// entryKey names one entry of one paper, which is what the resolver is keyed
// by, because an anchor is only unique inside the paper that printed it.
func entryKey(paper, id string) string { return paper + "/" + id }

func resolved(m refs.Manifest) int {
	n := 0
	for _, e := range m.Entries {
		if e.Resolved != "" {
			n++
		}
	}
	return n
}

// reportResolve says what resolved, by which step, and what was found and then
// refused.
//
// The near misses are the half of this worth reading. A resolution rate on its
// own cannot tell a threshold that is doing its job from one that is set wrong,
// and the entries that cleared the title and failed on the year or the author
// are where that shows.
func reportResolve(m refs.Manifest, misses []refs.Miss) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "%sv%d\t%s\n", m.Paper, m.Version, prose.Count(len(m.Entries), "reference"))
	counts := map[string]int{}
	for _, e := range m.Entries {
		if e.Resolved != "" {
			counts["via "+e.Via]++
		}
	}
	fmt.Fprintf(tw, "  resolved\t%d of %d\n", resolved(m), len(m.Entries))
	for _, k := range sorted(counts) {
		fmt.Fprintf(tw, "  %s\t%d\n", k, counts[k])
	}
	tw.Flush()
	for _, miss := range misses {
		if !strings.HasPrefix(miss.Key, m.Paper+"/") {
			continue
		}
		fmt.Printf("  near miss  %.2f  %s  %s  %s\n", miss.Score, strings.TrimPrefix(miss.Key, m.Paper+"/"), miss.Paper, miss.Why)
	}
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

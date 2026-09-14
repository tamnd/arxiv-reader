package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/fetch"
	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/tables"
)

// runTables writes each of a paper's tables twice.
//
// Markdown is what a reader sees and what a translator works on, and it is
// lossy on purpose: it cannot express a multi row span, a multi column header
// or a rule drawn under part of a row, and academic tables use all three
// constantly. The markup is what the loss is measured against, and rule F12 is
// the measurement.
//
// The Markdown written here is the same text the extractor puts inside the
// section the table belongs to, from the same emitter, so the two cannot say
// different things about the same table.
func runTables(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: ax tables <id>v<n> [...], or ax tables check <id>v<n> [...]")
	}
	if args[0] == "check" {
		return tablesCheck(args[1:])
	}
	return tablesWrite(args)
}

func tablesWrite(args []string) error {
	fs := flag.NewFlagSet("ax tables", flag.ContinueOnError)
	dry := fs.Bool("n", false, "say what would be written and write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	refs := fs.Args()
	if len(refs) == 0 {
		return errors.New("usage: ax tables <id>v<n> [...]")
	}
	plane := metadata.Plane{Root: corpusRoot()}
	sources, err := fetch.Load(corpus.SourcesPath(plane.Root))
	if err != nil {
		return err
	}
	for _, ref := range refs {
		id, err := axid.Parse(ref)
		if err != nil {
			return err
		}
		p, _, err := readRendering(plane.Root, sources, id, ref)
		if err != nil {
			return err
		}
		// The gate runs here too. Both files are the paper's content and not
		// its metadata, and a command that writes content into the corpus
		// checks for itself rather than trusting the one that ran before it.
		rec, err := record(plane, id)
		if err != nil {
			return err
		}
		if err := fetch.Gate(rec, p.Version); err != nil {
			return err
		}
		found := tablesIn(p)
		if *dry {
			reportTables(id, p, found)
			continue
		}
		if err := writeTables(corpus.TablesDir(plane.Root, id), id, found); err != nil {
			return err
		}
	}
	return nil
}

// tbl is one table and where it sits in the paper.
type tbl struct {
	// Tag is the number the paper prints, empty for an unnumbered table.
	tag  string
	rows []extract.Row
}

// tablesIn is every table in the paper, in printed order.
//
// A tabular inside a figure counts. A paper that lays a table out as a panel of
// a figure has still written a table, the cells still hold its measured
// results, and a reader still wants the columns.
func tablesIn(p *extract.Paper) []tbl {
	var found []tbl
	var walk func([]extract.Block)
	walk = func(bs []extract.Block) {
		for _, b := range bs {
			if len(b.Rows) > 0 {
				found = append(found, tbl{tag: b.Tag, rows: b.Rows})
			}
			walk(b.Blocks)
		}
	}
	var sections func([]extract.Section)
	sections = func(ss []extract.Section) {
		for _, s := range ss {
			walk(s.Blocks)
			sections(s.Sections)
		}
	}
	sections(p.Sections)
	return found
}

// writeTables puts the pair of files down for each table and takes away the
// pairs of any table the paper no longer has.
//
// Idempotent, like the content writer, because a re-extraction of a finished
// paper that rewrites forty files is a commit that says nothing.
func writeTables(dir string, id axid.ID, found []tbl) error {
	if len(found) == 0 {
		fmt.Fprintf(os.Stderr, "%s has no tables\n", corpus.PathID(id))
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	keep := map[string]bool{}
	written, unchanged := 0, 0
	for i, t := range found {
		name := corpus.TableName(i + 1)
		for ext, body := range map[string]string{
			".md":  extract.Table(t.rows) + "\n",
			".tex": tex(id, t),
		} {
			keep[name+ext] = true
			changed, err := put(filepath.Join(dir, name+ext), body)
			if err != nil {
				return err
			}
			if changed {
				written++
			} else {
				unchanged++
			}
		}
	}
	removed, err := sweep(dir, keep)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s in %s: %d written, %d unchanged, %d removed\n",
		prose.Count(len(found), "table"), dir, written, unchanged, removed)
	return nil
}

// tex is the markup file, with a header saying where it came from.
//
// The header is there because this is a reconstruction on the render path and
// not the author's bytes, and a file called t01.tex that does not say so is a
// file somebody will eventually quote as the source.
func tex(id axid.ID, t tbl) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%% Table %s of %s, rebuilt from arXiv's own rendering.\n", label(t.tag), corpus.PathID(id))
	b.WriteString("% Not the author's source. The spans, the alignment and the rules are LaTeXML's reading of it.\n")
	b.WriteString(extract.Tabular(t.rows))
	return b.String()
}

func label(tag string) string {
	if tag == "" {
		return "(unnumbered)"
	}
	return tag
}

// put writes a file and says whether the bytes changed.
func put(path, body string) (bool, error) {
	if old, err := os.ReadFile(path); err == nil && string(old) == body {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(body), 0o644)
}

// sweep takes away the files of a table the paper no longer has.
//
// A corpus holding a table that was cut between versions is a corpus publishing
// something the paper does not say.
func sweep(dir string, keep map[string]bool) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || keep[e.Name()] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func reportTables(id axid.ID, p *extract.Paper, found []tbl) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "%sv%d\t%s\n", id.Canonical, p.Version, prose.Count(len(found), "table"))
	for i, t := range found {
		rows, cols := shape(t.rows)
		fmt.Fprintf(tw, "  %s\tTable %s\t%s by %s\n", corpus.TableName(i+1), label(t.tag),
			prose.Count(rows, "row"), prose.Count(cols, "column"))
	}
	tw.Flush()
}

// tablesCheck is rules F11 and F12 over what is on disk.
//
// The same reading the audit does, out of the same package, because a command
// that says a pair agrees and a rule that says it does not is worse than
// having neither.
func tablesCheck(args []string) error {
	fs := flag.NewFlagSet("ax tables check", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	refs := fs.Args()
	if len(refs) == 0 {
		return errors.New("usage: ax tables check <id> [...]")
	}
	root := corpusRoot()
	bad := 0
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, ref := range refs {
		id, err := axid.Parse(ref)
		if err != nil {
			return err
		}
		dir := corpus.TablesDir(root, id)
		names, err := tables.Names(dir)
		if err != nil {
			return fmt.Errorf("%s has no tables to check: %w", dir, err)
		}
		fmt.Fprintf(tw, "%s\t%s in %s\n", id.Canonical, prose.Count(len(names), "table"), dir)
		for _, name := range names {
			why, err := tables.Agree(dir, name)
			if err != nil {
				return err
			}
			if why == nil {
				fmt.Fprintf(tw, "  %s\tF11 F12\tboth files agree\n", name)
				continue
			}
			bad++
			fmt.Fprintf(tw, "  %s\tfails\t%s\n", name, why)
		}
	}
	tw.Flush()
	if bad > 0 {
		return fmt.Errorf("%s of the two representations disagree", prose.Count(bad, "pair"))
	}
	return nil
}

func shape(rows []extract.Row) (int, int) {
	cols := 0
	for _, r := range rows {
		n := 0
		for _, c := range r.Cells {
			n += max(c.Span, 1)
		}
		if n > cols {
			cols = n
		}
	}
	return len(rows), cols
}

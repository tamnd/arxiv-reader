package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/fetch"
	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/metadata"
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

// tablesCheck is rule F11 and rule F12 over what is on disk.
//
// F11 says both files are there and F12 says they agree on the row count, the
// column count and every numeric cell. The numbers are the part that matters:
// a table is where a paper's measured results live, and a representation that
// quietly reformats one of them is worse than no second representation at all.
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
		names, err := tableNames(dir)
		if err != nil {
			return err
		}
		fmt.Fprintf(tw, "%s\t%s in %s\n", id.Canonical, prose.Count(len(names), "table"), dir)
		for _, name := range names {
			why, err := agree(dir, name)
			if err != nil {
				return err
			}
			if why == "" {
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

// tableNames is every table in a directory, by the stem of its files.
func tableNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("%s has no tables to check: %w", dir, err)
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		if ext != ".md" && ext != ".tex" {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ext)
		if !seen[stem] {
			seen[stem] = true
			out = append(out, stem)
		}
	}
	sort.Strings(out)
	return out, nil
}

// agree runs the two rules over one table and gives back what is wrong, or an
// empty string when nothing is.
func agree(dir, name string) (string, error) {
	md, mderr := os.ReadFile(filepath.Join(dir, name+".md"))
	tx, txerr := os.ReadFile(filepath.Join(dir, name+".tex"))
	switch {
	case mderr != nil:
		return "F11: there is no " + name + ".md, and a table is kept twice", nil
	case txerr != nil:
		return "F11: there is no " + name + ".tex, and a table is kept twice", nil
	}
	mdRows, txRows := mdCells(string(md)), texCells(string(tx))
	if len(mdRows) != len(txRows) {
		return fmt.Sprintf("F12: the Markdown has %d rows and the markup has %d", len(mdRows), len(txRows)), nil
	}
	mdCols := 0
	for _, r := range mdRows {
		if len(r) > mdCols {
			mdCols = len(r)
		}
	}
	if n := texCols(string(tx)); n != mdCols {
		return fmt.Sprintf("F12: the Markdown has %d columns and the markup has %d", mdCols, n), nil
	}
	a, b := numbers(mdRows), numbers(txRows)
	if len(a) != len(b) {
		return fmt.Sprintf("F12: the Markdown holds %d numbers and the markup holds %d", len(a), len(b)), nil
	}
	for i := range a {
		if a[i] != b[i] {
			return fmt.Sprintf("F12: number %d is %s in the Markdown and %s in the markup", i+1, a[i], b[i]), nil
		}
	}
	return "", nil
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

// mdCells is the cells of a Markdown table, row by row.
//
// The rule row is not a row of the table, and neither is the empty header the
// emitter puts in front of a table whose first row was not one. Both are
// artefacts of Markdown needing a header, and counting them would make the two
// representations disagree about a table they agree about.
func mdCells(s string) [][]string {
	var out [][]string
	for i, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for j := range cells {
			cells[j] = strings.TrimSpace(cells[j])
		}
		if i == 1 && isRule(cells) {
			continue
		}
		if i == 0 && blank(cells) {
			continue
		}
		out = append(out, cells)
	}
	return out
}

func isRule(cells []string) bool {
	for _, c := range cells {
		if c == "" || strings.Trim(c, ":-") != "" {
			return false
		}
	}
	return len(cells) > 0
}

func blank(cells []string) bool {
	for _, c := range cells {
		if c != "" {
			return false
		}
	}
	return true
}

// texCells is the cells of a tabular, row by row.
//
// Comments go first, because the header this writes says which table it is and
// which paper it came from, and a paper number read as a measurement would fail
// rule F12 on every table in the corpus.
func texCells(s string) [][]string {
	body := s
	if i := strings.Index(body, "}\n"); i >= 0 && strings.Contains(body[:i], "\\begin{tabular}") {
		body = body[i+2:]
	}
	if i := strings.Index(body, "\\end{tabular}"); i >= 0 {
		body = body[:i]
	}
	var out [][]string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "%") || line == "\\hline" {
			continue
		}
		line = strings.TrimSuffix(line, "\\\\")
		cells := strings.Split(line, "&")
		for j := range cells {
			cells[j] = plainTeX(strings.TrimSpace(cells[j]))
		}
		out = append(out, cells)
	}
	return out
}

// plainTeX strips the markup off a cell and leaves what it says.
//
// The counted arguments of multicolumn and multirow go with it. Those are the
// shape of the table and not a measurement in it, and a span of 2 read as a
// number is the difference between the two files agreeing and not.
//
// Mathematics is left exactly as it stands, because the emitter leaves it
// exactly as it stands: the formula in the markup file is the same bytes as the
// formula in the Markdown file, so the two only match when neither side is
// touched. Stripping it is also wrong on its own terms. A \frac{1}{2} with the
// macro and the braces taken out reads as the number twelve, which is not a
// number anywhere in the paper.
func plainTeX(s string) string {
	for _, m := range []string{"multicolumn", "multirow"} {
		for {
			i := strings.Index(s, "\\"+m+"{")
			if i < 0 {
				break
			}
			rest := s[i+len(m)+2:]
			// Two braced arguments, then the text in the third.
			for n := 0; n < 2; n++ {
				j := strings.IndexByte(rest, '}')
				if j < 0 {
					return s
				}
				rest = rest[j+1:]
				if !strings.HasPrefix(rest, "{") {
					return s
				}
				rest = rest[1:]
			}
			s = s[:i] + rest
		}
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '$' {
			if j := strings.IndexByte(s[i+1:], '$'); j >= 0 {
				b.WriteString(s[i : i+1+j+1])
				i += 1 + j + 1
				continue
			}
		}
		if s[i] == '\\' && i+1 < len(s) {
			// A backslash before a letter starts a macro and the whole name
			// goes. A backslash before anything else is protecting that
			// character, which is the ampersand and the per cent sign a cell
			// is full of, so the character stays and the backslash does not.
			if !letter(s[i+1]) {
				b.WriteByte(s[i+1])
				i += 2
				continue
			}
			j := i + 1
			for j < len(s) && letter(s[j]) {
				j++
			}
			i = j
			continue
		}
		if s[i] == '{' || s[i] == '}' {
			i++
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func letter(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }

// cols is the number of columns a row covers, which is its cell count for
// Markdown and is read out of the specification for a tabular.
func texCols(s string) int {
	n := 0
	if i := strings.Index(s, "\\begin{tabular}{"); i >= 0 {
		rest := s[i+len("\\begin{tabular}{"):]
		if j := strings.IndexByte(rest, '}'); j >= 0 {
			for _, r := range rest[:j] {
				if r == 'l' || r == 'c' || r == 'r' {
					n++
				}
			}
		}
	}
	return n
}

// numbers is every number in a set of cells, in order.
//
// A number is a run of digits with an optional decimal point, which is what a
// results table is full of. Read cell by cell rather than off the whole file,
// because the two files lay the same table out differently by design and the
// question the rule asks is whether the same measurements are in both.
func numbers(rows [][]string) []string {
	var out []string
	for _, r := range rows {
		for _, c := range r {
			for i := 0; i < len(c); {
				if !digit(c[i]) {
					i++
					continue
				}
				j := i
				for j < len(c) && (digit(c[j]) || c[j] == '.') {
					j++
				}
				out = append(out, strings.TrimRight(c[i:j], "."))
				i = j
			}
		}
	}
	return out
}

func digit(b byte) bool { return b >= '0' && b <= '9' }

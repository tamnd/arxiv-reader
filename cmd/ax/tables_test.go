package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/extract"
)

// The pair a real table is written as, so the check can be run over the same
// bytes the writer puts down.
const (
	sampleMD = `| Model | Accuracy |  |
| :--- | :---: | :---: |
| Nothing | 0.0 | 0.1 |
`
	sampleTeX = `% Table 1 of 2501.00001, rebuilt from arXiv's own rendering.
% Not the author's source. The spans, the alignment and the rules are LaTeXML's reading of it.
\begin{tabular}{lcc}
\textbf{Model} & \multicolumn{2}{c}{\textbf{Accuracy}} \\
\hline
Nothing & 0.0 & 0.1 \\
\end{tabular}
`
)

func pair(t *testing.T, md, tx string) string {
	t.Helper()
	dir := t.TempDir()
	if md != "" {
		if err := os.WriteFile(filepath.Join(dir, "t01.md"), []byte(md), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if tx != "" {
		if err := os.WriteFile(filepath.Join(dir, "t01.tex"), []byte(tx), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestTheTwoRepresentationsAgree(t *testing.T) {
	why, err := agree(pair(t, sampleMD, sampleTeX), "t01")
	if err != nil {
		t.Fatal(err)
	}
	if why != "" {
		t.Fatalf("the pair the writer produces does not pass its own check: %s", why)
	}
}

// F11 is the rule that a table is kept twice, so half a pair fails before
// anything is compared.
func TestOneHalfOfAPairFailsF11(t *testing.T) {
	for _, c := range []struct{ md, tx, want string }{
		{sampleMD, "", "no t01.tex"},
		{"", sampleTeX, "no t01.md"},
	} {
		why, err := agree(pair(t, c.md, c.tx), "t01")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(why, c.want) || !strings.HasPrefix(why, "F11") {
			t.Errorf("got %q, want F11 and %q", why, c.want)
		}
	}
}

// F12 is the measurement. A number that differs between the two files is the
// failure the rule exists for, because a table is where a paper's results live
// and a second representation that quietly reformats one of them is worse than
// no second representation.
func TestANumberThatDiffersFailsF12(t *testing.T) {
	tx := strings.Replace(sampleTeX, "0.1", "0.2", 1)
	why, err := agree(pair(t, sampleMD, tx), "t01")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(why, "0.1 in the Markdown and 0.2 in the markup") {
		t.Fatalf("got %q", why)
	}
}

func TestAMissingRowFailsF12(t *testing.T) {
	tx := strings.Replace(sampleTeX, "Nothing & 0.0 & 0.1 \\\\\n", "", 1)
	why, err := agree(pair(t, sampleMD, tx), "t01")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(why, "2 rows and the markup has 1") {
		t.Fatalf("got %q", why)
	}
}

func TestAMissingColumnFailsF12(t *testing.T) {
	tx := strings.Replace(sampleTeX, `{lcc}`, `{lc}`, 1)
	why, err := agree(pair(t, sampleMD, tx), "t01")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(why, "3 columns and the markup has 2") {
		t.Fatalf("got %q", why)
	}
}

// The header of the markup file names the paper, and a paper number read as a
// measurement would fail F12 on every table in the corpus.
func TestTheHeaderIsNotReadAsNumbers(t *testing.T) {
	if got := numbers(texCells(sampleTeX)); strings.Join(got, ",") != "0.0,0.1" {
		t.Fatalf("got %v, want the two measurements", got)
	}
}

// The counted arguments of multicolumn and multirow are the shape of the table
// and not a measurement in it, so a span of 2 must not read as a number.
func TestPlainTeX(t *testing.T) {
	cases := map[string]string{
		`\multicolumn{2}{c}{\textbf{Accuracy}}`:     "Accuracy",
		`\multirow{3}{*}{Both}`:                     "Both",
		`\multicolumn{2}{c}{\multirow{3}{*}{Both}}`: "Both",
		`\textbf{0.95}`:                             "0.95",
		// An escaped character is that character. It is the per cent sign and
		// the ampersand a results table is full of, and a cell that kept the
		// backslash would not match the Markdown it is checked against.
		`99.5\%`: "99.5%",
		`AT\&T`:  "AT&T",
		`plain`:  "plain",
	}
	for in, want := range cases {
		if got := plainTeX(in); got != want {
			t.Errorf("%q is %q, want %q", in, got, want)
		}
	}
}

// Mathematics is the same bytes in both files, so stripping it on one side is
// what makes them disagree. The cell is from Mamba's table 14, where taking the
// macro and the braces out of \frac{1}{2} leaves the number twelve, which is
// not a number anywhere in the paper.
func TestMathematicsIsNotStripped(t *testing.T) {
	cell := `\textbf{$\bm{A}_{n}=-\frac{1}{2}+ni$}`
	if got := plainTeX(cell); got != `$\bm{A}_{n}=-\frac{1}{2}+ni$` {
		t.Fatalf("got %q", got)
	}
	if got := numbers([][]string{{plainTeX(cell)}}); strings.Join(got, ",") != "1,2" {
		t.Fatalf("got %v, want the two the Markdown reads", got)
	}
}

// Markdown has to have a header row and a rule under it, and neither is a row
// of the table. Counting them would make the two files disagree about a table
// they agree about.
func TestTheRuleRowAndTheEmptyHeaderAreNotRows(t *testing.T) {
	md := "|  |  |\n| :--- | :--- |\n| a | b |\n| c | d |\n"
	if got := mdCells(md); len(got) != 2 {
		t.Fatalf("got %d rows, want 2: %v", len(got), got)
	}
}

// Writing the same paper twice must leave the same bytes, because a
// re-extraction of a finished paper that rewrites forty files is a commit that
// says nothing.
func TestWritingTwiceChangesNothing(t *testing.T) {
	dir := t.TempDir()
	id := axid.ID{Canonical: "2501.00001", Year: 2025, Month: 1}
	found := []tbl{{tag: "1", rows: []extract.Row{
		{Cells: []extract.Cell{{Text: "a", Span: 1, Down: 1}, {Text: "b", Span: 1, Down: 1}}},
	}}}
	if err := writeTables(dir, id, found); err != nil {
		t.Fatal(err)
	}
	before := read(t, dir)
	if err := writeTables(dir, id, found); err != nil {
		t.Fatal(err)
	}
	if after := read(t, dir); after != before {
		t.Fatalf("the second run wrote different bytes:\n%s\n%s", before, after)
	}
	if !strings.Contains(before, "t01.md") || !strings.Contains(before, "t01.tex") {
		t.Fatalf("a table was not written twice:\n%s", before)
	}
}

// A corpus holding a table that was cut between versions is a corpus publishing
// something the paper does not say.
func TestATableThePaperNoLongerHasIsTakenAway(t *testing.T) {
	dir := t.TempDir()
	id := axid.ID{Canonical: "2501.00001", Year: 2025, Month: 1}
	one := tbl{tag: "1", rows: []extract.Row{{Cells: []extract.Cell{{Text: "a", Span: 1, Down: 1}}}}}
	two := tbl{tag: "2", rows: []extract.Row{{Cells: []extract.Cell{{Text: "b", Span: 1, Down: 1}}}}}
	if err := writeTables(dir, id, []tbl{one, two}); err != nil {
		t.Fatal(err)
	}
	if err := writeTables(dir, id, []tbl{one}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir); strings.Contains(got, "t02") {
		t.Fatalf("the second table is still there:\n%s", got)
	}
}

func read(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, e := range entries {
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString("=== " + e.Name() + "\n")
		b.Write(body)
	}
	return b.String()
}

package tables

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func agreed(t *testing.T, md, tx string) *Problem {
	t.Helper()
	p, err := Agree(pair(t, md, tx), "t01")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTheTwoRepresentationsAgree(t *testing.T) {
	if p := agreed(t, sampleMD, sampleTeX); p != nil {
		t.Fatalf("the pair the writer produces does not pass its own check: %s", p)
	}
}

// F11 is the rule that a table is kept twice, so half a pair fails before
// anything is compared.
func TestOneHalfOfAPairFailsF11(t *testing.T) {
	for _, c := range []struct{ md, tx, want string }{
		{sampleMD, "", "no t01.tex"},
		{"", sampleTeX, "no t01.md"},
	} {
		p := agreed(t, c.md, c.tx)
		if p == nil {
			t.Fatalf("half a pair passed")
		}
		if !strings.Contains(p.What, c.want) || p.Rule != "F11" {
			t.Errorf("got %s, want F11 and %q", p, c.want)
		}
	}
}

// F12 is the measurement. A number that differs between the two files is the
// failure the rule exists for, because a table is where a paper's results live
// and a second representation that quietly reformats one of them is worse than
// no second representation.
func TestANumberThatDiffersFailsF12(t *testing.T) {
	p := agreed(t, sampleMD, strings.Replace(sampleTeX, "0.1", "0.2", 1))
	if p == nil || p.Rule != "F12" || !strings.Contains(p.What, "0.1 in the Markdown and 0.2 in the markup") {
		t.Fatalf("got %v", p)
	}
}

func TestAMissingRowFailsF12(t *testing.T) {
	p := agreed(t, sampleMD, strings.Replace(sampleTeX, "Nothing & 0.0 & 0.1 \\\\\n", "", 1))
	if p == nil || !strings.Contains(p.What, "2 rows and the markup has 1") {
		t.Fatalf("got %v", p)
	}
}

func TestAMissingColumnFailsF12(t *testing.T) {
	p := agreed(t, sampleMD, strings.Replace(sampleTeX, `{lcc}`, `{lc}`, 1))
	if p == nil || !strings.Contains(p.What, "3 columns and the markup has 2") {
		t.Fatalf("got %v", p)
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

// The count is what tells a paper whose tables were written from one whose
// tables were not, so it has to see one table where the writer wrote one file.
func TestCountingTheTablesInABody(t *testing.T) {
	body := "Some prose.\n\n" + sampleMD + "\nMore prose.\n\n" + sampleMD
	if got := Count(body); got != 2 {
		t.Fatalf("got %d, want 2", got)
	}
	if got := Count("Nothing but prose here.\n"); got != 0 {
		t.Fatalf("got %d, want 0", got)
	}
}

// A pipe inside a cell is written \|, and splitting on it reads a six column
// table as eight. KAN's table 4 has one in a formula, which is where this came
// from.
func TestAPipeInsideACellIsNotASeparator(t *testing.T) {
	md := "| a | $0.08\\|4.02x+6.28\\|$ |\n| :--- | :--- |\n| b | c |\n"
	rows := mdCells(md)
	if len(rows) != 2 || len(rows[0]) != 2 {
		t.Fatalf("got %v", rows)
	}
	if rows[0][1] != "$0.08|4.02x+6.28|$" {
		t.Errorf("the cell reads %q", rows[0][1])
	}
}

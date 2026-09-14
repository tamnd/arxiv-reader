package extract

import (
	"strings"
	"testing"
)

// The same table as TestATable in markdown_test.go, so the two emitters can be
// read side by side. The Markdown loses the span and writes an empty cell where
// the second column of the header would be, and this is what it loses it to.
func TestATabular(t *testing.T) {
	rows := []Row{
		{
			Cells: []Cell{
				{Text: "Model", Header: true, Align: "left", Span: 1, Down: 1},
				{Text: "Accuracy", Header: true, Align: "center", Span: 2, Down: 1},
			},
			Below: true,
		},
		{Cells: []Cell{
			{Text: "Nothing", Span: 1, Down: 1},
			{Text: "0.0", Span: 1, Down: 1},
			{Text: "0.1", Span: 1, Down: 1},
		}},
	}
	want := strings.Join([]string{
		`\begin{tabular}{lcc}`,
		`\textbf{Model} & \multicolumn{2}{c}{\textbf{Accuracy}} \\`,
		`\hline`,
		`Nothing & 0.0 & 0.1 \\`,
		`\end{tabular}`,
		"",
	}, "\n")
	if got := Tabular(rows); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// A cell that covers two rows is the thing Markdown cannot say at all, and a
// cell that covers both directions has to nest the two macros the way LaTeX
// takes them, which is the row span inside the column span.
func TestACellThatSpansBothWays(t *testing.T) {
	rows := []Row{
		{Cells: []Cell{{Text: "Both", Align: "center", Span: 2, Down: 3}}},
		{Cells: []Cell{{Text: "a", Span: 1, Down: 1}, {Text: "b", Span: 1, Down: 1}}},
	}
	got := Tabular(rows)
	if !strings.Contains(got, `\multicolumn{2}{c}{\multirow{3}{*}{Both}}`) {
		t.Fatalf("got\n%s", got)
	}
}

// The column specification comes from a row that fills the table, because a
// spanning title says nothing about the columns underneath it.
func TestTheColumnsComeFromARowThatFillsTheTable(t *testing.T) {
	rows := []Row{
		{Cells: []Cell{{Text: "Results", Align: "center", Span: 3, Down: 1}}},
		{Cells: []Cell{
			{Text: "a", Align: "left", Span: 1, Down: 1},
			{Text: "b", Align: "right", Span: 1, Down: 1},
			{Text: "c", Align: "center", Span: 1, Down: 1},
		}},
	}
	if got := Tabular(rows); !strings.HasPrefix(got, `\begin{tabular}{lrc}`) {
		t.Fatalf("got\n%s", got)
	}
}

// A rule above the first row and below the last is a booktabs table, and the
// rules are what such a table uses instead of saying which row is the header.
func TestTheRulesAreKept(t *testing.T) {
	rows := []Row{
		{Cells: []Cell{{Text: "a", Span: 1, Down: 1}}, Above: true, Below: true},
		{Cells: []Cell{{Text: "b", Span: 1, Down: 1}}, Below: true},
	}
	want := strings.Join([]string{
		`\begin{tabular}{l}`,
		`\hline`,
		`a \\`,
		`\hline`,
		`b \\`,
		`\hline`,
		`\end{tabular}`,
		"",
	}, "\n")
	if got := Tabular(rows); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestATableWithNoRowsIsNotWritten(t *testing.T) {
	if got := Tabular(nil); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestInlineTeX(t *testing.T) {
	cases := map[string]string{
		// A number passes through untouched, which is what rule F12 checks.
		"0.95":                "0.95",
		"99.5%":               `99.5\%`,
		"**bold**":            `\textbf{bold}`,
		"*italic*":            `\textit{italic}`,
		"`code`":              `\texttt{code}`,
		`a\|b`:                "a|b",
		"AT&T":                `AT\&T`,
		"x_1":                 `x\_1`,
		"[Theorem 1](#thm-1)": "Theorem 1",
		// An asterisk that opens nothing is a literal asterisk, not the start
		// of an emphasis that runs to the end of the cell.
		"2 * 3": "2 * 3",
	}
	for in, want := range cases {
		if got := InlineTeX(in); got != want {
			t.Errorf("%q is %q, want %q", in, got, want)
		}
	}
}

// Mathematics is passed through as it stands. A converter that escaped inside
// $...$ would turn every subscript in the table into a literal underscore, and
// a cell is mathematics far more often than it is prose.
func TestInlineTeXLeavesMathematicsAlone(t *testing.T) {
	cases := map[string]string{
		"$x_1^2$":           "$x_1^2$",
		"$\\alpha$ and 50%": `$\alpha$ and 50\%`,
		// An unclosed dollar is not a formula, so it is escaped like anything
		// else rather than swallowing the rest of the cell.
		"$5 or more": `\$5 or more`,
	}
	for in, want := range cases {
		if got := InlineTeX(in); got != want {
			t.Errorf("%q is %q, want %q", in, got, want)
		}
	}
}

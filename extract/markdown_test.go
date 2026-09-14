package extract

import (
	"strings"
	"testing"
)

func TestAnAttributeBlock(t *testing.T) {
	cases := []struct {
		attrs attrs
		want  string
	}{
		{attrs{}, ""},
		{attrs{id: "thm-1"}, "{#thm-1}"},
		{attrs{id: "thm-1", class: "statement"}, "{#thm-1 .statement}"},
		{attrs{id: "s4", class: "section"}.with("kind", "appendix"), "{#s4 .section kind=appendix}"},
		// A value with a space in it would otherwise split into two attributes
		// and a value with a brace in it would end the block early.
		{attrs{id: "thm-1", class: "statement"}.with("env", "open problem"), `{#thm-1 .statement env="open problem"}`},
		// An empty value is not written at all, because an attribute saying
		// nothing is noise on every line that carries it.
		{attrs{id: "eq-1", class: "equation"}.with("env", ""), "{#eq-1 .equation}"},
	}
	for _, c := range cases {
		if got := c.attrs.String(); got != c.want {
			t.Errorf("got %s, want %s", got, c.want)
		}
	}
}

func TestATheoremCarriesItsEnvironment(t *testing.T) {
	w := &body{names: newNames()}
	w.block(Block{
		Kind: KindTheorem, Env: "Lemma", Tag: "2.1",
		Blocks: []Block{{Kind: KindParagraph, Text: "Nothing is nothing."}},
	}, within{}, 0)
	want := "**Lemma 2.1** {#thm-2-1 .statement env=lemma}\n\nNothing is nothing.\n"
	if got := w.String(); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if len(w.statements) != 1 || w.statements[0] != "thm-2-1" {
		t.Fatalf("the front matter lists %v as its statements", w.statements)
	}
}

// The author's own word for the environment, because a paper with a
// \newtheorem{observation} has an observation in it and a reader holding the
// PDF open beside this should see the same words.
func TestATheoremKeepsTheAuthorsOwnWord(t *testing.T) {
	w := &body{names: newNames()}
	w.block(Block{Kind: KindTheorem, Env: "observation", Tag: "3", Title: "[after Nobody]"}, within{}, 0)
	if !strings.HasPrefix(w.String(), "**Observation 3 [after Nobody]** {#rem-3 .remark env=observation}") {
		t.Fatalf("got %q", w.String())
	}
}

func TestAnUnnumberedEquationIsWrittenWithNoAnchor(t *testing.T) {
	w := &body{names: newNames()}
	w.block(Block{Kind: KindEquation, Text: `x=1`}, within{}, 0)
	if got := w.String(); got != "$$\nx=1\n$$\n" {
		t.Fatalf("got %q", got)
	}
	if w.equations != 1 {
		t.Fatalf("counted %d equations, want 1", w.equations)
	}
}

// A float with nothing of its own in it is a container LaTeXML made so two
// numbered things could sit side by side. Labelling it would put a bare
// **Figure** above two algorithms that are already labelled.
func TestAContainerFloatIsNotWritten(t *testing.T) {
	w := &body{names: newNames()}
	w.block(Block{
		Kind: KindFigure,
		Blocks: []Block{
			{Kind: KindAlgorithm, Tag: "1", Caption: "Do nothing", Text: "return"},
			{Kind: KindAlgorithm, Tag: "2", Caption: "Do less", Text: "return"},
		},
	}, within{}, 0)
	got := w.String()
	if strings.Contains(got, ".figure") {
		t.Fatalf("the container was labelled:\n%s", got)
	}
	for _, want := range []string{"**Algorithm 1** {#lst-1 .code}", "**Algorithm 2** {#lst-2 .code}"} {
		if !strings.Contains(got, want) {
			t.Fatalf("want %q in\n%s", want, got)
		}
	}
	if w.figures != nil {
		t.Fatalf("the container was counted as a figure: %v", w.figures)
	}
}

// The body of an algorithm float is a listing, and the float carries the number
// the paper prints, so labelling the listing as well gives one algorithm two
// headings.
func TestAListingInsideAnAlgorithmIsNotLabelledTwice(t *testing.T) {
	w := &body{names: newNames()}
	w.block(Block{
		Kind: KindAlgorithm, Tag: "1", Caption: "Do nothing",
		Blocks: []Block{{Kind: KindListing, Text: "return $x$"}},
	}, within{}, 0)
	got := w.String()
	if strings.Count(got, ".code") != 1 {
		t.Fatalf("got %d labels, want 1:\n%s", strings.Count(got, ".code"), got)
	}
	if !strings.Contains(got, "return $x$") {
		t.Fatalf("the body is missing:\n%s", got)
	}
}

// Code goes in a fence, where a backslash is a backslash. Pseudocode does not,
// because pseudocode written with the algorithmic package is mathematics with
// keywords around it and a fence would print the LaTeX instead of the
// algorithm.
func TestCodeIsFencedAndPseudocodeIsNot(t *testing.T) {
	code := &body{names: newNames()}
	code.block(Block{Kind: KindListing, Verbatim: true, Text: "if x:\n    return 1"}, within{}, 0)
	if !strings.Contains(code.String(), "```\nif x:\n    return 1\n```") {
		t.Fatalf("code was not fenced:\n%s", code.String())
	}

	pseudo := &body{names: newNames()}
	pseudo.block(Block{Kind: KindListing, Text: "$x \\leftarrow 1$\nreturn $x$"}, within{}, 0)
	if !strings.Contains(pseudo.String(), "$x \\leftarrow 1$ \\\nreturn $x$") {
		t.Fatalf("pseudocode lost its hard line break:\n%s", pseudo.String())
	}
}

func TestAFigureKeepsItsImagesAndItsCaption(t *testing.T) {
	w := &body{names: newNames()}
	w.block(Block{
		Kind: KindFigure, Tag: "2", Caption: "Nothing, drawn.",
		Images: []Image{{Src: "2501.00001v3/a.png", Alt: "nothing"}},
	}, within{}, 0)
	want := "**Figure 2** {#fig-2 .figure}\n\n![nothing](2501.00001v3/a.png)\n\nNothing, drawn.\n"
	if got := w.String(); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestATable(t *testing.T) {
	rows := []Row{
		{Cells: []Cell{{Text: "Model", Header: true, Align: "left", Span: 1}, {Text: "Accuracy", Header: true, Align: "center", Span: 2}}},
		{Cells: []Cell{{Text: "Nothing", Span: 1}, {Text: "0.0", Span: 1}, {Text: "0.1", Span: 1}}},
	}
	want := strings.Join([]string{
		"| Model | Accuracy |  |",
		"| :--- | :---: | :---: |",
		"| Nothing | 0.0 | 0.1 |",
	}, "\n")
	if got := table(rows); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// Markdown has to have a header row, so a table whose first row is not one gets
// an empty header and keeps all of its rows rather than losing the first.
func TestATableWithNoHeaderKeepsEveryRow(t *testing.T) {
	rows := []Row{
		{Cells: []Cell{{Text: "a", Span: 1}, {Text: "b", Span: 1}}},
		{Cells: []Cell{{Text: "c", Span: 1}, {Text: "d", Span: 1}}},
	}
	got := table(rows)
	for _, want := range []string{"| a | b |", "| c | d |"} {
		if !strings.Contains(got, want) {
			t.Fatalf("want %q in\n%s", want, got)
		}
	}
	if lines := strings.Count(got, "\n") + 1; lines != 4 {
		t.Fatalf("got %d lines, want 4:\n%s", lines, got)
	}
}

// A pipe in a cell would end the column early, so it is escaped. Mathematics
// with a \mid in it arrives here constantly.
func TestAPipeInACellIsEscaped(t *testing.T) {
	got := table([]Row{{Cells: []Cell{{Text: "a|b", Span: 1}}}})
	if !strings.Contains(got, `a\|b`) {
		t.Fatalf("got\n%s", got)
	}
}

func TestAList(t *testing.T) {
	w := &body{names: newNames()}
	w.block(Block{Kind: KindList, Items: []string{"one", "two"}}, within{}, 0)
	if got := w.String(); got != "- one\n- two\n" {
		t.Fatalf("got %q", got)
	}
	w = &body{names: newNames()}
	w.block(Block{Kind: KindList, Ordered: true, Items: []string{"one", "two"}}, within{}, 0)
	if got := w.String(); got != "1. one\n2. two\n" {
		t.Fatalf("got %q", got)
	}
}

func TestProofTitle(t *testing.T) {
	cases := map[string]string{
		"":                    "Proof",
		"Proof.":              "Proof",
		"Proof of Theorem 1.": "Proof of Theorem 1",
		".":                   "Proof",
	}
	for in, want := range cases {
		if got := proofTitle(Block{Kind: KindProof, Title: in}); got != want {
			t.Errorf("%q is %q, want %q", in, got, want)
		}
	}
}

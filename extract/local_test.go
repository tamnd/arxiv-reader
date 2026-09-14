package extract

import "testing"

func TestTheClassOfABlock(t *testing.T) {
	cases := []struct {
		block Block
		want  string
	}{
		{Block{Kind: KindTheorem, Env: "theorem"}, "statement"},
		{Block{Kind: KindTheorem, Env: "Lemma"}, "statement"},
		{Block{Kind: KindTheorem, Env: "definition"}, "definition"},
		{Block{Kind: KindTheorem, Env: "example"}, "remark"},
		{Block{Kind: KindTheorem, Env: "conjecture"}, "problem"},
		{Block{Kind: KindTheorem, Env: "exercise"}, "exercise"},
		// An environment nobody has seen before is a statement, and the
		// author's own name for it is kept in the env attribute regardless.
		{Block{Kind: KindTheorem, Env: "sublemma"}, "statement"},
		{Block{Kind: KindProof}, "proof"},
		{Block{Kind: KindEquation}, "equation"},
		{Block{Kind: KindFigure}, "figure"},
		{Block{Kind: KindTable}, "table"},
		{Block{Kind: KindAlgorithm}, "code"},
		{Block{Kind: KindListing}, "code"},
		{Block{Kind: KindParagraph}, ""},
		{Block{Kind: KindList}, ""},
	}
	for _, c := range cases {
		if got := classOf(c.block); got != c.want {
			t.Errorf("%s %q is %q, want %q", c.block.Kind, c.block.Env, got, c.want)
		}
	}
}

func TestALocalIdentifierIsTheAuthorsOwnNumber(t *testing.T) {
	n := newNames()
	cases := []struct {
		block Block
		want  string
	}{
		{Block{Kind: KindTheorem, Env: "theorem", Tag: "1"}, "thm-1"},
		{Block{Kind: KindTheorem, Env: "definition", Tag: "2.3"}, "def-2-3"},
		{Block{Kind: KindFigure, Tag: "4"}, "fig-4"},
		{Block{Kind: KindTable, Tag: "A.1"}, "tab-a-1"},
		{Block{Kind: KindEquation, Tag: "7"}, "eq-7"},
		{Block{Kind: KindAlgorithm, Tag: "2"}, "lst-2"},
	}
	for _, c := range cases {
		if got := n.block(c.block); got != c.want {
			t.Errorf("%s %q is %q, want %q", c.block.Kind, c.block.Tag, got, c.want)
		}
	}
}

// An equation the author did not number gets no identifier at all. The corpus
// does not invent a locator with no counterpart in the PDF somebody might have
// open beside it, and that rule is only for equations: a figure still has a
// file and a theorem still needs something to point at.
func TestAnUnnumberedEquationGetsNoIdentifier(t *testing.T) {
	n := newNames()
	if got := n.block(Block{Kind: KindEquation}); got != "" {
		t.Fatalf("an unnumbered equation was named %q", got)
	}
	if got := n.block(Block{Kind: KindFigure}); got != "fig-u1" {
		t.Fatalf("an unnumbered figure was named %q, want fig-u1", got)
	}
	if got := n.block(Block{Kind: KindFigure}); got != "fig-u2" {
		t.Fatalf("the second unnumbered figure was named %q, want fig-u2", got)
	}
	if got := n.block(Block{Kind: KindTheorem, Env: "remark"}); got != "rem-u1" {
		t.Fatalf("an unnumbered remark was named %q, want rem-u1", got)
	}
}

// An author who numbers a theorem and a lemma both as 1 has handed this two
// blocks that want thm-1, and letting the second overwrite the first is how a
// cross reference ends up pointing at the wrong statement.
func TestTwoObjectsCannotClaimOneIdentifier(t *testing.T) {
	n := newNames()
	first := n.block(Block{Kind: KindTheorem, Env: "theorem", Tag: "1"})
	second := n.block(Block{Kind: KindTheorem, Env: "lemma", Tag: "1"})
	if first != "thm-1" || second != "thm-1-2" {
		t.Fatalf("got %q and %q, want thm-1 and thm-1-2", first, second)
	}
}

func TestAPanelHangsOffItsParent(t *testing.T) {
	n := newNames()
	parent := within{id: "fig-2", class: "figure"}
	a := n.panel(parent, Block{Kind: KindFigure}, 0)
	b := n.panel(parent, Block{Kind: KindFigure, Tag: "(b)"}, 1)
	if a != "fig-2a" || b != "fig-2b" {
		t.Fatalf("got %q and %q, want fig-2a and fig-2b", a, b)
	}
}

// Two numbered algorithms in one figure environment, which is how authors get
// them side by side, are Algorithm 1 and Algorithm 2 in every reference to them
// in the paper. They are not panels and they keep their own numbers.
func TestAPanelOfADifferentKindKeepsItsOwnNumber(t *testing.T) {
	n := newNames()
	parent := within{id: "fig-u1", class: "figure"}
	got := n.panel(parent, Block{Kind: KindAlgorithm, Tag: "1"}, 0)
	if got != "lst-1" {
		t.Fatalf("an algorithm inside a figure was named %q, want lst-1", got)
	}
}

func TestASectionIsNamedForItsPrintedNumber(t *testing.T) {
	n := newNames()
	cases := []struct {
		section Section
		path    []int
		want    string
	}{
		{Section{Tag: "4"}, []int{4}, "s4"},
		{Section{Tag: "4.1"}, []int{4, 1}, "s4-1"},
		{Section{Tag: "B.2"}, []int{2, 2}, "sb-2"},
		{Section{}, []int{1, 2}, "su1-2"},
	}
	for _, c := range cases {
		if got := n.section(c.section, c.path); got != c.want {
			t.Errorf("section %q is %q, want %q", c.section.Tag, got, c.want)
		}
	}
}

func TestNamingRecordsTheRenderingsAnchor(t *testing.T) {
	n := newNames()
	n.block(Block{Kind: KindFigure, ID: "S3.F2", Tag: "2"})
	n.section(Section{ID: "S3.SS1", Tag: "3.1"}, []int{3, 1})
	want := map[string]string{"S3.F2": "fig-2", "S3.SS1": "s3-1"}
	for from, to := range want {
		if got := n.Anchors()[from]; got != to {
			t.Errorf("%s maps to %q, want %q", from, got, to)
		}
	}
}

func TestSlugNumber(t *testing.T) {
	cases := map[string]string{
		"4":     "4",
		"4.1":   "4-1",
		"A.2":   "a-2",
		"(b)":   "b",
		"1a":    "1a",
		"IV.3":  "iv-3",
		"  7  ": "7",
	}
	for in, want := range cases {
		if got := slugNumber(in); got != want {
			t.Errorf("%q is %q, want %q", in, got, want)
		}
	}
}

func TestPanelLetter(t *testing.T) {
	cases := map[string]string{
		"(a)": "a",
		"b":   "b",
		"1":   "",
		"2.1": "",
		"":    "",
	}
	for in, want := range cases {
		if got := panelLetter(in); got != want {
			t.Errorf("%q is %q, want %q", in, got, want)
		}
	}
}

package extract

import (
	"strings"
	"testing"
)

// What LaTeXML writes between its two programs, cut down to the attributes this
// reads. The labels are on the element the author put \label inside, the xml:id
// is what the page carries as an id, and the numbers the paper prints are
// nowhere in it yet because the post processor is what works them out.
const middle = `<?xml version="1.0" encoding="UTF-8"?>
<document xmlns="http://dlmf.nist.gov/LaTeXML">
  <section xml:id="S1" labels="LABEL:sec:intro">
    <title>Introduction</title>
    <para xml:id="S1.p1"><p>A paper.</p></para>
    <theorem xml:id="S1.Thmtheorem1" labels="LABEL:thm:main LABEL:thm:alt">
      <title>Theorem 1</title>
    </theorem>
    <equation xml:id="S1.E1" labels="LABEL:eq:loss"/>
    <figure xml:id="S1.F1" labels="LABEL:fig:arch"/>
  </section>
</document>`

func TestLabelsReadsTheAuthorsOwnName(t *testing.T) {
	byID, err := Labels([]byte(middle))
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{
		"S1":             "sec:intro",
		"S1.Thmtheorem1": "thm:main",
		"S1.E1":          "eq:loss",
		"S1.F1":          "fig:arch",
	} {
		if byID[id] != want {
			t.Errorf("%s is labelled %q, want %q", id, byID[id], want)
		}
	}
	// An element the author named nothing is not in the map at all, rather than
	// in it with an empty name, because the passes that read this ask whether
	// there is a label and not what it is.
	if name, ok := byID["S1.p1"]; ok {
		t.Errorf("a paragraph with no label came back as %q", name)
	}
}

// Two \label in one environment is legal and authors do it. The first is the
// one kept, because what reads this wants one name per object.
func TestLabelsKeepsTheFirstOfSeveral(t *testing.T) {
	byID, err := Labels([]byte(middle))
	if err != nil {
		t.Fatal(err)
	}
	if got := byID["S1.Thmtheorem1"]; got != "thm:main" {
		t.Fatalf("got %q", got)
	}
}

// LaTeXML puts other things in the same attribute, so a value without the
// prefix is not the author's label and is stepped over rather than guessed at.
func TestLabelsIgnoresWhatIsNotALabel(t *testing.T) {
	byID, err := Labels([]byte(`<document><section xml:id="S1" labels="ID:something LABEL:sec:real"/></document>`))
	if err != nil {
		t.Fatal(err)
	}
	if got := byID["S1"]; got != "sec:real" {
		t.Fatalf("got %q", got)
	}
}

func TestLabelsRefusesWhatIsNotXML(t *testing.T) {
	if _, err := Labels([]byte("<html>a page and not the XML")); err == nil {
		t.Fatal("a document that is not XML read as one")
	}
}

func TestLabelPutsTheNamesBackOnThePaper(t *testing.T) {
	p := &Paper{
		Abstract: []Block{{Kind: KindParagraph, ID: "S0.p1"}},
		Sections: []Section{{
			ID: "S1",
			Blocks: []Block{
				{Kind: KindEquation, ID: "S1.E1"},
				{Kind: KindFigure, ID: "S1.F1"},
				{Kind: KindParagraph, ID: "S1.p1"},
			},
			Sections: []Section{{
				ID:     "S1.SS1",
				Blocks: []Block{{Kind: KindTheorem, ID: "S1.Thmtheorem1", Blocks: []Block{{Kind: KindParagraph, ID: "S1.Thmtheorem1.p1"}}}},
			}},
		}},
	}
	byID, err := Labels([]byte(middle))
	if err != nil {
		t.Fatal(err)
	}
	if n := p.Label(byID); n != 4 {
		t.Fatalf("labelled %d, want 4", n)
	}
	if p.Sections[0].Label != "sec:intro" {
		t.Errorf("the section is labelled %q", p.Sections[0].Label)
	}
	// Nested at both depths, because a theorem inside a subsection is where the
	// labels that matter most are.
	if got := p.Sections[0].Sections[0].Blocks[0].Label; got != "thm:main" {
		t.Errorf("the theorem is labelled %q", got)
	}
	if got := p.Sections[0].Blocks[2].Label; got != "" {
		t.Errorf("a paragraph the author named nothing is labelled %q", got)
	}
	if n := p.Labelled(); n != 4 {
		t.Fatalf("Labelled says %d", n)
	}
}

// Every paper off the render path, which is a good share of the corpus.
func TestLabelWithNothingToApplyChangesNothing(t *testing.T) {
	p := &Paper{Sections: []Section{{ID: "S1"}}}
	if n := p.Label(nil); n != 0 {
		t.Fatalf("labelled %d", n)
	}
	if p.Labelled() != 0 {
		t.Fatal("a paper with no labels says it has some")
	}
}

// The label is written out with the object, because the tag matcher reads the
// Markdown of the last version and not the XML, which is not kept per version.
func TestTheLabelIsWrittenIntoTheBlock(t *testing.T) {
	p := &Paper{
		ID:      "2201.11903",
		Version: 1,
		Title:   "A paper",
		Sections: []Section{{
			ID: "S1", Level: 1, Kind: "section", Tag: "1", Title: "Introduction", Label: "sec:intro",
			Blocks: []Block{
				{Kind: KindTheorem, ID: "S1.Thmtheorem1", Tag: "1", Env: "theorem", Label: "thm:main"},
				{Kind: KindEquation, ID: "S1.E1", Tag: "1", Text: "x = y", Label: "eq:loss"},
			},
		}},
	}
	files, err := Files(p, Front{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	all := ""
	for _, f := range files {
		all += f.Doc.Body
	}
	for _, want := range []string{`label=thm:main`, `label=eq:loss`} {
		if !strings.Contains(all, want) {
			t.Errorf("%s is not in what was written:\n%s", want, all)
		}
	}
	// A top level section is a whole file, so its heading is in the front
	// matter and there is no line in the body for an attribute block to sit on.
	// Its label goes up there with its identifier.
	if got := files[1].Doc.Front.Label; got != "sec:intro" {
		t.Errorf("the section file is labelled %q", got)
	}
}

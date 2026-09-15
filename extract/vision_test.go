package extract

import (
	"strings"
	"testing"
)

// The vision path is the one path whose input is Markdown, so every test here is a
// page written the way the prompt asks for one and a question about what was made
// of it.
//
// The pages are written out rather than built, for the same reason the native
// tests are: what is under test is what a model actually sends back, and a fixture
// assembled out of helpers is a fixture that tests the helpers.

// visionFront is page one of a paper, as the prompt asks for it.
const visionFront = `# Observation of Gravitational Waves from a Binary Neutron Star Inspiral

LIGO Scientific Collaboration and Virgo Collaboration

## Abstract

On August 17, 2017 at 12:41:04 UTC the Advanced LIGO and Advanced Virgo gravitational wave detectors made their first observation of a binary neutron star inspiral. The signal, GW170817, was detected with a combined signal to noise ratio of 32.4 and a false alarm rate estimate of less than one per 8.0 years.

# 1 Introduction

On August 17, 2017, the Advanced LIGO detectors and the Advanced Virgo detector observed a transient gravitational wave signal.`

// visionBody is a page with everything the prompt asks for on it at once.
const visionBody = `## 2.1 The measurement

The masses are inferred from the signal, and the total mass is well constrained.

$$
\mathcal{M} = \frac{(m_1 m_2)^{3/5}}{(m_1 + m_2)^{1/5}}
\tag{3}
$$

Table 2. The inferred parameters of the source, with the ninety per cent credible intervals.

| Parameter | Low spin | High spin |
| --- | --- | --- |
| Chirp mass | 1.188 | 1.188 |
| Total mass | 2.74 | 2.82 |

Figure 4. The gravitational wave spectrograms from the three detectors, with the signal visible in each.

Theorem 1. The inspiral is that of a binary neutron star.

Proof. The component masses are both under the maximum mass of a neutron star.

` + "```" + `
for page in pages:
    read(page)
` + "```" + `

- the first thing the section establishes, which runs over the end of the
  line the model wrote it on
- the second thing`

// visionRefs is a page of references, as the prompt asks for them.
const visionRefs = `# References

[1] B. P. Abbott et al., Observation of Gravitational Waves from a Binary Black
Hole Merger, Phys. Rev. Lett. 116, 061102 (2016).

[2] J. Aasi et al., Advanced LIGO, Class. Quantum Grav. 32, 074001 (2015).`

func TestTheTitlePageIsReadAndIsNotASection(t *testing.T) {
	p := Vision([]string{visionFront}, "1710.05832", 1)
	if !strings.HasPrefix(p.Title, "Observation of Gravitational Waves") {
		t.Errorf("the title came back as %q", p.Title)
	}
	if len(p.Abstract) == 0 {
		t.Fatal("the abstract was not found, and it is the one thing on the title page worth keeping")
	}
	if !strings.Contains(p.Abstract[0].Text, "August 17, 2017") {
		t.Errorf("the abstract came back as %q", p.Abstract[0].Text)
	}
	if len(p.Sections) != 1 {
		t.Fatalf("the title page produced %d sections, and only the introduction is one", len(p.Sections))
	}
	if p.Sections[0].Title != "Introduction" || p.Sections[0].Tag != "1" {
		t.Errorf("the first section is %q numbered %q", p.Sections[0].Title, p.Sections[0].Tag)
	}
	if p.FrontPages != "1" {
		t.Errorf("the front matter says it was read off page %q", p.FrontPages)
	}
}

// The depth of a heading is the number of hashes, which is the whole difference
// between this path and the one below it. The native path guesses from the shape of
// a printed line and gets subsections wrong; here the model was asked and answered.
func TestTheHashesSayHowDeepAHeadingIs(t *testing.T) {
	p := Vision([]string{"# 1 Introduction\n\nthe opening.", "## 1.1 The setting\n\nthe detail.\n\n### 1.1.1 A note\n\nthe note."}, "1710.05832", 1)
	if len(p.Sections) != 1 {
		t.Fatalf("there are %d top level sections and the paper has one", len(p.Sections))
	}
	if len(p.Sections[0].Sections) != 2 {
		t.Fatalf("the section has %d subsections", len(p.Sections[0].Sections))
	}
	if got := p.Sections[0].Sections[0]; got.Title != "The setting" || got.Tag != "1.1" {
		t.Errorf("the subsection is %q numbered %q", got.Title, got.Tag)
	}
}

func TestADisplayedEquationKeepsTheNumberThePagePrinted(t *testing.T) {
	p := Vision([]string{"# 2 Method\n\n" + visionBody}, "1710.05832", 1)
	eq := block(t, p, KindEquation)
	if eq.Tag != "3" {
		t.Errorf("the equation is numbered %q, and the page printed it as 3", eq.Tag)
	}
	if strings.Contains(eq.Text, `\tag`) {
		t.Errorf("the tag is still in the mathematics: %q", eq.Text)
	}
	if !strings.Contains(eq.Text, `\mathcal{M}`) {
		t.Errorf("the mathematics came back as %q", eq.Text)
	}
	if strings.Contains(eq.Text, "$$") {
		t.Errorf("the dollars are still in the mathematics: %q", eq.Text)
	}
}

// A page prints a table's caption and its rows as two things and a model writes
// them as two things. What the corpus stores is one table, because the emitters, the
// audit and the object model all read a table as a block with rows and a caption.
func TestATableAndItsCaptionBecomeOneTable(t *testing.T) {
	p := Vision([]string{"# 2 Method\n\n" + visionBody}, "1710.05832", 1)
	tab := block(t, p, KindTable)
	if tab.Tag != "2" {
		t.Errorf("the table is numbered %q", tab.Tag)
	}
	if !strings.Contains(tab.Caption, "inferred parameters") {
		t.Errorf("the caption came back as %q", tab.Caption)
	}
	if len(tab.Rows) != 3 {
		t.Fatalf("the table has %d rows, and the page printed a header and two", len(tab.Rows))
	}
	if !tab.Rows[0].Cells[0].Header {
		t.Error("the row above the dashes is not marked as the header")
	}
	if tab.Rows[1].Cells[0].Header {
		t.Error("a body row is marked as a header")
	}
	if got := tab.Rows[1].Cells[1].Text; got != "1.188" {
		t.Errorf("the first body row reads %q", got)
	}
	// And the row of dashes is not a row. It is typography, and the fact it states
	// is the header flag above.
	for _, row := range tab.Rows {
		if strings.Trim(row.Cells[0].Text, "-: ") == "" {
			t.Error("the rule row was published as a row of the table")
		}
	}
}

// The caption above the table, which is where half of all styles print one.
func TestATableCaptionPrintedUnderTheTableIsStillItsCaption(t *testing.T) {
	page := "# 2 Method\n\n| a | b |\n| --- | --- |\n| 1 | 2 |\n\nTable 7. What the run measured."
	p := Vision([]string{page}, "1710.05832", 1)
	tab := block(t, p, KindTable)
	if tab.Tag != "7" || len(tab.Rows) != 2 {
		t.Errorf("the table came back numbered %q with %d rows", tab.Tag, len(tab.Rows))
	}
}

func TestAFigureIsItsCaptionAndNothingElse(t *testing.T) {
	p := Vision([]string{"# 2 Method\n\n" + visionBody}, "1710.05832", 1)
	fig := block(t, p, KindFigure)
	if fig.Tag != "4" {
		t.Errorf("the figure is numbered %q", fig.Tag)
	}
	if !strings.Contains(fig.Caption, "spectrograms") {
		t.Errorf("the caption came back as %q", fig.Caption)
	}
}

func TestTheStatementsAndTheProofsAreRead(t *testing.T) {
	p := Vision([]string{"# 2 Method\n\n" + visionBody}, "1710.05832", 1)
	st := block(t, p, KindTheorem)
	if st.Env != "theorem" || st.Tag != "1" {
		t.Errorf("the statement came back as %q numbered %q", st.Env, st.Tag)
	}
	if !strings.Contains(st.Text, "binary neutron star") {
		t.Errorf("the statement reads %q", st.Text)
	}
	pr := block(t, p, KindProof)
	if !strings.Contains(pr.Blocks[0].Text, "maximum mass") {
		t.Errorf("the proof reads %q", pr.Blocks[0].Text)
	}
}

func TestAFencedListingIsAListing(t *testing.T) {
	p := Vision([]string{"# 2 Method\n\n" + visionBody}, "1710.05832", 1)
	l := block(t, p, KindListing)
	if !strings.Contains(l.Text, "for page in pages:") {
		t.Errorf("the listing reads %q", l.Text)
	}
	if !l.Verbatim {
		t.Error("a listing with no mathematics in it is not marked verbatim")
	}
}

// A list item that ran over its line is one item. Two would be the corpus saying
// the paper lists three things where it lists two.
func TestAWrappedListItemIsOneItem(t *testing.T) {
	p := Vision([]string{"# 2 Method\n\n" + visionBody}, "1710.05832", 1)
	l := block(t, p, KindList)
	if len(l.Items) != 2 {
		t.Fatalf("the list came back with %d items and the page printed two: %q", len(l.Items), l.Items)
	}
	if !strings.Contains(l.Items[0], "over the end of the line") {
		t.Errorf("the first item reads %q", l.Items[0])
	}
	if l.Ordered {
		t.Error("a list written with dashes came back ordered")
	}
}

func TestTheReferencesOfAReadingBecomeTheBibliography(t *testing.T) {
	p := Vision([]string{visionFront, visionRefs}, "1710.05832", 1)
	if len(p.Bibliography) != 2 {
		t.Fatalf("the bibliography has %d entries and the page printed two", len(p.Bibliography))
	}
	if p.Bibliography[0].Label != "[1]" {
		t.Errorf("the first entry is labelled %q", p.Bibliography[0].Label)
	}
	// The entry the page wrapped is one entry. A reader following [1] to half a
	// citation has been sent to the wrong place.
	if p.Bibliography[0].ID != "vision.bib1" {
		t.Errorf("an entry read off a picture calls itself %q", p.Bibliography[0].ID)
	}
	if !strings.Contains(strings.Join(p.Bibliography[0].Blocks, " "), "Phys. Rev. Lett. 116") {
		t.Errorf("the wrapped tail of the first entry was lost: %q", p.Bibliography[0].Blocks)
	}
	// And the references are not a section of the paper.
	for _, s := range p.Sections {
		if strings.EqualFold(s.Title, "references") {
			t.Error("the reference list was published as a section as well as a bibliography")
		}
	}
}

// Every section says which pages it was read off, because that is what audit rules
// S08 and S09 measure a file against and this path knows it exactly.
func TestEverySectionOfAReadingSaysWhichPagesItWasReadOff(t *testing.T) {
	p := Vision([]string{
		visionFront,
		"the introduction carries on over the page and says a good deal more about the detectors and what they saw.",
		"## 1.1 The detectors\n\nand then a subsection of it starts here.",
	}, "1710.05832", 1)
	if p.Pages != "1-3" {
		t.Errorf("the paper says it was read off pages %q", p.Pages)
	}
	if got := p.Sections[0].Pages; got != "1-3" {
		t.Errorf("the introduction says it was read off pages %q", got)
	}
	if len(p.Sections[0].Sections) != 1 {
		t.Fatalf("the subsection on page three was not filed under the introduction")
	}
	if got := p.Sections[0].Sections[0].Pages; got != "3" {
		t.Errorf("the subsection says it was read off pages %q", got)
	}
}

// A page nothing could read comes in empty, and the paper is the rest of it. That
// is a decision the caller has already made by the time this runs, so it is not an
// error here, and the pages after the hole keep their numbers.
func TestAPageNothingReadLeavesTheRestOfThePaperAlone(t *testing.T) {
	p := Vision([]string{visionFront, "", "# 2 Method\n\nand the method is described here at some length."}, "1710.05832", 1)
	if len(p.Sections) != 2 {
		t.Fatalf("the paper came back with %d sections", len(p.Sections))
	}
	if got := p.Sections[1].Pages; got != "3" {
		t.Errorf("the method says it was read off pages %q, and the hole is page two", got)
	}
}

func TestAnAppendixSaysItIsOne(t *testing.T) {
	p := Vision([]string{"# 1 Introduction\n\nthe opening.", "# Appendix A: The instrument\n\nthe detail of it."}, "1710.05832", 1)
	last := p.Sections[len(p.Sections)-1]
	if last.Kind != "appendix" {
		t.Errorf("the appendix came back as kind %q", last.Kind)
	}
	if last.Tag != "A" || last.Title != "The instrument" {
		t.Errorf("the appendix is %q numbered %q", last.Title, last.Tag)
	}
}

// A short note with no headings on it at all, which is also what a paper every
// page of which failed the rules comes out as.
func TestAPaperWithNoHeadingsIsOneSection(t *testing.T) {
	// Long enough to be taken for the abstract, because the first paragraph of a
	// paper with no heading over it is the abstract on this path and on the native
	// one alike, and a shorter one would be the byline as far as either can tell.
	long := "A note on a small point, written at enough length to be taken for the abstract of a paper, which is what the first long paragraph of a paper with no headings on it has to be, since there is nothing else on the page saying which of the two it is."
	p := Vision([]string{long + "\n\nand then the rest of the note follows on from it."}, "1710.05832", 1)
	if len(p.Sections) != 1 {
		t.Fatalf("the note came back as %d sections", len(p.Sections))
	}
	if p.Sections[0].Title != "Body" {
		t.Errorf("the one section of a note is called %q", p.Sections[0].Title)
	}
}

// Nothing here escapes what Markdown would read as a marker, because on this path
// the marks were written on purpose. The native path does escape them, and doing it
// here would publish a backslash in front of every emphasis the model wrote.
func TestTheMarkdownTheModelWroteIsLeftAlone(t *testing.T) {
	p := Vision([]string{"# 1 Introduction\n\nthe *emphasis* is the paper's own and the $x$ is mathematics."}, "1710.05832", 1)
	got := p.Sections[0].Blocks[0].Text
	if strings.Contains(got, `\*`) || strings.Contains(got, `\$`) {
		t.Errorf("the marks the model wrote on purpose were escaped: %q", got)
	}
}

// A paper with no pages at all, which is what an empty argument list produces, and
// the one thing that must not panic.
func TestAPaperWithNoPagesIsEmptyAndNotAPanic(t *testing.T) {
	p := Vision(nil, "1710.05832", 1)
	if p == nil {
		t.Fatal("no paper came back at all")
	}
	if p.Pages != "" {
		t.Errorf("a paper with no pages says it was read off %q", p.Pages)
	}
}

// find is the first block of a kind anywhere in the paper, which is how these
// tests ask about one thing without walking the tree in every one of them.
func block(t *testing.T, p *Paper, kind Kind) Block {
	t.Helper()
	var hunt func(blocks []Block) (Block, bool)
	hunt = func(blocks []Block) (Block, bool) {
		for _, b := range blocks {
			if b.Kind == kind {
				return b, true
			}
			if got, ok := hunt(b.Blocks); ok {
				return got, true
			}
		}
		return Block{}, false
	}
	var walk func(sections []Section) (Block, bool)
	walk = func(sections []Section) (Block, bool) {
		for _, s := range sections {
			if got, ok := hunt(s.Blocks); ok {
				return got, true
			}
			if got, ok := walk(s.Sections); ok {
				return got, true
			}
		}
		return Block{}, false
	}
	if got, ok := hunt(p.Abstract); ok {
		return got
	}
	if got, ok := walk(p.Sections); ok {
		return got
	}
	t.Fatalf("the paper holds no %s at all", kind)
	return Block{}
}

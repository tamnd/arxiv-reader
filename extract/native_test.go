package extract

import (
	"slices"
	"strings"
	"testing"
)

// The native path is the one path in this package with no markup to read, so
// every test here is a page of text laid out the way pdftotext -layout lays one
// out and a question about what was recovered from the shape of it.
//
// The pages are written as raw strings with the columns lined up, which makes the
// tests wide and is the only honest way to write them: the thing under test is
// where the characters are on the page, and a fixture built by joining strings
// hides exactly the thing that matters.

// widePage is a page whose table was set across both columns, so it states
// nothing about where its own gutter is.
const widePage = `
A table that was altogether too wide for one column and so the typesetter set it
across both of them, plus a second line of it that also runs the whole distance
across the page, and a third that does the same thing again, and a fourth, which
between them leave no column of this page quiet enough to be taken for a gutter.
Nothing in particular was measured, and nothing in particular came of measuring
it, which is the finding this table reports across the whole width of the sheet.
   Then eight short lines that are                 The paper is what carries a page
in two columns after all, and there             like this one. Every page states the
are not enough of them on their own             gutter it found, the paper takes the
to say where the gutter of this page            middle of those, and a page that
is, because the lines above them put            found none of its own is cut at that
a character in every column of the              instead, which is the one way a page
middle of it. The gutter this page is           of a two column paper gets read in
cut at is the one its paper agreed.             the order it was printed in.
`

// twoColumnPage is a page of a two column paper, gutter and all.
const twoColumnPage = `
A paper about nothing in particular
1   Introduction                                stops being about nothing in
   This paper is about nothing in               particular, which is the point of
particular, which is a thing papers             it.
are allowed to be about, and it is              2   Method
set in two columns with a gutter                   Nothing in particular was done
running down the middle of it. The              to it, and then the whole of it was
rest of it is about nothing much                written up in the manner of a paper
either, until the very end, at which            about nothing in particular at all.
point it briefly                                Theorem 1. Nothing is nothing.
`

func TestAPageIsCutAtItsGutter(t *testing.T) {
	cols := Columns(twoColumnPage)
	if len(cols) != 2 {
		t.Fatalf("the page came back in %d columns", len(cols))
	}
	if strings.Contains(cols[0], "stops being about nothing") {
		t.Errorf("the left column holds the right one:\n%s", cols[0])
	}
	if !strings.Contains(cols[1], "2   Method") {
		t.Errorf("the right column lost its heading:\n%s", cols[1])
	}
}

// The title line of a one column page has a wide left margin and a gap down the
// middle of the page, and neither of those is a gutter.
func TestAPageWithNoGutterIsNotCut(t *testing.T) {
	page := `
                         A paper about nothing in particular

                                  Grace Hopper

   This paper is about nothing in particular, which is a thing papers are
allowed to be about, and it goes on in that manner for some considerable time
without ever once being about anything in particular at all.
`
	if cols := Columns(page); len(cols) != 1 {
		t.Fatalf("a one column page came back in %d columns", len(cols))
	}
}

// The gutter of a paper is the one its pages agree on, which is what carries the
// page whose own gutter is buried under a table that was set across both columns.
func TestThePaperCarriesThePageThatCannotFindItsOwnGutter(t *testing.T) {
	// The page states nothing about where its own gutter is, which is the whole
	// reason it needs carrying.
	if cols := Columns(widePage); len(cols) != 1 {
		t.Fatalf("the page found a gutter of its own after all, in %d columns", len(cols))
	}
	p := Native([]string{twoColumnPage, widePage, twoColumnPage}, "2501.00001", 1)
	if p.Pages != "1-3" {
		t.Errorf("the paper says it was read off pages %q", p.Pages)
	}
	if len(p.Sections) == 0 {
		t.Fatal("nothing was recovered")
	}
	// Cut at the paper's gutter, the two columns of the middle page are read one
	// after the other, so the line that opens the left column and the line that
	// opens the right one are whole sentences and not halves of both.
	var text strings.Builder
	for _, s := range p.Sections {
		for _, b := range s.Blocks {
			text.WriteString(b.Text)
			text.WriteString("\n")
		}
	}
	for _, want := range []string{
		"Then eight short lines that are in two columns after all",
		"The paper is what carries a page like this one.",
	} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("the carried page came back interleaved, with no %q in:\n%s", want, text.String())
		}
	}
}

func TestTheStampSaysWhichVersionThePdfIs(t *testing.T) {
	page := `
arXiv:2501.00001v3 [cs.DL] 5 Jan 2025
                         A paper about nothing in particular

Abstract
   This paper is about nothing in particular, which is a thing papers are
allowed to be about, and it goes on in that manner for some considerable time
without ever once being about anything in particular at all.
`
	p := Native([]string{page}, "2501.00001", 3)
	if p.Stamp != "arXiv:2501.00001v3 [cs.DL] 5 Jan 2025" {
		t.Errorf("the stamp came back as %q", p.Stamp)
	}
	if got := p.StampVersion(); got != 3 {
		t.Errorf("the stamp says v%d", got)
	}
	// The stamp is a line of the page and it is not a line of the paper.
	for _, s := range p.Sections {
		for _, b := range s.Blocks {
			if strings.Contains(b.Text, "arXiv:") {
				t.Errorf("the stamp was published as prose: %q", b.Text)
			}
		}
	}
}

// The abstract ends where the body begins, which on nearly every paper there is
// means where the indent goes back out to the margin. Without that it runs to
// the first heading the paper prints, and a paper whose first heading is on page
// six then has six pages of abstract.
func TestTheAbstractEndsWhereTheBodyBegins(t *testing.T) {
	page := `
                         A paper about nothing in particular

                                  Abstract

       This paper is about nothing in particular, which is a thing papers
    are allowed to be about, and the whole of this indented block is the
    abstract of it and nothing else at all.

This sentence is the first line of the body and it is set at the margin, which
is the only mark on the page that says the abstract has ended, and it runs on
for long enough to be a paragraph of a paper rather than a line of a title.
`
	p := Native([]string{page}, "2501.00001", 1)
	if len(p.Abstract) != 1 {
		t.Fatalf("the abstract came back as %d blocks: %v", len(p.Abstract), p.Abstract)
	}
	if strings.Contains(p.Abstract[0].Text, "first line of the body") {
		t.Errorf("the abstract swallowed the body: %q", p.Abstract[0].Text)
	}
	// The page prints no heading under the abstract, so the prose that follows it
	// is a section with no name, and the name it gets is the one the splitter gives
	// the single file of a paper with no sections.
	if len(p.Sections) != 1 || p.Sections[0].Title != "Body" {
		t.Fatalf("the body was not kept: %v", p.Sections)
	}
	if !strings.Contains(p.Sections[0].Blocks[0].Text, "first line of the body") {
		t.Errorf("the body came back as %q", p.Sections[0].Blocks[0].Text)
	}
}

// The title is a short line with a gap above and below it and it is a heading by
// every test this path has. It is not a section, and the affiliations under it
// are not sections either.
func TestTheTitlePageIsNotASection(t *testing.T) {
	page := `
                         A paper about nothing in particular

                                  Grace Hopper
                        1 Independent Researcher, Logan, Utah, USA
                            2 Enthought, Inc., Austin, TX, USA

                                  Abstract

       This paper is about nothing in particular, which is a thing papers
    are allowed to be about, and the whole of this indented block is the
    abstract of it and nothing else at all.

1   Introduction
   This sentence is the first line of the body and it is set under a heading
that says which section it belongs to, so there is nothing to invent here.
`
	p := Native([]string{page}, "2501.00001", 1)
	for _, s := range p.Sections {
		if strings.Contains(s.Title, "nothing in particular") || strings.Contains(s.Title, "Enthought") {
			t.Errorf("the title page came back as a section: %q", s.Title)
		}
	}
	if len(p.Sections) != 1 {
		t.Fatalf("the paper came back with %d sections: %v", len(p.Sections), p.Sections)
	}
	if s := p.Sections[0]; s.Tag != "1" || s.Title != "Introduction" {
		t.Errorf("the heading came back as %q %q", s.Tag, s.Title)
	}
}

func TestAParagraphBrokenAcrossAColumnIsPutBackTogether(t *testing.T) {
	// The last line of the left column and the first line of the right one are
	// halves of one sentence, and the half in the right column starts in lower
	// case, which is what says so.
	first := `
1   Introduction                                together again by the join, and
   This paper is about nothing in               without it the last line of that
particular, which is a thing papers             column is a single line with a gap
are allowed to be about. The rest of            above it and a gap below it, which
it is about nothing in particular               is the shape of a heading and not
either, until the very end, at which            the shape of the end of one, so a
point it stops.                                 page of a two column paper invents
   This sentence runs off the bottom            a section out of it.
of the left column and on at the top
of the right one, where it is put
`
	p := Native([]string{first}, "2501.00001", 1)
	if len(p.Sections) != 1 {
		t.Fatalf("the paper came back with %d sections: %v", len(p.Sections), p.Sections)
	}
	for _, b := range p.Sections[0].Blocks {
		if strings.HasPrefix(b.Text, "together again") {
			t.Errorf("the tail of the paragraph came back on its own: %q", b.Text)
		}
	}
	joined := false
	for _, b := range p.Sections[0].Blocks {
		if strings.Contains(b.Text, "on at the top of the right one") && strings.Contains(b.Text, "together again by the join") {
			joined = true
		}
	}
	if !joined {
		t.Errorf("the paragraph was not put back together: %v", p.Sections[0].Blocks)
	}
}

// A heading set with space above it and none below is glued to the paragraph
// under it by the gap rule, and a printed number or one of the names papers use
// is enough on its own to cut there.
func TestAHeadingWithNoGapUnderItIsStillAHeading(t *testing.T) {
	page := `
1   Introduction
   This paper is about nothing in particular, which is a thing papers are
allowed to be about, and it goes on in that manner for some considerable time.

Discussion
Nothing in particular turned out to be about nothing in particular after all,
which is the result this paper reports and the whole of what it has to say.
`
	p := Native([]string{page}, "2501.00001", 1)
	var titles []string
	for _, s := range p.Sections {
		titles = append(titles, s.Title)
	}
	if strings.Join(titles, "|") != "Introduction|Discussion" {
		t.Errorf("the sections came back as %v", titles)
	}
}

func TestTheStatementsAndTheCaptionsAreRecovered(t *testing.T) {
	page := `
1   Introduction
   This paper is about nothing in particular, which is a thing papers are
allowed to be about, and it goes on in that manner for some considerable time.
Theorem 1.1. Nothing in particular is nothing in particular, and the proof of
it is the one below.
Proof. Nothing in particular is nothing in particular by inspection.
Figure 2: Nothing in particular, drawn to scale and labelled as such.
Table III. What nothing in particular looks like when it is set as a table.
`
	p := Native([]string{page}, "2501.00001", 1)
	if len(p.Sections) != 1 {
		t.Fatalf("the paper came back with %d sections: %v", len(p.Sections), p.Sections)
	}
	got := map[Kind]string{}
	for _, b := range p.Sections[0].Blocks {
		got[b.Kind] = b.Tag
	}
	for kind, tag := range map[Kind]string{KindTheorem: "1.1", KindProof: "", KindFigure: "2", KindTable: "III"} {
		if _, ok := got[kind]; !ok {
			t.Errorf("no %s was recovered: %v", kind, p.Sections[0].Blocks)
			continue
		}
		if got[kind] != tag {
			t.Errorf("the %s came back tagged %q rather than %q", kind, got[kind], tag)
		}
	}
}

func TestTheReferencesBecomeTheBibliography(t *testing.T) {
	page := `
1   Introduction
   This paper is about nothing in particular, which is a thing papers are
allowed to be about, and it goes on in that manner for some considerable time.

References
[1] G. Hopper, "A paper about nothing in particular," Journal of Nothing in
    Particular, vol. 1, no. 1, pp. 1-2, 1952.
[2] A. Turing, "Another paper about nothing in particular," ibid., 1950.
`
	p := Native([]string{page}, "2501.00001", 1)
	if len(p.Bibliography) != 2 {
		t.Fatalf("the bibliography came back with %d entries: %v", len(p.Bibliography), p.Bibliography)
	}
	if !strings.Contains(p.Bibliography[0].Text(), "Journal of Nothing in Particular") {
		t.Errorf("the first entry reads %q", p.Bibliography[0].Text())
	}
	// A references heading is not a section of the paper's body, so nothing
	// downstream has to work out which of its sections is the reference list.
	for _, s := range p.Sections {
		if strings.EqualFold(s.Title, "references") {
			t.Error("the references came back as a section as well")
		}
	}
}

// A reference list set with no heading over it, which is the house style of a
// whole publisher. The printed numbers are the only thing on the page that says
// where the list begins, and a heading inside it is not the end of it.
func TestAReferenceListWithNoHeadingOverItIsStillOne(t *testing.T) {
	front := `
                    A paper about nothing in particular

                              Grace Hopper

Abstract

   This paper is about nothing in particular, which is a thing papers are
allowed to be about, and it goes on in that manner for some considerable
time without ever once being about anything in particular at all.

1   Introduction
   The first paragraph of the body says nothing either, at some length, in
the manner of a paper with nothing much to say about anything at all.
   The second says as little as the one above it, and with it the front half
of this paper is long enough that what comes after it is the back half, and
the back half is where a paper keeps its references.
`
	back := `
ACKNOWLEDGMENTS
   We thank nobody in particular, which is the manner the whole of this
paper has been conducted in from its first line.

[1] G. Hopper, A paper about nothing in particular, Journal of Nothing in
    Particular 1, 1 (1952).
[2] A. Turing, Another paper about nothing at all, ibid. 2, 1 (1950).

                            Nothing In Particular

[3] B. Pascal, A third paper about nothing, Journal of Nothing 3, 1 (1654).
[4] E. Noether, A fourth one, Journal of Nothing in Particular 4, 1 (1918).

Funding

   Nobody funded any of it.
`
	p := Native([]string{front, back}, "2501.00001", 1)
	if len(p.Bibliography) != 4 {
		t.Fatalf("the bibliography came back with %d entries: %v", len(p.Bibliography), p.Bibliography)
	}
	if !strings.Contains(p.Bibliography[0].Text(), "Journal of Nothing in Particular 1, 1 (1952)") {
		t.Errorf("the first entry reads %q", p.Bibliography[0].Text())
	}
	for _, b := range p.Bibliography {
		if strings.Contains(b.Text(), "We thank nobody") {
			t.Errorf("the acknowledgments came back as a reference: %q", b.Text())
		}
	}
	// The list has an end as well as a beginning, so the heading the paper prints
	// under it is a section and the heading printed inside it is not.
	var titles []string
	for _, s := range p.Sections {
		titles = append(titles, s.Title)
	}
	if !slices.Contains(titles, "Funding") {
		t.Errorf("the section under the list was lost, and the paper came back as %v", titles)
	}
	if slices.Contains(titles, "Nothing In Particular") {
		t.Errorf("a line inside the list came back as a section, and the paper came back as %v", titles)
	}
}

// The gutter of a page is not one column wide on every row of it, and a row read
// at the wrong column is two columns of the paper run into one line.
func TestARowWhoseGutterMovedIsStillCutAtIt(t *testing.T) {
	page := `
   This page is set in two columns                 The right hand column of this
and most of its rows put the right              page starts where the paper says it
hand column in the same place, which            does on every row but one of it, and
is the place the paper is cut at.               on that row it starts a little to
One row of it does not, because         the left of where the cut is.
the typesetter set the first word of            Taken at the cut and not at the gap
it in italic and pdftotext rounded              the row has, that row is read as one
the start of it the other way, so it            line of full width text, and a row
is a few columns over from the rest.            of a reference list read that way is
A line that was set the whole way across the page and has no gap down the middle of it at all.
`
	cols := Columns(page)
	if len(cols) != 2 {
		t.Fatalf("the page came back in %d columns", len(cols))
	}
	if strings.Contains(cols[0], "the left of where the cut is") {
		t.Errorf("the row whose gutter moved was read as full width:\n%s", cols[0])
	}
	if !strings.Contains(cols[1], "the left of where the cut is") {
		t.Errorf("the right hand column lost the row whose gutter moved:\n%s", cols[1])
	}
	// The other half of it. A line set the whole way across the page has word
	// spaces near the cut and no gutter, and cutting it there would file half of a
	// sentence under the column after it.
	if !strings.Contains(cols[0], "across the page and has no gap down the middle of it at all") {
		t.Errorf("a full width line was cut in half:\n%s", cols[0])
	}
}

// The running head of a journal is printed on every page and is not a sentence
// of the paper. A bare page number is the commonest one there is, so the test
// that matters is the one where the repeated line is a single digit.
func TestTheRunningHeadAndThePageNumbersAreNotProse(t *testing.T) {
	var pages []string
	for i := 1; i <= 4; i++ {
		pages = append(pages, `Journal of Nothing in Particular, volume 1
1   Introduction
   This paper is about nothing in particular, which is a thing papers are
allowed to be about, and it goes on in that manner for some considerable time.
`+string(rune('0'+i))+"\n")
	}
	p := Native(pages, "2501.00001", 1)
	for _, s := range p.Sections {
		for _, b := range s.Blocks {
			if strings.Contains(b.Text, "Journal of Nothing in Particular") {
				t.Errorf("the running head came back as prose: %q", b.Text)
			}
			if len(strings.TrimSpace(b.Text)) == 1 {
				t.Errorf("a page number came back as a paragraph: %q", b.Text)
			}
		}
	}
}

// The running head of a two column journal is printed inside the right hand
// column on the pages where the left hand one starts with a heading, and the top
// of the right hand column is the middle of the page's line sequence.
func TestARunningHeadInsideAColumnIsStillFurniture(t *testing.T) {
	var pages []string
	for i := 1; i <= 4; i++ {
		pages = append(pages, "\n"+strings.Repeat(" ", 48)+"Journal of Nothing in Particular, vol. 1\n"+
			strings.TrimPrefix(twoColumnPage, "\n")+strings.Repeat(" ", 48)+string(rune('0'+i))+"\n")
	}
	p := Native(pages, "2501.00001", 1)
	for _, s := range p.Sections {
		for _, b := range s.Blocks {
			if strings.Contains(b.Text, "Journal of Nothing in Particular") {
				t.Errorf("the running head came back as prose: %q", b.Text)
			}
		}
		if strings.Contains(s.Title, "Journal of Nothing in Particular") {
			t.Errorf("the running head came back as a section: %q", s.Title)
		}
	}
}

// A paper with no heading this path could recover is one file, and the splitter
// calls that file the body, so this calls the section the same thing.
func TestAPaperWithNoHeadingIsOneSection(t *testing.T) {
	page := `
                         A paper about nothing in particular

   This paper is about nothing in particular, which is a thing papers are
allowed to be about, and it goes on in that manner for some considerable time
without ever once being about anything in particular at all.
   This second paragraph does the same, at the same length, so that there is
something for the abstract rule to take and something left over for the body.
`
	p := Native([]string{page}, "2501.00001", 1)
	if len(p.Sections) != 1 || p.Sections[0].Title != "Body" {
		t.Fatalf("the paper came back as %v", p.Sections)
	}
}

// The failure this catches is a real one and it is not repaired here. A journal
// PDF built with a Type 1 font carrying its own character map comes back with
// perfect prose and mathematics where every equals sign is a vulgar fraction,
// and the only honest thing to do about it is to say so and send the paper to
// the vision path.
func TestABrokenFontMapIsReported(t *testing.T) {
	page := `
1   Introduction
   This paper is about nothing in particular, which is a thing papers are
allowed to be about, and it goes on in that manner for some considerable time.
   ð1þ where x ¼ y and y ¼ z and z ¼ x, so x ¼ x, which is ð2þ and is also
the whole of ð3þ, and every one of those is an operator this font map lost.
`
	p := Native([]string{page}, "2501.00001", 1)
	if p.Encoding == "" {
		t.Fatal("the broken font map was not reported")
	}
	if !strings.Contains(p.Encoding, "vision path") {
		t.Errorf("the report does not say what to do about it: %q", p.Encoding)
	}
	// It is a report and not a fault. A fault is a rejection, and the prose of
	// this paper is perfectly readable.
	if _, bad := p.Reject(); bad {
		t.Error("a paper with mojibake mathematics was rejected outright")
	}
}

func TestAPaperWithSoundMathematicsIsNotReported(t *testing.T) {
	page := `
1   Introduction
   This paper is about nothing in particular, which is a thing papers are
allowed to be about, and it goes on in that manner for some considerable time,
including the part where x = y and y = z, so that x = z, as one would expect.
`
	if p := Native([]string{page}, "2501.00001", 1); p.Encoding != "" {
		t.Errorf("a sound paper was reported as mojibake: %q", p.Encoding)
	}
}

// Flattened mathematics is published as the shape it was printed in, under the
// one kind that says a renderer must not typeset it.
func TestDisplayedMathematicsIsPublishedAsTheShapeItWasPrintedIn(t *testing.T) {
	page := `
1   Introduction
   This paper is about nothing in particular, which is a thing papers are
allowed to be about, and it goes on in that manner for some considerable time.
                                 n
                              X   1
                                 --- = 2
                              i=0 2^i
`
	p := Native([]string{page}, "2501.00001", 1)
	found := false
	for _, s := range p.Sections {
		for _, b := range s.Blocks {
			if b.Kind == KindNote {
				found = true
				if !strings.Contains(b.Text, "\n") {
					t.Errorf("the lines were joined up: %q", b.Text)
				}
			}
		}
	}
	if !found {
		t.Errorf("the mathematics came back as prose: %v", p.Sections)
	}
}

// Every section says which pages it was read off, which is what audit rules S08
// and S09 measure a file against, and no other path in this package can say it.
func TestEverySectionSaysWhichPagesItWasReadOff(t *testing.T) {
	first := `
                         A paper about nothing in particular

                                  Abstract

       This paper is about nothing in particular, which is a thing papers
    are allowed to be about, and that is the whole of the abstract of it.

1   Introduction
   This sentence is the first line of the body, and it is on page one of the
paper, under a heading that says which section of it this is.
`
	second := `   The second page of the paper carries on where the first one left off, at
the same length, and it is still inside the first section when it does so.

2   Method
   Nothing in particular was done to nothing in particular, at some length,
and this is the paragraph that says so and that sits on the second page.
`
	p := Native([]string{first, second}, "2501.00001", 1)
	if p.Pages != "1-2" {
		t.Errorf("the paper says it was read off pages %q", p.Pages)
	}
	if p.FrontPages != "1" {
		t.Errorf("the abstract says it was read off pages %q", p.FrontPages)
	}
	if len(p.Sections) != 2 {
		t.Fatalf("the paper came back with %d sections: %v", len(p.Sections), p.Sections)
	}
	if got := p.Sections[0].Pages; got != "1-2" {
		t.Errorf("the first section says it was read off pages %q", got)
	}
	if got := p.Sections[1].Pages; got != "2" {
		t.Errorf("the second section says it was read off pages %q", got)
	}
}

// The pages are what the splitter writes into each file's source_pages, and a
// file that named the whole paper's pages would be a file S08 and S09 measured
// against twenty pages when it holds one section off two.
func TestThePagesReachTheFiles(t *testing.T) {
	p := &Paper{
		ID: "2501.00001", Version: 1,
		Pages:      "1-9",
		FrontPages: "1",
		Abstract:   []Block{{Kind: KindParagraph, Text: "An abstract."}},
		Sections: []Section{
			{Level: 1, Title: "One", Pages: "1-4", Blocks: []Block{{Kind: KindParagraph, Text: "Prose."}}},
			{Level: 1, Title: "Two", Pages: "5-9", Blocks: []Block{{Kind: KindParagraph, Text: "More prose."}}},
		},
	}
	files, err := Files(p, Front{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"00_front.md": "1", "01_one.md": "1-4", "02_two.md": "5-9"}
	for _, f := range files {
		if got := f.Doc.Front.SourcePages; got != want[f.Name] {
			t.Errorf("%s says it was read off pages %q rather than %q", f.Name, got, want[f.Name])
		}
	}
}

// A code comment that a flattened listing left at the start of a paragraph is
// prose and not a heading. This is measured on the NumPy paper, where a Python
// comment inside a listing becomes the first characters of a paragraph, and
// before this the corpus published "# random local array" as a heading of its
// own and the audit's T05 was what noticed.
func TestAHashPrintedInsideAListingIsNotAHeading(t *testing.T) {
	page := `
1   Introduction
   This paper is about nothing in particular, which is a thing papers are
allowed to be about, and it goes on in that manner for some considerable time.

# random local array x_distr = da . random . random ([10000 , 10000]) and the
sentence carries on after the comment the way a flattened listing does.
`
	p := Native([]string{page}, "2501.00001", 1)
	found := false
	for _, s := range p.Sections {
		if strings.HasPrefix(s.Title, "#") || strings.Contains(s.Title, "random local array") {
			t.Errorf("a code comment became the section %q", s.Title)
		}
		for _, b := range s.Blocks {
			if strings.Contains(b.Text, "random local array") {
				found = true
				if !strings.HasPrefix(b.Text, `\#`) {
					t.Errorf("the paragraph is written as %q, which Markdown reads as a heading", b.Text)
				}
			}
		}
	}
	if !found {
		t.Error("the comment reached no paragraph at all")
	}
}

package extract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func parse(t *testing.T, name string) *Paper {
	t.Helper()
	p, err := Parse(read(t, name), "2501.00001", 3)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseRefusesSomethingThatIsNotARendering(t *testing.T) {
	if _, err := Parse([]byte("<html><body><p>hello</p></body></html>"), "2501.00001", 3); err == nil {
		t.Fatal("parsed a page with no ltx_document in it")
	}
}

func TestTheFrontMatter(t *testing.T) {
	p := parse(t, "rendering.html")
	if p.ID != "2501.00001" || p.Version != 3 {
		t.Fatalf("got %sv%d, want the id and version the caller passed", p.ID, p.Version)
	}
	if p.Title != "A Paper About Nothing" {
		t.Fatalf("title is %q", p.Title)
	}
	if p.Stamp != "arXiv:2501.00001v3 [cs.LG] 2 February 2025" {
		t.Fatalf("stamp is %q", p.Stamp)
	}
	if p.Licence != "CC BY 4.0" {
		t.Fatalf("licence label is %q", p.Licence)
	}
	if p.Unparsed != 1 {
		t.Fatalf("counted %d unparsed pieces of mathematics, want 1", p.Unparsed)
	}
}

// The version in the stamp is the version arXiv thinks it served, and the whole
// reason to read it is to notice when it is not the version that was asked for.
func TestTheStampNamesAVersion(t *testing.T) {
	cases := map[string]int{
		"arXiv:2501.00001v3 [cs.LG] 2 February 2025": 3,
		"arXiv:2501.00001v12 [math.AG] 1 May 2025":   12,
		"arXiv:2501.00001 [cs.LG] 2 February 2025":   0,
		"":                    0,
		"nothing here at all": 0,
	}
	for stamp, want := range cases {
		if got := (Paper{Stamp: stamp}).StampVersion(); got != want {
			t.Errorf("%q gave v%d, want v%d", stamp, got, want)
		}
	}
}

func TestTheAuthors(t *testing.T) {
	p := parse(t, "rendering.html")
	if len(p.Authors) != 2 {
		t.Fatalf("got %d authors, want 2: %+v", len(p.Authors), p.Authors)
	}
	// The thanks note sits inside the name span, so a reader that took the
	// whole span would call the first author "Ada First Alphabetical by first
	// name."
	if p.Authors[0].Name != "Ada First" {
		t.Fatalf("first author is %q", p.Authors[0].Name)
	}
	if p.Authors[1].Name != "Bo Second" {
		t.Fatalf("second author is %q", p.Authors[1].Name)
	}
	if p.Authors[1].Affiliation != "Department of Nothing, University of Nowhere" {
		t.Fatalf("second author's affiliation is %q, and the Affiliation: label is the rendering's and not the paper's", p.Authors[1].Affiliation)
	}
}

func TestTheAbstract(t *testing.T) {
	p := parse(t, "rendering.html")
	if len(p.Abstract) != 1 {
		t.Fatalf("got %d abstract blocks, want 1: %+v", len(p.Abstract), p.Abstract)
	}
	want := "We say nothing at all, at some length, and we say it in $O(n)$ time."
	if p.Abstract[0].Text != want {
		t.Fatalf("got %q\nwant %q", p.Abstract[0].Text, want)
	}
}

func TestTheSectionTree(t *testing.T) {
	p := parse(t, "rendering.html")
	if len(p.Sections) != 2 {
		t.Fatalf("got %d top level sections, want 2", len(p.Sections))
	}
	one := p.Sections[0]
	if one.ID != "S1" || one.Kind != "section" || one.Level != 1 {
		t.Fatalf("section 1 is %+v", one)
	}
	// A section holds the title of every heading beneath it, so a reader that
	// took the last match rather than the first would call this "A Run-in
	// Heading."
	if one.Title != "Introduction" || one.Tag != "1" {
		t.Fatalf("section 1 is tagged %q and titled %q", one.Tag, one.Title)
	}
	if len(one.Sections) != 1 {
		t.Fatalf("section 1 has %d subsections, want 1", len(one.Sections))
	}
	sub := one.Sections[0]
	if sub.Kind != "subsection" || sub.Level != 2 || sub.Tag != "1.1" || sub.Title != "What Nothing Is" {
		t.Fatalf("subsection is %+v", sub)
	}
	if len(sub.Sections) != 1 {
		t.Fatalf("the subsection has %d children, want the run-in heading", len(sub.Sections))
	}
	run := sub.Sections[0]
	if run.Kind != "paragraph" || run.Level != 4 || run.Title != "A Run-in Heading." {
		t.Fatalf("the run-in heading is %+v", run)
	}
	if p.Headings() != 4 {
		t.Fatalf("counted %d headings, want 4", p.Headings())
	}
}

func TestTheBlockCounts(t *testing.T) {
	p := parse(t, "rendering.html")
	want := map[Kind]int{
		KindParagraph: 6,
		KindEquation:  4,
		KindFigure:    4,
		KindTable:     1,
		KindAlgorithm: 1,
		KindListing:   1,
		KindList:      1,
		KindQuote:     1,
		KindTheorem:   1,
		KindProof:     1,
	}
	got := p.Counts()
	for kind, n := range want {
		if got[kind] != n {
			t.Errorf("counted %d %s blocks, want %d", got[kind], kind, n)
		}
	}
	for kind, n := range got {
		if want[kind] == 0 {
			t.Errorf("counted %d %s blocks, and nothing in the fixture is one", n, kind)
		}
	}
}

func TestAParagraphKeepsItsMarkup(t *testing.T) {
	p := parse(t, "rendering.html")
	got := p.Sections[0].Blocks[0]
	if got.Kind != KindParagraph || got.ID != "S1.p1.1" {
		t.Fatalf("the first block is %+v", got)
	}
	want := "Nothing has been studied before [Nobody (1999)](#bib.bibx1), but never **properly**, and never *at length*. Run `nothing --help` to see."
	if got.Text != want {
		t.Fatalf("got %q\nwant %q", got.Text, want)
	}
}

// A rendering wraps a paragraph however it likes, and one sentence split over
// three lines of HTML is still one sentence.
func TestAParagraphComesBackOnOneLine(t *testing.T) {
	p := parse(t, "rendering.html")
	for _, b := range p.Abstract {
		if strings.Contains(b.Text, "\n") {
			t.Fatalf("the abstract has a newline in the middle of it: %q", b.Text)
		}
	}
}

func TestAnEquationGroupKeepsItsRows(t *testing.T) {
	p := parse(t, "rendering.html")
	sub := p.Sections[0].Sections[0]
	group := sub.Blocks[1]
	if group.Kind != KindEquation || group.ID != "S1.E1" {
		t.Fatalf("the block after the paragraph is %+v", group)
	}
	// The group has no number of its own. Both its rows do, and a cross
	// reference to (1b) points at the row.
	if group.Tag != "" {
		t.Fatalf("the group is tagged %q, and only its rows are numbered", group.Tag)
	}
	if len(group.Blocks) != 2 {
		t.Fatalf("got %d rows, want 2", len(group.Blocks))
	}
	if group.Blocks[0].Tag != "1a" || group.Blocks[1].Tag != "1b" {
		t.Fatalf("the rows are tagged %q and %q", group.Blocks[0].Tag, group.Blocks[1].Tag)
	}
	// The cells are joined back into one display and the \displaystyle LaTeXML
	// put in front of each of them on the way out is dropped, because the
	// author did not write it and the $$ block it lands in is display
	// mathematics already.
	want := `h^{\prime}(t) =\bm{A}h(t)`
	if group.Blocks[0].Text != want {
		t.Fatalf("got %q\nwant %q", group.Blocks[0].Text, want)
	}
}

func TestASingleEquationKeepsItsNumber(t *testing.T) {
	p := parse(t, "rendering.html")
	eq := p.Sections[0].Sections[0].Blocks[2]
	if eq.Kind != KindEquation || eq.ID != "S1.E2" {
		t.Fatalf("got %+v", eq)
	}
	if eq.Tag != "2" {
		t.Fatalf("the equation is tagged %q, and the brackets are the rendering's", eq.Tag)
	}
	if eq.Text != `\overline{\bm{A}}=\exp(\Delta\bm{A})` {
		t.Fatalf("got %q", eq.Text)
	}
	if len(eq.Blocks) != 0 {
		t.Fatalf("one equation came back as %d rows", len(eq.Blocks))
	}
}

func TestAFigureKeepsItsGraphicAndItsCaption(t *testing.T) {
	p := parse(t, "rendering.html")
	fig := p.Sections[0].Blocks[1]
	if fig.Kind != KindFigure || fig.ID != "S1.F1" {
		t.Fatalf("got %+v", fig)
	}
	if fig.Tag != "1" {
		t.Fatalf("the figure is tagged %q, and the word Figure is already its kind", fig.Tag)
	}
	if len(fig.Images) != 1 {
		t.Fatalf("got %d images, want 1", len(fig.Images))
	}
	// An SVG comes as an object with a data attribute rather than an img with
	// a src, and a reader that only knew about img would lose every vector
	// figure in the paper.
	if fig.Images[0].Src != "2501.00001v3/nothing.svg" {
		t.Fatalf("the image is %+v", fig.Images[0])
	}
	if fig.Caption != "(**Overview**.) Nothing, drawn." {
		t.Fatalf("the caption is %q, and the tag belongs in Tag", fig.Caption)
	}
}

func TestAFigureOfPanelsReadsNothingOfItsPanels(t *testing.T) {
	p := parse(t, "rendering.html")
	var outer Block
	for _, b := range p.Sections[1].Blocks {
		if b.ID == "S2.F2" {
			outer = b
		}
	}
	if outer.ID == "" {
		t.Fatal("the figure of panels is not there")
	}
	// The outer figure has no caption, no number and no picture of its own.
	// Every one of those belongs to a panel, and a reader that descended into
	// the panels would give the outer figure all of them.
	if outer.Caption != "" || outer.Tag != "" || len(outer.Images) != 0 || len(outer.Rows) != 0 {
		t.Fatalf("the outer figure took something that belongs to a panel: %+v", outer)
	}
	if len(outer.Blocks) != 2 {
		t.Fatalf("got %d panels, want 2", len(outer.Blocks))
	}
	table, picture := outer.Blocks[0], outer.Blocks[1]
	// A panel that is a table is still numbered as a figure, because that is
	// what the author numbered it as and what every cross reference calls it.
	if table.Kind != KindFigure || table.Tag != "2" || len(table.Rows) != 1 {
		t.Fatalf("the table panel is %+v", table)
	}
	if picture.Tag != "3" || len(picture.Images) != 1 || picture.Images[0].Src != "2501.00001v3/second.png" {
		t.Fatalf("the picture panel is %+v", picture)
	}
	if picture.Images[0].Alt != "Nothing again" {
		t.Fatalf("the alt text is %q", picture.Images[0].Alt)
	}
}

func TestATableKeepsItsCells(t *testing.T) {
	p := parse(t, "rendering.html")
	tbl := p.Sections[1].Blocks[0]
	if tbl.Kind != KindTable || tbl.ID != "S2.T1" || tbl.Tag != "1" {
		t.Fatalf("got %+v", tbl)
	}
	if tbl.Caption != "Nothing, measured." {
		t.Fatalf("the caption is %q", tbl.Caption)
	}
	if len(tbl.Rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(tbl.Rows))
	}
	head := tbl.Rows[0].Cells
	if len(head) != 2 || !head[0].Header || !head[1].Header {
		t.Fatalf("the header row is %+v", head)
	}
	if head[0].Text != "Model" || head[0].Align != "left" || head[0].Span != 1 {
		t.Fatalf("the first heading cell is %+v", head[0])
	}
	if head[1].Span != 2 {
		t.Fatalf("the spanning cell covers %d columns, want 2", head[1].Span)
	}
	body := tbl.Rows[1].Cells
	if len(body) != 3 || body[0].Header {
		t.Fatalf("the body row is %+v", body)
	}
	if body[2].Text != "0.1" || body[2].Align != "right" {
		t.Fatalf("the last cell is %+v", body[2])
	}
}

func TestAnAlgorithmKeepsItsLines(t *testing.T) {
	p := parse(t, "rendering.html")
	var alg Block
	for _, b := range p.Sections[1].Blocks {
		if b.Kind == KindAlgorithm {
			alg = b
		}
	}
	if alg.ID != "alg1" || alg.Tag != "1" || alg.Caption != "Do Nothing" {
		t.Fatalf("got %+v", alg)
	}
	if len(alg.Blocks) != 1 || alg.Blocks[0].Kind != KindListing {
		t.Fatalf("the algorithm holds %+v", alg.Blocks)
	}
	// Code is the one thing in this package that keeps its line breaks, the
	// line numbers are the rendering's and not the author's, and the
	// mathematics is the author's LaTeX and not the MathML beside it.
	want := "$x:\\mathtt{(B,L,D)}$\nreturn nothing"
	if got := alg.Blocks[0].Text; got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestAListKeepsItsItems(t *testing.T) {
	p := parse(t, "rendering.html")
	var list Block
	for _, b := range p.Sections[0].Blocks {
		if b.Kind == KindList {
			list = b
		}
	}
	if list.ID != "S1.I1" || list.Ordered {
		t.Fatalf("got %+v", list)
	}
	if len(list.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(list.Items))
	}
	if list.Items[0] != "**The first thing.** It is nothing." {
		t.Fatalf("the first item is %q, and the bullet is the rendering's", list.Items[0])
	}
}

func TestATheoremKeepsWhatTheAuthorCalledIt(t *testing.T) {
	p := parse(t, "rendering.html")
	var thm, prf Block
	for _, b := range p.Sections[1].Blocks {
		switch b.Kind {
		case KindTheorem:
			thm = b
		case KindProof:
			prf = b
		}
	}
	if thm.Env != "theorem" || thm.Tag != "1" {
		t.Fatalf("the theorem is %+v", thm)
	}
	if len(thm.Blocks) != 1 || thm.Blocks[0].Text != "Nothing is nothing." {
		t.Fatalf("the theorem holds %+v", thm.Blocks)
	}
	if len(prf.Blocks) != 1 || prf.Blocks[0].Text != "Immediate." {
		t.Fatalf("the proof holds %+v", prf.Blocks)
	}
}

func TestAQuoteIsKept(t *testing.T) {
	p := parse(t, "rendering.html")
	for _, b := range p.Sections[1].Blocks {
		if b.Kind == KindQuote {
			if b.Text != "Nothing comes of nothing." {
				t.Fatalf("the quote is %q", b.Text)
			}
			return
		}
	}
	t.Fatal("the quote is not there")
}

// The bibliography is parsed from its own markup by ax refs build, so leaving
// it out of the section tree is not losing it.
func TestTheBibliographyIsNotASection(t *testing.T) {
	p := parse(t, "rendering.html")
	for _, s := range p.Sections {
		if s.ID == "bib" {
			t.Fatal("the bibliography came back as a section")
		}
	}
}

// The typography is read here and the fields are read by refs. What this owes
// that package is the anchor every citation in the paper points at, the label
// the paper prints, and the blocks in the order they were printed.
func TestTheBibliographyIsRead(t *testing.T) {
	p := parse(t, "rendering.html")
	if len(p.Bibliography) != 3 {
		t.Fatalf("got %d entries, want 3: %+v", len(p.Bibliography), p.Bibliography)
	}
	b := p.Bibliography[0]
	if b.ID != "bib.bibx1" {
		t.Errorf("the anchor is %q, and that is what a citation links to", b.ID)
	}
	if b.Label != "Nobody (1999)" {
		t.Errorf("the label is %q", b.Label)
	}
	if b.Text() != "N. Nobody. On nothing. 1999." {
		t.Errorf("the entry reads %q", b.Text())
	}
}

func TestAFaultInTheBodyIsStillAnEntry(t *testing.T) {
	p := parse(t, "rendering.html")
	if len(p.Bibliography) != 3 {
		t.Fatalf("got %d entries, want 3", len(p.Bibliography))
	}
	// The second entry is the one with the undefined macro in it. It is printed
	// badly and it is still a reference the paper made, so it is kept and the
	// fault is recorded separately.
	if b := p.Bibliography[1]; !strings.Contains(b.Text(), "On something") {
		t.Errorf("the entry reads %q", b.Text())
	}
}

func TestAFaultInTheBibliographyIsForgiven(t *testing.T) {
	p := parse(t, "rendering.html")
	if len(p.Faults) != 1 {
		t.Fatalf("got %d faults, want 1: %+v", len(p.Faults), p.Faults)
	}
	if p.Faults[0].Where != "bibliography" {
		t.Fatalf("the fault was found at %q", p.Faults[0].Where)
	}
	if r, bad := p.Reject(); bad {
		t.Fatalf("rejected a paper whose only fault prints one reference badly: %v", r)
	}
}

func TestAFaultInTheBodyIsARejection(t *testing.T) {
	p := parse(t, "faulty.html")
	if len(p.Faults) != 1 {
		t.Fatalf("got %d faults, want 1: %+v", len(p.Faults), p.Faults)
	}
	if p.Faults[0].Where != "S1" {
		t.Fatalf("the fault was found at %q, want the section it is in", p.Faults[0].Where)
	}
	r, bad := p.Reject()
	if !bad {
		t.Fatal("kept a rendering with a piece of the paper missing from it")
	}
	if !strings.Contains(r.Error(), "inside the body") {
		t.Fatalf("the rejection does not say where the fault is: %v", r)
	}
}

func TestTooManyFaultsAnywhereIsARejection(t *testing.T) {
	var p Paper
	for i := 0; i <= FaultLimit; i++ {
		p.Faults = append(p.Faults, Fault{Where: "bibliography", Text: "\\citauthoryear"})
	}
	r, bad := p.Reject()
	if !bad {
		t.Fatalf("kept a rendering with %d faults in it", len(p.Faults))
	}
	if !strings.Contains(r.Error(), "bibliography is forgiven") {
		t.Fatalf("the rejection does not say why the limit is there: %v", r)
	}

	p.Faults = p.Faults[:FaultLimit]
	if _, bad := p.Reject(); bad {
		t.Fatalf("rejected a rendering with exactly the %d faults the limit allows", FaultLimit)
	}
}

func TestAPaperWithNoFaultsIsKept(t *testing.T) {
	if _, bad := (Paper{}).Reject(); bad {
		t.Fatal("rejected a paper with nothing wrong with it")
	}
}

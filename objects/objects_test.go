package objects

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/refs"
)

// paper is the fixture every test in here builds under.
const paper = "2106.09685"

func id(t *testing.T) axid.ID {
	t.Helper()
	got, err := axid.Parse(paper)
	if err != nil {
		t.Fatalf("parsing the fixture id: %v", err)
	}
	return got
}

// front is a content file's front matter with the fields Build reads set and
// everything else left alone.
func front(local, title, kind string) extract.Front {
	return extract.Front{
		Paper: paper, Version: "v2", Lang: "en", Path: "render",
		Kind: kind, LocalID: local, SectionTitle: title,
	}
}

// write puts one content file in the plane.
func write(t *testing.T, root, name string, f extract.Front, body string) {
	t.Helper()
	dir := corpus.ContentDir(root, "en", id(t))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := extract.Document{Front: f, Body: body}.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// build reads the plane back, and fails the test when it cannot.
func build(t *testing.T, root string) Paper {
	t.Helper()
	p, err := Build(root, "en", id(t))
	if err != nil {
		t.Fatalf("building the fixture: %v", err)
	}
	return p
}

// find is one object by its local identifier.
func find(t *testing.T, p Paper, local string) Record {
	t.Helper()
	for _, o := range p.Objects {
		if o.Local == local {
			return o
		}
	}
	t.Fatalf("no object %s in %v", local, locals(p))
	return Record{}
}

func locals(p Paper) []string {
	var out []string
	for _, o := range p.Objects {
		out = append(out, o.Local)
	}
	return out
}

// section is the body of the fixture used by most of these tests, which is one
// file holding the shapes a rendering actually produces.
const section = `Nothing has been studied before [Nobody (1999)](#bib.bibx1), and never *at length*.

**Figure 1** {#fig-1 .figure}

![](nothing.svg)

Nothing, drawn.

## 1.1 What Nothing Is {#s1-1 .section}

Nothing is defined by the following equation.

$$
h^{\prime}(t) =\bm{A}h(t)
$$
{#eq-1a .equation}

### A Run-in Heading. {#su1-1-1 .section}

A paragraph holding $\bm{A}\bm{B}$ and a link to [Figure 1](#fig-1).

**Theorem 1** {#thm-1 .statement env=theorem label="thm:nothing"}

Nothing is nothing.
`

func fixture(t *testing.T) (string, Paper) {
	t.Helper()
	root := t.TempDir()
	write(t, root, "00_front.md", front("front", "Front matter", "front"), "We say nothing at all, in $O(n)$ time.\n")
	write(t, root, "01_introduction.md", front("s1", "Introduction", "section"), section)
	return root, build(t, root)
}

func TestAPaperComesOutInReadingOrder(t *testing.T) {
	_, p := fixture(t)
	want := []string{"front", "s1", "fig-1", "s1-1", "eq-1a", "su1-1-1", "thm-1"}
	got := locals(p)
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
		if p.Objects[i].Order != i {
			t.Fatalf("%s is at order %d and is the %dth object", got[i], p.Objects[i].Order, i)
		}
	}
}

func TestTheFileIsTheFirstObjectInIt(t *testing.T) {
	_, p := fixture(t)
	s1 := find(t, p, "s1")
	if s1.Kind != "section" || s1.Title != "Introduction" {
		t.Fatalf("the file's own object is %q titled %q", s1.Kind, s1.Title)
	}
	if s1.File != "01_introduction.md" {
		t.Fatalf("it says it came from %q", s1.File)
	}
	if want := "Nothing has been studied before [Nobody (1999)](#bib.bibx1), and never *at length*."; s1.BodyMD != want {
		t.Fatalf("its body is %q", s1.BodyMD)
	}
}

func TestTheFrontMatterIsTheOneCertainObject(t *testing.T) {
	_, p := fixture(t)
	f := find(t, p, "front")
	if f.Kind != "front" || f.Confidence != "certain" {
		t.Fatalf("the front matter is %q at %q", f.Kind, f.Confidence)
	}
	if f.Section != "" {
		t.Fatalf("the front matter is inside section %q", f.Section)
	}
	if f.Math == nil || f.Math[0] != "O(n)" {
		t.Fatalf("its mathematics is %v", f.Math)
	}
}

func TestADisplayEquationTakesTheDisplayAboveItsBlock(t *testing.T) {
	_, p := fixture(t)
	eq := find(t, p, "eq-1a")
	if len(eq.Math) != 1 || eq.Math[0] != `h^{\prime}(t) =\bm{A}h(t)` {
		t.Fatalf("the equation's mathematics is %v", eq.Math)
	}
	if want := "$$\nh^{\\prime}(t) =\\bm{A}h(t)\n$$"; eq.BodyMD != want {
		t.Fatalf("the equation's body is %q", eq.BodyMD)
	}
}

func TestTheObjectAboveADisplayDoesNotTakeIt(t *testing.T) {
	_, p := fixture(t)
	s := find(t, p, "s1-1")
	if len(s.Math) != 0 {
		t.Fatalf("the subsection above the display carries %v", s.Math)
	}
	if want := "Nothing is defined by the following equation."; s.BodyMD != want {
		t.Fatalf("the subsection's body is %q", s.BodyMD)
	}
}

func TestASubsectionHeadingGivesItsNumberAndItsTitle(t *testing.T) {
	_, p := fixture(t)
	s := find(t, p, "s1-1")
	if s.Number != "1.1" || s.Title != "What Nothing Is" {
		t.Fatalf("the subsection is number %q titled %q", s.Number, s.Title)
	}
}

func TestAHeadingThatStartsWithAnArticleIsNotNumberedByIt(t *testing.T) {
	_, p := fixture(t)
	s := find(t, p, "su1-1-1")
	if s.Number != "" || s.Title != "A Run-in Heading." {
		t.Fatalf("the heading is number %q titled %q", s.Number, s.Title)
	}
}

func TestARunInHeadingLosesTheWordThatNamesItsKind(t *testing.T) {
	_, p := fixture(t)
	thm := find(t, p, "thm-1")
	if thm.Number != "1" || thm.Title != "" {
		t.Fatalf("the theorem is number %q titled %q", thm.Number, thm.Title)
	}
	fig := find(t, p, "fig-1")
	if fig.Number != "1" {
		t.Fatalf("the figure is number %q", fig.Number)
	}
}

func TestARunInHeadingKeepsWhatFollowsItsNumber(t *testing.T) {
	root := t.TempDir()
	write(t, root, "01.md", front("s1", "Introduction", "section"), "**Lemma 4.2 Nothing at all** {#lem-42 .statement env=lemma}\n\nSo it is.\n")
	p := build(t, root)
	lem := find(t, p, "lem-42")
	if lem.Number != "4.2" || lem.Title != "Nothing at all" {
		t.Fatalf("the lemma is number %q titled %q", lem.Number, lem.Title)
	}
}

func TestALetteredNumberIsKeptOnARunInHeading(t *testing.T) {
	root := t.TempDir()
	write(t, root, "01.md", front("sa", "Appendix A", "appendix"), "**Table B** {#tab-b .table}\n\nNothing, tabulated.\n")
	p := build(t, root)
	if tab := find(t, p, "tab-b"); tab.Number != "B" {
		t.Fatalf("the table is number %q", tab.Number)
	}
}

func TestAnAppendixIsASectionThatSaysItIsAnAppendix(t *testing.T) {
	root := t.TempDir()
	write(t, root, "01.md", front("sa", "Appendix A", "appendix"), "Nothing more.\n")
	p := build(t, root)
	sa := find(t, p, "sa")
	if sa.Kind != "section" || sa.Subkind != "appendix" {
		t.Fatalf("the appendix is %q of subkind %q", sa.Kind, sa.Subkind)
	}
}

func TestATagInAHeadingIsNotPartOfTheTitle(t *testing.T) {
	root := t.TempDir()
	write(t, root, "01.md", front("s1", "Introduction", "section"), "## 03BZ 1.1 What Nothing Is {#s1-1 .section tag=03BZ}\n\nNothing.\n")
	p := build(t, root)
	s := find(t, p, "s1-1")
	if s.Tag != "03BZ" {
		t.Fatalf("the tag on the record is %q", s.Tag)
	}
	if s.Number != "1.1" || s.Title != "What Nothing Is" {
		t.Fatalf("the heading is number %q titled %q", s.Number, s.Title)
	}
}

func TestEverythingUnderASubsectionIsInIt(t *testing.T) {
	_, p := fixture(t)
	for _, c := range []struct{ local, section string }{
		{"fig-1", "s1"},
		{"s1-1", "s1-1"},
		{"eq-1a", "s1-1"},
		{"su1-1-1", "su1-1-1"},
		{"thm-1", "su1-1-1"},
	} {
		if got := find(t, p, c.local).Section; got != c.section {
			t.Errorf("%s is in section %q and should be in %q", c.local, got, c.section)
		}
	}
}

func TestTheEnvironmentIsTheSubkindAndTheLabelIsKept(t *testing.T) {
	_, p := fixture(t)
	thm := find(t, p, "thm-1")
	if thm.Kind != "statement" || thm.Subkind != "theorem" {
		t.Fatalf("the theorem is %q of subkind %q", thm.Kind, thm.Subkind)
	}
	if thm.Label != "thm:nothing" {
		t.Fatalf("its label is %q", thm.Label)
	}
}

func TestALinkInsideThePaperAndACitationAreDifferentEdges(t *testing.T) {
	_, p := fixture(t)
	s1 := find(t, p, "s1")
	if len(s1.Cites) != 1 || s1.Cites[0] != "bib.bibx1" {
		t.Fatalf("the section cites %v", s1.Cites)
	}
	if len(s1.RefsOut) != 0 {
		t.Fatalf("the section refers to %v", s1.RefsOut)
	}
	su := find(t, p, "su1-1-1")
	if len(su.RefsOut) != 1 || su.RefsOut[0] != "fig-1" {
		t.Fatalf("the subsection refers to %v", su.RefsOut)
	}
	if len(su.Cites) != 0 {
		t.Fatalf("the subsection cites %v", su.Cites)
	}
}

func TestTheSameLinkTwiceIsOneEdge(t *testing.T) {
	root := t.TempDir()
	write(t, root, "01.md", front("s1", "Introduction", "section"), "As in [Figure 1](#fig-1), and again in [Figure 1](#fig-1).\n")
	p := build(t, root)
	if got := find(t, p, "s1").RefsOut; len(got) != 1 {
		t.Fatalf("one anchor linked twice came out as %v", got)
	}
}

func TestMathIsDisplayFirstAndThenInlineAndNeverTwice(t *testing.T) {
	root := t.TempDir()
	body := "The bound is $n$, and $n$ again.\n\n$$\nx = 1\n$$\n{#eq-1 .equation}\n"
	write(t, root, "01.md", front("s1", "Introduction", "section"), body)
	p := build(t, root)
	if got := find(t, p, "s1").Math; len(got) != 1 || got[0] != "n" {
		t.Fatalf("the section's mathematics is %v", got)
	}
	if got := find(t, p, "eq-1").Math; len(got) != 1 || got[0] != "x = 1" {
		t.Fatalf("the equation's mathematics is %v", got)
	}
}

func TestAnEquationWithNoDisplayAboveItStillGetsABody(t *testing.T) {
	root := t.TempDir()
	write(t, root, "01.md", front("s1", "Introduction", "section"), "Nothing.\n\nthe bound on n\n{#eq-1 .equation}\n")
	p := build(t, root)
	if got := find(t, p, "eq-1").BodyMD; got != "the bound on n" {
		t.Fatalf("an equation the extractor could not read has body %q", got)
	}
}

func TestAnUnclassifiedBlockKeepsItsAnchorAndSaysItHasNoKind(t *testing.T) {
	root := t.TempDir()
	write(t, root, "01.md", front("s1", "Introduction", "section"), "**Something** {#x-1}\n\nNothing.\n")
	p := build(t, root)
	if got := find(t, p, "x-1"); got.Kind != "" {
		t.Fatalf("a block with no class came out as kind %q", got.Kind)
	}
}

func TestConfidenceFollowsThePathTheFileWasReadOn(t *testing.T) {
	for _, c := range []struct{ path, want string }{
		{"render", "high"},
		{"source", "high"},
		{"native", "medium"},
		{"vision", "medium"},
	} {
		root := t.TempDir()
		f := front("s1", "Introduction", "section")
		f.Path = c.path
		write(t, root, "01.md", f, "**Theorem 1** {#thm-1 .statement}\n\nNothing.\n")
		p := build(t, root)
		if got := find(t, p, "thm-1").Confidence; got != c.want {
			t.Errorf("a theorem read on the %s path is %q and should be %q", c.path, got, c.want)
		}
	}
}

func TestTheBibliographyComesOutOfTheRefsManifest(t *testing.T) {
	root, _ := fixture(t)
	m := refs.Manifest{Paper: paper, Version: 2, Entries: []refs.Entry{
		{ID: "bib.bibx1", Label: "[1]", Title: "Nothing", Text: "Nobody. Nothing. 1999.", Resolved: "1706.03762", Via: "arxiv"},
		{ID: "bib.bibx2", Label: "[2]", Title: "Nothing again", Text: "Somebody. Nothing again. 2001."},
	}}
	if _, err := m.Save(corpus.RefsPath(root, id(t))); err != nil {
		t.Fatal(err)
	}
	p := build(t, root)
	one := find(t, p, "bib.bibx1")
	if one.Kind != "reference" || one.Number != "[1]" || one.Title != "Nothing" {
		t.Fatalf("the first entry is %q number %q titled %q", one.Kind, one.Number, one.Title)
	}
	if one.Resolved != "1706.03762" || one.Via != "arxiv" || one.Confidence != refs.Confidence("arxiv") {
		t.Fatalf("the first entry resolved to %q via %q at %q", one.Resolved, one.Via, one.Confidence)
	}
	if one.File != corpus.RefsPath("", id(t)) {
		t.Fatalf("the first entry says it came from %q", one.File)
	}
	two := find(t, p, "bib.bibx2")
	if two.Resolved != "" || two.Confidence != "high" {
		t.Fatalf("an entry that resolved to nothing is %q at %q", two.Resolved, two.Confidence)
	}
	if one.Order != len(p.Objects)-2 || two.Order != len(p.Objects)-1 {
		t.Fatalf("the bibliography is at orders %d and %d of %d", one.Order, two.Order, len(p.Objects))
	}
}

func TestAPaperWithNoRefsManifestHasNoReferences(t *testing.T) {
	_, p := fixture(t)
	if got := p.Counts()["reference"]; got != 0 {
		t.Fatalf("a paper nobody ran ax refs over has %d references", got)
	}
}

func TestTwoPapersInOneDirectoryIsRefused(t *testing.T) {
	root := t.TempDir()
	write(t, root, "01.md", front("s1", "Introduction", "section"), "Nothing.\n")
	f := front("s2", "Results", "section")
	f.Paper = "1706.03762"
	write(t, root, "02.md", f, "Nothing.\n")
	if _, err := Build(root, "en", id(t)); err == nil {
		t.Fatal("a directory holding two papers was accepted")
	}
}

func TestADirectoryWithNoContentInItIsRefused(t *testing.T) {
	if _, err := Build(t.TempDir(), "en", id(t)); err == nil {
		t.Fatal("a paper that has not been extracted was accepted")
	}
}

func TestCountsSaysHowManyOfEachKind(t *testing.T) {
	_, p := fixture(t)
	want := map[string]int{"front": 1, "section": 3, "figure": 1, "equation": 1, "statement": 1}
	got := p.Counts()
	if len(got) != len(want) {
		t.Fatalf("the counts are %v", got)
	}
	for kind, n := range want {
		if got[kind] != n {
			t.Fatalf("the counts are %v and should be %v", got, want)
		}
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	root, p := fixture(t)
	path := corpus.ObjectsPath(root, id(t))
	if err := p.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Paper != p.Paper || back.Version != p.Version {
		t.Fatalf("it came back as %s %s", back.Paper, back.Version)
	}
	if len(back.Objects) != len(p.Objects) {
		t.Fatalf("%d objects went in and %d came back", len(p.Objects), len(back.Objects))
	}
	for i := range p.Objects {
		if !reflect.DeepEqual(back.Objects[i], empty(p.Objects[i])) {
			t.Fatalf("object %d came back as %+v", i, back.Objects[i])
		}
	}
}

// empty is the record as it survives a round trip through JSON, which writes an
// empty slice as an absent field and reads an absent field back as nil.
func empty(r Record) Record {
	for _, p := range []*[]string{&r.Math, &r.RefsOut, &r.Cites, &r.Concepts} {
		if len(*p) == 0 {
			*p = nil
		}
	}
	return r
}

func TestBuildingTheSamePaperTwiceGivesTheSameBytes(t *testing.T) {
	root, p := fixture(t)
	first, err := p.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	second, err := build(t, root).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("two builds of one paper wrote two different records")
	}
}

func TestEveryKindEmittedIsOneOfTheSixteen(t *testing.T) {
	root, _ := fixture(t)
	m := refs.Manifest{Paper: paper, Version: 2, Entries: []refs.Entry{{ID: "bib.bibx1", Text: "Nobody. Nothing. 1999."}}}
	if _, err := m.Save(corpus.RefsPath(root, id(t))); err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, k := range Kinds {
		known[k] = true
	}
	for _, o := range build(t, root).Objects {
		if !known[o.Kind] {
			t.Errorf("%s came out as kind %q, which is not one of the sixteen", o.Local, o.Kind)
		}
	}
}

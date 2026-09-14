package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/figures"
)

// oneFigure is the entry the fixture's manifest holds, which is a picture with
// nothing whatever wrong with it.
//
// Every test in this file takes it and breaks one field, so the thing under
// test is that field and not the seven others a hand written entry would get
// wrong at the same time.
func oneFigure() figures.Figure {
	return figures.Figure{
		Paper:      fixture,
		Version:    1,
		Number:     "1",
		Source:     "2501.00001v1/one.svg",
		File:       "/figures/2501/2501.00001/one.svg",
		SHA256:     figures.Digest([]byte(drawing)),
		Bytes:      len(drawing),
		Format:     figures.FormatSVG,
		Width:      800,
		Height:     600,
		ThirdParty: figures.Owned,
		Caption:    "Figure 1: a picture of nothing in particular.",
		Licence:    corpus.LicenceCCBY,
	}
}

// refigure rewrites the manifest with the entries given.
func refigure(t *testing.T, root string, entries ...figures.Figure) {
	t.Helper()
	if err := (figures.Manifest{Figures: entries}).Save(manifestPath(root)); err != nil {
		t.Fatal(err)
	}
}

// recorded puts the fixture's manifest entry back with one field changed.
func recorded(t *testing.T, root string, edit func(*figures.Figure)) {
	t.Helper()
	f := oneFigure()
	edit(&f)
	refigure(t, root, f)
}

// shown is the fixture with one more image line in its last section.
func shown(t *testing.T, src string) string {
	return paper(t, front(), section(1, "One", prose(8)+"\n\n![Something]("+src+")"))
}

func TestF01ReportsAPictureStillServedOffArXiv(t *testing.T) {
	f := fires(t, shown(t, "2501.00001v1/two.svg"), "F01")
	if !strings.Contains(f.What, "where the picture sits on arXiv") {
		t.Errorf("the finding reads %q", f.What)
	}
}

func TestF01ReportsAFigureThatIsNotThere(t *testing.T) {
	f := fires(t, shown(t, "/figures/2501/2501.00001/two.svg"), "F01")
	if !strings.Contains(f.What, "there is no such file") {
		t.Errorf("the finding reads %q", f.What)
	}
}

// The manifest is the record and the bytes are the copy, so an entry saying a
// picture was committed with nothing behind it is the same hole read from the
// other side.
func TestF01ReportsAnEntryWithNoBytesBehindIt(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	if err := os.Remove(filepath.Join(root, "figures", "2501", "2501.00001", "one.svg")); err != nil {
		t.Fatal(err)
	}
	// Twice over, once from the body and once from the manifest, because the
	// hole is in the corpus and both sides of it are worth naming.
	fires(t, root, "F01")
	var what []string
	for _, f := range audited(t, root)["F01"].Findings {
		what = append(what, f.What)
	}
	if !strings.Contains(strings.Join(what, "\n"), "records 2501.00001v1/one.svg as committed") {
		t.Errorf("the findings read %q", what)
	}
}

func TestF02ReportsAPictureTooSmallToBeAFigure(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	recorded(t, root, func(f *figures.Figure) { f.Width, f.Height = 20, 20 })
	if f := fires(t, root, "F02"); !strings.Contains(f.What, "20 by 20 pixels") {
		t.Errorf("the finding reads %q", f.What)
	}
}

func TestF03ReportsAFigureOverTheCap(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	recorded(t, root, func(f *figures.Figure) { f.Bytes = figures.SizeCap + 1 })
	if f := fires(t, root, "F03"); !strings.Contains(f.What, "the cap is 500 KB") {
		t.Errorf("the finding reads %q", f.What)
	}
}

func TestF05ReportsOnePictureCommittedTwice(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	second := oneFigure()
	second.Source = "2501.00001v1/two.svg"
	second.File = "/figures/2501/2501.00001/two.svg"
	second.Number = "2"
	if err := os.WriteFile(filepath.Join(root, "figures", "2501", "2501.00001", "two.svg"), []byte(drawing), 0o644); err != nil {
		t.Fatal(err)
	}
	refigure(t, root, oneFigure(), second)
	if f := fires(t, root, "F05"); !strings.Contains(f.What, "the same bytes twice") {
		t.Errorf("the finding reads %q", f.What)
	}
}

func TestF06ReportsAPictureTheSizeOfAPage(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	recorded(t, root, func(f *figures.Figure) { f.PageFrom, f.PageFraction = "svg", 0.9 })
	if f := fires(t, root, "F06"); !strings.Contains(f.What, "90% of a page") {
		t.Errorf("the finding reads %q", f.What)
	}
}

// A physical size cannot be guessed from a pixel count, so a file that states
// none is not judged by this rule at all.
func TestF06LeavesAFigureThatStatesNoResolution(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	recorded(t, root, func(f *figures.Figure) { f.PageFraction = 0.9 })
	if got := audited(t, root)["F06"]; got.Total != 0 {
		t.Errorf("F06 found %v", got.Findings)
	}
}

func TestF07ReportsAFigureNobodyCanIdentify(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	recorded(t, root, func(f *figures.Figure) { f.Caption = "" })
	if f := fires(t, root, "F07"); !strings.Contains(f.What, "with no caption, which rule F07 forbids") {
		t.Errorf("the finding reads %q", f.What)
	}
}

// A paper that prints one caption over four panels gives three of them no
// caption of their own, and Mamba prints eight that way. The float above them
// says what they are, so a picture with no number is not one nobody can
// identify.
func TestF07LeavesAPanelOfAFloat(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	recorded(t, root, func(f *figures.Figure) { f.Number, f.Caption = "", "" })
	if got := audited(t, root)["F07"]; got.Total != 0 {
		t.Errorf("F07 found %v", got.Findings)
	}
}

// The manifest is read by this rule, the way the register is read by G01, so a
// file that does not load is one finding here rather than six across the group.
func TestF07ReportsAManifestThatDoesNotLoad(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	if err := os.WriteFile(manifestPath(root), []byte("figures:\n  - paper: \"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if f := fires(t, root, "F07"); !strings.Contains(f.What, "names no paper") {
		t.Errorf("the finding reads %q", f.What)
	}
}

// A record paper is one the corpus may hold the metadata and the structure of
// and nothing else, so a figure of one in the corpus is a leak of the thing the
// gate exists to stop.
func TestF08ReportsAFigureOfAPaperTheCorpusMayNotPublish(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	rewrite(t, root, "00_front.md", func(d *extract.Document) {
		d.Front.Access = string(corpus.AccessRecord)
		d.Front.LicenceOfSource = string(corpus.LicenceArXiv)
	})
	if f := fires(t, root, "F08"); !strings.Contains(f.What, "is a record paper") {
		t.Errorf("the finding reads %q", f.What)
	}
}

func TestF09ReportsSomebodyElsesFigureInTheCorpus(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	recorded(t, root, func(f *figures.Figure) {
		f.ThirdParty, f.Why = figures.Suspected, "the caption credits Smith et al."
	})
	if f := fires(t, root, "F09"); !strings.Contains(f.What, "suspected of being somebody else's") {
		t.Errorf("the finding reads %q", f.What)
	}
}

func TestF10ReportsAFigureThePaperSendsAReaderTo(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)+"\n\nThe result is in Figure 4, which says it all."))
	if f := fires(t, root, "F10"); !strings.Contains(f.What, "sends a reader to Figure 4") {
		t.Errorf("the finding reads %q", f.What)
	}
}

// The number the paper prints is what the reader is told to look for, and a
// panel is found through its identifier, because a paper that prints Figure 6
// over two pictures refers to them as 6a and 6b.
func TestF10LeavesAFigureThePaperHas(t *testing.T) {
	for _, body := range []string{
		"Figure 1 says it all.",
		"Figure 1a says half of it.",
	} {
		root := paper(t, front(), section(1, "One", prose(8)+"\n\n"+body+
			"\n\n**Figure** {#fig-1a .figure}\n\n![A panel](/figures/2501/2501.00001/one.svg)"))
		if got := audited(t, root)["F10"]; got.Total != 0 {
			t.Errorf("%q gives %v", body, got.Findings)
		}
	}
}

// The commonest way for a table not to exist twice is for it to exist no times
// at all, which is a paper ax tables has never been run over.
func TestF11ReportsATableThatIsNotKeptAtAll(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	if err := os.RemoveAll(filepath.Join(root, "tables")); err != nil {
		t.Fatal(err)
	}
	if f := fires(t, root, "F11"); !strings.Contains(f.What, "lays out 1 table in its sections and keeps 0 tables") {
		t.Errorf("the finding reads %q", f.What)
	}
}

func TestF11ReportsHalfAPair(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	if err := os.Remove(filepath.Join(root, "tables", "2501", "2501.00001", "t01.tex")); err != nil {
		t.Fatal(err)
	}
	if f := fires(t, root, "F11"); !strings.Contains(f.What, "has no t01.tex") {
		t.Errorf("the finding reads %q", f.What)
	}
}

// The numbers are what makes keeping a table twice worth the disk.
func TestF12ReportsANumberThatDiffers(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	name := filepath.Join(root, "tables", "2501", "2501.00001", "t01.tex")
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(strings.Replace(string(b), "0.0", "0.1", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if f := fires(t, root, "F12"); !strings.Contains(f.What, "0.0 in the Markdown and 0.1 in the markup") {
		t.Errorf("the finding reads %q", f.What)
	}
}

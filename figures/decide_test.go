package figures

import (
	"strings"
	"testing"
)

// F06. A picture that covers a whole page is a page, and the rule is what keeps
// this a corpus rather than a mirror of copyrighted PDFs.
func TestAFigureThatIsAWholePageIsRefused(t *testing.T) {
	// 1275 by 1650 at 150 dots per inch is 8.5 by 11 inches, which is a letter
	// page and is the shape a scan of one arrives in.
	page, err := Read(png(1275, 1650, dpi(150), dpi(150), 1))
	if err != nil {
		t.Fatal(err)
	}
	if f := page.PageFraction(); f < 0.99 {
		t.Fatalf("a letter page measured %.3f of a page", f)
	}
	d := Decide("Figure 1: our architecture.", page)
	if d.Commit {
		t.Fatal("a whole page was committed")
	}
	if d.Rule != "F06" {
		t.Fatalf("refused by %s, want F06: %s", d.Rule, d.Why)
	}
}

// An A4 page is three per cent larger than a letter page, so the fraction has
// to be clamped or the rule reports a hundred and three per cent of a page.
func TestAnA4PageMeasuresAsAWholePage(t *testing.T) {
	a4, err := Read(png(2480, 3508, dpi(300), dpi(300), 1))
	if err != nil {
		t.Fatal(err)
	}
	if f := a4.PageFraction(); f != 1 {
		t.Fatalf("an A4 page measured %.3f of a page, want 1", f)
	}
}

// The ordinary case, and the one that has to keep working. A four by three inch
// plot is about a sixth of a page and nothing is wrong with it.
func TestAnOrdinaryFigureIsCommitted(t *testing.T) {
	im, err := Read(png(1200, 900, dpi(300), dpi(300), 1))
	if err != nil {
		t.Fatal(err)
	}
	d := Decide("Figure 2: accuracy against sequence length.", im)
	if !d.Commit {
		t.Fatalf("refused by %s: %s", d.Rule, d.Why)
	}
	if f := d.PageFraction; f < 0.12 || f > 0.13 {
		t.Errorf("a four by three inch figure measured %.3f of a page", f)
	}
}

// A figure with no stated resolution is not judged by F06 at all, and a tall
// multi panel figure is exactly the picture a guessed resolution would throw
// away.
func TestAFigureWithNoStatedResolutionIsNotJudgedByF06(t *testing.T) {
	im, err := Read(png(1600, 2200, 0, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	d := Decide("Figure 3: every panel of the ablation.", im)
	if !d.Commit {
		t.Fatalf("refused by %s: %s", d.Rule, d.Why)
	}
	if d.PageFraction != 0 {
		t.Errorf("page fraction is %v, want 0", d.PageFraction)
	}
}

// F09. The four words are ordinary English, so the citation beside them is what
// turns one into a claim about who drew the picture.
func TestACaptionThatCreditsSomebodyElseWithholdsTheFigure(t *testing.T) {
	for name, caption := range map[string]string{
		"reproduced": "Figure 4: the benchmark, reproduced from [Smith et al., 2019](#bib.bibx12).",
		"adapted":    "Adapted from [12](#bib.bibx12), with our results added.",
		"reprinted":  "Reprinted with permission from [Lee and Park](#ref-lee2020).",
		"courtesy":   "Image courtesy of [the Hubble archive](#bib.bibx3).",
	} {
		t.Run(name, func(t *testing.T) {
			d := Decide(caption, fine(t))
			if d.Commit {
				t.Fatal("a figure credited to somebody else was committed")
			}
			if d.Rule != "F09" || d.Suspicion != Suspected {
				t.Fatalf("refused by %s as %s: %s", d.Rule, d.Suspicion, d.Why)
			}
		})
	}
}

// A caption read off a submission has the citations the author typed, because
// nothing has converted it yet, and that is the caption every TikZ drawing
// arrives with. A rule that only knew the link form would say owned about all of
// them.
func TestTheAuthorsOwnCiteIsACitation(t *testing.T) {
	for name, caption := range map[string]string{
		"cite":            `The benchmark, reproduced from \cite{smith2019}.`,
		"citep":           `Adapted from \citep{smith2019}, with our results added.`,
		"citet with page": `Reprinted with permission from \citet[p. 4]{lee2020}.`,
		"nocite":          `Image courtesy of the archive \nocite{hubble}.`,
	} {
		t.Run(name, func(t *testing.T) {
			d := Decide(caption, fine(t))
			if d.Commit {
				t.Fatal("a drawing credited to somebody else in the author's own TeX was committed")
			}
			if d.Rule != "F09" || d.Suspicion != Suspected {
				t.Fatalf("refused by %s as %s: %s", d.Rule, d.Suspicion, d.Why)
			}
		})
	}
}

// The words on their own are how people write about their own work, and
// withholding on the word alone would take a picture out of every paper that
// used it.
func TestTheSameWordsAwayFromACitationAreOrdinaryEnglish(t *testing.T) {
	for name, caption := range map[string]string{
		"adapted architecture": "Figure 5: our adapted architecture, with the gate in place.",
		"reproduced runs":      "Figure 6: accuracy reproduced across five seeds.",
		"far from the cite":    "Figure 7: an adapted layout. The numbers are ours and the task is the standard one described at length in the section above, see [Smith et al., 2019](#bib.bibx12).",
		"far from a tex cite":  `Figure 8: our adapted layout. The numbers are ours and the task is the standard one described at length in the section above, see \cite{smith2019}.`,
	} {
		t.Run(name, func(t *testing.T) {
			d := Decide(caption, fine(t))
			if !d.Commit {
				t.Fatalf("refused by %s: %s", d.Rule, d.Why)
			}
		})
	}
}

func TestACaptionThatClaimsACopyrightWithholdsTheFigure(t *testing.T) {
	for name, caption := range map[string]string{
		"the symbol": "Figure 8: the telescope. © 2019 the observatory.",
		"the word":   "Figure 9: used by permission, copyright the publisher.",
		"the ascii":  "Figure 10: the schematic, (c) 2016 the vendor.",
	} {
		t.Run(name, func(t *testing.T) {
			d := Decide(caption, fine(t))
			if d.Commit || d.Rule != "F09" {
				t.Fatalf("committed, or refused by %s: %s", d.Rule, d.Why)
			}
		})
	}
}

// The licence rule runs before the rest, because a figure the corpus is not
// entitled to should be refused for that reason and not for being large.
func TestTheLicenceRuleIsReportedFirst(t *testing.T) {
	page, err := Read(png(1275, 1650, dpi(150), dpi(150), 1))
	if err != nil {
		t.Fatal(err)
	}
	d := Decide("Figure 11: reproduced from [Smith et al., 2019](#bib.bibx12).", page)
	if d.Rule != "F09" {
		t.Fatalf("refused by %s, want F09", d.Rule)
	}
}

// F02. A twenty pixel picture is a rule, a spacer or a piece of a formula
// LaTeXML could not set.
func TestATinyPictureIsNotAFigure(t *testing.T) {
	im, err := Read(png(20, 8, 0, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	d := Decide("", im)
	if d.Commit || d.Rule != "F02" {
		t.Fatalf("committed, or refused by %s: %s", d.Rule, d.Why)
	}
	if !strings.Contains(d.Why, "20 by 8") {
		t.Errorf("the reason does not say how big it is: %s", d.Why)
	}
}

func TestAFigureOverTheSizeCapIsRefused(t *testing.T) {
	im := Image{Format: FormatPNG, Width: 1200, Height: 900, Bytes: SizeCap + 1}
	d := Decide("", im)
	if d.Commit || d.Rule != "F03" {
		t.Fatalf("committed, or refused by %s: %s", d.Rule, d.Why)
	}
	if !strings.Contains(d.Why, "KB") {
		t.Errorf("the reason does not say the size: %s", d.Why)
	}
}

// F09 confirmed. The same bytes under two licences is the only signal that
// finds a figure lifted without a caption admitting it.
func TestTheSameBytesUnderADifferentLicenceIsConfirmed(t *testing.T) {
	seen := map[string]Owner{"abc": {Paper: "2106.09685", Licence: "cc-by-nc-nd"}}
	s, why := Elsewhere("abc", "2312.00752", "cc-by", seen)
	if s != Confirmed {
		t.Fatalf("got %s, want confirmed", s)
	}
	if !strings.Contains(why, "2106.09685") {
		t.Errorf("the reason does not name the other paper: %s", why)
	}
}

// Across three million papers the same institutional logo and the same standard
// schematic appear in thousands of documents, so a byte match is evidence of
// nothing until the two papers disagree about the licence.
func TestTheSameBytesUnderTheSameLicenceIsNotAFinding(t *testing.T) {
	seen := map[string]Owner{"abc": {Paper: "2106.09685", Licence: "cc-by"}}
	if s, _ := Elsewhere("abc", "2312.00752", "cc-by", seen); s != Owned {
		t.Errorf("got %s, want owned", s)
	}
	// And a figure matching itself is the same figure.
	if s, _ := Elsewhere("abc", "2106.09685", "cc-by-nc-nd", seen); s != Owned {
		t.Errorf("a paper was accused of copying itself, got %s", s)
	}
	if s, _ := Elsewhere("nothing seen", "2312.00752", "cc-by", seen); s != Owned {
		t.Errorf("got %s for a hash nobody has seen, want owned", s)
	}
}

func fine(t *testing.T) Image {
	t.Helper()
	im, err := Read(png(1200, 900, dpi(300), dpi(300), 1))
	if err != nil {
		t.Fatal(err)
	}
	return im
}

package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/selection"
)

func entry(id string, p selection.Path, at selection.Status) selection.Entry {
	return selection.Entry{ID: id, Path: p, Status: at}
}

func build(es ...selection.Entry) Paths {
	return BuildPaths(selection.Manifest{Selected: es}, "2026-09-15")
}

func row(t *testing.T, p Paths, want selection.Path) PathRow {
	t.Helper()
	for _, r := range p.Rows {
		if r.Path == want {
			return r
		}
	}
	t.Fatalf("there is no row for %q in %+v", want, p.Rows)
	return PathRow{}
}

func TestBuildCountsEachPath(t *testing.T) {
	p := build(
		entry("2501.00001", selection.PathRender, selection.StatusExtracted),
		entry("2501.00002", selection.PathRender, selection.StatusTagged),
		entry("2501.00003", selection.PathVision, selection.StatusSelected),
	)
	if p.Papers != 3 {
		t.Fatalf("three entries are three papers, got %d", p.Papers)
	}
	if got := row(t, p, selection.PathRender); got.Papers != 2 {
		t.Fatalf("two papers are on the render path, got %d", got.Papers)
	}
	if got := row(t, p, selection.PathNative); got.Papers != 0 {
		t.Fatalf("a path with nothing on it is still a row, and it says nought, got %d", got.Papers)
	}
}

func TestBuildKeepsThePathsInPreferenceOrder(t *testing.T) {
	// The order is the order of cost, and the report is about cost, so the rows
	// are in the order the paths are preferred in and never in count order.
	p := build(entry("2501.00001", selection.PathVision, selection.StatusSelected))
	for i, want := range selection.Paths {
		if p.Rows[i].Path != want {
			t.Fatalf("row %d is %q and should be %q", i, p.Rows[i].Path, want)
		}
	}
}

func TestBuildCountsRungsAsReachedAndNotAsStoppedAt(t *testing.T) {
	p := build(
		entry("2501.00001", selection.PathRender, selection.StatusExtracted),
		entry("2501.00002", selection.PathRender, selection.StatusTagged),
	)
	r := row(t, p, selection.PathRender)
	if r.Reached[selection.StatusSelected] != 2 {
		t.Fatalf("both papers have been selected, got %d", r.Reached[selection.StatusSelected])
	}
	if r.Reached[selection.StatusExtracted] != 2 {
		t.Fatalf("a tagged paper has been extracted, got %d", r.Reached[selection.StatusExtracted])
	}
	if r.Reached[selection.StatusTagged] != 1 {
		t.Fatalf("one of the two is tagged, got %d", r.Reached[selection.StatusTagged])
	}
}

func TestFurthestIsTheRungThePathHasAllReached(t *testing.T) {
	p := build(
		entry("2501.00001", selection.PathRender, selection.StatusExtracted),
		entry("2501.00002", selection.PathRender, selection.StatusTagged),
	)
	if got := row(t, p, selection.PathRender).Furthest(); got != selection.StatusExtracted {
		t.Fatalf("one paper of two being tagged does not make the path tagged, got %q", got)
	}
}

func TestFurthestOfAnEmptyPathIsNowhere(t *testing.T) {
	p := build(entry("2501.00001", selection.PathRender, selection.StatusTagged))
	if got := row(t, p, selection.PathNative).Furthest(); got != "" {
		t.Fatalf("a path with no papers on it has got nowhere, got %q", got)
	}
}

func TestUndecidedPapersGetTheirOwnRow(t *testing.T) {
	p := build(
		entry("2501.00001", selection.PathRender, selection.StatusExtracted),
		entry("2501.00002", "", selection.StatusSelected),
	)
	if got := row(t, p, ""); got.Papers != 1 {
		t.Fatalf("one paper has no path, got %d", got.Papers)
	}
}

func TestAPathNobodyKnowsCountsAsUndecided(t *testing.T) {
	// A field somebody hand edited into the manifest is not a fifth path, and
	// silently growing a row for it would hide the mistake rather than show it.
	p := build(entry("2501.00001", "ocr", selection.StatusSelected))
	if got := row(t, p, ""); got.Papers != 1 {
		t.Fatalf("an unknown path is undecided, got %d", got.Papers)
	}
}

func TestTheUndecidedRowIsGoneWhenThereIsNothingOnIt(t *testing.T) {
	p := build(entry("2501.00001", selection.PathRender, selection.StatusSelected))
	for _, r := range p.Rows {
		if r.Path == "" {
			t.Fatal("a corpus where every paper has a path should not carry a row of noughts saying so")
		}
	}
}

func TestSharesAddUp(t *testing.T) {
	p := build(
		entry("2501.00001", selection.PathRender, selection.StatusSelected),
		entry("2501.00002", selection.PathRender, selection.StatusSelected),
		entry("2501.00003", selection.PathSource, selection.StatusSelected),
		entry("2501.00004", selection.PathVision, selection.StatusSelected),
	)
	if got := row(t, p, selection.PathRender).Share; got != 0.5 {
		t.Fatalf("two of four is a half, got %v", got)
	}
	if got := row(t, p, selection.PathVision).Share; got != 0.25 {
		t.Fatalf("one of four is a quarter, got %v", got)
	}
}

func TestDemotionsAreCountedBySentenceAndNotByPath(t *testing.T) {
	// Both of these are on the source path and only one of them was ever on the
	// render path, which is the whole distinction this number exists to make.
	demoted := entry("2501.00001", selection.PathSource, selection.StatusExtracted)
	demoted.PathWhy = selection.DemotedWhy
	asked := entry("2501.00002", selection.PathSource, selection.StatusExtracted)
	asked.PathWhy = "the submission holds TeX and arXiv serves no rendering of this version, so the TeX is compiled here"
	p := build(demoted, asked)
	if p.Demoted != 1 {
		t.Fatalf("one of the two was demoted, got %d", p.Demoted)
	}
}

func TestMonthsAreInOrderAndSplitByPath(t *testing.T) {
	p := build(
		entry("2501.00001", selection.PathRender, selection.StatusSelected),
		entry("2412.00002", selection.PathVision, selection.StatusSelected),
		entry("2412.00003", selection.PathRender, selection.StatusSelected),
	)
	if len(p.Months) != 2 {
		t.Fatalf("two months, got %d", len(p.Months))
	}
	if p.Months[0].Month != "2412" || p.Months[1].Month != "2501" {
		t.Fatalf("the months are not in order: %+v", p.Months)
	}
	if got := p.Months[0].Counts[selection.PathRender]; got != 1 {
		t.Fatalf("one render paper in 2412, got %d", got)
	}
	if p.Months[0].Papers != 2 {
		t.Fatalf("two papers in 2412, got %d", p.Months[0].Papers)
	}
}

func TestMonthOfAnOldStyleID(t *testing.T) {
	if got := monthOf("math/0211159"); got != "0211" {
		t.Fatalf("an old style id carries its month after the slash, got %q", got)
	}
	if got := monthOf("2501.00001"); got != "2501" {
		t.Fatalf("got %q", got)
	}
	if got := monthOf("nope"); got != "unknown" {
		t.Fatalf("an id this report cannot read is not a reason to stop counting, got %q", got)
	}
}

func TestBlockersAreRolledUpMostPapersFirst(t *testing.T) {
	one := entry("2501.00001", "", selection.StatusSelected)
	one.PathWhy = "nobody has asked arXiv whether it renders this version"
	two := entry("2501.00002", "", selection.StatusSelected)
	two.PathWhy = one.PathWhy
	three := entry("2501.00003", "", selection.StatusSelected)
	three.PathWhy = "nobody has looked at what the submission holds"
	p := build(one, two, three)
	if len(p.Undecided) != 2 {
		t.Fatalf("three papers behind two blockers are two rows, got %d", len(p.Undecided))
	}
	if p.Undecided[0].Papers != 2 {
		t.Fatalf("the blocker in front of the most papers comes first, got %+v", p.Undecided)
	}
}

func TestMarkdownSaysWhatTheCorpusIs(t *testing.T) {
	demoted := entry("2501.00001", selection.PathSource, selection.StatusExtracted)
	demoted.PathWhy = selection.DemotedWhy
	p := build(
		entry("2501.00002", selection.PathRender, selection.StatusTagged),
		demoted,
	)
	md := p.Markdown()
	for _, want := range []string{
		"# How the selection divides",
		"2026-09-15",
		"| render | 1 | 50.0% |",
		"1 paper on the source path is there because",
		"Every paper in the selection has a path.",
		"2501",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the report does not say %q:\n%s", want, md)
		}
	}
}

func TestMarkdownSaysSoWhenNothingWasDemoted(t *testing.T) {
	p := build(entry("2501.00001", selection.PathRender, selection.StatusSelected))
	if !strings.Contains(p.Markdown(), "No paper in the selection is on the source path because a rendering was rejected.") {
		t.Fatalf("a corpus with no demotions should say so rather than print nothing:\n%s", p.Markdown())
	}
}

func TestMarkdownNamesThePapersNobodyHasDecidedAbout(t *testing.T) {
	p := build(entry("2501.00001", "", selection.StatusSelected))
	md := p.Markdown()
	if !strings.Contains(md, "ax path decide has not been run over this paper") {
		t.Fatalf("an empty reason is itself a reason, and the report should say which:\n%s", md)
	}
}

func TestTextIsTheSameNumbersWithoutTheTableMarkup(t *testing.T) {
	p := build(
		entry("2501.00001", selection.PathRender, selection.StatusExtracted),
		entry("2501.00002", selection.PathVision, selection.StatusSelected),
	)
	txt := p.Text()
	if strings.Contains(txt, "|") {
		t.Fatalf("the terminal output carries table markup:\n%s", txt)
	}
	for _, want := range []string{"render", "50.0%", "extracted", "vision", "papers\t2", "months\t1"} {
		if !strings.Contains(txt, want) {
			t.Errorf("the terminal output does not say %q:\n%s", want, txt)
		}
	}
}

func TestExpectedCoversEveryPath(t *testing.T) {
	for _, p := range selection.Paths {
		if Expected(p) == "" {
			t.Errorf("%s has no column saying what it is for", p)
		}
	}
	if Expected("") == "" {
		t.Error("the undecided row has no column either")
	}
}

func TestSaveMakesTheDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reports", "paths.md")
	if err := Save(path, "hello\n"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\n" {
		t.Fatalf("got %q", got)
	}
}

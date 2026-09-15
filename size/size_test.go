package size

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// write puts n bytes at a path under root, making the directories on the way.
func write(t *testing.T, root, at string, n int) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(at))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

// corpus is a small checkout with two years of content in it.
func corpus(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, "content/en/2106/2106.09685/00-front.md", 4000)
	write(t, root, "content/en/2106/2106.09685/01-introduction.md", 6000)
	write(t, root, "content/en/2106/2106.09686/00-front.md", 2000)
	write(t, root, "content/en/1706/1706.03762/00-front.md", 1000)
	write(t, root, "figures/2106/2106.09685/fig-01.png", 8000)
	write(t, root, "manifests/selected.yaml", 500)
	return root
}

func measure(t *testing.T, root string) Corpus {
	t.Helper()
	c, err := Measure(root)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func plane(t *testing.T, c Corpus, name string) Plane {
	t.Helper()
	for _, p := range c.Planes {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("there is no row for the %s plane in %+v", name, c.Planes)
	return Plane{}
}

func TestMeasureAddsUpEveryCommittedPlane(t *testing.T) {
	c := measure(t, corpus(t))
	if c.Checkout != 21500 {
		t.Fatalf("the checkout is 4000+6000+2000+1000+8000+500, got %d", c.Checkout)
	}
	if got := plane(t, c, "content"); got.Bytes != 13000 || got.Files != 4 {
		t.Fatalf("the content plane is 13000 bytes in four files, got %+v", got)
	}
}

func TestAPlaneWithNothingInItIsNotARow(t *testing.T) {
	// A corpus that has not been tagged yet should not print a tags row of
	// noughts, because the list of rows is what this checkout is made of.
	c := measure(t, corpus(t))
	for _, p := range c.Planes {
		if p.Name == "tags" || p.Name == "graph" {
			t.Fatalf("a plane with no files in it got a row: %+v", p)
		}
	}
}

func TestThePlanesAreLargestFirst(t *testing.T) {
	c := measure(t, corpus(t))
	for i := 1; i < len(c.Planes); i++ {
		if c.Planes[i-1].Bytes < c.Planes[i].Bytes {
			t.Fatalf("the planes are not in size order: %+v", c.Planes)
		}
	}
	if c.Planes[0].Name != "content" {
		t.Fatalf("content is the largest plane here, got %q", c.Planes[0].Name)
	}
}

func TestMeasureOfANonExistentRootIsAnEmptyCorpus(t *testing.T) {
	// A root that is not there is the ordinary case of somebody running this
	// outside a corpus, and it is a corpus of nothing rather than an error.
	c := measure(t, filepath.Join(t.TempDir(), "nowhere"))
	if c.Checkout != 0 || len(c.Planes) != 0 || c.Papers != 0 {
		t.Fatalf("an empty root weighs nothing, got %+v", c)
	}
}

func TestWorkIsNotWeighed(t *testing.T) {
	// work/ holds arXiv's own bytes, is gitignored, and a fresh checkout does
	// not carry it, so counting it would put the trigger years too early.
	root := corpus(t)
	write(t, root, "work/2106/2106.09685/paper.pdf", 5<<20)
	if got := measure(t, root).Checkout; got != 21500 {
		t.Fatalf("work/ was counted into the checkout, got %d", got)
	}
}

func TestTheObjectsAreCountedApartFromTheCheckout(t *testing.T) {
	root := corpus(t)
	write(t, root, ".git/objects/pack/pack-1.pack", 9000)
	write(t, root, ".git/index", 1<<20)
	c := measure(t, root)
	if c.Objects != 9000 {
		t.Fatalf("the objects are the pack and not the index, got %d", c.Objects)
	}
	if c.Checkout != 21500 {
		t.Fatalf("the history is not part of the checkout, got %d", c.Checkout)
	}
}

func TestTheYearsAreTheShardsRolledUp(t *testing.T) {
	c := measure(t, corpus(t))
	if len(c.Years) != 2 {
		t.Fatalf("two years, got %+v", c.Years)
	}
	if c.Years[0].Year != "2021" || c.Years[0].Bytes != 12000 || c.Years[0].Papers != 2 {
		t.Fatalf("2021 is two papers and 12000 bytes, got %+v", c.Years[0])
	}
	if c.Years[1].Year != "2017" || c.Years[1].Papers != 1 {
		t.Fatalf("2017 is one paper, got %+v", c.Years[1])
	}
}

func TestAYearCountsAPaperOnceHoweverManyLanguagesHoldIt(t *testing.T) {
	// A split moves a year and takes every translation of it, so the year row
	// carries every language of bytes and the paper count stays a paper count.
	root := corpus(t)
	write(t, root, "content/vi/2106/2106.09685/00-front.md", 5000)
	c := measure(t, root)
	if c.Years[0].Year != "2021" || c.Years[0].Bytes != 17000 {
		t.Fatalf("the Vietnamese bytes belong to 2021 too, got %+v", c.Years[0])
	}
	if c.Years[0].Papers != 2 {
		t.Fatalf("translating a paper does not make it two papers, got %d", c.Years[0].Papers)
	}
	if c.Papers != 3 {
		t.Fatalf("the corpus holds three papers in its fullest language, got %d", c.Papers)
	}
}

func TestTheFirstYearToMoveIsTheLargest(t *testing.T) {
	c := measure(t, corpus(t))
	first, ok := c.First()
	if !ok || first.Year != "2021" {
		t.Fatalf("2021 is the largest year here, got %+v", first)
	}
}

func TestFirstOfAnEmptyContentPlaneSaysSo(t *testing.T) {
	if _, ok := measure(t, t.TempDir()).First(); ok {
		t.Fatal("a corpus with no content has no year to split off")
	}
}

func TestYearOf(t *testing.T) {
	for _, tc := range []struct{ shard, want string }{
		{"2106", "2021"},
		{"0704", "2007"},
		{"9107", "1991"},
		{"9912", "1999"},
		{"9012", "2090"},
		{"21", "unknown"},
		{"", "unknown"},
		{"nope", "unknown"},
		{"21a6", "unknown"},
	} {
		if got := YearOf(tc.shard); got != tc.want {
			t.Errorf("YearOf(%q) is %q, want %q", tc.shard, got, tc.want)
		}
	}
}

func TestTheCloneIsTheObjectsOverTheStatedRate(t *testing.T) {
	c := Corpus{Objects: 10 << 20}
	if got := c.Clone(); got != time.Second {
		t.Fatalf("ten megabytes at ten megabytes a second is one second, got %s", got)
	}
}

func TestNeitherHalfOfTheTriggerIsReachedByASmallCorpus(t *testing.T) {
	c := measure(t, corpus(t))
	if tripped, half := c.Tripped(); tripped {
		t.Fatalf("a corpus of 21 KB has reached %s", half)
	}
}

func TestTheCheckoutHalfOfTheTriggerTrips(t *testing.T) {
	c := Corpus{Checkout: Trigger}
	tripped, half := c.Tripped()
	if !tripped || half != "the checkout" {
		t.Fatalf("20 GB is the trigger, got %v %q", tripped, half)
	}
}

func TestTheCloneHalfTripsOnItsOwn(t *testing.T) {
	// This is the half the spec put in for a reason: a repository of small
	// files with a long history clones slowly while the checkout stays small.
	c := Corpus{Checkout: 1 << 20, Objects: int64(Rate) * int64(CloneTrigger/time.Second)}
	tripped, half := c.Tripped()
	if !tripped || half != "the clone" {
		t.Fatalf("fifteen minutes at the stated rate is the trigger, got %v %q", tripped, half)
	}
}

func TestBothHalvesAtOnceSaysBoth(t *testing.T) {
	c := Corpus{Checkout: Trigger + 1, Objects: Trigger}
	if _, half := c.Tripped(); half != "both halves of the trigger" {
		t.Fatalf("got %q", half)
	}
}

func TestTheNearerHalfIsWhicheverIsFurtherAlong(t *testing.T) {
	// Whichever comes first is the rule, so the number to watch is the larger
	// share and not the one that is easier to measure.
	c := Corpus{Checkout: Trigger / 10, Objects: int64(Rate) * 450}
	half, share := c.Nearest()
	if half != "the clone" {
		t.Fatalf("half the clone trigger beats a tenth of the checkout, got %q at %v", half, share)
	}
	if share != 0.5 {
		t.Fatalf("got %v", share)
	}
}

func TestHeadroomIsWhatIsLeftAtTheCurrentCostPerPaper(t *testing.T) {
	c := Corpus{Checkout: Trigger / 2, Scaling: Trigger / 2, Papers: 1000}
	if got := c.PerPaper(); got != Trigger/2/1000 {
		t.Fatalf("got %d", got)
	}
	if got := c.Headroom(); got != 1000 {
		t.Fatalf("half the trigger spent on a thousand papers leaves room for a thousand, got %d", got)
	}
}

func TestTheCostPerPaperLeavesOutThePlanesThatDoNotGrowWithTheSelection(t *testing.T) {
	// The metadata plane is every paper arXiv has whether it was selected or
	// not. Charging it to the ten papers in the content plane would say each of
	// them costs a gigabyte and would put the trigger a thousand times too near.
	root := corpus(t)
	write(t, root, "metadata/2106.jsonl", 1<<20)
	write(t, root, "reports/paths.md", 4000)
	c := measure(t, root)
	if c.Checkout != 21500+(1<<20)+4000 {
		t.Fatalf("the fixed planes are still part of the checkout, got %d", c.Checkout)
	}
	if c.Scaling != 21500 {
		t.Fatalf("the fixed planes are not part of what grows, got %d", c.Scaling)
	}
	if got := c.PerPaper(); got != 21500/3 {
		t.Fatalf("three papers share the scaling planes, got %d", got)
	}
	if !strings.Contains(c.Text(), "leaves out the 1.0 MB of metadata and reports") {
		t.Fatalf("the output should say what it left out of the cost per paper:\n%s", c.Text())
	}
}

func TestHeadroomCountsTheFixedPlanesAgainstTheTriggerEvenSo(t *testing.T) {
	// They are not charged to a paper and they are still on the disk, so the
	// room left is the trigger less the whole checkout and not less the part
	// of it that grows.
	c := Corpus{Checkout: 3 * (Trigger / 4), Scaling: Trigger / 2, Papers: 1000}
	if got := c.Headroom(); got != 500 {
		t.Fatalf("a quarter of the trigger left at a two thousandth each is five hundred papers, got %d", got)
	}
}

func TestHeadroomOfAnEmptyContentPlaneIsNought(t *testing.T) {
	// Not a very large number. A corpus with no papers in it has no cost per
	// paper, and dividing by it would print a projection out of nothing.
	c := Corpus{Checkout: 1 << 20, Scaling: 1 << 20}
	if got := c.Headroom(); got != 0 {
		t.Fatalf("got %d", got)
	}
}

func TestHeadroomPastTheTriggerIsNought(t *testing.T) {
	c := Corpus{Checkout: Trigger + (1 << 20), Scaling: Trigger, Papers: 10}
	if got := c.Headroom(); got != 0 {
		t.Fatalf("a corpus over the trigger has no room left, got %d", got)
	}
}

func TestTextSaysWhereTheCorpusStands(t *testing.T) {
	c := measure(t, corpus(t))
	txt := c.Text()
	for _, want := range []string{
		"content",
		"13 KB",
		"checkout",
		"of the 20.00 GB trigger",
		"The checkout is the nearer half",
		"neither half has been reached",
		"2021",
		"Splitting 2021 off first",
		"3 papers in the content plane",
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("the output does not say %q:\n%s", want, txt)
		}
	}
}

func TestTextSaysSoWhenTheTriggerIsReached(t *testing.T) {
	c := Corpus{Checkout: Trigger + 1, Papers: 10}
	if !strings.Contains(c.Text(), "The checkout has reached the trigger, so the content plane splits by year now.") {
		t.Fatalf("a corpus over the trigger should say so plainly:\n%s", c.Text())
	}
}

func TestTextOfAnEmptyCorpusSaysThereIsNothingToProjectFrom(t *testing.T) {
	txt := measure(t, t.TempDir()).Text()
	for _, want := range []string{
		"no plane of this corpus has a file in it",
		"there is no cost per paper to project from yet",
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("the output does not say %q:\n%s", want, txt)
		}
	}
	if strings.Contains(txt, "Splitting") {
		t.Errorf("an empty corpus has no year to split off:\n%s", txt)
	}
}

func TestTextCarriesNoTableMarkup(t *testing.T) {
	// This one is read in a terminal and never committed, so it is columns and
	// not a Markdown table like the reports are.
	if txt := measure(t, corpus(t)).Text(); strings.Contains(txt, "|") {
		t.Fatalf("the output carries table markup:\n%s", txt)
	}
}

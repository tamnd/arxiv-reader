package tags

import (
	"strings"
	"testing"
	"time"
)

// revised is a census of one paper with the revisions given, which is the shape
// every test here starts from.
func revised(rs ...Revision) Census {
	return Census{
		Generated: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC),
		Papers: []PaperTags{{
			ID: "2305.18290", Version: 3, Live: 58, Runs: 2,
			Cached: []int{1, 2, 3}, Revisions: rs,
		}},
	}
}

// The number the whole report exists for, taken over every revision in the
// corpus rather than per paper, because a paper with four objects and a paper
// with four hundred are not two readings that should weigh the same.
func TestTheRateIsEveryObjectOverEveryRevision(t *testing.T) {
	c := revised(
		Revision{From: 1, To: 2, Was: 10, Now: 10, Label: 8, Sequence: 1, Added: 1},
		Revision{From: 2, To: 3, Was: 10, Now: 90, Label: 89, Added: 1},
	)
	if c.Matched() != 100 {
		t.Errorf("the denominator is %d and it is the new version's objects, so 100", c.Matched())
	}
	if c.Fell() != 3 {
		t.Errorf("%d fell through, and pass four plus the unmatched is 3", c.Fell())
	}
	if got := c.Rate(); got != 0.03 {
		t.Errorf("the rate is %v and not 0.03, so the two revisions were averaged rather than added", got)
	}
}

// A corpus nobody has compared anything in has no rate, and printing 0.0% for it
// would say every name was read off when nothing has looked.
func TestACorpusWithNothingComparedHasNoRate(t *testing.T) {
	c := revised()
	if got := c.Rate(); got != -1 {
		t.Errorf("the rate is %v and a rate with no denominator is -1", got)
	}
	md := c.Markdown()
	if !strings.Contains(md, "Nothing has been compared across a revision yet") {
		t.Errorf("the report reads:\n%s", md)
	}
	if strings.Contains(md, "0.0%") {
		t.Error("the report prints a rate for a corpus that has compared nothing")
	}
	if !strings.Contains(c.Text(), "nothing was compared") {
		t.Errorf("the summary reads %q", c.Text())
	}
}

// The same line ax tags diff refuses to write on, read over the whole corpus so
// somebody sees it coming rather than meeting it in a run.
func TestARevisionOverTheFifthIsCalledOut(t *testing.T) {
	c := revised(
		Revision{From: 1, To: 2, Was: 10, Now: 10, Label: 9, Added: 1},
		Revision{From: 2, To: 3, Was: 10, Now: 10, Label: 6, Sequence: 3, Added: 1},
	)
	over := c.Over()
	if len(over) != 1 || over[0].From != 2 {
		t.Fatalf("over the line: %+v", over)
	}
	md := c.Markdown()
	if !strings.Contains(md, "1 revision a run would refuse to write") {
		t.Errorf("the report reads:\n%s", md)
	}
	if !strings.Contains(md, "| 2305.18290 | v2 to v3 | 10 | 40.0% |") {
		t.Errorf("the table of revisions over the line reads:\n%s", md)
	}
}

// A revision at exactly the threshold is written, because 03-tags.md says more
// than a fifth and the gate and the report have to draw the line in the same
// place.
func TestARevisionAtTheThresholdIsNotCalledOut(t *testing.T) {
	c := revised(Revision{From: 1, To: 2, Was: 10, Now: 10, Label: 8, Sequence: 2})
	if len(c.Over()) != 0 {
		t.Errorf("a fifth exactly is over the line here and it is not over it in Diff.Err")
	}
	d := Diff{Total: 10}
	for i := 0; i < 2; i++ {
		d.Pairs = append(d.Pairs, Pair{Pass: BySequence})
	}
	for i := 0; i < 8; i++ {
		d.Pairs = append(d.Pairs, Pair{Pass: ByLabel})
	}
	if d.Err() != nil {
		t.Errorf("the gate refuses a fifth and the report does not, so the two disagree: %v", d.Err())
	}
}

// Revise is the only place the report reads a Diff, so it is the only place the
// two could come to different counts.
func TestReviseCountsTheSamePassesTheDiffPrints(t *testing.T) {
	d := Match(
		[]Object{
			obj("thm-1", "statement", "thm:main", "One."),
			obj("thm-2", "statement", "", "Two."),
			obj("eq-7", "equation", "", "$$\na = b\n$$"),
		},
		[]Object{
			obj("thm-3", "statement", "thm:main", "One."),
			obj("thm-4", "statement", "", "Two."),
			obj("fig-1", "figure", "", "A picture."),
		},
	)
	r := Revise(2, 3, 3, d)
	if r.From != 2 || r.To != 3 || r.Was != 3 || r.Now != 3 {
		t.Errorf("the revision came out %+v", r)
	}
	if r.Label != d.Count(ByLabel) || r.Number != d.Count(ByNumber) || r.Content != d.Count(ByContent) || r.Sequence != d.Count(BySequence) {
		t.Errorf("the revision %+v does not count the diff's passes", r)
	}
	if r.Added != len(d.Added) || r.Gone != len(d.Gone) {
		t.Errorf("the revision has %d new and %d gone against the diff's %d and %d", r.Added, r.Gone, len(d.Added), len(d.Gone))
	}
	if r.Fell() != d.Fell() {
		t.Errorf("the revision fell %v and the diff fell %v, and they are the same number", r.Fell(), d.Fell())
	}
}

// The by paper table is what says why a paper with three versions has two
// revisions in it, or none.
func TestTheByPaperTableSaysWhichVersionsAreCached(t *testing.T) {
	c := revised(Revision{From: 1, To: 2, Was: 58, Now: 58, Label: 58})
	md := c.Markdown()
	if !strings.Contains(md, "| 2305.18290 | v3 | 58 | 58 | 0 | 2 | v1 v2 v3 |") {
		t.Errorf("the by paper table reads:\n%s", md)
	}
}

// A register written before the version line existed does not carry one, and a
// report that printed v0 for it would be inventing a version of the paper.
func TestARegisterWithNoVersionSaysSo(t *testing.T) {
	c := revised(Revision{From: 1, To: 2, Was: 4, Now: 4, Label: 4})
	c.Papers[0].Version = 0
	if !strings.Contains(c.Markdown(), "| 2305.18290 | not said |") {
		t.Errorf("the by paper table reads:\n%s", c.Markdown())
	}
}

// Tombstones are tags and they are not live ones, and the headline counts both
// because a tombstone is still a promise the corpus has made.
func TestTombstonesAreCountedAndNamed(t *testing.T) {
	c := revised()
	c.Papers[0].Buried = 2
	if c.Tags() != 60 || c.Buried() != 2 {
		t.Errorf("%d tags of which %d buried, and it is 60 of which 2", c.Tags(), c.Buried())
	}
	if !strings.Contains(c.Markdown(), "60 tags over 1 paper, 2 of them tombstones.") {
		t.Errorf("the headline reads:\n%s", c.Markdown())
	}
}

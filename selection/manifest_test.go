package selection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// chosen is one good entry, which the tests break one field at a time.
func chosen() Entry {
	return Entry{
		ID: "2106.09685", Version: 2, Reason: Requested, Issue: 214, By: "tamnd",
		Added: "2026-10-02", Status: StatusSelected, Languages: []string{"en"},
	}
}

func TestSaveAndLoadComeBackTheSame(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifests", "selected.yaml")
	m := Manifest{}
	m.Put(chosen())
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := back.Find("2106.09685")
	if !ok {
		t.Fatal("the paper that was just written is not in the file")
	}
	if e.Reason != Requested || e.Issue != 214 || e.By != "tamnd" || e.Ref() != "2106.09685v2" {
		t.Errorf("the entry came back as %+v", e)
	}
}

// A file that is not there is an empty selection, because a corpus with nothing
// chosen yet is an ordinary state and not a broken one.
func TestLoadOfNothingIsEmpty(t *testing.T) {
	m, err := Load(filepath.Join(t.TempDir(), "selected.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Selected) != 0 {
		t.Errorf("a selection nobody wrote holds %d papers", len(m.Selected))
	}
}

// Every reason demands the evidence that makes it checkable. This is the whole value
// of the manifest: a reason on its own is a word, and cited-by-corpus with no count
// could be two papers.
func TestEveryReasonDemandsItsEvidence(t *testing.T) {
	for _, c := range []struct {
		what  string
		entry Entry
		ok    bool
	}{
		{"seed needs nothing else", Entry{ID: "1", Version: 1, Reason: Seed, Status: StatusSelected}, true},
		{"cited by three is the floor", Entry{ID: "1", Version: 1, Reason: CitedByCorpus, Cited: 3, Status: StatusSelected}, true},
		{"cited by two is not the reason", Entry{ID: "1", Version: 1, Reason: CitedByCorpus, Cited: 2, Status: StatusSelected}, false},
		{"cited by nobody is not the reason", Entry{ID: "1", Version: 1, Reason: CitesCorpus, Status: StatusSelected}, false},
		{"canon needs a category and a rank", Entry{ID: "1", Version: 1, Reason: CategoryCanon, Category: "cs.LG", Rank: 12, Status: StatusSelected}, true},
		{"canon with no rank is a claim about nothing", Entry{ID: "1", Version: 1, Reason: CategoryCanon, Category: "cs.LG", Status: StatusSelected}, false},
		{"a request needs an issue and a name", Entry{ID: "1", Version: 1, Reason: Requested, Issue: 214, By: "tamnd", Status: StatusSelected}, true},
		{"a request with no issue cannot be found again", Entry{ID: "1", Version: 1, Reason: Requested, By: "tamnd", Status: StatusSelected}, false},
		{"a request with no name is the corpus growing by itself", Entry{ID: "1", Version: 1, Reason: Requested, Issue: 214, Status: StatusSelected}, false},
		{"a collection needs the list", Entry{ID: "1", Version: 1, Reason: Collection, Collection: "foundations", Status: StatusSelected}, true},
		{"a collection with no list names nothing", Entry{ID: "1", Version: 1, Reason: Collection, Status: StatusSelected}, false},
	} {
		err := c.entry.Check()
		if c.ok && err != nil {
			t.Errorf("%s: %v", c.what, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: was accepted", c.what)
		}
	}
}

func TestAnEntryWithNoVersionOrNoReasonIsRefused(t *testing.T) {
	for _, c := range []struct {
		what  string
		entry Entry
	}{
		{"no paper", Entry{Version: 1, Reason: Seed, Status: StatusSelected}},
		{"no version", Entry{ID: "2106.09685", Reason: Seed, Status: StatusSelected}},
		{"no reason", Entry{ID: "2106.09685", Version: 1, Status: StatusSelected}},
		{"a seventh reason", Entry{ID: "2106.09685", Version: 1, Reason: "i-like-it", Status: StatusSelected}},
		{"no status", Entry{ID: "2106.09685", Version: 1, Reason: Seed}},
		{"a date that is not a date", Entry{ID: "2106.09685", Version: 1, Reason: Seed, Status: StatusSelected, Added: "last tuesday"}},
	} {
		if err := c.entry.Check(); err == nil {
			t.Errorf("an entry with %s was accepted", c.what)
		}
	}
}

// The file says do not edit by hand, and this is what backs that up: an entry
// somebody typed in with a word and no evidence stops the next read rather than
// sitting there looking selected.
func TestAHandWrittenEntryWithNoEvidenceStopsTheRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "selected.yaml")
	body := "selected:\n  - id: 2106.09685\n    version: 2\n    reason: requested\n    status: selected\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("an entry claiming a request with no issue and no name was read in")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("the refusal does not say which file it is in: %v", err)
	}
}

// Explain is what ax select explain prints, and it carries this entry's own numbers
// rather than the general sentence, because the general sentence is true of every
// paper under the reason.
func TestExplainPutsTheEvidenceInTheSentence(t *testing.T) {
	for _, c := range []struct {
		entry Entry
		want  string
	}{
		{Entry{Reason: CitedByCorpus, Cited: 7}, "cited by 7 papers"},
		{Entry{Reason: CitesCorpus, Cited: 9}, "cites 9 papers"},
		{Entry{Reason: CategoryCanon, Category: "cs.LG", Rank: 12, Year: 2021}, "ranked 12 by citation count in cs.LG for 2021"},
		{Entry{Reason: Requested, Issue: 214, By: "tamnd"}, "tamnd asked for it in issue 214"},
		{Entry{Reason: Collection, Collection: "foundations"}, "the reading list foundations"},
		{Entry{Reason: Seed}, "seed list"},
	} {
		if got := c.entry.Explain(); !strings.Contains(got, c.want) {
			t.Errorf("%s explained itself as %q, want %q in it", c.entry.Reason, got, c.want)
		}
	}
}

// One entry per paper and not one per version, because the content plane holds the
// one version the licence gate decided may be published. Choosing a second version
// replaces the decision rather than making two.
func TestPutReplacesByPaperAndNotByVersion(t *testing.T) {
	m := Manifest{}
	m.Put(chosen())
	second := chosen()
	second.Version = 3
	second.Reason = Seed
	m.Put(second)
	if len(m.Selected) != 1 {
		t.Fatalf("one paper at two versions made %d entries", len(m.Selected))
	}
	if e, _ := m.Find("2106.09685"); e.Version != 3 || e.Reason != Seed {
		t.Errorf("the entry is %+v", e)
	}
}

func TestDropTakesAPaperOut(t *testing.T) {
	m := Manifest{}
	m.Put(chosen())
	if !m.Drop("2106.09685") {
		t.Fatal("dropping a paper that is there said it was not")
	}
	if len(m.Selected) != 0 {
		t.Errorf("the paper is still there")
	}
	if m.Drop("2106.09685") {
		t.Error("dropping a paper twice said it worked twice")
	}
}

// Every reason is in the count, including the ones with nothing under them, because
// a reason that never fires is a fact about the selection and not an absence.
func TestTheCountsHoldEveryReasonEvenTheEmptyOnes(t *testing.T) {
	m := Manifest{}
	m.Put(chosen())
	counts := m.ByReason()
	if len(counts) != len(Reasons) {
		t.Errorf("the counts hold %d reasons and there are %d", len(counts), len(Reasons))
	}
	if counts[Requested] != 1 || counts[Seed] != 0 {
		t.Errorf("the counts are %v", counts)
	}
	states := m.ByStatus()
	if states[StatusSelected] != 1 || len(states) != len(Statuses) {
		t.Errorf("the statuses are %v", states)
	}
}

func TestSaveSortsSoARerunHasNoDiff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selected.yaml")
	m := Manifest{}
	for _, id := range []string{"2310.06825", "1706.03762", "2106.09685"} {
		m.Put(Entry{ID: id, Version: 1, Reason: Seed, Added: "2026-10-02", Status: StatusSelected})
	}
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := back.Save(path); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Error("writing the same selection twice produced two different files")
	}
	if got := string(first); !strings.Contains(got, "id: \"1706.03762\"\n") && !strings.Contains(got, "id: 1706.03762\n") {
		t.Errorf("the oldest paper is not first:\n%s", got)
	}
	if !strings.Contains(string(first), "# Every paper in the content plane") {
		t.Error("the file has no header saying what it is and who writes it")
	}
}

// Save checks on the way out as well as Load on the way in, because the caller that
// built a bad entry is the one that should hear about it.
func TestSaveRefusesAnEntryItCannotCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selected.yaml")
	m := Manifest{Selected: []Entry{{ID: "2106.09685", Version: 1, Reason: Requested, Status: StatusSelected}}}
	if err := m.Save(path); err == nil {
		t.Fatal("a request with no issue and no name was written out")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("the file was written anyway")
	}
}

func TestLanguagesKeepTheirOrderAndDropRepeats(t *testing.T) {
	got := Languages([]string{"en", "vi", "en", "", " ja "})
	want := []string{"en", "vi", "ja"}
	if len(got) != len(want) {
		t.Fatalf("the languages came back as %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("the languages came back as %v", got)
		}
	}
}

func TestTodayIsADate(t *testing.T) {
	if !day.MatchString(Today()) {
		t.Errorf("today is %q, and a date is YYYY-MM-DD", Today())
	}
}

package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/refs"
)

// oneEntry is the bibliography entry the fixture cites, which is a reference
// with nothing whatever wrong with it.
//
// Every test in this file takes it and breaks one field, so the thing under
// test is that field and not the six others a hand written entry would get
// wrong at the same time.
func oneEntry() refs.Entry {
	return refs.Entry{
		ID:       "bib.bibx1",
		Label:    "[1]",
		Authors:  []string{"Ada Lovelace"},
		Title:    "A note on nothing in particular",
		Venue:    "Proceedings of Nothing",
		Year:     2024,
		ArXiv:    "2401.01234",
		Resolved: "2401.01234",
		Via:      refs.ViaArXiv,
		Text:     "Ada Lovelace. A note on nothing in particular. Proceedings of Nothing, 2024. arXiv:2401.01234.",
	}
}

// oneRecord is the paper that entry resolves to, as the metadata plane holds
// it.
func oneRecord() metadata.Record {
	return metadata.Record{
		ID:         "2401.01234",
		Title:      "A note on nothing in particular",
		Abstract:   "There is nothing in particular to say about it.",
		Authors:    []metadata.Author{{Surname: "Lovelace", Forename: "Ada"}},
		Categories: []string{"cs.DL"},
		Versions: []metadata.Version{{
			Version: 1,
			Created: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		}},
		Source:    metadata.SourceKaggle,
		Harvested: "2026-01-01",
	}
}

// entered puts the fixture's bibliography back with one field changed.
func entered(t *testing.T, root string, edit func(*refs.Entry)) {
	t.Helper()
	e := oneEntry()
	edit(&e)
	bibbed(t, root, e)
}

// said is the fixture with one more sentence in its last section.
func said(t *testing.T, sentence string) string {
	return paper(t, front(), section(1, "One", prose(8)+"\n\n"+sentence))
}

func TestR01ReportsAnIdentifierArXivDoesNotHave(t *testing.T) {
	f := fires(t, said(t, "The argument is laid out in arXiv:2401.99999 at length."), "R01")
	if !strings.Contains(f.What, "names arXiv:2401.99999, and the metadata plane has no paper") {
		t.Errorf("the finding reads %q", f.What)
	}
}

// The plane covers the months that have been harvested and not all of arXiv
// yet, and a reference to a month nobody has fetched is a fact about the
// harvest rather than about the paper. Group S is where the plane's own
// coverage is answered.
func TestR01LeavesAnIdentifierInAMonthThePlaneHasNot(t *testing.T) {
	root := said(t, "It follows the line of arXiv:1706.03762 throughout.")
	if got := audited(t, root)["R01"]; got.Total != 0 {
		t.Errorf("R01 found %v", got.Findings)
	}
}

// A field written by ax refs and then edited by hand is the way a bibliography
// comes to name something that is not an identifier at all.
func TestR01ReportsAnEntryNamingSomethingThatIsNotAnIdentifier(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	entered(t, root, func(e *refs.Entry) { e.ArXiv, e.Resolved = "2401.1", "" })
	if f := fires(t, root, "R01"); !strings.Contains(f.What, "which is not an arXiv identifier at all") {
		t.Errorf("the finding reads %q", f.What)
	}
}

// One hole read from two sides is one finding. The entry names the preprint and
// the resolution points at it, and reporting both would say the plane is
// missing two papers when it is missing one.
func TestR01ReportsAnEntryAndItsResolutionOnce(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	entered(t, root, func(e *refs.Entry) { e.ArXiv, e.Resolved = "2401.99999", "2401.99999" })
	if got := audited(t, root)["R01"]; got.Total != 1 {
		t.Errorf("R01 found %v", got.Findings)
	}
}

func TestR02ReportsACitationWithNoEntryBehindIt(t *testing.T) {
	root := said(t, "The bound is due to [9](#bib.bibx9), who proved it first.")
	if f := fires(t, root, "R02"); !strings.Contains(f.What, "cites bib.bibx9, and this paper's bibliography has no entry") {
		t.Errorf("the finding reads %q", f.What)
	}
}

// The commonest way for a citation to have no entry behind it is for the paper
// to have no bibliography at all, which is a paper ax refs has never been run
// over.
func TestR02ReportsAPaperWithNoBibliography(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	if err := os.Remove(refsPath(root)); err != nil {
		t.Fatal(err)
	}
	if f := fires(t, root, "R02"); !strings.Contains(f.What, "cites bib.bibx1") {
		t.Errorf("the finding reads %q", f.What)
	}
}

// The manifest is read by this rule, the way the register is read by G01, so a
// file that does not load is one finding here rather than four across the
// group.
func TestR03ReportsABibliographyThatDoesNotLoad(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	body := "paper: 2501.00001\nversion: 1\nentries:\n  - id: bib.bibx1\n    text: \"\"\n"
	if err := os.WriteFile(refsPath(root), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// R02 goes too, because a manifest that did not load has no entries in it
	// and the paper cites one.
	got := audited(t, root)
	if got["R03"].Total == 0 || !strings.Contains(got["R03"].Findings[0].What, "has no text") {
		t.Errorf("R03 found %v", got["R03"].Findings)
	}
	for id, res := range got {
		if id != "R03" && id != "R02" && res.Total > 0 {
			t.Errorf("%s also fired: %v", id, res.Findings)
		}
	}
}

// The matcher is the thing that built the edge, so running it again over the
// record the edge points at is what says the edge would be built again today.
func TestR04ReportsAResolutionTheMatcherNoLongerMakes(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	entered(t, root, func(e *refs.Entry) {
		e.ArXiv, e.Via = "", refs.ViaTitle
		e.Title = "Something else entirely, by somebody else"
	})
	if f := fires(t, root, "R04"); !strings.Contains(f.What, "resolves bib.bibx1 to 2401.01234, and the matcher does not agree") {
		t.Errorf("the finding reads %q", f.What)
	}
}

// R01 has already said the identifier is not in the plane, and a resolution to
// a paper that does not exist is that one fact and not two.
func TestR04SaysNothingAboutAPaperThePlaneHasNot(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	entered(t, root, func(e *refs.Entry) { e.ArXiv, e.Resolved = "2401.99999", "2401.99999" })
	if got := audited(t, root)["R04"]; got.Total != 0 {
		t.Errorf("R04 found %v", got.Findings)
	}
}

func TestR05ReportsAnEntryThatResolvesToTheCitingPaper(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	entered(t, root, func(e *refs.Entry) { e.Resolved = fixture })
	if f := fires(t, root, "R05"); !strings.Contains(f.What, "which is the paper doing the citing") {
		t.Errorf("the finding reads %q", f.What)
	}
}

// A paper cannot cite work that does not exist yet, so a year that far ahead is
// a year read off the wrong part of the entry.
func TestR06ReportsACitationFromTheFuture(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	entered(t, root, func(e *refs.Entry) { e.Year = 2030 })
	if f := fires(t, root, "R06"); !strings.Contains(f.What, "cites work dated 2030, and this paper was submitted in 2025") {
		t.Errorf("the finding reads %q", f.What)
	}
}

// A preprint cited in one year and published two years later is a style
// printing the publication year, which is not a defect.
func TestR06LeavesATwoYearSlack(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	entered(t, root, func(e *refs.Entry) { e.Year = 2027 })
	if got := audited(t, root)["R06"]; got.Total != 0 {
		t.Errorf("R06 found %v", got.Findings)
	}
}

func TestT12ReportsALinkIntoNothing(t *testing.T) {
	root := said(t, "The proof is in [Appendix C](#app-c), which nobody wrote.")
	if f := fires(t, root, "T12"); !strings.Contains(f.What, "links to #app-c, which is neither an identifier in this paper") {
		t.Errorf("the finding reads %q", f.What)
	}
}

// A link into the paper's own floats and sections is what the render path
// writes on purpose, so the rule is only about the ones it could not place.
func TestT12LeavesALinkThePaperCanAnswer(t *testing.T) {
	root := said(t, "The picture is [Figure 1](#fig-1) and the section is [one](#s1).")
	if got := audited(t, root)["T12"]; got.Total != 0 {
		t.Errorf("T12 found %v", got.Findings)
	}
}

// Two rules of this group ask the metadata plane a question, and a corpus with
// no metadata plane in it cannot answer either. Not run and not passed, which
// is the whole point of having four states.
//
// S04 is the one rule that reads the same absence as an answer, because a
// content file is written out of a record and a corpus that has the file and
// not the record has lost something. The two readings live together on purpose.
func TestTheRulesThatNeedThePlaneDoNotRunWithoutIt(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	if err := os.RemoveAll(filepath.Join(root, "metadata")); err != nil {
		t.Fatal(err)
	}
	for id, res := range audited(t, root) {
		want := Pass
		switch id {
		case "R01", "R04":
			want = NotRun
		case "G08", "F04":
			// This fixture is not a repository, which is a different absence
			// with the same answer. One rule reads what the history took out
			// and the other reads what the history never had.
			want = NotRun
		case "S04":
			want = Fail
		}
		if res.State() != want {
			t.Errorf("%s is %s with %v", id, res.State(), res.Findings)
		}
	}
}

package audit

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/metadata"
)

// ownRecord is the fixture paper's own line of the metadata plane.
//
// One version, under a licence read off the abs page, which is what every file
// of the fixture says about itself. Group S is those two being the same thing.
func ownRecord() metadata.Record {
	return metadata.Record{
		ID:         fixture,
		Title:      "A paper about nothing in particular",
		Abstract:   "There is nothing in particular in it either.",
		Authors:    []metadata.Author{{Surname: "Hopper", Forename: "Grace"}},
		Categories: []string{"cs.DL"},
		Versions: []metadata.Version{{
			Version:     1,
			Created:     time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC),
			Licence:     corpus.LicenceCCBY,
			LicenceFrom: metadata.SourceAbs,
		}},
		Source:    metadata.SourceKaggle,
		Harvested: "2026-01-01",
	}
}

// filed puts the fixture's own record back with one thing changed about it.
func filed(t *testing.T, root string, edit func(r *metadata.Record)) {
	t.Helper()
	rec := ownRecord()
	edit(&rec)
	planed(t, root, rec)
}

// statedAs puts one file's front matter back with something changed about what
// it says of itself, which is the half of group S that reads the file.
func statedAs(t *testing.T, root, name string, edit func(f *extract.Front)) {
	t.Helper()
	rewrite(t, root, name, func(d *extract.Document) { edit(&d.Front) })
}

func TestS10ReportsALicenceReadOffABulkSurface(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	statedAs(t, root, "01_one.md", func(f *extract.Front) { f.LicenceFrom = string(metadata.SourceKaggle) })
	f := fires(t, root, "S10")
	if !strings.Contains(f.What, "carries a licence read from kaggle") {
		t.Errorf("the finding reads %q", f.What)
	}
	// The reader has to be told what to do about it, and what to do about it is
	// one command.
	if !strings.Contains(f.What, "ax licence resolve") {
		t.Errorf("the finding does not say how to fix it: %q", f.What)
	}
}

func TestS10ReportsAFileThatSaysNothingAboutWhereItsLicenceCameFrom(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	statedAs(t, root, "01_one.md", func(f *extract.Front) { f.LicenceFrom = "" })
	if f := fires(t, root, "S10"); !strings.Contains(f.What, "does not say where its licence was read from") {
		t.Errorf("the finding reads %q", f.What)
	}
}

func TestS07ReportsAFileThatNamesNoVersion(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	statedAs(t, root, "01_one.md", func(f *extract.Front) { f.Version = "" })
	if f := fires(t, root, "S07"); !strings.Contains(f.What, "names no version") {
		t.Errorf("the finding reads %q", f.What)
	}
}

func TestS07ReportsAVersionThePlaneHasNeverHeardOf(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	filed(t, root, func(r *metadata.Record) { r.Versions[0].Version = 2 })
	f := fires(t, root, "S07")
	if !strings.Contains(f.What, "says it is of v1 and the plane holds v2") {
		t.Errorf("the finding reads %q", f.What)
	}
}

// A file naming a version the plane has not got has had its licence checked
// against a version that does not exist, so the licence question is not asked
// of it a second time under another rule's name.
func TestS07TakesTheLicenceQuestionWithIt(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	filed(t, root, func(r *metadata.Record) {
		r.Versions[0].Version = 2
		r.Versions[0].Licence = corpus.LicenceCCBYSA
	})
	if got := audited(t, root)["S12"]; got.Total != 0 {
		t.Errorf("S12 found %v", got.Findings)
	}
}

func TestS12ReportsAFilePublishedUnderALicenceItsSourceDoesNotGive(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	statedAs(t, root, "01_one.md", func(f *extract.Front) { f.Licence = corpus.LicenceCCBYSA.SPDX() })
	f := fires(t, root, "S12")
	if !strings.Contains(f.What, "is published as CC-BY-SA-4.0 and the article is under cc-by") {
		t.Errorf("the finding reads %q", f.What)
	}
}

// The version trap, which is the whole reason this rule exists. The paper was
// relicensed at v2 and the corpus holds v1, so a file carrying the licence the
// abs page shows today is a file published under a permission that arrived
// after the text it holds.
func TestS12ReportsAPaperRelicensedAfterTheVersionOnDisk(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	filed(t, root, func(r *metadata.Record) {
		r.Versions[0].Licence = corpus.LicenceCCBYNCND
		r.Versions = append(r.Versions, metadata.Version{
			Version:     2,
			Created:     time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
			Licence:     corpus.LicenceCCBY,
			LicenceFrom: metadata.SourceAbs,
		})
	})
	f := fires(t, root, "S12")
	if !strings.Contains(f.What, "says the article is under cc-by and the plane says v1 is under cc-by-nc-nd") {
		t.Errorf("the finding reads %q", f.What)
	}
	if !strings.Contains(f.What, "which is the licence of v2, the latest") {
		t.Errorf("the finding does not name the trap: %q", f.What)
	}
}

func TestS04ReportsAPaperTheMetadataPlaneHasNoRecordFor(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	filed(t, root, func(r *metadata.Record) { r.ID = "2501.00002" })
	f := fires(t, root, "S04")
	if !strings.Contains(f.What, "has content and the metadata plane has no record for it") {
		t.Errorf("the finding reads %q", f.What)
	}
	// One finding for the paper and not one for each of its files, and it names
	// the directory, because the directory is what has to go or be explained.
	if f.Where() != "content/en/2501/2501.00001" {
		t.Errorf("the finding is at %q", f.Where())
	}
}

// A month the harvest has not reached is a month R01 says nothing about,
// because an identifier in it is a fact about the harvest. A paper with content
// in it is the other way round: the content was written out of a record, so the
// month it is filed under is a month that was harvested once.
func TestS04ReportsAPaperInAMonthThePlaneHasNotGot(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	if err := os.Remove((metadata.Plane{Root: root}).Path("2501")); err != nil {
		t.Fatal(err)
	}
	if f := fires(t, root, "S04"); !strings.Contains(f.What, "no record for it") {
		t.Errorf("the finding reads %q", f.What)
	}
}

// Nothing else is asked about a paper with no record, because every other
// question in this group is a question about the record and four findings about
// one missing line is a report nobody reads.
func TestS04TakesTheRestOfTheGroupWithIt(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	filed(t, root, func(r *metadata.Record) { r.ID = "2501.00002" })
	if got := audited(t, root)["S04"]; got.Total != 1 {
		t.Errorf("S04 found %v", got.Findings)
	}
}

func TestS01ReportsAFileWhoseAccessLineAndLicenceDisagree(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	statedAs(t, root, "01_one.md", func(f *extract.Front) { f.Access = string(corpus.AccessVerbatim) })
	f := fires(t, root, "S01")
	if !strings.Contains(f.What, "says it is access verbatim and the article is under cc-by, which is access open") {
		t.Errorf("the finding reads %q", f.What)
	}
}

func TestS01ReportsALicenceArXivDoesNotIssue(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	statedAs(t, root, "01_one.md", func(f *extract.Front) { f.LicenceOfSource = "mit" })
	if f := fires(t, root, "S01"); !strings.Contains(f.What, `says the article is under "mit", which is not a licence arXiv issues`) {
		t.Errorf("the finding reads %q", f.What)
	}
}

// The case the whole gate is for. The licence was resolved properly later, the
// paper turned out to be one the corpus may only hold a record of, and the text
// is still sitting there.
func TestS01ReportsTextForAPaperTheCorpusMayNotPublish(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	for _, name := range []string{"00_front.md", "01_one.md"} {
		statedAs(t, root, name, func(f *extract.Front) {
			f.LicenceOfSource = string(corpus.LicenceArXiv)
			f.Access = string(corpus.AccessRecord)
		})
	}
	filed(t, root, func(r *metadata.Record) { r.Versions[0].Licence = corpus.LicenceArXiv })
	f := fires(t, root, "S01", "F08")
	if !strings.Contains(f.What, "the corpus may hold the metadata, the structure and the tags of such a paper and not its text") {
		t.Errorf("the finding reads %q", f.What)
	}
}

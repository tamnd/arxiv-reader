package seed

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/policy"
	"github.com/tamnd/arxiv-reader/selection"
)

// today is what the tests measure age against, so that a test is not a
// different test next year.
var today = time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)

func when(year int) time.Time { return time.Date(year, 6, 1, 0, 0, 0, 0, time.UTC) }

// rec is one metadata record with one version and a licence on it.
func rec(id, category string, year int, l corpus.Licence) metadata.Record {
	return metadata.Record{
		ID: id, Title: "A paper about " + category, Categories: []string{category},
		Versions: []metadata.Version{{Version: 1, Created: when(year), Licence: l}},
		Source:   metadata.SourceKaggle, Harvested: "2026-09-15",
	}
}

// plane writes the records into a metadata plane and hands back its root.
func plane(t *testing.T, recs ...metadata.Record) metadata.Plane {
	t.Helper()
	p := metadata.Plane{Root: t.TempDir()}
	byShard, unplaceable := metadata.Group(recs)
	if len(unplaceable) > 0 {
		t.Fatalf("the fixture has records with no shard: %+v", unplaceable)
	}
	for shard, in := range byShard {
		if _, err := p.Write(shard, in); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func propose(t *testing.T, p metadata.Plane, opt Options) Proposal {
	t.Helper()
	if opt.Top == 0 {
		opt.Top = 3
	}
	if opt.Now.IsZero() {
		opt.Now = today
	}
	if opt.Weights.Zero() {
		opt.Weights = policy.DefaultSelection()
	}
	out, err := Propose(p, opt)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func ids(p Proposal) []string {
	out := make([]string, 0, len(p.Seed))
	for _, c := range p.Seed {
		out = append(out, c.ID)
	}
	return out
}

func TestOnlyPapersTheCorpusMayPublishAreOffered(t *testing.T) {
	// This is the whole point of the seed being licence first. A paper under
	// arXiv's own distribution licence may be held as a record and nothing
	// more, so it is not a candidate for the content plane at all.
	p := propose(t, plane(t,
		rec("2106.00001", "cs.LG", 2021, corpus.LicenceCCBY),
		rec("2106.00002", "cs.LG", 2021, corpus.LicenceArXiv),
		rec("2106.00003", "cs.LG", 2021, corpus.LicenceCCBYNCND),
	), Options{})
	if got := ids(p); len(got) != 2 {
		t.Fatalf("two of the three may be published, got %v", got)
	}
	if p.Counts.Refused != 1 {
		t.Fatalf("one paper was refused by the gate, got %d", p.Counts.Refused)
	}
	if p.Counts.Eligible != 2 || p.Counts.Records != 3 {
		t.Fatalf("got %+v", p.Counts)
	}
}

func TestAPaperNobodyHasResolvedALicenceForIsCountedApartFromOneThatFailed(t *testing.T) {
	// Empty is not the same as unknown. A large unresolved count means the pool
	// was drawn from a fraction of arXiv and is narrower than it looks, which
	// is a different problem from a corpus full of papers it may not publish.
	p := propose(t, plane(t,
		rec("2106.00001", "cs.LG", 2021, corpus.LicenceCCBY),
		rec("2106.00002", "cs.LG", 2021, ""),
	), Options{})
	if p.Counts.Unresolved != 1 || p.Counts.Refused != 0 {
		t.Fatalf("one paper has nobody's answer on it, which is not a refusal: %+v", p.Counts)
	}
	if p.Counts.Offered != 1 {
		t.Fatalf("got %d", p.Counts.Offered)
	}
}

func TestTheClassFilterCanBeNarrowed(t *testing.T) {
	in := plane(t,
		rec("2106.00001", "cs.LG", 2021, corpus.LicenceCC0),
		rec("2106.00002", "cs.LG", 2021, corpus.LicenceCCBYSA),
		rec("2106.00003", "cs.LG", 2021, corpus.LicenceCCBYNCND),
	)
	p := propose(t, in, Options{Classes: []corpus.Access{corpus.AccessOpen}})
	if got := ids(p); len(got) != 1 || got[0] != "2106.00001" {
		t.Fatalf("only the cc0 paper is open, got %v", got)
	}
	// And the two classes that permit a translation, which is the filter a
	// corpus that means to publish in four languages would actually run.
	p = propose(t, in, Options{Classes: []corpus.Access{corpus.AccessOpen, corpus.AccessShareAlike}})
	if got := ids(p); len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}

func TestAClassNobodyKnowsIsRefused(t *testing.T) {
	_, err := Propose(plane(t), Options{Top: 1, Classes: []corpus.Access{"public-domain"}})
	if err == nil || !strings.Contains(err.Error(), "not one of the four access classes") {
		t.Fatalf("got %v", err)
	}
}

func TestATopOfNoughtIsRefusedRatherThanWritingAnEmptyFile(t *testing.T) {
	_, err := Propose(plane(t), Options{Top: 0})
	if err == nil || !strings.Contains(err.Error(), "at least 1") {
		t.Fatalf("got %v", err)
	}
}

func TestThePoolIsSpreadAcrossTheArchivesAndTheYears(t *testing.T) {
	// A top of one over a plane where one archive has ten papers and another
	// has one gives one from each, which is what stops a seed being a thousand
	// machine learning papers.
	var recs []metadata.Record
	for i := 1; i <= 10; i++ {
		recs = append(recs, rec(fmt.Sprintf("2106.%05d", i), "cs.LG", 2021, corpus.LicenceCCBY))
	}
	recs = append(recs, rec("2107.00001", "math.AG", 2021, corpus.LicenceCCBY))
	p := propose(t, plane(t, recs...), Options{Top: 1})
	if len(p.Seed) != 2 {
		t.Fatalf("one from each archive, got %v", ids(p))
	}
	if len(p.Groups) != 2 {
		t.Fatalf("two groups, got %+v", p.Groups)
	}
	for _, g := range p.Groups {
		if g.Taken != 1 {
			t.Fatalf("each group offered one, got %+v", g)
		}
	}
}

func TestAYearIsItsOwnGroup(t *testing.T) {
	// Without this the top N of an archive is its oldest N papers, because the
	// only term the score has at seed time is age, and a candidate pool that is
	// all 1992 is not a pool anybody can build a corpus out of.
	p := propose(t, plane(t,
		rec("hep-th/9201001", "hep-th", 1992, corpus.LicenceCCBY),
		rec("hep-th/9202001", "hep-th", 1992, corpus.LicenceCCBY),
		rec("2106.00001", "hep-th", 2021, corpus.LicenceCCBY),
	), Options{Top: 1})
	if len(p.Seed) != 2 {
		t.Fatalf("one from 1992 and one from 2021, got %v", ids(p))
	}
	if p.Seed[0].Year != 1992 || p.Seed[1].Year != 2021 {
		t.Fatalf("the years are not in order: %+v", p.Seed)
	}
}

func TestTheGroupRecordsHowManyItPassedOver(t *testing.T) {
	// The eligible count is what says a group of three was chosen out of four
	// hundred rather than out of three, and a person editing the file cannot
	// tell those apart from the rows alone.
	p := propose(t, plane(t,
		rec("2106.00001", "cs.LG", 2021, corpus.LicenceCCBY),
		rec("2106.00002", "cs.LG", 2021, corpus.LicenceCCBY),
		rec("2106.00003", "cs.LG", 2021, corpus.LicenceCCBY),
	), Options{Top: 1})
	if p.Groups[0].Eligible != 3 || p.Groups[0].Taken != 1 {
		t.Fatalf("got %+v", p.Groups[0])
	}
}

func TestTheRanksAreOneBasedInsideTheirGroup(t *testing.T) {
	p := propose(t, plane(t,
		rec("2106.00001", "cs.LG", 2021, corpus.LicenceCCBY),
		rec("2106.00002", "cs.LG", 2021, corpus.LicenceCCBY),
		rec("2107.00001", "math.AG", 2021, corpus.LicenceCCBY),
	), Options{Top: 5})
	got := map[string]int{}
	for _, c := range p.Seed {
		got[c.ID] = c.Rank
	}
	if got["2106.00001"] != 1 || got["2106.00002"] != 2 || got["2107.00001"] != 1 {
		t.Fatalf("the rank is a place inside a group and not a place in the file: %+v", got)
	}
}

func TestTiesAreBrokenByIdentifierSoTwoRunsAgree(t *testing.T) {
	// Two papers from the same month score within days of each other, and a
	// proposal that reorders itself between runs over the same plane is one
	// nobody can review as a diff.
	in := plane(t,
		rec("2106.00002", "cs.LG", 2021, corpus.LicenceCCBY),
		rec("2106.00001", "cs.LG", 2021, corpus.LicenceCCBY),
	)
	first, second := propose(t, in, Options{}), propose(t, in, Options{})
	if ids(first)[0] != "2106.00001" {
		t.Fatalf("got %v", ids(first))
	}
	if strings.Join(ids(first), ",") != strings.Join(ids(second), ",") {
		t.Fatalf("%v and %v", ids(first), ids(second))
	}
}

func TestAnOlderPaperOutranksANewerOneInTheSameGroup(t *testing.T) {
	april, may := rec("0704.0001", "math.AG", 2007, corpus.LicenceCCBY), rec("0705.0001", "math.AG", 2007, corpus.LicenceCCBY)
	april.Versions[0].Created = time.Date(2007, 4, 2, 0, 0, 0, 0, time.UTC)
	may.Versions[0].Created = time.Date(2007, 5, 2, 0, 0, 0, 0, time.UTC)
	p := propose(t, plane(t, may, april), Options{Top: 2})
	if p.Seed[0].ID != "0704.0001" {
		t.Fatalf("the older of the two comes first, got %v", ids(p))
	}
	if !(p.Seed[0].Score > p.Seed[1].Score) {
		t.Fatalf("got %v and %v", p.Seed[0].Score, p.Seed[1].Score)
	}
}

func TestScoreIsTheFourTermsOfTheSpec(t *testing.T) {
	w := policy.Selection{Inside: 10, Outside: 0.01, Age: 1, AgeCap: 25, Vision: -20}
	if got := Score(3, 100, 5, false, w); got != 3*10+100*0.01+5 {
		t.Fatalf("got %v", got)
	}
	if got := Score(3, 0, 5, true, w); got != 30+5-20 {
		t.Fatalf("the vision penalty is not applied, got %v", got)
	}
}

func TestTheAgeTermIsCapped(t *testing.T) {
	// Thirty years is not four times as good a reason as ten. Without the cap
	// the whole of the seed is 1991, which is a fifth of a percent of arXiv.
	w := policy.Selection{Age: 1, AgeCap: 25}
	if got := Score(0, 0, 34, false, w); got != 25 {
		t.Fatalf("got %v", got)
	}
	if got := Score(0, 0, -3, false, w); got != 0 {
		t.Fatalf("a paper dated in the future is not worth a negative score, got %v", got)
	}
}

func TestAgeIsTakenFromTheFirstVersion(t *testing.T) {
	// A paper revised last month is not a new paper, and what the age term
	// stands in for is how long the work has been out to be built on.
	r := rec("0704.0001", "math.AG", 2007, corpus.LicenceCCBY)
	r.Versions = append(r.Versions, metadata.Version{Version: 2, Created: when(2025), Licence: corpus.LicenceCCBY})
	if got := Age(r, today); got < 19 || got > 20 {
		t.Fatalf("the paper is nineteen years old and not one, got %v", got)
	}
}

func TestAgeOfARecordWithNoVersionsIsNought(t *testing.T) {
	if got := Age(metadata.Record{ID: "2106.00001"}, today); got != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestAProposalCarriesTheWeightsItWasBuiltWith(t *testing.T) {
	// A file that says which papers without saying under what weights is a file
	// nobody can reproduce, and the weights are meant to be reviewable.
	w := policy.Selection{Inside: 5, Age: 2, AgeCap: 10, Vision: -1}
	p := propose(t, plane(t, rec("2106.00001", "cs.LG", 2021, corpus.LicenceCCBY)), Options{Weights: w})
	if p.Weights != w {
		t.Fatalf("got %+v", p.Weights)
	}
	if p.Top != 3 || p.Built == "" {
		t.Fatalf("got %+v", p)
	}
}

func TestAnEmptyPlaneProposesNothingRatherThanFailing(t *testing.T) {
	p := propose(t, metadata.Plane{Root: t.TempDir()}, Options{})
	if len(p.Seed) != 0 || len(p.Groups) != 0 {
		t.Fatalf("got %+v", p)
	}
	if !strings.Contains(p.Text(), "there is nothing to propose") {
		t.Fatalf("an empty plane should say so:\n%s", p.Text())
	}
	if !strings.Contains(p.Text(), "Run ax harvest to fill the plane") {
		t.Fatalf("and should say what to do about it:\n%s", p.Text())
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	p := propose(t, plane(t, rec("2106.00001", "cs.LG", 2021, corpus.LicenceCCBY)), Options{})
	path := filepath.Join(t.TempDir(), "manifests", "seed.yaml")
	if err := p.Save(path); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "# The seed: a candidate list, not a selection.") {
		t.Fatalf("the file does not open by saying what it is:\n%s", b)
	}
	if !strings.Contains(string(b), "the judgement is yours") {
		t.Fatalf("the header does not say who is meant to edit it:\n%s", b)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Seed) != 1 || back.Seed[0].ID != "2106.00001" {
		t.Fatalf("got %+v", back.Seed)
	}
	if back.Top != p.Top || back.Weights != p.Weights {
		t.Fatalf("got %+v", back)
	}
}

func TestLoadRefusesARowWithNoIdentifier(t *testing.T) {
	// The file is one a person edits by hand, so a half deleted row is the
	// ordinary mistake rather than an exotic one.
	path := filepath.Join(t.TempDir(), "seed.yaml")
	if err := os.WriteFile(path, []byte("seed:\n  - version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "row 1 has no id") {
		t.Fatalf("got %v", err)
	}
}

func TestLoadRefusesARowWithNoVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seed.yaml")
	if err := os.WriteFile(path, []byte("seed:\n  - id: 2106.00001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "names no version") {
		t.Fatalf("got %v", err)
	}
}

func TestEntriesAreSeedEntriesWithTheNameOfWhoeverEditedTheList(t *testing.T) {
	p := propose(t, plane(t, rec("2106.00001", "cs.LG", 2021, corpus.LicenceCCBY)), Options{})
	es := p.Entries("A Developer <dev@example.com>", "2026-09-15")
	if len(es) != 1 {
		t.Fatalf("got %+v", es)
	}
	e := es[0]
	if e.Reason != selection.Seed {
		t.Fatalf("got %q", e.Reason)
	}
	if e.By != "A Developer <dev@example.com>" {
		t.Fatalf("got %q", e.By)
	}
	if e.Status != selection.StatusSelected {
		t.Fatalf("a seeded paper has been chosen and nothing more, got %q", e.Status)
	}
	if e.Category != "cs.LG" || e.Year != 2021 || e.Rank != 1 {
		t.Fatalf("got %+v", e)
	}
	if err := e.Check(); err != nil {
		t.Fatalf("a proposed entry does not pass the manifest's own check: %v", err)
	}
}

func TestTextLeadsWithHowMuchOfArXivItDrewFrom(t *testing.T) {
	p := propose(t, plane(t,
		rec("2106.00001", "cs.LG", 2021, corpus.LicenceCCBY),
		rec("2106.00002", "cs.LG", 2021, corpus.LicenceArXiv),
		rec("2107.00001", "math.AG", 2021, ""),
		rec("hep-th/9201001", "hep-th", 1992, corpus.LicenceCCBYSA),
	), Options{Top: 2})
	txt := p.Text()
	for _, want := range []string{
		"records",
		"unresolved",
		"nobody has run ax licence resolve over them",
		"refused",
		"the corpus may publish the record and nothing else",
		"open, share-alike and verbatim",
		"the top 2 of each archive and year",
		"cs",
		"hep-th",
		"This is a candidate list and not a selection.",
		"is not a ranking of anything",
		"ax select seed --commit",
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("the output does not say %q:\n%s", want, txt)
		}
	}
}

func TestTextFoldsAnArchivesYearsTogether(t *testing.T) {
	// Somebody reading a proposal reads cs and math, not cs in 2007 and cs in
	// 2008 and cs in 2009 for thirty rows.
	p := propose(t, plane(t,
		rec("hep-th/9201001", "hep-th", 1992, corpus.LicenceCCBY),
		rec("0704.0001", "hep-th", 2007, corpus.LicenceCCBY),
		rec("2106.00001", "hep-th", 2021, corpus.LicenceCCBY),
	), Options{Top: 1})
	txt := p.Text()
	if !strings.Contains(txt, "3 papers") || !strings.Contains(txt, "1992 to 2021") {
		t.Fatalf("the archive row should fold its years:\n%s", txt)
	}
	if strings.Count(txt, "hep-th") != 1 {
		t.Fatalf("one row per archive:\n%s", txt)
	}
}

func TestTextSaysASingleYearAsAYearAndNotAsARange(t *testing.T) {
	p := propose(t, plane(t, rec("2106.00001", "cs.LG", 2021, corpus.LicenceCCBY)), Options{})
	if !strings.Contains(p.Text(), "2021\t") && !strings.Contains(p.Text(), "2021 ") {
		t.Fatalf("got:\n%s", p.Text())
	}
	if strings.Contains(p.Text(), "2021 to 2021") {
		t.Fatalf("one year is a year:\n%s", p.Text())
	}
}

func TestTextCarriesNoTableMarkup(t *testing.T) {
	p := propose(t, plane(t, rec("2106.00001", "cs.LG", 2021, corpus.LicenceCCBY)), Options{})
	if strings.Contains(p.Text(), "|") {
		t.Fatalf("the terminal output carries table markup:\n%s", p.Text())
	}
}

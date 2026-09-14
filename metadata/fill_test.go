package metadata

import (
	"testing"

	"github.com/tamnd/arxiv-reader/corpus"
)

// The arXivRaw format gives the author list as one string and the arXiv format
// gives it split into surnames and forenames, so a real harvest runs both. If
// the second pass cleared what the first one wrote, running both would be worse
// than running either alone.
func TestFillDoesNotLetOneSurfaceEraseAnother(t *testing.T) {
	raw := Record{
		ID:         "math/9602216",
		Title:      "On the Hausdorff dimension of the Sierpinski gasket",
		Abstract:   "We compute the dimension.",
		Categories: []string{"math.CA"},
		Versions: []Version{
			{Version: 1, Created: day("1996-02-28"), Licence: corpus.LicenceCCBY, LicenceFrom: SourceOAI},
			{Version: 2, Created: day("1996-04-02"), Licence: corpus.LicenceCCBY, LicenceFrom: SourceOAI},
			{Version: 3, Created: day("1997-01-09"), Licence: corpus.LicenceCCBY, LicenceFrom: SourceOAI},
		},
		Source:    SourceOAI,
		Harvested: "2026-09-14",
	}
	// What the arXiv format knows: the authors, split, and nothing about the
	// version history at all.
	structured := Record{
		ID:         "math/9602216",
		Title:      "On the Hausdorff dimension of the Sierpinski gasket",
		Abstract:   "We compute the dimension.",
		Authors:    []Author{{Surname: "Strichartz", Forename: "Robert S."}},
		Categories: []string{"math.CA"},
		Source:     SourceOAI,
		Harvested:  "2026-09-15",
	}

	got := Fill(raw, structured)
	if len(got.Versions) != 3 {
		t.Fatalf("the structured pass left %d versions, want 3", len(got.Versions))
	}
	for _, v := range got.Versions {
		if v.Licence != corpus.LicenceCCBY {
			t.Errorf("v%d lost its licence", v.Version)
		}
	}
	if len(got.Authors) != 1 {
		t.Fatalf("the authors did not survive: %v", got.Authors)
	}
	if got.Harvested != "2026-09-15" {
		t.Errorf("harvested = %q, want the later pass to win", got.Harvested)
	}

	// And in the other order, which is the order a catch up would use.
	got = Fill(structured, raw)
	if len(got.Versions) != 3 {
		t.Fatalf("got %d versions, want 3", len(got.Versions))
	}
	if len(got.Authors) != 1 {
		t.Fatalf("the raw pass cleared the authors: %v", got.Authors)
	}
	if err := got.Validate(); err != nil {
		t.Errorf("the two passes together should make a valid record: %v", err)
	}
}

// A field with a value in it wins over one without, in both directions, and a
// new value wins over an old one.
func TestFillPrefersTheIncomingValue(t *testing.T) {
	stored := Record{ID: "2106.09685", Title: "LoRA", DOI: "10.1000/old", Comments: "9 pages"}
	incoming := Record{ID: "2106.09685", Title: "LoRA: Low-Rank Adaptation", DOI: "", JournalRef: "ICLR 2022"}

	got := Fill(stored, incoming)
	if got.Title != "LoRA: Low-Rank Adaptation" {
		t.Errorf("title = %q, want the incoming one", got.Title)
	}
	if got.DOI != "10.1000/old" {
		t.Errorf("doi = %q, want the stored one to survive an empty incoming field", got.DOI)
	}
	if got.JournalRef != "ICLR 2022" {
		t.Errorf("journal_ref = %q, want the incoming one", got.JournalRef)
	}
	if got.Comments != "9 pages" {
		t.Errorf("comments = %q, want the stored one", got.Comments)
	}
}

// A catch up harvest finds a version the bootstrap did not have, because the
// paper was revised in between. The new version arrives and the old ones stay.
func TestFillAddsANewVersion(t *testing.T) {
	stored := Record{Versions: []Version{
		{Version: 1, Created: day("2021-06-17"), Licence: corpus.LicenceCCBY, LicenceFrom: SourceOAI},
		{Version: 2, Created: day("2021-07-01"), Licence: corpus.LicenceCCBY, LicenceFrom: SourceOAI},
	}}
	incoming := Record{Versions: []Version{
		{Version: 2, Created: day("2021-07-01")},
		{Version: 3, Created: day("2021-10-16"), Licence: corpus.LicenceCCBYNCND, LicenceFrom: SourceOAI},
	}}

	got := Fill(stored, incoming)
	if len(got.Versions) != 3 {
		t.Fatalf("got %v, want three versions", got.Versions)
	}
	for i, v := range got.Versions {
		if v.Version != i+1 {
			t.Fatalf("versions came out %v", got.Versions)
		}
	}
	// v2 came back without a licence and keeps the one it had.
	if got.Versions[1].Licence != corpus.LicenceCCBY {
		t.Errorf("v2 licence = %q, want it kept", got.Versions[1].Licence)
	}
	// v3 is new, and a paper whose latest version is CC BY-NC-ND cannot be
	// translated even though its first two versions could have been.
	if got.Versions[2].Licence != corpus.LicenceCCBYNCND {
		t.Errorf("v3 licence = %q, want the incoming one", got.Versions[2].Licence)
	}
	latest, _ := got.Latest()
	if corpus.AccessFor(latest.Licence).MayTranslate() {
		t.Error("a CC BY-NC-ND latest version should not be translatable")
	}
}

// Replace is the escape hatch for a caller that knows its record is the whole
// truth, and it still refuses to unresolve a licence. The Kaggle snapshot has no
// per version licence, so a re-bootstrap under Replace would otherwise throw
// away the census across three million records without saying anything.
func TestReplaceKeepsAResolvedLicence(t *testing.T) {
	stored := Record{
		Title: "the old title",
		Versions: []Version{
			{Version: 1, Created: day("2021-06-17"), Licence: corpus.LicenceCC0, LicenceFrom: SourceOAI},
		},
	}
	incoming := Record{
		Versions: []Version{{Version: 1, Created: day("2021-06-17")}},
	}

	got := Replace(stored, incoming)
	if got.Title != "" {
		t.Errorf("title = %q, want Replace to take the incoming record whole", got.Title)
	}
	if got.Versions[0].Licence != corpus.LicenceCC0 {
		t.Errorf("v1 licence = %q, want the resolved one kept", got.Versions[0].Licence)
	}
	if got.Versions[0].LicenceFrom != SourceOAI {
		t.Errorf("v1 authority = %q, want it kept with the licence", got.Versions[0].LicenceFrom)
	}
}

// A licence that changed really did change, and a later harvest is entitled to
// say so. Only an empty one falls back.
func TestAChangedLicenceWins(t *testing.T) {
	stored := Record{Versions: []Version{
		{Version: 1, Created: day("2021-06-17"), Licence: corpus.LicenceArXiv, LicenceFrom: SourceKaggle},
	}}
	incoming := Record{Versions: []Version{
		{Version: 1, Created: day("2021-06-17"), Licence: corpus.LicenceCCBY, LicenceFrom: SourceOAI},
	}}

	for name, got := range map[string]Record{"fill": Fill(stored, incoming), "replace": Replace(stored, incoming)} {
		if got.Versions[0].Licence != corpus.LicenceCCBY {
			t.Errorf("%s: licence = %q, want the incoming one", name, got.Versions[0].Licence)
		}
		if got.Versions[0].LicenceFrom != SourceOAI {
			t.Errorf("%s: authority = %q, want it to move with the licence", name, got.Versions[0].LicenceFrom)
		}
	}
}

// Filling onto nothing is the first harvest, which is most of them.
func TestFillOntoAnEmptyRecord(t *testing.T) {
	incoming := good()
	got := Fill(Record{}, incoming)
	if err := got.Validate(); err != nil {
		t.Fatalf("%v", err)
	}
	if got.ID != incoming.ID || len(got.Versions) != len(incoming.Versions) {
		t.Errorf("got %+v, want %+v", got, incoming)
	}
}

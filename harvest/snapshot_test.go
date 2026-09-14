package harvest

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
)

// snapshot.jsonl holds five lines taken straight out of the published file,
// chosen for the things that are awkward: an old style id with a null licence,
// a CC BY paper, a paper on the default licence with three versions, and one
// with a DOI and a journal reference.
func snapshotRecords(t *testing.T) map[string]metadata.Record {
	t.Helper()
	f, err := os.Open("testdata/snapshot.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	s := Snapshot{
		Source: metadata.SourceKaggle,
		Now:    func() time.Time { return time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC) },
	}
	out := map[string]metadata.Record{}
	stats, err := s.Read(f, func(r metadata.Record) error {
		out[r.ID] = r
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Skipped != 0 {
		t.Errorf("skipped %d lines of a file that has nothing wrong with it", stats.Skipped)
	}
	if stats.Read != len(out) {
		t.Errorf("read %d records but kept %d", stats.Read, len(out))
	}
	return out
}

func TestSnapshotRecords(t *testing.T) {
	recs := snapshotRecords(t)
	if len(recs) == 0 {
		t.Fatal("the fixture is empty")
	}
	for id, r := range recs {
		if err := r.Validate(); err != nil {
			t.Errorf("%s: %v", id, err)
		}
		if r.Source != metadata.SourceKaggle {
			t.Errorf("%s: source %q", id, r.Source)
		}
		if r.Harvested != "2026-09-14" {
			t.Errorf("%s: harvested %q", id, r.Harvested)
		}
	}
}

// The 1998 papers have license: null, because arXiv only started offering a
// choice in 2004. That is the ordinary case for the older half of the file and
// not a gap in it, and it has to resolve to the default licence rather than to
// unknown or to nothing at all.
func TestANullLicenceIsTheDefaultLicence(t *testing.T) {
	recs := snapshotRecords(t)
	r, ok := recs["solv-int/9806004"]
	if !ok {
		t.Fatalf("the fixture lost its pre-2004 paper: %v", keys(recs))
	}
	for _, v := range r.Versions {
		if v.Licence != corpus.LicenceArXiv {
			t.Errorf("v%d licence = %q, want the default", v.Version, v.Licence)
		}
		if v.LicenceFrom != metadata.SourceKaggle {
			t.Errorf("v%d authority = %q", v.Version, v.LicenceFrom)
		}
	}
	// Which means no text, no figures and no translation, for a paper whose
	// licence field was simply empty.
	access := corpus.AccessFor(corpus.LicenceArXiv)
	if access.MayPublishText() || access.MayTranslate() {
		t.Error("the default licence came out permitting something")
	}
}

// Old style ids keep their slash and land in the right month. Every tool that
// has got this wrong got it wrong here and not on a 2106.09685.
func TestSnapshotPlacesAnOldStyleID(t *testing.T) {
	recs := snapshotRecords(t)
	r := recs["solv-int/9806004"]
	shard, err := r.Shard()
	if err != nil {
		t.Fatal(err)
	}
	if shard != "9806" {
		t.Errorf("shard = %q, want 9806", shard)
	}
	if r.Archive() != "solv-int" {
		t.Errorf("archive = %q, want solv-int", r.Archive())
	}
}

// This file has the authors already split, which is the one thing the OAI
// arXivRaw format cannot do. It is the reason the bootstrap does not need an
// authors pass behind it.
func TestSnapshotAuthorsAreAlreadySplit(t *testing.T) {
	recs := snapshotRecords(t)
	for id, r := range recs {
		if len(r.Authors) == 0 {
			t.Errorf("%s has no authors", id)
			continue
		}
		for _, a := range r.Authors {
			if a.Surname == "" {
				t.Errorf("%s: an author with no surname: %+v", id, a)
			}
		}
	}
	r := recs["0909.0774"]
	if len(r.Authors) != 1 || r.Authors[0].String() != "Zlil Sela" {
		t.Errorf("authors = %v, want one Zlil Sela", r.Authors)
	}
}

// authors_parsed is surname, forenames, suffix, in that order. Getting the
// order wrong would put every author's given name in the surname field, which
// is the field M3 matches references on.
//
// This line is written by hand rather than taken from the file, because a
// suffix is rare enough that none of the five real lines has one. It is
// otherwise the shape of math-ph/0510026, which does.
func TestParsedAuthorsTripleOrder(t *testing.T) {
	line := `{"id":"math-ph/0510026","title":"A comment on the Dirac equation","abstract":"A note.",` +
		`"categories":"math-ph math.MP","license":null,` +
		`"versions":[{"version":"v1","created":"Mon, 24 Oct 2005 11:00:00 GMT"}],` +
		`"authors_parsed":[["Rodrigues","Waldyr A.","Jr"],["Bourbaki","",""]]}`

	s := Snapshot{Source: metadata.SourceKaggle, Now: func() time.Time { return time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC) }}
	var got metadata.Record
	if _, err := s.Read(strings.NewReader(line), func(r metadata.Record) error {
		got = r
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if len(got.Authors) != 2 {
		t.Fatalf("authors = %v", got.Authors)
	}
	first := got.Authors[0]
	if first.Surname != "Rodrigues" || first.Forename != "Waldyr A." || first.Suffix != "Jr" {
		t.Errorf("first author = %+v", first)
	}
	if first.String() != "Waldyr A. Rodrigues Jr" {
		t.Errorf("first author reads %q", first.String())
	}
	// An empty forename and suffix survive as a one name author rather than as
	// a name with two trailing spaces.
	if got.Authors[1].String() != "Bourbaki" {
		t.Errorf("second author reads %q", got.Authors[1].String())
	}
}

// The file gives one licence for the paper, so every version carries it. A
// paper that was on the default licence for v1 and moved to CC BY on v3 looks
// here like a CC BY paper all the way down, and there is no way to tell from
// this file alone. That is why LicenceFrom says kaggle: the audit refuses to
// publish on the strength of it and the M2 census replaces it.
func TestTheSnapshotLicenceIsPaperLevel(t *testing.T) {
	recs := snapshotRecords(t)
	r, ok := recs["0909.0774"]
	if !ok {
		t.Fatalf("the fixture lost its three version paper: %v", keys(recs))
	}
	if len(r.Versions) < 3 {
		t.Fatalf("versions = %d, want at least 3", len(r.Versions))
	}
	first := r.Versions[0].Licence
	for _, v := range r.Versions {
		if v.Licence != first {
			t.Fatalf("v%d differs, so the file grew a per version licence", v.Version)
		}
		if v.LicenceFrom != metadata.SourceKaggle {
			t.Errorf("v%d authority = %q, and the audit needs this to say kaggle", v.Version, v.LicenceFrom)
		}
	}
}

// A CC BY paper is one this project may republish and may translate, which is
// the only reason any of the rest of the milestones exist.
func TestASnapshotCCBYPaperIsUsable(t *testing.T) {
	recs := snapshotRecords(t)
	var found bool
	for _, r := range recs {
		latest, _ := r.Latest()
		if latest.Licence != corpus.LicenceCCBY {
			continue
		}
		found = true
		access := corpus.AccessFor(latest.Licence)
		if !access.MayPublishText() || !access.MayTranslate() {
			t.Errorf("%s is cc-by and came out unusable", r.ID)
		}
	}
	if !found {
		t.Fatalf("the fixture lost its cc-by paper: %v", keys(recs))
	}
}

// The version dates in this file are in the same shape as the OAI ones, so one
// parser serves both. Worth a test, because "Thu, 03 Sep 2009 22:17:07 GMT" and
// a plain date are both things arXiv has written over the years.
func TestSnapshotVersionDates(t *testing.T) {
	recs := snapshotRecords(t)
	r := recs["0909.0774"]
	if got := r.Versions[0].Created.Format("2006-01-02"); got != "2009-09-03" {
		t.Errorf("v1 created %s, want 2009-09-03", got)
	}
	for _, v := range r.Versions {
		if v.Created.IsZero() {
			t.Errorf("v%d has no date", v.Version)
		}
		if v.Created.Location() != time.UTC {
			t.Errorf("v%d is not in UTC", v.Version)
		}
	}
}

// A line the parser cannot place is counted and stepped over. One bad line in a
// five gigabyte file should not throw away the three million good ones behind
// it, and a harvest that dies on line 900,000 is a harvest nobody can finish.
func TestABadIDIsSkippedAndCounted(t *testing.T) {
	good := `{"id":"2106.09685","title":"LoRA","abstract":"a","categories":"cs.CL","license":null,` +
		`"versions":[{"version":"v1","created":"Thu, 17 Jun 2021 00:00:00 GMT"}],"authors_parsed":[["Hu","Edward J.",""]]}`
	bad := `{"id":"not an arxiv id","title":"nonsense","abstract":"a","categories":"cs.CL","license":null,` +
		`"versions":[{"version":"v1","created":"Thu, 17 Jun 2021 00:00:00 GMT"}],"authors_parsed":[["X","",""]]}`

	s := Snapshot{Source: metadata.SourceKaggle, Now: func() time.Time { return time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC) }}
	var seen []string
	stats, err := s.Read(strings.NewReader(bad+"\n"+good+"\n\n"+good+"\n"), func(r metadata.Record) error {
		seen = append(seen, r.ID)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Read != 2 || stats.Skipped != 1 {
		t.Errorf("stats = %+v, want 2 read and 1 skipped", stats)
	}
	if len(seen) != 2 {
		t.Errorf("saw %v", seen)
	}
	if got := stats.String(); !strings.Contains(got, "skipped") {
		t.Errorf("stats read %q and should say a line was skipped", got)
	}
}

// A line that is not JSON at all is a different matter. That is a corrupt or
// truncated download rather than one odd record, and carrying on would write a
// partial corpus that looks complete.
func TestAMalformedLineStopsTheRead(t *testing.T) {
	s := Snapshot{Source: metadata.SourceKaggle}
	_, err := s.Read(strings.NewReader("{\"id\":\"2106.09685\",\n"), func(metadata.Record) error { return nil })
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "line 1") {
		t.Errorf("error %q should say which line", err)
	}
}

// A licence URL nobody has seen before is an error and not a guess, in this
// reader as much as in the OAI one. The alternative is a URL one character off
// the CC BY one publishing a paper that may not be published.
func TestAnUnknownSnapshotLicenceIsAnError(t *testing.T) {
	line := `{"id":"2106.09685","title":"LoRA","abstract":"a","categories":"cs.CL",` +
		`"license":"https://opensource.org/licenses/MIT",` +
		`"versions":[{"version":"v1","created":"Thu, 17 Jun 2021 00:00:00 GMT"}],"authors_parsed":[["Hu","",""]]}`

	s := Snapshot{Source: metadata.SourceKaggle}
	_, err := s.Read(strings.NewReader(line), func(metadata.Record) error { return nil })
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "2106.09685") {
		t.Errorf("error %q should name the paper", err)
	}
}

func keys(m map[string]metadata.Record) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// The two surfaces over the same paper, which is what a real bootstrap does:
// the snapshot first and the OAI catch up on top of it.
//
// The snapshot has the authors already split and the catch up cannot, because
// arXivRaw gives them as one comma separated string. The catch up has the
// licence from arXiv itself and the snapshot's is second hand. So the merged
// record has to end up with both, and neither pass may undo the other.
//
// testdata/list_0909_0774.xml holds a `<record>` element copied verbatim out of
// a live GetRecord for this paper. Only the envelope around it was rewritten
// from GetRecord to ListRecords, so that the same parser reads it.
func TestTheSnapshotAndTheCatchUpCombine(t *testing.T) {
	fromSnapshot := snapshotRecords(t)["0909.0774"]
	if fromSnapshot.ID == "" {
		t.Fatal("the fixture lost 0909.0774")
	}

	srv, _ := serve(t, "list_0909_0774.xml")
	fromOAI, err := client(srv.URL).Records(context.Background(), Query{Format: FormatRaw})
	if err != nil {
		t.Fatal(err)
	}
	if len(fromOAI) != 1 || fromOAI[0].ID != "0909.0774" {
		t.Fatalf("the OAI fixture gave %v", fromOAI)
	}

	// arXivRaw knows nothing about author names it can hand over, so this is
	// the pass that would quietly empty the field under a merge that let the
	// last writer win.
	if len(fromOAI[0].Authors) != 0 {
		t.Fatalf("arXivRaw produced authors, which it cannot do reliably: %v", fromOAI[0].Authors)
	}

	merged := metadata.Fill(fromSnapshot, fromOAI[0])
	if err := merged.Validate(); err != nil {
		t.Fatalf("%v", err)
	}
	if len(merged.Authors) == 0 {
		t.Error("the catch up cleared the authors the snapshot had split")
	}
	if merged.Authors[0].Surname != "Sela" {
		t.Errorf("authors = %v, want the snapshot's", merged.Authors)
	}

	// The licence is now first hand, which is what audit rule S10 checks
	// before anything from this paper may be published.
	latest, _ := merged.Latest()
	if latest.LicenceFrom != metadata.SourceOAI {
		t.Errorf("authority = %q, want the catch up to upgrade it from kaggle", latest.LicenceFrom)
	}
	if latest.Licence != corpus.LicenceArXiv {
		t.Errorf("licence = %q, and both surfaces say the default here", latest.Licence)
	}
	if merged.Source != metadata.SourceOAI {
		t.Errorf("source = %q, want the surface that wrote last", merged.Source)
	}

	// The catch up also fills in what the snapshot file simply does not carry.
	if len(merged.Versions) < len(fromSnapshot.Versions) {
		t.Errorf("versions went from %d to %d", len(fromSnapshot.Versions), len(merged.Versions))
	}
}

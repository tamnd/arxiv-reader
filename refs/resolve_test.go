package refs

import (
	"strings"
	"testing"
	"time"

	"github.com/tamnd/arxiv-reader/metadata"
)

// rec is one record of the metadata plane, with the year on its first version,
// which is the year the resolver compares against.
func rec(id, title string, year int, surnames ...string) metadata.Record {
	r := metadata.Record{
		ID:       id,
		Title:    title,
		Versions: []metadata.Version{{Version: 1, Created: time.Date(year, 6, 1, 0, 0, 0, 0, time.UTC)}},
	}
	for _, s := range surnames {
		r.Authors = append(r.Authors, metadata.Author{Surname: s})
	}
	return r
}

func resolve(entries map[string]Entry, plane ...metadata.Record) *Resolver {
	r := NewResolver()
	for k, e := range entries {
		r.Want(k, e)
	}
	for _, p := range plane {
		r.Offer(p)
	}
	return r
}

func one(t *testing.T, r *Resolver, key string) Match {
	t.Helper()
	m, ok := r.Matches()[key]
	if !ok {
		t.Fatalf("%s resolved to nothing, and the misses are %+v", key, r.Misses())
	}
	return m
}

// The common case in a modern paper, and the one that costs nothing.
func TestAnArXivIDResolves(t *testing.T) {
	r := resolve(map[string]Entry{
		"a": {ID: "bib.bibx1", ArXiv: "2111.00396", Text: "Albert Gu et al, 2021"},
	}, rec("2111.00396", "Efficiently Modeling Long Sequences with Structured State Spaces", 2021, "Gu"))
	m := one(t, r, "a")
	if m.Paper != "2111.00396" || m.Via != ViaArXiv {
		t.Errorf("resolved to %+v", m)
	}
	if Confidence(m.Via) != "certain" {
		t.Errorf("an identifier match is %q", Confidence(m.Via))
	}
}

// The old style id is written several ways in a bibliography and the canonical
// form is the one the plane is keyed by.
func TestAnOldStyleArXivIDResolves(t *testing.T) {
	r := resolve(map[string]Entry{
		"a": {ID: "bib.bib1", ArXiv: "math.CO/0605137", Text: "a 2006 preprint"},
	}, rec("math/0605137", "A paper from before April 2007", 2006, "Nobody"))
	if m := one(t, r, "a"); m.Paper != "math/0605137" {
		t.Errorf("resolved to %+v", m)
	}
}

func TestADOIResolves(t *testing.T) {
	r := resolve(map[string]Entry{
		"a": {ID: "bib.bibx2", DOI: "10.21437/Interspeech.2023-1036", Text: "a paper with a DOI"},
	}, func() metadata.Record {
		p := rec("2306.00001", "Multi-Head State Space Model for Speech Recognition", 2023, "Fathullah")
		// The plane holds the DOI in whatever case the publisher registered it
		// and a bibliography prints it in whatever case the author typed, and a
		// DOI is defined to be compared without regard to either.
		p.DOI = "10.21437/interspeech.2023-1036"
		return p
	}())
	m := one(t, r, "a")
	if m.Via != ViaDOI || m.Paper != "2306.00001" {
		t.Errorf("resolved to %+v", m)
	}
}

// The title step is the one that matches prose, and the two sides never print
// the same punctuation or the same capitals.
func TestATitleResolves(t *testing.T) {
	r := resolve(map[string]Entry{
		"a": {
			ID:      "bib.bibx3",
			Title:   "Attention is all you need",
			Authors: []string{"Ashish Vaswani", "Noam Shazeer"},
			Year:    2017,
			Text:    "Ashish Vaswani et al, Attention is all you need, 2017",
		},
	}, rec("1706.03762", "Attention Is All You Need", 2017, "Vaswani", "Shazeer"))
	m := one(t, r, "a")
	if m.Via != ViaTitle || m.Paper != "1706.03762" {
		t.Errorf("resolved to %+v", m)
	}
	if Confidence(m.Via) != "medium" {
		t.Errorf("a prose match is %q, and it is a reading of prose", Confidence(m.Via))
	}
}

// A year is off by one constantly, because a bibliography prints the year of
// the proceedings and the plane holds the year of the preprint.
func TestAYearOffByOneIsAllowed(t *testing.T) {
	r := resolve(map[string]Entry{
		"a": {ID: "bib.bibx3", Title: "Attention is all you need", Authors: []string{"Vaswani"}, Year: 2018, Text: "x"},
	}, rec("1706.03762", "Attention Is All You Need", 2017, "Vaswani"))
	if m := one(t, r, "a"); m.Paper != "1706.03762" {
		t.Errorf("resolved to %+v", m)
	}
}

func TestAYearTooFarOffIsRefused(t *testing.T) {
	r := resolve(map[string]Entry{
		"a": {ID: "bib.bibx3", Title: "Attention is all you need", Authors: []string{"Vaswani"}, Year: 2021, Text: "x"},
	}, rec("1706.03762", "Attention Is All You Need", 2017, "Vaswani"))
	if m, ok := r.Matches()["a"]; ok {
		t.Fatalf("resolved to %+v on a year four out", m)
	}
	misses := r.Misses()
	if len(misses) != 1 || !strings.Contains(misses[0].Why, "2021") {
		t.Fatalf("the misses are %+v", misses)
	}
}

// Two different papers with the same title in the same year is not a thought
// experiment. It is a survey and the paper it surveys, or a workshop version
// and the conference one, and the author is what tells them apart.
func TestATitleWithNoAuthorInCommonIsRefused(t *testing.T) {
	r := resolve(map[string]Entry{
		"a": {ID: "bib.bibx3", Title: "Attention is all you need", Authors: []string{"Ada First"}, Year: 2017, Text: "x"},
	}, rec("1706.03762", "Attention Is All You Need", 2017, "Vaswani", "Shazeer"))
	if _, ok := r.Matches()["a"]; ok {
		t.Fatal("resolved on a title alone")
	}
	if misses := r.Misses(); len(misses) != 1 || !strings.Contains(misses[0].Why, "author") {
		t.Fatalf("the misses are %+v", misses)
	}
}

// A surname is matched against every word of the author block, because a style
// prints Vaswani, A. as readily as A. Vaswani.
func TestASurnamePrintedFirstStillMatches(t *testing.T) {
	r := resolve(map[string]Entry{
		"a": {ID: "bib.bibx3", Title: "Attention is all you need", Authors: []string{"Vaswani A."}, Year: 2017, Text: "x"},
	}, rec("1706.03762", "Attention Is All You Need", 2017, "Vaswani"))
	if _, ok := r.Matches()["a"]; !ok {
		t.Fatalf("the misses are %+v", r.Misses())
	}
}

func TestATitleThatIsOnlyNearlyRightIsRefusedAndReported(t *testing.T) {
	r := resolve(map[string]Entry{
		"a": {ID: "bib.bibx3", Title: "Attention is not all you need", Authors: []string{"Vaswani"}, Year: 2017, Text: "x"},
	}, rec("1706.03762", "Attention Is All You Need", 2017, "Vaswani"))
	if _, ok := r.Matches()["a"]; ok {
		t.Fatal("resolved a title that says the opposite of the record's")
	}
	misses := r.Misses()
	if len(misses) != 1 || !strings.Contains(misses[0].Why, "similarity") {
		t.Fatalf("the misses are %+v", misses)
	}
	if misses[0].Score >= Similar || misses[0].Score < Near {
		t.Errorf("the score is %.3f, and a near miss is between %.2f and %.2f", misses[0].Score, Near, Similar)
	}
}

// The ladder stops at the first hit, so a record found by its id is what the
// entry resolves to even when another record's title is a perfect match.
func TestTheLadderPrefersAnIdentifier(t *testing.T) {
	e := Entry{
		ID:      "bib.bibx3",
		ArXiv:   "1706.03762",
		Title:   "Attention is all you need",
		Authors: []string{"Vaswani"},
		Year:    2017,
		Text:    "x",
	}
	// Offered in the order that would get it wrong if the last one won.
	r := resolve(map[string]Entry{"a": e},
		rec("2101.00001", "Attention Is All You Need", 2017, "Vaswani"),
		rec("1706.03762", "Attention Is All You Need", 2017, "Vaswani"),
	)
	if m := one(t, r, "a"); m.Paper != "1706.03762" || m.Via != ViaArXiv {
		t.Errorf("resolved to %+v", m)
	}
}

// An ambiguity is dropped rather than decided. A coin toss between two papers
// is an edge that is wrong half the time and says nothing about which half.
func TestTwoPapersThatMatchEquallyWellResolveToNeither(t *testing.T) {
	r := resolve(map[string]Entry{
		"a": {ID: "bib.bibx3", Title: "On nothing", Authors: []string{"Nobody"}, Year: 2020, Text: "x"},
	},
		rec("2001.00001", "On Nothing", 2020, "Nobody"),
		rec("2002.00002", "On Nothing", 2020, "Nobody"),
	)
	if m, ok := r.Matches()["a"]; ok {
		t.Fatalf("picked %s out of two papers that match equally well", m.Paper)
	}
	if misses := r.Misses(); len(misses) != 1 || !strings.Contains(misses[0].Why, "equally") {
		t.Fatalf("the misses are %+v", misses)
	}
}

// The year and the author are guards, and an entry with neither cannot be
// matched on its title at all, so it is never put in front of the plane.
func TestAnEntryWithNoYearIsNotMatchedOnItsTitle(t *testing.T) {
	r := resolve(map[string]Entry{
		"a": {ID: "bib.bibx3", Title: "Attention is all you need", Authors: []string{"Vaswani"}, Text: "x"},
	}, rec("1706.03762", "Attention Is All You Need", 2017, "Vaswani"))
	if _, ok := r.Matches()["a"]; ok {
		t.Fatal("matched an entry that states no year")
	}
}

func TestAnEntryWithNoAuthorIsNotMatchedOnItsTitle(t *testing.T) {
	r := resolve(map[string]Entry{
		"a": {ID: "bib.bibx3", Title: "Attention is all you need", Year: 2017, Text: "x"},
	}, rec("1706.03762", "Attention Is All You Need", 2017, "Vaswani"))
	if _, ok := r.Matches()["a"]; ok {
		t.Fatal("matched an entry that names no author")
	}
}

// Nothing is a fine outcome. A reference to a textbook resolves to nothing and
// the entry is still a bibliography line.
func TestAnEntryThatMatchesNothingIsNotAFailure(t *testing.T) {
	r := resolve(map[string]Entry{
		"a": {ID: "bib.bib9", Title: "Elements of Mathematics", Authors: []string{"Bourbaki"}, Year: 1968, Text: "x"},
	}, rec("1706.03762", "Attention Is All You Need", 2017, "Vaswani"))
	if len(r.Matches()) != 0 {
		t.Errorf("resolved %+v", r.Matches())
	}
	if len(r.Misses()) != 0 {
		t.Errorf("reported %+v as a near miss, and it is two unrelated papers", r.Misses())
	}
}

func TestConfidence(t *testing.T) {
	for via, want := range map[string]string{ViaArXiv: "certain", ViaDOI: "high", ViaTitle: "medium"} {
		if got := Confidence(via); got != want {
			t.Errorf("%s is %q, want %q", via, got, want)
		}
	}
	if got := (Entry{}).Confidence(); got != "" {
		t.Errorf("an entry that resolved to nothing is %q", got)
	}
	if got := (Entry{Resolved: "1706.03762", Via: ViaTitle}).Confidence(); got != "medium" {
		t.Errorf("got %q", got)
	}
}

func TestNormalise(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Attention Is All You Need", "attention is all you need"},
		{"Mamba: Linear-Time Sequence Modeling", "mamba linear time sequence modeling"},
		{"Sorting in $O(n)$ time", "sorting in o n time"},
		{"", ""},
	} {
		if got := Normalise(c.in); got != c.want {
			t.Errorf("Normalise(%q) is %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSimilarity(t *testing.T) {
	if got := similarity("attention is all you need", "attention is all you need"); got != 1 {
		t.Errorf("a string against itself is %v", got)
	}
	if got := similarity("abc", "xyz"); got != 0 {
		t.Errorf("two strings with nothing in common are %v", got)
	}
	if got := similarity("", "abc"); got != 0 {
		t.Errorf("an empty string is %v", got)
	}
}

// The cheap guard has to let through everything the expensive comparison would
// have accepted, or it is not a guard, it is a second threshold nobody wrote
// down.
func TestTheLengthGuardKeepsWhatTheThresholdWouldAccept(t *testing.T) {
	long := "attention is all you need for sequence transduction"
	for _, other := range []string{long, long + " x", strings.TrimSuffix(long, "n")} {
		if !comparable(long, other) {
			t.Errorf("%q was refused before it was compared, and it scores %.3f", other, similarity(long, other))
		}
	}
	if comparable("short", "a very much longer title than that one") {
		t.Error("two strings of wildly different length were sent to the expensive comparison")
	}
}

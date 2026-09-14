package metadata

import (
	"strings"
	"testing"
	"time"

	"github.com/tamnd/arxiv-reader/corpus"
)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func good() Record {
	return Record{
		ID:         "2106.09685",
		Title:      "LoRA: Low-Rank Adaptation of Large Language Models",
		Abstract:   "An important paradigm of natural language processing.",
		Authors:    []Author{{Surname: "Hu", Forename: "Edward J."}},
		Categories: []string{"cs.CL", "cs.AI", "cs.LG"},
		Versions:   []Version{{Version: 1, Created: day("2021-06-17")}},
		Source:     SourceOAI,
		Harvested:  "2026-09-14",
	}
}

// arXiv wraps titles and abstracts at about eighty columns and three surfaces
// wrap them in three different places. If the unwrapping is not identical, the
// same paper harvested twice is two different lines in git.
func TestUnwrap(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  LoRA: Low-Rank\n  Adaptation of Large\n  Language Models  ", "LoRA: Low-Rank Adaptation of Large Language Models"},
		{"one\ntwo", "one two"},
		{"one\r\ntwo", "one two"},
		{"one\n\n  two", "one two"},
		// Two spaces inside a line are left alone, because a handful of
		// abstracts align a small table with them and collapsing every run of
		// whitespace would flatten it.
		{"a  b", "a  b"},
		{"a\tb", "a\tb"},
		{"", ""},
		{"   ", ""},
	}
	for _, c := range cases {
		if got := unwrap(c.in); got != c.want {
			t.Errorf("unwrap(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormaliseCanonicalisesTheID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"arXiv:2106.09685v2", "2106.09685"},
		{"https://arxiv.org/abs/2106.09685", "2106.09685"},
		// The subject class goes, which is arXiv's own rule and not ours.
		{"math.GT/0309136", "math/0309136"},
		{"  2106.09685  ", "2106.09685"},
	}
	for _, c := range cases {
		r := Record{ID: c.in}.Normalise()
		if r.ID != c.want {
			t.Errorf("Normalise(%q).ID = %q, want %q", c.in, r.ID, c.want)
		}
	}
}

// Categories are deduplicated and never sorted. The first one is the category
// the paper was submitted to, and a moderator can cross list it into five more
// archives years later without that changing.
func TestNormaliseKeepsCategoryOrder(t *testing.T) {
	r := Record{Categories: []string{"cs.CL", "cs.AI", "cs.CL", " cs.LG ", ""}}.Normalise()
	want := []string{"cs.CL", "cs.AI", "cs.LG"}
	if len(r.Categories) != len(want) {
		t.Fatalf("got %v, want %v", r.Categories, want)
	}
	for i := range want {
		if r.Categories[i] != want[i] {
			t.Fatalf("got %v, want %v", r.Categories, want)
		}
	}
	if r.Primary() != "cs.CL" {
		t.Errorf("Primary() = %q, want cs.CL", r.Primary())
	}
	if r.Archive() != "cs" {
		t.Errorf("Archive() = %q, want cs", r.Archive())
	}
}

func TestArchiveOfAHyphenatedOldArchive(t *testing.T) {
	r := Record{Categories: []string{"cond-mat.supr-con"}}
	if got := r.Archive(); got != "cond-mat" {
		t.Errorf("Archive() = %q, want cond-mat", got)
	}
	r = Record{Categories: []string{"hep-th"}}
	if got := r.Archive(); got != "hep-th" {
		t.Errorf("Archive() = %q, want hep-th", got)
	}
}

func TestNormaliseSortsVersions(t *testing.T) {
	r := Record{Versions: []Version{
		{Version: 3, Created: day("2021-10-16")},
		{Version: 1, Created: day("2021-06-17")},
		{Version: 2, Created: day("2021-07-01")},
	}}.Normalise()
	for i, v := range r.Versions {
		if v.Version != i+1 {
			t.Fatalf("versions came out %v", r.Versions)
		}
	}
	latest, ok := r.Latest()
	if !ok || latest.Version != 3 {
		t.Errorf("Latest() = %v %v, want v3", latest, ok)
	}
}

// Sorting by the id as text puts the whole of the 1990s after the whole of the
// 2020s, because "h" is greater than "2". This is the single most common way to
// get an arXiv corpus subtly wrong.
func TestSortIsChronologicalAcrossBothIDSchemes(t *testing.T) {
	recs := []Record{
		{ID: "2106.09685"},
		{ID: "hep-th/9711200"},
		{ID: "0704.0001"},
		{ID: "math/0309136"},
	}
	Sort(recs)
	want := []string{"hep-th/9711200", "math/0309136", "0704.0001", "2106.09685"}
	for i := range want {
		if recs[i].ID != want[i] {
			t.Fatalf("sorted to %v, want %v", ids(recs), want)
		}
	}
}

func ids(recs []Record) []string {
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = r.ID
	}
	return out
}

func TestShard(t *testing.T) {
	cases := []struct{ id, want string }{
		{"2106.09685", "2106"},
		{"hep-th/9711200", "9711"},
		{"0704.0001", "0704"},
	}
	for _, c := range cases {
		got, err := Record{ID: c.id}.Shard()
		if err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
		if got != c.want {
			t.Errorf("%s: shard %q, want %q", c.id, got, c.want)
		}
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Record)
		want string
	}{
		{"good", func(*Record) {}, ""},
		{"bad id", func(r *Record) { r.ID = "not an id" }, "id"},
		{"no title", func(r *Record) { r.Title = "  " }, "no title"},
		{"no category", func(r *Record) { r.Categories = nil }, "no category"},
		{"no versions", func(r *Record) { r.Versions = nil }, "no versions"},
		{
			"a gap in the versions",
			func(r *Record) {
				r.Versions = []Version{{Version: 1, Created: day("2021-06-17")}, {Version: 3, Created: day("2021-07-01")}}
			},
			"run from v1",
		},
		{"no creation date", func(r *Record) { r.Versions[0].Created = time.Time{} }, "no creation date"},
		{"unknown source", func(r *Record) { r.Source = "scraped" }, "not a surface"},
		{"no harvest date", func(r *Record) { r.Harvested = "" }, "YYYY-MM-DD"},
		{"harvest date is a timestamp", func(r *Record) { r.Harvested = "2026-09-14T10:00:00Z" }, "YYYY-MM-DD"},
		{
			"an authority with no licence",
			func(r *Record) { r.Versions[0].LicenceFrom = SourceOAI },
			"where a licence came from and not what it was",
		},
		{
			"a licence with no authority",
			func(r *Record) { r.Versions[0].Licence = corpus.LicenceCCBY },
			"no authority",
		},
		{
			"a licence that is not one of the six",
			func(r *Record) {
				r.Versions[0].Licence = corpus.Licence("mit")
				r.Versions[0].LicenceFrom = SourceOAI
			},
			"mit",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := good()
			c.edit(&r)
			err := r.Validate()
			if c.want == "" {
				if err != nil {
					t.Fatalf("want no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want an error mentioning %q, got none", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not mention %q", err, c.want)
			}
		})
	}
}

// A version with no licence is one nobody has looked at. A version with
// LicenceUnknown is one somebody looked at and arXiv did not say, which is a
// decision that has been made and which resolves to the strictest of the six.
// The two have to stay apart or the content plane cannot be trusted.
func TestEmptyLicenceIsNotUnknownLicence(t *testing.T) {
	r := good()
	if r.Versions[0].Licence != "" {
		t.Fatal("a freshly harvested version should carry no licence at all")
	}
	if corpus.AccessFor(corpus.LicenceUnknown) == corpus.AccessOpen {
		t.Fatal("unknown must not be open, it is the default licence and the default forbids republishing")
	}
	r.Versions[0].Licence = corpus.LicenceUnknown
	r.Versions[0].LicenceFrom = SourceOAI
	if err := r.Validate(); err != nil {
		t.Fatalf("a resolved unknown is a legal state: %v", err)
	}
}

func TestAuthorString(t *testing.T) {
	cases := []struct {
		in   Author
		want string
	}{
		{Author{Surname: "Hu", Forename: "Edward J."}, "Edward J. Hu"},
		{Author{Surname: "Nguyen", Forename: "Duc-Tam"}, "Duc-Tam Nguyen"},
		{Author{Surname: "King", Forename: "Martin Luther", Suffix: "Jr."}, "Martin Luther King Jr."},
		{Author{Surname: "Bourbaki"}, "Bourbaki"},
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

func TestVersionAt(t *testing.T) {
	r := good()
	r.Versions = append(r.Versions, Version{Version: 2, Created: day("2021-07-01")})
	if v, ok := r.VersionAt(2); !ok || !v.Created.Equal(day("2021-07-01")) {
		t.Errorf("VersionAt(2) = %v %v", v, ok)
	}
	if _, ok := r.VersionAt(9); ok {
		t.Error("VersionAt(9) found a version that is not there")
	}
}

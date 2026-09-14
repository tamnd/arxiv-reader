package harvest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
)

// The fixtures under testdata are real responses from oaipmh.arxiv.org, saved
// so that the parser is tested against what arXiv sends rather than against
// what the documentation says it sends. The two have already disagreed once, on
// whether a version carries its own licence, and they will again.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// serve answers each request with the next fixture in the list.
func serve(t *testing.T, names ...string) (*httptest.Server, *int32) {
	t.Helper()
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(atomic.AddInt32(&n, 1)) - 1
		if i >= len(names) {
			t.Errorf("request %d asked for a page that does not exist: %s", i+1, r.URL.RawQuery)
			http.Error(w, "no more pages", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/xml")
		w.Write(fixture(t, names[i]))
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

// A pace of one nanosecond rather than of zero, because zero means the package
// default and the package default is three seconds. Zero is the safe value for
// anyone who forgets to set the field, and a test is the one caller that has a
// good reason to opt out.
func client(endpoint string) *OAI {
	return &OAI{
		Endpoint: endpoint,
		Pace:     time.Nanosecond,
		Now:      func() time.Time { return time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC) },
	}
}

func TestListFollowsTheResumptionToken(t *testing.T) {
	srv, calls := serve(t, "list_page1.xml", "list_page2.xml")
	o := client(srv.URL)

	recs, err := o.Records(context.Background(), Query{Format: FormatRaw, From: "2024-03-05", Until: "2024-03-05"})
	if err != nil {
		t.Fatal(err)
	}
	if *calls != 2 {
		t.Errorf("made %d requests, want 2", *calls)
	}
	want := []string{"math/9602216", "1711.03463", "1907.01743", "2103.08413"}
	if len(recs) != len(want) {
		t.Fatalf("got %d records, want %d", len(recs), len(want))
	}
	for i, r := range recs {
		if err := r.Validate(); err != nil {
			t.Errorf("%s: %v", r.ID, err)
		}
		_ = i
	}
	// The records come back in the order arXiv sent them, which is not sorted.
	// Sorting is the plane's job and it happens on the way to disk.
	got := make(map[string]metadata.Record, len(recs))
	for _, r := range recs {
		got[r.ID] = r
	}
	for _, id := range want {
		if _, ok := got[id]; !ok {
			t.Errorf("%s did not come back", id)
		}
	}
}

// The second page's resumption token is present and empty, which is how arXiv
// says there is no more. A parser that tests for the element rather than for
// its content asks for a third page and gets an error.
func TestAnEmptyResumptionTokenEndsTheWalk(t *testing.T) {
	srv, calls := serve(t, "list_page1.xml", "list_page2.xml")
	o := client(srv.URL)
	if _, err := o.Records(context.Background(), Query{Format: FormatRaw}); err != nil {
		t.Fatal(err)
	}
	if *calls != 2 {
		t.Fatalf("made %d requests, want to stop after 2", *calls)
	}
}

// A resumption token replaces every other argument. Sending from and until
// alongside it is a badArgument error rather than the next page.
func TestTheSecondRequestSendsOnlyTheToken(t *testing.T) {
	var second string
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(atomic.AddInt32(&n, 1))
		if i == 1 {
			w.Write(fixture(t, "list_page1.xml"))
			return
		}
		second = r.URL.RawQuery
		w.Write(fixture(t, "list_page2.xml"))
	}))
	defer srv.Close()

	o := client(srv.URL)
	if _, err := o.Records(context.Background(), Query{Format: FormatRaw, From: "2024-03-05", Until: "2024-03-05", Set: "math"}); err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{"from=", "until=", "set=", "metadataPrefix="} {
		if strings.Contains(second, unwanted) {
			t.Errorf("the second request carried %s alongside the token: %s", unwanted, second)
		}
	}
	if !strings.Contains(second, "resumptionToken=") {
		t.Errorf("the second request had no token: %s", second)
	}
}

func TestRawRecord(t *testing.T) {
	srv, _ := serve(t, "list_page1.xml", "list_page2.xml")
	o := client(srv.URL)
	recs, err := o.Records(context.Background(), Query{Format: FormatRaw})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]metadata.Record{}
	for _, r := range recs {
		byID[r.ID] = r
	}

	// An old style id keeps its slash and loses nothing else.
	shelah := byID["math/9602216"]
	if shelah.Title != `Categoricity and amalgamation for AEC and $ \kappa $ measurable` {
		t.Errorf("title = %q, and the TeX in it should be untouched", shelah.Title)
	}
	if len(shelah.Versions) != 3 {
		t.Fatalf("got %d versions, want 3", len(shelah.Versions))
	}
	if got := shelah.Versions[0].Created.Format("2006-01-02"); got != "1996-02-15" {
		t.Errorf("v1 created %s, want 1996-02-15", got)
	}
	if shelah.Primary() != "math.LO" {
		t.Errorf("primary = %q", shelah.Primary())
	}
	if shelah.MSCClass == "" || shelah.JournalRef == "" || shelah.ReportNo == "" {
		t.Errorf("the optional fields were dropped: %+v", shelah)
	}
	if got, err := shelah.Shard(); err != nil || got != "9602" {
		t.Errorf("shard = %q %v, want 9602", got, err)
	}

	// The abstract is unwrapped to one line, because arXiv hard wraps it and
	// three surfaces wrap it in three different places.
	if strings.Contains(shelah.Abstract, "\n") {
		t.Error("the abstract still has a line break in the middle of a sentence")
	}

	// Categories come as one space separated string and the first one is the
	// primary, so the order has to survive.
	dou := byID["1907.01743"]
	want := []string{"eess.IV", "cs.AI", "cs.CV", "cs.LG"}
	if len(dou.Categories) != len(want) {
		t.Fatalf("categories = %v, want %v", dou.Categories, want)
	}
	for i := range want {
		if dou.Categories[i] != want[i] {
			t.Fatalf("categories = %v, want %v", dou.Categories, want)
		}
	}
}

// The licence is the only field the content plane cannot proceed without, so it
// gets its own test against the four real papers in the fixtures.
func TestTheLicenceComesOffTheWire(t *testing.T) {
	srv, _ := serve(t, "list_page1.xml", "list_page2.xml")
	o := client(srv.URL)
	recs, err := o.Records(context.Background(), Query{Format: FormatRaw})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]corpus.Licence{
		"math/9602216": corpus.LicenceCCBY,
		"1907.01743":   corpus.LicenceCCBY,
		"1711.03463":   corpus.LicenceCC0,
		"2103.08413":   corpus.LicenceCCBYNCND,
	}
	for _, r := range recs {
		latest, ok := r.Latest()
		if !ok {
			t.Fatalf("%s has no versions", r.ID)
		}
		if latest.Licence != want[r.ID] {
			t.Errorf("%s: licence %q, want %q", r.ID, latest.Licence, want[r.ID])
		}
		if latest.LicenceFrom != metadata.SourceOAI {
			t.Errorf("%s: authority %q, want oai", r.ID, latest.LicenceFrom)
		}
	}

	// The one that matters. A translation is a derivative work, so the ND in
	// CC BY-NC-ND forbids one, and this paper can only ever be a record and a
	// link no matter how good the extraction gets.
	for _, r := range recs {
		if r.ID != "2103.08413" {
			continue
		}
		latest, _ := r.Latest()
		if corpus.AccessFor(latest.Licence).MayTranslate() {
			t.Error("a CC BY-NC-ND paper came out translatable")
		}
	}
}

// OAI-PMH states one licence for the whole paper. Its version elements carry a
// date and a size and no licence attribute at all, which the spec for this
// project had wrong. Until the M2 census reads the abs page, every version of a
// paper carries the paper's licence, and the test says so out loud rather than
// letting a later reader assume the per version values were measured.
func TestEveryVersionCarriesThePapersLicence(t *testing.T) {
	srv, _ := serve(t, "list_page1.xml", "list_page2.xml")
	o := client(srv.URL)
	recs, err := o.Records(context.Background(), Query{Format: FormatRaw})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		first := r.Versions[0].Licence
		for _, v := range r.Versions {
			if v.Licence != first {
				t.Errorf("%s: v%d differs from v1, so OAI grew a per version licence and the census plan should change", r.ID, v.Version)
			}
		}
	}
}

// The arXiv format exists for one field. It splits the authors, which arXivRaw
// cannot, and it mangles the title and the abstract, which arXivRaw does not.
func TestTheArXivFormatIsAnAuthorsPass(t *testing.T) {
	srv, _ := serve(t, "list_arxiv_format.xml")
	o := client(srv.URL)
	recs, err := o.Records(context.Background(), Query{Format: FormatArXiv})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	r := recs[0]
	if len(r.Authors) != 2 {
		t.Fatalf("authors = %v, want two", r.Authors)
	}
	if r.Authors[0].Surname != "Kolman" || r.Authors[0].Forename != "Oren" {
		t.Errorf("first author = %+v", r.Authors[0])
	}
	if got := r.Authors[1].String(); got != "Saharon Shelah" {
		t.Errorf("second author = %q", got)
	}
	// This format writes the title as "$ κ$ measurable" where the raw format
	// writes "$ \kappa $ measurable". A Unicode kappa inside maths mode is not
	// something LaTeX will typeset, so the field is dropped rather than
	// carried, and Fill then leaves the good title alone.
	if r.Title != "" || r.Abstract != "" {
		t.Errorf("the arXiv format's mangled title or abstract got through: %q / %q", r.Title, r.Abstract)
	}
	// It has no version history either, so on its own it is not a record.
	if len(r.Versions) != 0 {
		t.Errorf("versions = %v, want none from this format", r.Versions)
	}
	if err := r.Validate(); err == nil {
		t.Error("an authors pass on its own should not validate as a whole record")
	}
}

// The two passes together are what a real harvest writes, and the order they
// run in must not matter.
func TestTheTwoFormatsCombineIntoOneRecord(t *testing.T) {
	rawSrv, _ := serve(t, "list_page1.xml", "list_page2.xml")
	authorSrv, _ := serve(t, "list_arxiv_format.xml")

	raw, err := client(rawSrv.URL).Records(context.Background(), Query{Format: FormatRaw})
	if err != nil {
		t.Fatal(err)
	}
	authors, err := client(authorSrv.URL).Records(context.Background(), Query{Format: FormatArXiv})
	if err != nil {
		t.Fatal(err)
	}

	var shelah metadata.Record
	for _, r := range raw {
		if r.ID == "math/9602216" {
			shelah = r
		}
	}
	merged := metadata.Fill(shelah, authors[0])
	if err := merged.Validate(); err != nil {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(merged.Title, `\kappa`) {
		t.Errorf("title = %q, want the raw format's TeX", merged.Title)
	}
	if len(merged.Authors) != 2 {
		t.Errorf("authors = %v, want the arXiv format's two", merged.Authors)
	}
	if len(merged.Versions) != 3 {
		t.Errorf("versions = %v, want the raw format's three", merged.Versions)
	}

	other := metadata.Fill(authors[0], shelah)
	if other.Title != merged.Title || len(other.Authors) != 2 || len(other.Versions) != 3 {
		t.Errorf("the order of the two passes changed the answer: %+v", other)
	}
}

// arXiv answers a busy endpoint with 503 and a Retry-After in seconds, and it
// means it. A harvest that treats that as a failure loses the page it was on
// and a harvest that ignores the header gets blocked.
func TestA503IsRetried(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch atomic.AddInt32(&n, 1) {
		case 1:
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
		case 2:
			w.Write(fixture(t, "list_page1.xml"))
		default:
			w.Write(fixture(t, "list_page2.xml"))
		}
	}))
	defer srv.Close()

	o := client(srv.URL)
	recs, err := o.Records(context.Background(), Query{Format: FormatRaw})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 4 {
		t.Errorf("got %d records, want the 4 from both pages", len(recs))
	}
	if n != 3 {
		t.Errorf("made %d requests, want 3", n)
	}
}

// The pace is the promise this project makes to arXiv in exchange for being
// allowed to read three million records, so the default has to survive someone
// constructing an OAI without thinking about it.
func TestTheDefaultPaceIsKept(t *testing.T) {
	if testing.Short() {
		t.Skip("this one waits out a real pace")
	}
	srv, _ := serve(t, "list_page1.xml", "list_page2.xml")
	o := &OAI{Endpoint: srv.URL, Now: func() time.Time { return time.Time{} }}

	start := time.Now()
	if _, err := o.Records(context.Background(), Query{Format: FormatRaw}); err != nil {
		t.Fatal(err)
	}
	// Two pages is one gap. The first request does not wait, because there is
	// nothing to wait after.
	if elapsed := time.Since(start); elapsed < Pace-time.Second {
		t.Errorf("two pages took %s, and a zero Pace field should have meant %s between them", elapsed, Pace)
	}
}

func TestRetryAfter(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"20", 20 * time.Second},
		{" 5 ", 5 * time.Second},
		// A missing or unparseable header falls back to the pace rather than to
		// nothing, because hammering a busy endpoint is how a harvest gets
		// blocked for the day.
		{"", Pace},
		{"soon", Pace},
		{"0", Pace},
		// Capped, so that a bad header cannot hang a harvest until tomorrow.
		{"86400", 5 * time.Minute},
	}
	for _, c := range cases {
		if got := retryAfter(c.in, Pace); got != c.want {
			t.Errorf("retryAfter(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

// An error element is arXiv telling us the request was wrong, and it comes back
// with a 200. A harvest that only checks the status code reads it as a page
// with no records and stops quietly.
func TestAnOAIErrorIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<OAI-PMH xmlns="http://www.openarchives.org/OAI/2.0/">
  <responseDate>2026-09-14T10:49:50Z</responseDate>
  <request>http://oaipmh.arxiv.org/oai</request>
  <error code="badArgument">The from argument is not a valid date</error>
</OAI-PMH>`))
	}))
	defer srv.Close()

	_, err := client(srv.URL).Records(context.Background(), Query{Format: FormatRaw})
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "badArgument") {
		t.Errorf("error %q should name the OAI code", err)
	}
}

// noRecordsMatch is arXiv saying the range is empty. On a catch up that runs
// every day, a quiet weekend is the normal case and not a failure.
func TestNoRecordsMatchIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<OAI-PMH xmlns="http://www.openarchives.org/OAI/2.0/">
  <error code="noRecordsMatch">no records match</error>
</OAI-PMH>`))
	}))
	defer srv.Close()

	recs, err := client(srv.URL).Records(context.Background(), Query{Format: FormatRaw, From: "2030-01-01"})
	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("got %d records from an empty range", len(recs))
	}
}

func TestQueryValidate(t *testing.T) {
	cases := []struct {
		name string
		q    Query
		want string
	}{
		{"good", Query{Format: FormatRaw, From: "2024-03-05", Until: "2024-03-05"}, ""},
		{"no dates", Query{Format: FormatArXiv}, ""},
		{"no format", Query{}, "metadata format"},
		{"a format arXiv does not serve", Query{Format: "oai_dc"}, "metadata format"},
		{"a timestamp", Query{Format: FormatRaw, From: "2024-03-05T00:00:00Z"}, "YYYY-MM-DD"},
		{"backwards", Query{Format: FormatRaw, From: "2024-03-06", Until: "2024-03-05"}, "is after"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.q.Validate()
			if c.want == "" {
				if err != nil {
					t.Fatalf("want no error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %v should mention %q", err, c.want)
			}
		})
	}
}

// A limit stops the walk at the end of the page that crossed it, because a page
// is what arXiv gives and there is no way to ask for half of one.
func TestLimitStopsTheWalk(t *testing.T) {
	srv, calls := serve(t, "list_page1.xml", "list_page2.xml")
	recs, err := client(srv.URL).Records(context.Background(), Query{Format: FormatRaw, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if *calls != 1 {
		t.Errorf("made %d requests, want to stop after 1", *calls)
	}
	if len(recs) != 3 {
		t.Errorf("got %d records, want the whole first page", len(recs))
	}
}

// A cancelled context stops a harvest between pages. A full one runs for days
// at three seconds a page and someone will want it back.
func TestContextStopsTheWalk(t *testing.T) {
	srv, calls := serve(t, "list_page1.xml", "list_page2.xml")
	ctx, cancel := context.WithCancel(context.Background())
	o := client(srv.URL)

	// Cancel from inside the callback, part way through the first page.
	seen := 0
	err := o.List(ctx, Query{Format: FormatRaw}, func(metadata.Record) error {
		seen++
		if seen == 2 {
			cancel()
		}
		return nil
	})
	if err == nil {
		t.Fatal("want the cancellation reported, not a harvest that looks complete")
	}
	if *calls != 1 {
		t.Errorf("made %d requests after being cancelled, want 1", *calls)
	}
}

func TestParseDate(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Thu, 15 Feb 1996 00:00:00 GMT", "1996-02-15T00:00:00Z"},
		{"Wed, 03 Jul 2019 05:21:52 GMT", "2019-07-03T05:21:52Z"},
		{"Sun, 03 Mar 2024 11:58:08 GMT", "2024-03-03T11:58:08Z"},
	}
	for _, c := range cases {
		got, err := parseDate(c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if got.Format(time.RFC3339) != c.want {
			t.Errorf("%s gave %s, want %s", c.in, got.Format(time.RFC3339), c.want)
		}
	}
	if _, err := parseDate("last Tuesday"); err == nil {
		t.Error("want an error for a date that is not one")
	}
}

func TestVersionNumber(t *testing.T) {
	for in, want := range map[string]int{"v1": 1, "v12": 12, "3": 3} {
		got, err := versionNumber(in)
		if err != nil || got != want {
			t.Errorf("versionNumber(%q) = %d %v, want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "v", "v0", "latest"} {
		if _, err := versionNumber(in); err == nil {
			t.Errorf("versionNumber(%q) should be an error", in)
		}
	}
}

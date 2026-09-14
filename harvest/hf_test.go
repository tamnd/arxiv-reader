package harvest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
)

// rows_0.json and rows_2.json are two pages taken straight off the datasets
// server, two rows each, and rows_end.json is what it answers past the end of
// the split. rows_error.json is a dataset name with a typo in it.
//
// Row zero of the mirror is 0909.0774, which is the same paper the OAI fixture
// holds, and row one is 1101.4616. That is luck rather than design and it is
// worth keeping, because it lets one test watch the third surface merge with
// the first.

// serveJSON answers each request with the next fixture, as JSON.
func serveJSON(t *testing.T, names ...string) (*httptest.Server, *int32, *[]string) {
	t.Helper()
	var n int32
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(atomic.AddInt32(&n, 1)) - 1
		queries = append(queries, r.URL.RawQuery)
		if i >= len(names) {
			t.Errorf("request %d asked for a page that does not exist: %s", i+1, r.URL.RawQuery)
			http.Error(w, "no more pages", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(fixture(t, names[i]))
	}))
	t.Cleanup(srv.Close)
	return srv, &n, &queries
}

// A pace of one nanosecond, for the reason given above client.
func rows(endpoint string) *Rows {
	return &Rows{
		Endpoint: endpoint,
		Pace:     time.Nanosecond,
		Now:      func() time.Time { return time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC) },
	}
}

func collect(t *testing.T, r *Rows, q RowsQuery) ([]metadata.Record, SnapshotStats) {
	t.Helper()
	var out []metadata.Record
	stats, err := r.List(context.Background(), q, func(rec metadata.Record) error {
		out = append(out, rec)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out, stats
}

func TestRowsWalksThePages(t *testing.T) {
	srv, calls, _ := serveJSON(t, "rows_0.json", "rows_2.json", "rows_end.json")
	recs, stats := collect(t, rows(srv.URL), RowsQuery{})

	if *calls != 3 {
		t.Errorf("made %d requests, want 3", *calls)
	}
	want := []string{"0909.0774", "1101.4616", "1303.2033", "1402.0690"}
	if len(recs) != len(want) {
		t.Fatalf("got %d records, want %d", len(recs), len(want))
	}
	for i, id := range want {
		if recs[i].ID != id {
			t.Errorf("record %d is %s, want %s", i, recs[i].ID, id)
		}
		if err := recs[i].Validate(); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
	if stats.Read != 4 || stats.Skipped != 0 {
		t.Errorf("stats %+v, want 4 read and none skipped", stats)
	}
}

// The source has to say hf and not kaggle, because it is what audit rule S10
// reads and the two mirrors are not the same authority for a licence.
func TestRowsAreTaggedHF(t *testing.T) {
	srv, _, _ := serveJSON(t, "rows_0.json", "rows_end.json")
	recs, _ := collect(t, rows(srv.URL), RowsQuery{})

	for _, r := range recs {
		if r.Source != metadata.SourceHF {
			t.Errorf("%s: source %q, want hf", r.ID, r.Source)
		}
		for _, v := range r.Versions {
			if v.LicenceFrom != metadata.SourceHF {
				t.Errorf("%s v%d: licence from %q, want hf", r.ID, v.Version, v.LicenceFrom)
			}
		}
	}
}

// The offset walks forward by the number of rows the last page held rather than
// by the page size, which is the only way the walk stays right on a short page.
func TestRowsAskForTheNextOffset(t *testing.T) {
	srv, _, queries := serveJSON(t, "rows_0.json", "rows_2.json", "rows_end.json")
	collect(t, rows(srv.URL), RowsQuery{})

	want := []string{"0", "2", "4"}
	if len(*queries) != len(want) {
		t.Fatalf("made %d requests, want %d", len(*queries), len(want))
	}
	for i, offset := range want {
		got := query(t, (*queries)[i], "offset")
		if got != offset {
			t.Errorf("request %d asked for offset %s, want %s", i+1, got, offset)
		}
	}
}

func TestRowsSendTheDatasetAndTheSplit(t *testing.T) {
	srv, _, queries := serveJSON(t, "rows_end.json")
	r := rows(srv.URL)
	collect(t, r, RowsQuery{})

	for name, want := range map[string]string{
		"dataset": Mirror,
		"config":  "default",
		"split":   "train",
		"length":  strconv.Itoa(RowsPage),
	} {
		if got := query(t, (*queries)[0], name); got != want {
			t.Errorf("%s is %q, want %q", name, got, want)
		}
	}
}

func TestARowsDatasetCanBeChosen(t *testing.T) {
	srv, _, queries := serveJSON(t, "rows_end.json")
	r := rows(srv.URL)
	r.Dataset = "someone/their-own-mirror"
	collect(t, r, RowsQuery{})

	if got := query(t, (*queries)[0], "dataset"); got != "someone/their-own-mirror" {
		t.Errorf("dataset is %q", got)
	}
}

// An empty page is the end of the split, and it is the only end condition the
// server gives. It does not say "no more", it just stops having rows.
func TestAnEmptyPageEndsTheWalk(t *testing.T) {
	srv, calls, _ := serveJSON(t, "rows_end.json")
	recs, _ := collect(t, rows(srv.URL), RowsQuery{})

	if len(recs) != 0 {
		t.Errorf("got %d records off the end of the split", len(recs))
	}
	if *calls != 1 {
		t.Errorf("made %d requests after an empty page, want 1", *calls)
	}
}

// A limit under the page size has to shrink the request rather than ask for a
// hundred and throw ninety eight away.
func TestALimitShrinksThePage(t *testing.T) {
	srv, calls, queries := serveJSON(t, "rows_0.json")
	recs, _ := collect(t, rows(srv.URL), RowsQuery{Limit: 2})

	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	if *calls != 1 {
		t.Errorf("made %d requests for a limit of 2, want 1", *calls)
	}
	if got := query(t, (*queries)[0], "length"); got != "2" {
		t.Errorf("asked for length %s, want 2", got)
	}
}

func TestAnOffsetIsWhereTheWalkStarts(t *testing.T) {
	srv, _, queries := serveJSON(t, "rows_2.json", "rows_end.json")
	collect(t, rows(srv.URL), RowsQuery{Offset: 2})

	if got := query(t, (*queries)[0], "offset"); got != "2" {
		t.Errorf("started at offset %s, want 2", got)
	}
}

func TestANegativeOffsetIsAnError(t *testing.T) {
	r := rows("http://127.0.0.1:0")
	if _, err := r.List(context.Background(), RowsQuery{Offset: -1}, func(metadata.Record) error {
		return nil
	}); err == nil {
		t.Fatal("an offset before the start of the split was accepted")
	}
}

// The whole point of reading this mirror is that it is the same data, so the
// licence has to come out the same way it does off the Kaggle file: one value
// for the paper, written onto every version.
func TestTheRowsLicenceIsPaperLevel(t *testing.T) {
	srv, _, _ := serveJSON(t, "rows_2.json", "rows_end.json")
	recs, _ := collect(t, rows(srv.URL), RowsQuery{})

	var got metadata.Record
	for _, r := range recs {
		if r.ID == "1303.2033" {
			got = r
		}
	}
	// Nineteen versions, which is why this row is in the fixture.
	if len(got.Versions) != 19 {
		t.Fatalf("1303.2033 has %d versions, want 19", len(got.Versions))
	}
	for _, v := range got.Versions {
		if v.Licence != corpus.LicenceArXiv {
			t.Errorf("v%d is %s, want the default licence on every version", v.Version, v.Licence)
		}
	}
}

// Hugging Face shortens large cells and names the ones it shortened. A cut off
// abstract looks exactly like a complete one, so a row that says it was
// truncated is skipped rather than written.
func TestATruncatedRowIsSkipped(t *testing.T) {
	page := fixture(t, "rows_0.json")
	var doc map[string]any
	if err := json.Unmarshal(page, &doc); err != nil {
		t.Fatal(err)
	}
	// Hand edited from the fixture above: the server sets this field itself and
	// it is not worth waiting for a row long enough to trigger it.
	doc["rows"].([]any)[0].(map[string]any)["truncated_cells"] = []any{"abstract"}
	edited, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "offset=0") {
			w.Write(edited)
			return
		}
		w.Write(fixture(t, "rows_end.json"))
	}))
	t.Cleanup(srv.Close)

	recs, stats := collect(t, rows(srv.URL), RowsQuery{})
	if stats.Skipped != 1 {
		t.Errorf("skipped %d truncated rows, want 1", stats.Skipped)
	}
	for _, r := range recs {
		if r.ID == "0909.0774" {
			t.Error("a row the server said it had truncated was written anyway")
		}
	}
}

// The error comes back in the body with a 404 over it, and the body is the half
// worth reading: it says the dataset does not exist rather than saying 404.
func TestARowsErrorSaysWhatWasWrong(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write(fixture(t, "rows_error.json"))
	}))
	t.Cleanup(srv.Close)

	_, err := rows(srv.URL).List(context.Background(), RowsQuery{}, func(metadata.Record) error {
		return nil
	})
	if err == nil {
		t.Fatal("a dataset that does not exist read as an empty split")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("the error is %q, which does not carry what the server said", err)
	}
}

func TestRowsRetryA429(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		w.Write(fixture(t, "rows_end.json"))
	}))
	t.Cleanup(srv.Close)

	if _, _, err := listOnce(rows(srv.URL)); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("made %d requests, want a retry after the 429", n)
	}
}

// A 502 is the one that actually stopped a live walk, about forty pages into a
// 5000 row run. The datasets server sits behind a proxy and the proxy answers
// for it while it is slow or restarting.
func TestRowsRetryAGatewayError(t *testing.T) {
	for _, status := range []int{http.StatusBadGateway, http.StatusGatewayTimeout, http.StatusServiceUnavailable} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var n int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if atomic.AddInt32(&n, 1) == 1 {
					http.Error(w, "bad gateway", status)
					return
				}
				w.Write(fixture(t, "rows_end.json"))
			}))
			t.Cleanup(srv.Close)

			if _, _, err := listOnce(rows(srv.URL)); err != nil {
				t.Fatal(err)
			}
			if n != 2 {
				t.Errorf("made %d requests, want a retry after the %d", n, status)
			}
		})
	}
}

// A status that is not temporary has to stop rather than be tried five times.
// A 404 is not going to become a 200.
func TestARowsNotFoundIsNotRetried(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		w.WriteHeader(http.StatusNotFound)
		w.Write(fixture(t, "rows_error.json"))
	}))
	t.Cleanup(srv.Close)

	if _, _, err := listOnce(rows(srv.URL)); err == nil {
		t.Fatal("a 404 read as an empty split")
	}
	if n != 1 {
		t.Errorf("made %d requests for a 404, want 1", n)
	}
}

// Five tries and then stop. A harvest that retries forever is a harvest nobody
// notices has stopped.
func TestRowsGiveUpAfterFiveTries(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		w.Header().Set("Retry-After", "0")
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	_, _, err := listOnce(rows(srv.URL))
	if err == nil {
		t.Fatal("an endpoint that is always down read as an empty split")
	}
	if n != 5 {
		t.Errorf("made %d requests, want 5", n)
	}
	// The message has to name the status, or a 502 that lasted all night reads
	// the same as a 429 that did.
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("the error is %q, which does not say what the server kept answering", err)
	}
}

// A walk that the far end stops still has to report what it read, because the
// caller writes that and resumes from the count. Losing four thousand good rows
// because the four thousand and first failed is how an interrupted harvest
// becomes an afternoon of repeating work.
func TestAStoppedWalkKeepsWhatItRead(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			w.Write(fixture(t, "rows_0.json"))
			return
		}
		http.Error(w, "rate limited by the front end, as HTML with no Retry-After", http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	var got []metadata.Record
	stats, err := rows(srv.URL).List(context.Background(), RowsQuery{}, func(rec metadata.Record) error {
		got = append(got, rec)
		return nil
	})
	if err == nil {
		t.Fatal("a walk the server stopped came back without an error")
	}
	if stats.Read != 2 {
		t.Errorf("the stopped walk reports %d rows read, want the 2 it got", stats.Read)
	}
	if len(got) != 2 {
		t.Errorf("handed over %d records before it stopped, want 2", len(got))
	}
}

func TestBackoff(t *testing.T) {
	// A server that says how long knows when it will be ready and this does
	// not, so the header wins and it is not doubled.
	for try := 1; try <= 5; try++ {
		if got := backoff("2", time.Second, try); got != 2*time.Second {
			t.Errorf("try %d with a Retry-After waited %s, want 2s", try, got)
		}
	}
	// A server that says nothing gets a doubling wait.
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	for i, w := range want {
		if got := backoff("", time.Second, i+1); got != w {
			t.Errorf("try %d waited %s, want %s", i+1, got, w)
		}
	}
}

func TestRowsTotal(t *testing.T) {
	srv, _, queries := serveJSON(t, "rows_0.json")
	total, err := rows(srv.URL).Total(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if total != 3164528 {
		t.Errorf("the split holds %d rows by this count", total)
	}
	// One row and not a hundred. The count is in the envelope, so there is no
	// reason to make the server build a full page to get at it.
	if got := query(t, (*queries)[0], "length"); got != "1" {
		t.Errorf("asked for length %s, want 1", got)
	}
}

// Zero has to keep meaning the published pace. A caller who forgets the field
// gets the slow behaviour, which is the whole point of the default.
func TestTheDefaultRowsPaceIsKept(t *testing.T) {
	if (&Rows{}).Pace != 0 {
		t.Fatal("the zero value stopped being zero")
	}
	// Three seconds, because one was measured to die on a CloudFront 429 forty
	// pages into a walk. Anything faster than that is a number somebody guessed
	// after the person who measured it had gone.
	if RowsPace < 3*time.Second {
		t.Errorf("the default pace is %s, which is faster than the pace that was measured to work", RowsPace)
	}
}

func TestContextStopsTheRowsWalk(t *testing.T) {
	srv, calls, _ := serveJSON(t, "rows_0.json", "rows_2.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := rows(srv.URL).List(ctx, RowsQuery{}, func(metadata.Record) error {
		cancel()
		return nil
	})
	if err == nil {
		t.Fatal("a cancelled walk finished normally")
	}
	if *calls != 1 {
		t.Errorf("made %d requests after the cancel, want 1", *calls)
	}
}

// The third surface has to merge with the first the same way the second did.
// 0909.0774 is in both this fixture and the OAI one, and the OAI record is the
// better authority for the licence while the mirror has the split authors.
func TestTheMirrorAndTheCatchUpCombine(t *testing.T) {
	srv, _, _ := serveJSON(t, "rows_0.json", "rows_end.json")
	fromHF, _ := collect(t, rows(srv.URL), RowsQuery{})

	oaiSrv, _ := serve(t, "list_0909_0774.xml")
	fromOAI, err := client(oaiSrv.URL).Records(context.Background(), Query{Format: FormatRaw, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}

	var mirror, catchUp metadata.Record
	for _, r := range fromHF {
		if r.ID == "0909.0774" {
			mirror = r
		}
	}
	for _, r := range fromOAI {
		if r.ID == "0909.0774" {
			catchUp = r
		}
	}
	if mirror.ID == "" || catchUp.ID == "" {
		t.Fatal("0909.0774 is missing from one of the two fixtures")
	}

	merged := metadata.Fill(mirror, catchUp)
	if merged.Source != metadata.SourceOAI {
		t.Errorf("source is %q after the catch up, want oai", merged.Source)
	}
	if len(merged.Authors) == 0 {
		t.Fatal("the merge lost the authors the mirror had already split")
	}
	if merged.Authors[0].Surname != "Sela" {
		t.Errorf("the first author is %+v, want the split name from the mirror", merged.Authors[0])
	}
	for _, v := range merged.Versions {
		if v.LicenceFrom != metadata.SourceOAI {
			t.Errorf("v%d still credits %q for its licence, want oai", v.Version, v.LicenceFrom)
		}
	}
}

// listOnce runs a walk that wants nothing, for the tests that only care that
// the request happened.
func listOnce(r *Rows) ([]metadata.Record, SnapshotStats, error) {
	var out []metadata.Record
	stats, err := r.List(context.Background(), RowsQuery{}, func(rec metadata.Record) error {
		out = append(out, rec)
		return nil
	})
	return out, stats, err
}

func query(t *testing.T, raw, name string) string {
	t.Helper()
	v, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	return v.Get(name)
}

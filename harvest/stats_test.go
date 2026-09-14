package harvest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const statsCSV = `month,submissions,historical_delta
1991-07,2,-2
1991-08,28,-1
2021-06,16250,0
`

func TestParseStats(t *testing.T) {
	got, err := ParseStats(strings.NewReader(statsCSV))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("read %d months, want 3", len(got))
	}
	if got[0].Shard != "9107" {
		t.Errorf("1991-07 is shard %q, want 9107", got[0].Shard)
	}
	if got[2].Shard != "2106" {
		t.Errorf("2021-06 is shard %q, want 2106", got[2].Shard)
	}
	// arXiv's own correction is applied here and not left to the caller,
	// because a caller that forgets it reports a gap that does not exist.
	if got[1].Live() != 27 {
		t.Errorf("1991-08 is %d live, want 28 less the 1 arXiv removed", got[1].Live())
	}
	if got[0].Live() != 0 {
		t.Errorf("1991-07 is %d live, want 0", got[0].Live())
	}
}

// The header is checked rather than skipped, because the day arXiv adds a
// column in the middle is the day a report starts counting the wrong one.
func TestParseStatsRefusesAFileItDoesNotUnderstand(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"empty", "", "empty"},
		{"a different header", "month,count\n1991-07,2\n", "want month,submissions"},
		{"a reordered header", "month,historical_delta,submissions\n1991-07,-2,2\n", "want month,submissions"},
		{"a month that is not one", statsCSV + "not-a-month,1,0\n", "is not a month"},
		{"a count that is not one", statsCSV + "2021-07,many,0\n", "is not a count"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseStats(strings.NewReader(tc.body))
			if err == nil {
				t.Fatal("it was accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("said %q, which does not mention %q", err, tc.want)
			}
		})
	}
}

// The committed copy is the one the report uses when nobody asked for a fetch,
// so it is worth knowing it parses and that it is the shape the report assumes.
func TestTheCommittedStatsAreSound(t *testing.T) {
	got, err := PublishedStats()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 400 {
		t.Fatalf("the committed copy has %d months, want at least 400", len(got))
	}
	if got[0].Shard != "9107" {
		t.Errorf("the file starts at %s, want 9107, which is arXiv's first month", got[0].Shard)
	}

	total := 0
	for i, p := range got {
		total += p.Live()
		if p.Submissions < 0 {
			t.Errorf("%s announced %d papers", p.Shard, p.Submissions)
		}
		// arXiv's correction only ever removes, and a positive one would mean
		// the field means something other than what this code assumes.
		if p.Delta > 0 {
			t.Errorf("%s has a correction of %+d, and corrections only remove", p.Shard, p.Delta)
		}
		// Every month between the first and the last, with none skipped, is
		// what lets a month with no row be read as a month with no papers.
		if i > 0 {
			want := got[i-1].Month.AddDate(0, 1, 0)
			if !p.Month.Equal(want) {
				t.Errorf("%s follows %s, and %s is missing", p.Shard, got[i-1].Shard, want.Format("2006-01"))
			}
		}
	}
	// The mirror served 3,164,528 rows the day this was written and arXiv's
	// own total was 3,163,381, which is 0.04% apart. That agreement is the
	// whole basis for using these numbers as a yardstick, so a copy that has
	// drifted into a different order of magnitude should fail here.
	if total < 3_000_000 || total > 4_000_000 {
		t.Errorf("the committed copy totals %d, which is not the size arXiv is", total)
	}
}

func TestFetchStats(t *testing.T) {
	var agent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent = r.Header.Get("User-Agent")
		w.Write([]byte(statsCSV))
	}))
	defer srv.Close()

	stats, body, err := fetchFrom(srv.URL, "arxiv-reader/test")
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 3 {
		t.Errorf("read %d months, want 3", len(stats))
	}
	if body != statsCSV {
		t.Errorf("the body came back changed:\n%s", body)
	}
	// arXiv asks for a user agent that names the tool, and a fetch that does
	// not send one is a fetch that gets the project blocked.
	if agent != "arxiv-reader/test" {
		t.Errorf("sent the agent %q", agent)
	}
}

func TestFetchStatsReportsABadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, _, err := fetchFrom(srv.URL, "")
	if err == nil {
		t.Fatal("a 503 was read as statistics")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("said %q, which does not mention the status", err)
	}
}

// fetchFrom is FetchStats pointed at a test server, since the URL is a
// constant everywhere else on purpose.
func fetchFrom(url, agent string) ([]Published, string, error) {
	old := statsURL
	statsURL = url
	defer func() { statsURL = old }()
	return FetchStats(context.Background(), nil, agent)
}

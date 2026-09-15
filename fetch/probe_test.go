package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The probe asks for the status and not the file, which is the only reason it is
// worth having: a rendering is a few hundred kilobytes, and deciding the path for a
// thousand papers by downloading a thousand renderings would cost as much as
// extracting them.
func TestTheProbeAsksWithHeadAndDownloadsNothing(t *testing.T) {
	var methods []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if strings.Contains(r.URL.Path, "1710.05832") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write([]byte(rendering))
	}))
	defer s.Close()
	f := &Fetcher{Base: s.URL + "/html/", Pace: time.Nanosecond, UserAgent: "arxiv-reader/test"}

	got, err := f.Rendered(context.Background(), "2312.00752v2")
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("a version arXiv renders came back as not rendered")
	}
	// A version with no rendering is the answer and not a failure. It is most of
	// arXiv, and a caller made to tell that error apart from a network error itself
	// would get it wrong once and route a paper down the wrong path for good.
	got, err = f.Rendered(context.Background(), "1710.05832v1")
	if err != nil {
		t.Fatalf("a version with no rendering came back as an error: %v", err)
	}
	if got {
		t.Error("a version arXiv does not render came back as rendered")
	}
	for _, m := range methods {
		if m != http.MethodHead {
			t.Errorf("the probe asked with %s, and it only wants the status", m)
		}
	}
	if len(methods) != 2 {
		t.Errorf("two questions made %d requests", len(methods))
	}
}

// A probe is still a request to arXiv, so it waits like every other one. A probe
// that ran flat out would be the one part of this tool that ignored the agreement
// the rest of it keeps.
func TestTheProbeWaitsLikeEveryOtherRequest(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer s.Close()
	f := &Fetcher{Base: s.URL + "/html/", Pace: 40 * time.Millisecond, UserAgent: "arxiv-reader/test"}
	start := time.Now()
	for _, ref := range []string{"2312.00752v1", "2312.00752v2", "2312.00752v3"} {
		if _, err := f.Rendered(context.Background(), ref); err != nil {
			t.Fatal(err)
		}
	}
	if took := time.Since(start); took < 80*time.Millisecond {
		t.Errorf("three probes at forty milliseconds apart took %s", took)
	}
}

// And a probe shares the clock with the fetches, because two clocks at fifteen
// seconds each is one request every seven and a half.
func TestTheProbeSharesItsClockWithTheFetches(t *testing.T) {
	var asked []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.Method)
		w.Write([]byte(rendering))
	}))
	defer s.Close()
	f := &Fetcher{Base: s.URL + "/html/", Pace: 40 * time.Millisecond, UserAgent: "arxiv-reader/test"}
	start := time.Now()
	if _, err := f.Rendered(context.Background(), "2312.00752v2"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Get(context.Background(), f.URL("2312.00752v2"), "2312.00752v2"); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took < 40*time.Millisecond {
		t.Errorf("a probe and a fetch took %s, so they are not sharing the clock", took)
	}
	if len(asked) != 2 || asked[0] != http.MethodHead || asked[1] != http.MethodGet {
		t.Errorf("the server was asked %v", asked)
	}
}

package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
)

// The e-print of a paper, which is a gzip stream and is opaque here. What is
// inside it is the source package's question, and this package writing bytes it
// has not looked inside is the point.
const submission = "\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\x03not really gzip, and nothing here cares"

// eprints serves one body for every reference off the e-print surface, and
// counts what was asked for.
func eprints(t *testing.T, status int, body string) (base string, hits *[]string) {
	t.Helper()
	var asked []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, strings.TrimPrefix(r.URL.Path, "/e-print/"))
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s.URL + "/e-print/", &asked
}

func sourceFetcher(base string) *Fetcher {
	return &Fetcher{SourceBase: base, Pace: time.Nanosecond, UserAgent: "arxiv-reader/test"}
}

func TestEPrintWritesTheFileAndTheEntry(t *testing.T) {
	root := t.TempDir()
	base, asked := eprints(t, http.StatusOK, submission)
	var m Manifest

	res, err := sourceFetcher(base).EPrint(context.Background(), root, &m, Order{
		Record:  paper("2312.00752", 2, corpus.LicenceCCBY, metadata.SourceAbs),
		Version: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeFetched {
		t.Fatalf("got %s, want fetched", res.Outcome)
	}
	if len(*asked) != 1 || (*asked)[0] != "2312.00752v2" {
		t.Fatalf("asked for %v", *asked)
	}
	if res.Entry.Route != RouteSource {
		t.Fatalf("the entry is on route %s", res.Entry.Route)
	}
	if res.Entry.Path != "work/source/2312/2312.00752v2.gz" {
		t.Fatalf("wrote to %s", res.Entry.Path)
	}
	if res.Entry.SHA256 != Digest([]byte(submission)) {
		t.Fatal("the entry does not hash what arrived")
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(res.Entry.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != submission {
		t.Fatal("the file is not what the server sent")
	}
	if _, ok := m.Find("2312.00752", 2, RouteSource); !ok {
		t.Fatal("the manifest does not hold the entry")
	}
}

// One version can be fetched for more than one path, and the two files are
// different files with different hashes. Keying the manifest by route is what
// keeps the second fetch from being read as the first one having changed.
func TestTheTwoRoutesAreTwoEntries(t *testing.T) {
	root := t.TempDir()
	html, _ := server(t, http.StatusOK, rendering)
	gzip, _ := eprints(t, http.StatusOK, submission)
	f := &Fetcher{Base: html, SourceBase: gzip, Pace: time.Nanosecond, UserAgent: "arxiv-reader/test"}
	var m Manifest
	order := Order{Record: paper("2312.00752", 2, corpus.LicenceCCBY, metadata.SourceAbs), Version: 2}

	if _, err := f.Render(context.Background(), root, &m, order); err != nil {
		t.Fatal(err)
	}
	if _, err := f.EPrint(context.Background(), root, &m, order); err != nil {
		t.Fatal(err)
	}
	if len(m.Sources) != 2 {
		t.Fatalf("the manifest holds %d entries for one version on two routes", len(m.Sources))
	}
}

func TestEPrintSecondTimeIsCachedAndAsksForNothing(t *testing.T) {
	root := t.TempDir()
	base, asked := eprints(t, http.StatusOK, submission)
	f := sourceFetcher(base)
	var m Manifest
	order := Order{Record: paper("2312.00752", 2, corpus.LicenceCCBY, metadata.SourceAbs), Version: 2}

	if _, err := f.EPrint(context.Background(), root, &m, order); err != nil {
		t.Fatal(err)
	}
	res, err := f.EPrint(context.Background(), root, &m, order)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeCached {
		t.Fatalf("got %s, want cached", res.Outcome)
	}
	if len(*asked) != 1 {
		t.Fatalf("asked for %v, want one request for two fetches", *asked)
	}
}

// The gate is the same gate, which is the part worth a test of its own: the
// source path is the fallback for every paper with no rendering, and a fallback
// that fetched past the licence gate would be a way around it.
func TestTheGateRunsBeforeTheEPrintRequest(t *testing.T) {
	root := t.TempDir()
	base, asked := eprints(t, http.StatusOK, submission)
	var m Manifest

	_, err := sourceFetcher(base).EPrint(context.Background(), root, &m, Order{
		Record:  paper("2407.21783", 3, corpus.LicenceArXiv, metadata.SourceAbs),
		Version: 3,
	})
	if err == nil {
		t.Fatal("fetched a paper the corpus may never publish")
	}
	if len(*asked) != 0 {
		t.Fatalf("asked for %v before deciding it was not allowed to", *asked)
	}
}

// Rarer than a missing rendering and worse news, because there is no third
// surface to fall through to.
func TestEPrintSaysSoWhenArXivServesNone(t *testing.T) {
	root := t.TempDir()
	base, _ := eprints(t, http.StatusNotFound, "not found")
	var m Manifest

	_, err := sourceFetcher(base).EPrint(context.Background(), root, &m, Order{
		Record:  paper("hep-th/9711200", 3, corpus.LicenceCCBY, metadata.SourceAbs),
		Version: 3,
	})
	var missing *NoEPrint
	if !errors.As(err, &missing) {
		t.Fatalf("got %v, want a version with no e-print", err)
	}
	if missing.Ref != "hep-th/9711200v3" {
		t.Fatalf("the error is about %q", missing.Ref)
	}
}

func TestEPrintNeedsAVersion(t *testing.T) {
	root := t.TempDir()
	base, asked := eprints(t, http.StatusOK, submission)
	var m Manifest

	_, err := sourceFetcher(base).EPrint(context.Background(), root, &m, Order{
		Record: paper("2312.00752", 2, corpus.LicenceCCBY, metadata.SourceAbs),
	})
	if err == nil || !strings.Contains(err.Error(), "an e-print") {
		t.Fatalf("got %v, and the refusal has to say what a version belongs to", err)
	}
	if len(*asked) != 0 {
		t.Fatalf("asked for %v", *asked)
	}
}

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

// A PDF, as far as anything in this package is concerned. The five bytes at the
// front are the whole of what is checked, because reading further would be
// parsing the file and that happens in the pdftext package over bytes that are
// already on disk.
const document = "%PDF-1.5\nnot really a PDF, and only the first line matters here"

// The page arXiv serves while it is still compiling a submission, which arrives
// with a 200 and is the one answer in this package that a status code does not
// describe.
const building = "<!DOCTYPE html>\n<html><body><p>Preparing your PDF, please try again shortly.</p></body></html>"

// pdfs serves one body for every reference off the PDF surface, and counts what
// was asked for.
func pdfs(t *testing.T, status int, body string) (base string, hits *[]string) {
	t.Helper()
	var asked []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, strings.TrimPrefix(r.URL.Path, "/pdf/"))
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s.URL + "/pdf/", &asked
}

func pdfFetcher(base string) *Fetcher {
	return &Fetcher{PDFBase: base, Pace: time.Nanosecond, UserAgent: "arxiv-reader/test"}
}

func TestPDFWritesTheFileAndTheEntry(t *testing.T) {
	root := t.TempDir()
	base, asked := pdfs(t, http.StatusOK, document)
	var m Manifest

	res, err := pdfFetcher(base).PDF(context.Background(), root, &m, Order{
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
	if res.Entry.Route != RouteNative {
		t.Fatalf("the entry is on route %s", res.Entry.Route)
	}
	if res.Entry.Path != "work/pdf/2312/2312.00752v2.pdf" {
		t.Fatalf("wrote to %s", res.Entry.Path)
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(res.Entry.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != document {
		t.Fatal("the file is not what the server sent")
	}
	if _, ok := m.Find("2312.00752", 2, RouteNative); !ok {
		t.Fatal("the manifest does not hold the entry")
	}
}

// The three routes are three entries for one version, because they are three
// different files with three different hashes. A paper whose rendering was
// rejected, whose conversion timed out and which ended up on the native path
// has all three.
func TestTheThreeRoutesAreThreeEntries(t *testing.T) {
	root := t.TempDir()
	html, _ := server(t, http.StatusOK, rendering)
	gzip, _ := eprints(t, http.StatusOK, submission)
	pdf, _ := pdfs(t, http.StatusOK, document)
	f := &Fetcher{Base: html, SourceBase: gzip, PDFBase: pdf, Pace: time.Nanosecond, UserAgent: "arxiv-reader/test"}
	var m Manifest
	order := Order{Record: paper("2312.00752", 2, corpus.LicenceCCBY, metadata.SourceAbs), Version: 2}

	for _, fetch := range []func(context.Context, string, *Manifest, Order) (Result, error){f.Render, f.EPrint, f.PDF} {
		if _, err := fetch(context.Background(), root, &m, order); err != nil {
			t.Fatal(err)
		}
	}
	if len(m.Sources) != 3 {
		t.Fatalf("the manifest holds %d entries for one version on three routes", len(m.Sources))
	}
}

func TestPDFSecondTimeIsCachedAndAsksForNothing(t *testing.T) {
	root := t.TempDir()
	base, asked := pdfs(t, http.StatusOK, document)
	f := pdfFetcher(base)
	var m Manifest
	order := Order{Record: paper("2312.00752", 2, corpus.LicenceCCBY, metadata.SourceAbs), Version: 2}

	if _, err := f.PDF(context.Background(), root, &m, order); err != nil {
		t.Fatal(err)
	}
	res, err := f.PDF(context.Background(), root, &m, order)
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

// The native path is the last resort for a paper with no rendering and no
// readable source, which is exactly the shape of thing that ends up being used
// as a way around the gate. It is the same gate.
func TestTheGateRunsBeforeThePDFRequest(t *testing.T) {
	root := t.TempDir()
	base, asked := pdfs(t, http.StatusOK, document)
	var m Manifest

	_, err := pdfFetcher(base).PDF(context.Background(), root, &m, Order{
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

// The rarest of the three absences and the end of the road, because a version
// with no PDF has no fourth surface behind it.
func TestPDFSaysSoWhenArXivServesNone(t *testing.T) {
	root := t.TempDir()
	base, _ := pdfs(t, http.StatusNotFound, "not found")
	var m Manifest

	_, err := pdfFetcher(base).PDF(context.Background(), root, &m, Order{
		Record:  paper("hep-th/9711200", 3, corpus.LicenceCCBY, metadata.SourceAbs),
		Version: 3,
	})
	var missing *NoPDF
	if !errors.As(err, &missing) {
		t.Fatalf("got %v, want a version with no PDF", err)
	}
	if missing.Ref != "hep-th/9711200v3" {
		t.Fatalf("the error is about %q", missing.Ref)
	}
}

// A 200 carrying something that is not a PDF, which is what arXiv answers with
// while a submission is still being compiled. Nothing is written, because a file
// under work/pdf that is a page of HTML would be read as a PDF by everything
// downstream of here and would hash into the manifest as one.
func TestAPDFStillBeingCompiledIsRefusedAndNothingIsWritten(t *testing.T) {
	root := t.TempDir()
	base, _ := pdfs(t, http.StatusOK, building)
	var m Manifest

	_, err := pdfFetcher(base).PDF(context.Background(), root, &m, Order{
		Record:  paper("2312.00752", 1, corpus.LicenceCCBY, metadata.SourceAbs),
		Version: 1,
	})
	var not *NotBuilt
	if !errors.As(err, &not) {
		t.Fatalf("got %v, want the page arXiv serves while it compiles", err)
	}
	if not.Ref != "2312.00752v1" {
		t.Fatalf("the error is about %q", not.Ref)
	}
	if _, err := os.Stat(filepath.Join(root, "work", "pdf", "2312", "2312.00752v1.pdf")); err == nil {
		t.Fatal("wrote a file that is not a PDF into work/pdf")
	}
	if len(m.Sources) != 0 {
		t.Fatalf("the manifest holds %d entries for bytes that were refused", len(m.Sources))
	}
}

func TestPDFNeedsAVersion(t *testing.T) {
	root := t.TempDir()
	base, asked := pdfs(t, http.StatusOK, document)
	var m Manifest

	_, err := pdfFetcher(base).PDF(context.Background(), root, &m, Order{
		Record: paper("2312.00752", 2, corpus.LicenceCCBY, metadata.SourceAbs),
	})
	if err == nil || !strings.Contains(err.Error(), "a PDF") {
		t.Fatalf("got %v, and the refusal has to say what a version belongs to", err)
	}
	if len(*asked) != 0 {
		t.Fatalf("asked for %v", *asked)
	}
}

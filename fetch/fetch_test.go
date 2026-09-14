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
	"github.com/tamnd/arxiv-reader/licence"
	"github.com/tamnd/arxiv-reader/metadata"
)

// The rendering of Mamba v2, cut down to the shape that matters. The real one
// is 668 kilobytes and none of it is worth committing to a test.
const rendering = `<!DOCTYPE html><html><head><title>Mamba</title></head><body>
<h1 class="ltx_title">Mamba: Linear-Time Sequence Modeling with Selective State Spaces</h1>
</body></html>`

// paper is a record with one licence on every version, which is what a record
// looks like after ax licence resolve has been over it.
func paper(id string, versions int, l corpus.Licence, from metadata.Source) metadata.Record {
	r := metadata.Record{ID: id}
	for n := 1; n <= versions; n++ {
		r.Versions = append(r.Versions, metadata.Version{
			Version:     n,
			Created:     time.Date(2023, 12, n, 0, 0, 0, 0, time.UTC),
			Licence:     l,
			LicenceFrom: from,
		})
	}
	return r
}

// server serves one body for every reference, and counts what was asked for.
func server(t *testing.T, status int, body string) (base string, hits *[]string) {
	t.Helper()
	var asked []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, strings.TrimPrefix(r.URL.Path, "/html/"))
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s.URL + "/html/", &asked
}

func fetcher(base string) *Fetcher {
	return &Fetcher{Base: base, Pace: time.Nanosecond, UserAgent: "arxiv-reader/test"}
}

// The pace belongs to arxiv.org and not to either package that reads it, so the
// two constants have to be the same number. Halving one of them by editing the
// other and forgetting this one is exactly the mistake this catches.
func TestThePaceIsTheSameAsTheLicenceResolverPace(t *testing.T) {
	if Pace != licence.Pace {
		t.Fatalf("fetch waits %s and licence waits %s, and they read the same host", Pace, licence.Pace)
	}
}

func TestParseRoute(t *testing.T) {
	for _, want := range []Route{RouteRender, RouteSource} {
		if got, err := ParseRoute(string(want)); err != nil || got != want {
			t.Fatalf("got %q, %v", got, err)
		}
	}
	for _, s := range []string{"native", "vision", ""} {
		if _, err := ParseRoute(s); err == nil {
			t.Fatalf("%q parsed as a route that can be fetched", s)
		}
	}
}

func TestGateLetsAnOpenPaperThrough(t *testing.T) {
	if err := Gate(paper("2312.00752", 2, corpus.LicenceCCBY, metadata.SourceAbs), 2); err != nil {
		t.Fatalf("a cc-by version read off the abs page: %v", err)
	}
}

// Verbatim access forbids translating and permits republishing the English, and
// the fetch is about the English, so it is allowed through. Getting this
// backwards would drop about five per cent of arXiv for no reason.
func TestGateLetsAVerbatimPaperThrough(t *testing.T) {
	if err := Gate(paper("2312.00752", 1, corpus.LicenceCCBYNCND, metadata.SourceAbs), 1); err != nil {
		t.Fatalf("a cc-by-nc-nd version: %v", err)
	}
}

func TestGateRefusesRecordAccess(t *testing.T) {
	err := Gate(paper("2407.21783", 3, corpus.LicenceArXiv, metadata.SourceAbs), 3)
	if err == nil {
		t.Fatal("let a nonexclusive distribution paper through")
	}
	if !strings.Contains(err.Error(), "record") {
		t.Fatalf("the refusal does not name the access class: %v", err)
	}
}

// Empty is not unknown. Nobody having looked is a different state from somebody
// having looked and arXiv not having said, and the refusal has to send the
// caller to the resolver rather than tell them the answer is no.
func TestGateRefusesAVersionNobodyHasLookedAt(t *testing.T) {
	err := Gate(paper("2312.00752", 2, "", ""), 2)
	if err == nil {
		t.Fatal("let a version with no licence through")
	}
	if !strings.Contains(err.Error(), "ax licence resolve") {
		t.Fatalf("the refusal does not say what to run: %v", err)
	}
}

// The one that catches real mistakes. Every bulk surface states one licence for
// the whole paper, so a record straight out of a harvest has the latest
// version's licence sitting on v1, and it looks exactly like an answer.
func TestGateRefusesAPaperLevelLicence(t *testing.T) {
	for _, from := range []metadata.Source{metadata.SourceKaggle, metadata.SourceHF, metadata.SourceOAI} {
		err := Gate(paper("2312.00752", 2, corpus.LicenceCCBY, from), 1)
		if err == nil {
			t.Fatalf("let a licence from %s through", from)
		}
		if !strings.Contains(err.Error(), string(from)) {
			t.Fatalf("the refusal does not name the surface: %v", err)
		}
	}
}

func TestGateRefusesAVersionThePlaneDoesNotHave(t *testing.T) {
	if err := Gate(paper("2312.00752", 2, corpus.LicenceCCBY, metadata.SourceAbs), 3); err == nil {
		t.Fatal("let through a version the record has never heard of")
	}
}

func TestRenderWritesTheFileAndTheEntry(t *testing.T) {
	root := t.TempDir()
	base, asked := server(t, http.StatusOK, rendering)
	var m Manifest

	res, err := fetcher(base).Render(context.Background(), root, &m, Order{
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
	if res.Entry.Path != "work/html/2312/2312.00752v2.html" {
		t.Fatalf("wrote to %s", res.Entry.Path)
	}
	if res.Entry.SHA256 != Digest([]byte(rendering)) {
		t.Fatal("the entry does not hash what arrived")
	}
	if res.Entry.Bytes != int64(len(rendering)) {
		t.Fatalf("got %d bytes, want %d", res.Entry.Bytes, len(rendering))
	}
	if res.Entry.Licence != corpus.LicenceCCBY || res.Entry.LicenceFrom != metadata.SourceAbs {
		t.Fatalf("the entry forgot what the gate read: %+v", res.Entry)
	}
	if res.Entry.Fetched.IsZero() {
		t.Fatal("the entry records no hour")
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(res.Entry.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != rendering {
		t.Fatal("the file is not what the server sent")
	}
	if _, ok := m.Find("2312.00752", 2, RouteRender); !ok {
		t.Fatal("the manifest does not hold the entry")
	}
}

// The whole reason a run of a thousand papers can be interrupted and restarted.
func TestRenderSecondTimeIsCachedAndAsksForNothing(t *testing.T) {
	root := t.TempDir()
	base, asked := server(t, http.StatusOK, rendering)
	f := fetcher(base)
	var m Manifest
	order := Order{Record: paper("2312.00752", 2, corpus.LicenceCCBY, metadata.SourceAbs), Version: 2}

	if _, err := f.Render(context.Background(), root, &m, order); err != nil {
		t.Fatal(err)
	}
	res, err := f.Render(context.Background(), root, &m, order)
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

// A fresh clone has the manifest and none of the files, and that has to be an
// ordinary fetch rather than a hash complaint.
func TestRenderRefetchesAFileThatIsNotThere(t *testing.T) {
	root := t.TempDir()
	base, asked := server(t, http.StatusOK, rendering)
	f := fetcher(base)
	var m Manifest
	order := Order{Record: paper("2312.00752", 2, corpus.LicenceCCBY, metadata.SourceAbs), Version: 2}

	res, err := f.Render(context.Background(), root, &m, order)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(res.Entry.Path))); err != nil {
		t.Fatal(err)
	}
	again, err := f.Render(context.Background(), root, &m, order)
	if err != nil {
		t.Fatal(err)
	}
	if again.Outcome != OutcomeFetched {
		t.Fatalf("got %s, want fetched", again.Outcome)
	}
	if len(*asked) != 2 {
		t.Fatalf("asked for %v, want two", *asked)
	}
}

func TestRenderRefusesAFileThatChangedOnDisk(t *testing.T) {
	root := t.TempDir()
	base, asked := server(t, http.StatusOK, rendering)
	f := fetcher(base)
	var m Manifest
	order := Order{Record: paper("2312.00752", 2, corpus.LicenceCCBY, metadata.SourceAbs), Version: 2}

	res, err := f.Render(context.Background(), root, &m, order)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, filepath.FromSlash(res.Entry.Path))
	if err := os.WriteFile(file, []byte("somebody edited this"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = f.Render(context.Background(), root, &m, order)
	var changed *Changed
	if !errors.As(err, &changed) {
		t.Fatalf("got %v, want a changed source", err)
	}
	if changed.Where != "on disk" {
		t.Fatalf("got %q, want on disk", changed.Where)
	}
	if len(*asked) != 1 {
		t.Fatalf("asked for %v, and a file that is already wrong is not a reason to fetch", *asked)
	}
	b, _ := os.ReadFile(file)
	if string(b) != "somebody edited this" {
		t.Fatal("the file was overwritten, and the overwrite destroyed the only evidence")
	}
	// Accepting is for a source that moved and not for a cache that did, so
	// the advice here has to be the other one.
	if strings.Contains(err.Error(), "-accept") {
		t.Fatalf("a cached file that changed was offered -accept: %v", err)
	}
}

func TestRenderRefusesASourceThatChanged(t *testing.T) {
	root := t.TempDir()
	base, _ := server(t, http.StatusOK, rendering)
	f := fetcher(base)
	order := Order{Record: paper("2312.00752", 2, corpus.LicenceCCBY, metadata.SourceAbs), Version: 2}

	// The manifest holds a hash for bytes that are not what the server will
	// send, and the file itself is not on disk, so the mismatch can only be
	// found after the request.
	held := Source{ID: "2312.00752", Version: 2, Route: RouteRender, SHA256: Digest([]byte("what it used to be")), Bytes: 18, Path: "work/html/2312/2312.00752v2.html"}
	m := Manifest{Sources: []Source{held}}

	_, err := f.Render(context.Background(), root, &m, order)
	var changed *Changed
	if !errors.As(err, &changed) {
		t.Fatalf("got %v, want a changed source", err)
	}
	if changed.Where != "at the source" {
		t.Fatalf("got %q, want at the source", changed.Where)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(held.Path))); err == nil {
		t.Fatal("the new bytes were written anyway")
	}
	if m.Sources[0].SHA256 != held.SHA256 {
		t.Fatal("the manifest was updated to the new bytes")
	}
	if !strings.Contains(err.Error(), "-accept") {
		t.Fatalf("the refusal does not say how to accept the new bytes: %v", err)
	}
}

func TestAcceptRecordsTheNewBytes(t *testing.T) {
	root := t.TempDir()
	base, _ := server(t, http.StatusOK, rendering)
	f := fetcher(base)
	held := Source{ID: "2312.00752", Version: 2, Route: RouteRender, SHA256: Digest([]byte("what it used to be")), Path: "work/html/2312/2312.00752v2.html"}
	m := Manifest{Sources: []Source{held}}

	res, err := f.Render(context.Background(), root, &m, Order{
		Record:  paper("2312.00752", 2, corpus.LicenceCCBY, metadata.SourceAbs),
		Version: 2,
		Accept:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Entry.SHA256 != Digest([]byte(rendering)) {
		t.Fatal("the entry was not updated")
	}
	if m.Sources[0].SHA256 != Digest([]byte(rendering)) {
		t.Fatal("the manifest was not updated")
	}
}

// The gate runs before the request, which is the point of it. A refusal that
// arrives after fifteen seconds and a download has already spent the thing it
// was meant to save.
func TestTheGateRunsBeforeTheRequest(t *testing.T) {
	root := t.TempDir()
	base, asked := server(t, http.StatusOK, rendering)
	var m Manifest

	_, err := fetcher(base).Render(context.Background(), root, &m, Order{
		Record:  paper("2407.21783", 3, corpus.LicenceArXiv, metadata.SourceAbs),
		Version: 3,
	})
	if err == nil {
		t.Fatal("fetched a paper the corpus may never publish")
	}
	if len(*asked) != 0 {
		t.Fatalf("asked for %v before deciding it was not allowed to", *asked)
	}
	if len(m.Sources) != 0 {
		t.Fatal("a refused fetch left an entry in the manifest")
	}
}

func TestAnywayFetchesPastTheGate(t *testing.T) {
	root := t.TempDir()
	base, asked := server(t, http.StatusOK, rendering)
	var m Manifest

	res, err := fetcher(base).Render(context.Background(), root, &m, Order{
		Record:  paper("2407.21783", 3, corpus.LicenceArXiv, metadata.SourceAbs),
		Version: 3,
		Anyway:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(*asked) != 1 {
		t.Fatalf("asked for %v", *asked)
	}
	// The entry still records what the gate would have refused, so that
	// anything reading the manifest later can see it.
	if res.Entry.Licence != corpus.LicenceArXiv {
		t.Fatalf("the entry says %s, and the manifest has to say what was known", res.Entry.Licence)
	}
}

func TestRenderSaysSoWhenThereIsNoRendering(t *testing.T) {
	root := t.TempDir()
	base, _ := server(t, http.StatusNotFound, "not found")
	var m Manifest

	_, err := fetcher(base).Render(context.Background(), root, &m, Order{
		Record:  paper("hep-th/9711200", 3, corpus.LicenceCCBY, metadata.SourceAbs),
		Version: 3,
	})
	var missing *NotRendered
	if !errors.As(err, &missing) {
		t.Fatalf("got %v, want a paper with no rendering", err)
	}
	if !strings.Contains(err.Error(), "source path") {
		t.Fatalf("the error does not name the fallback: %v", err)
	}
}

func TestRenderReportsAnyOtherStatus(t *testing.T) {
	root := t.TempDir()
	base, _ := server(t, http.StatusInternalServerError, "")
	var m Manifest

	_, err := fetcher(base).Render(context.Background(), root, &m, Order{
		Record:  paper("2312.00752", 2, corpus.LicenceCCBY, metadata.SourceAbs),
		Version: 2,
	})
	if err == nil {
		t.Fatal("a 500 was read as a rendering")
	}
	var missing *NotRendered
	if errors.As(err, &missing) {
		t.Fatal("a 500 was read as a paper with no rendering, which is a different thing")
	}
}

func TestRenderRefusesAReferenceWithNoVersion(t *testing.T) {
	root := t.TempDir()
	base, asked := server(t, http.StatusOK, rendering)
	var m Manifest

	_, err := fetcher(base).Render(context.Background(), root, &m, Order{
		Record: paper("2312.00752", 2, corpus.LicenceCCBY, metadata.SourceAbs),
	})
	if err == nil {
		t.Fatal("fetched a paper rather than a version")
	}
	if len(*asked) != 0 {
		t.Fatalf("asked for %v", *asked)
	}
}

// The old style id carries a slash, which is a directory separator, and the
// path form has to have turned it into something else before anything opens a
// file.
func TestRenderHandlesAnOldStyleID(t *testing.T) {
	root := t.TempDir()
	base, asked := server(t, http.StatusOK, rendering)
	var m Manifest

	res, err := fetcher(base).Render(context.Background(), root, &m, Order{
		Record:  paper("hep-th/9711200", 3, corpus.LicenceCCBY, metadata.SourceAbs),
		Version: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Entry.Path != "work/html/9711/hep-th-9711200v3.html" {
		t.Fatalf("wrote to %s", res.Entry.Path)
	}
	// The URL keeps the slash, because that is what arXiv answers to.
	if (*asked)[0] != "hep-th/9711200v3" {
		t.Fatalf("asked for %q", (*asked)[0])
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(res.Entry.Path))); err != nil {
		t.Fatal(err)
	}
}

func TestRenderRefusesSomethingTooBigToBeAPaper(t *testing.T) {
	root := t.TempDir()
	base, _ := server(t, http.StatusOK, strings.Repeat("x", maxBody+1))
	var m Manifest

	_, err := fetcher(base).Render(context.Background(), root, &m, Order{
		Record:  paper("2312.00752", 2, corpus.LicenceCCBY, metadata.SourceAbs),
		Version: 2,
	})
	if err == nil {
		t.Fatal("read sixteen megabytes as a rendering")
	}
}

func TestRenderStopsWhenTheContextIsCancelled(t *testing.T) {
	root := t.TempDir()
	base, _ := server(t, http.StatusOK, rendering)
	f := &Fetcher{Base: base, Pace: time.Hour}
	var m Manifest
	order := Order{Record: paper("2312.00752", 2, corpus.LicenceCCBY, metadata.SourceAbs), Version: 2}

	if _, err := f.Render(context.Background(), root, &m, order); err != nil {
		t.Fatal(err)
	}
	// The second version waits an hour behind the first, and cancelling has to
	// come back out of the wait rather than out of the request.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	order.Version = 1
	if _, err := f.Render(ctx, root, &m, order); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want the cancellation", err)
	}
}

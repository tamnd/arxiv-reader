package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tamnd/arxiv-reader/fetch"
)

// eprint writes a gzipped e-print the way arXiv serves one, which is the bytes of
// the submission with no tar around them when the submission is one file.
func eprint(t *testing.T, root, rel, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, rel), b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(rel)
}

// The two facts the path needs are learnt while the bytes are in hand, and they are
// the reason the fetch writes anything into the manifest beyond a hash. A fetch that
// printed them and moved on would leave the next machine, which has no work/ because
// work/ is not committed, with nothing to decide a path from.
func TestWhatASubmissionHoldsIsReadOffTheBytes(t *testing.T) {
	root := t.TempDir()
	tex := eprint(t, root, filepath.Join("work", "src", "2501", "2501.00001v1"), "\\documentclass{article}\n\\begin{document}\nhello\n\\end{document}\n")
	pdf := eprint(t, root, filepath.Join("work", "src", "2501", "2501.00002v1"), "%PDF-1.5\nnot tex at all\n")

	if got := reportMain(root, tex); got != fetch.HoldsTeX {
		t.Errorf("a submission of TeX came back as %q", got)
	}
	if got := reportMain(root, pdf); got != fetch.HoldsPDF {
		t.Errorf("a submission that is a PDF came back as %q", got)
	}
}

func TestWhetherAPdfHoldsTextIsReadOffThePdf(t *testing.T) {
	root, m := onDisk(t, "2501.00001", 1)
	rel := m.Sources[0].Path
	ctx := context.Background()

	if got := reportText(ctx, stub(t, typeset()), root, rel); got != fetch.Yes {
		t.Errorf("a typeset PDF came back as %q", got)
	}
	// A scan is no and not empty, because empty means nobody looked and this is
	// somebody looking and finding nothing.
	if got := reportText(ctx, stub(t, "a\nr\nX\ni\nv\n"), root, rel); got != fetch.No {
		t.Errorf("a scan came back as %q", got)
	}
}

// And a manifest that came back from disk with either fact in it keeps the fact,
// because the whole point of writing them down is that they are read again.
func TestTheTwoFactsSurviveTheManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifests", "sources.yaml")
	var m fetch.Manifest
	m.Put(fetch.Source{
		ID: "2501.00001", Version: 1, Route: fetch.RouteSource,
		Path: "work/src/2501/2501.00001v1", SHA256: "0", Holds: fetch.HoldsPDF,
	})
	m.Put(fetch.Source{
		ID: "2501.00001", Version: 1, Route: fetch.RouteNative,
		Path: "work/pdf/2501/2501.00001v1.pdf", SHA256: "1", Text: fetch.No,
	})
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := fetch.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	src, ok := back.Find("2501.00001", 1, fetch.RouteSource)
	if !ok || src.Holds != fetch.HoldsPDF {
		t.Errorf("the e-print came back as %q, %v", src.Holds, ok)
	}
	pdf, ok := back.Find("2501.00001", 1, fetch.RouteNative)
	if !ok || pdf.Text != fetch.No {
		t.Errorf("the PDF came back as %q, %v", pdf.Text, ok)
	}
	// The other route has neither fact and says neither, so a rendering does not
	// carry two empty keys around in the diff.
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(b, []byte("holds:")) != 1 || bytes.Count(b, []byte("text:")) != 1 {
		t.Errorf("the manifest reads:\n%s", b)
	}
}

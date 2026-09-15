package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/fetch"
	"github.com/tamnd/arxiv-reader/pdftext"
)

// The native path runs a program, so every test here runs a program that behaves
// the way pdftotext behaves and is not pdftotext. CI has no poppler on it, the
// interesting cases are a PDF holding a scan and a PDF holding the wrong version,
// and neither of those is a file anybody wants to keep in a repository.
func stub(t *testing.T, text string) *pdftext.Reader {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pdftotext")
	script := "#!/bin/sh\ncat <<'PAGE'\n" + text + "\nPAGE\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &pdftext.Reader{Binary: path}
}

// onDisk is a corpus with one PDF in it and a manifest that says so, which is what
// this command requires before it reads anything.
func onDisk(t *testing.T, id string, version int) (string, fetch.Manifest) {
	t.Helper()
	root := t.TempDir()
	rel := filepath.Join("work", "pdf", "2501", id+"v1.pdf")
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, rel), []byte("%PDF-1.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var m fetch.Manifest
	m.Put(fetch.Source{
		ID: id, Version: version, Route: fetch.RouteNative,
		Path: filepath.ToSlash(rel), SHA256: "0", Bytes: 9,
	})
	return root, m
}

// typeset is a page holding as much text as a page of a paper does, laid out the
// way -layout lays a two column page out.
func typeset() string {
	var b strings.Builder
	b.WriteString("1   Introduction\n")
	for i := 0; i < 20; i++ {
		b.WriteString("   this paper is about nothing in particular at all     and it goes on in the other column\n")
	}
	return b.String()
}

// A PDF with no text layer is a fact about the paper and not a failure of the
// command, so it comes back as its own type and a batch of ten papers steps over
// it rather than stopping on the third.
func TestAPdfHoldingAScanIsRefusedAsAScan(t *testing.T) {
	root, m := onDisk(t, "2501.00001", 1)
	id := axid.ID{Canonical: "2501.00001", Year: 2025, Month: 1, Version: 1}
	_, _, _, err := readPDF(context.Background(), stub(t, "a\nr\nX\ni\nv\n"), root, m, id, "2501.00001v1", false)
	var scan *scanned
	if !errors.As(err, &scan) {
		t.Fatalf("the refusal came back as %v", err)
	}
	if !strings.Contains(err.Error(), "2501.00001v1") {
		t.Errorf("the refusal does not say which paper it is about: %v", err)
	}
}

// The paper that missed the threshold by a page is the reason the flag exists, so
// the flag has to actually read the file.
func TestTheFlagReadsAPdfThisPathWouldRefuse(t *testing.T) {
	root, m := onDisk(t, "2501.00001", 1)
	id := axid.ID{Canonical: "2501.00001", Year: 2025, Month: 1, Version: 1}
	p, _, _, err := readPDF(context.Background(), stub(t, "a\nr\nX\ni\nv\n"), root, m, id, "2501.00001v1", true)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("nothing came back")
	}
}

// The margin stamp is the one statement inside a PDF about which version of a
// paper it holds. A corpus that decided it may publish v1 and then extracted v4
// is publishing something nobody gave it, so this stops rather than reports.
func TestAPdfOfAnotherVersionIsNotExtracted(t *testing.T) {
	root, m := onDisk(t, "2501.00001", 1)
	id := axid.ID{Canonical: "2501.00001", Year: 2025, Month: 1, Version: 1}
	page := "arXiv:2501.00001v4 [cs.DL] 5 Jan 2025\n" + typeset()
	_, _, _, err := readPDF(context.Background(), stub(t, page), root, m, id, "2501.00001v1", false)
	if err == nil {
		t.Fatal("a PDF of v4 was extracted as v1")
	}
	if !strings.Contains(err.Error(), "v4") {
		t.Errorf("the refusal does not say which version the file is of: %v", err)
	}
}

// A file nobody recorded is a file nobody knows the licence of, so the manifest
// and not the disk says what may be read.
func TestAPdfNobodyRecordedIsNotRead(t *testing.T) {
	root, _ := onDisk(t, "2501.00001", 1)
	id := axid.ID{Canonical: "2501.00001", Year: 2025, Month: 1, Version: 1}
	_, _, _, err := readPDF(context.Background(), stub(t, typeset()), root, fetch.Manifest{}, id, "2501.00001v1", false)
	if err == nil {
		t.Fatal("a PDF that is in no manifest was read")
	}
	if !strings.Contains(err.Error(), "ax fetch native") {
		t.Errorf("the refusal does not say what to do about it: %v", err)
	}
}

// A PDF is of one version of a paper and there is no such thing as the PDF of a
// paper, so a reference with no version on it is a refusal and not a guess.
func TestAReferenceWithNoVersionIsRefused(t *testing.T) {
	root, m := onDisk(t, "2501.00001", 1)
	id := axid.ID{Canonical: "2501.00001", Year: 2025, Month: 1}
	_, _, _, err := readPDF(context.Background(), stub(t, typeset()), root, m, id, "2501.00001", false)
	if err == nil {
		t.Fatal("a reference with no version was read")
	}
	if !strings.Contains(err.Error(), "names no version") {
		t.Errorf("the refusal reads %v", err)
	}
}

// What the path is for: the sentences were printed, pdftotext read them back, and
// the structure over them was recovered from the shape of the page.
func TestAPaperComesBackWithItsSectionsAndItsPages(t *testing.T) {
	root, m := onDisk(t, "2501.00001", 1)
	id := axid.ID{Canonical: "2501.00001", Year: 2025, Month: 1, Version: 1}
	p, doc, entry, err := readPDF(context.Background(), stub(t, typeset()), root, m, id, "2501.00001v1", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Sections) != 1 || p.Sections[0].Title != "Introduction" {
		t.Fatalf("the paper came back with %d sections: %v", len(p.Sections), p.Sections)
	}
	if p.Pages != "1" {
		t.Errorf("the paper says it was read off pages %q", p.Pages)
	}
	if doc.Chars() == 0 {
		t.Error("the document says it holds no characters")
	}
	if entry.Route != fetch.RouteNative {
		t.Errorf("the entry came back on route %q", entry.Route)
	}
}

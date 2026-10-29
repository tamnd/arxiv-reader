package fetch

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
)

// Way R2 of AX2.5 reads a record paper, so the read gate lets it through, and
// it still wants the licence of the version from the abs page.
func TestReadGateLetsARecordPaperThroughAndWantsTheAbsPage(t *testing.T) {
	if err := ReadGate(paper("2407.21783", 3, corpus.LicenceArXiv, metadata.SourceAbs), 3); err != nil {
		t.Fatalf("refused a record paper: %v", err)
	}
	if err := ReadGate(paper("2407.21783", 3, corpus.LicenceArXiv, metadata.SourceOAI), 3); err == nil {
		t.Fatal("let a paper level licence through")
	}
	if err := ReadGate(paper("2407.21783", 3, "", ""), 3); err == nil {
		t.Fatal("let a version through that nobody has read a licence for")
	}
}

func TestEPrintInMemoryGivesTheBytesAndAnEntry(t *testing.T) {
	base, asked := server(t, http.StatusOK, "\\documentclass{article}")
	f := fetcher(base)
	f.SourceBase = base
	var m Manifest
	body, entry, err := f.EPrintInMemory(context.Background(), &m, Order{Record: paper("2407.21783", 3, corpus.LicenceArXiv, metadata.SourceAbs), Version: 3})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "\\documentclass{article}" || len(*asked) != 1 || (*asked)[0] != "2407.21783v3" {
		t.Fatalf("got %q after asking for %v", body, *asked)
	}
	if entry.SHA256 != Digest(body) || entry.Route != RouteSource || entry.Licence != corpus.LicenceArXiv {
		t.Fatalf("the entry is %+v", entry)
	}
	if len(m.Sources) != 0 {
		t.Fatal("the fetch put the entry in the manifest, which is for the caller")
	}
}

func TestEPrintInMemoryRefusesASourceThatChanged(t *testing.T) {
	base, _ := server(t, http.StatusOK, "new bytes")
	f := fetcher(base)
	f.SourceBase = base
	var m Manifest
	m.Put(Source{ID: "2407.21783", Version: 3, Route: RouteSource, SHA256: Digest([]byte("old bytes")), Path: "work/source/2407/2407.21783v3.gz"})
	o := Order{Record: paper("2407.21783", 3, corpus.LicenceArXiv, metadata.SourceAbs), Version: 3}
	_, _, err := f.EPrintInMemory(context.Background(), &m, o)
	var changed *Changed
	if !errors.As(err, &changed) || changed.Where != "at the source" {
		t.Fatalf("got %v", err)
	}
	o.Accept = true
	if _, _, err := f.EPrintInMemory(context.Background(), &m, o); err != nil {
		t.Fatalf("refused new bytes with Accept: %v", err)
	}
}

func TestEPrintInMemorySaysSoWhenThereIsNoEPrint(t *testing.T) {
	base, _ := server(t, http.StatusNotFound, "")
	f := fetcher(base)
	f.SourceBase = base
	_, _, err := f.EPrintInMemory(context.Background(), &Manifest{}, Order{Record: paper("2407.21783", 3, corpus.LicenceArXiv, metadata.SourceAbs), Version: 3})
	var no *NoEPrint
	if !errors.As(err, &no) || !strings.Contains(err.Error(), "2407.21783v3") {
		t.Fatalf("got %v", err)
	}
}

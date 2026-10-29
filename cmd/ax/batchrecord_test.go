package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/fetch"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/witness"
)

// A synthetic e-print: one theorem with a name in its head, which is text of
// the paper and must not reach a record of a record paper (rule D06).
const recordTeX = "\\documentclass{article}\n\\newtheorem{theorem}{Theorem}\n\\begin{document}\n\\begin{theorem}[Main estimate]\\label{thm:main}\nEach $x$ is at most $y$.\n\\end{theorem}\n\\end{document}\n"

// Way R2 of AX2.5: with -record, a record paper is read in memory, and the
// batch writes its structure and nothing of its text, and no file in work/.
func TestARecordPaperIsReadInMemoryAndKeepsNoText(t *testing.T) {
	root := t.TempDir()
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	w.Write([]byte(recordTeX))
	w.Close()
	s := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { rw.Write(gz.Bytes()) }))
	defer s.Close()

	rec := metadata.Record{ID: "2407.21783", Versions: []metadata.Version{{Version: 1, Created: time.Date(2024, 7, 31, 0, 0, 0, 0, time.UTC), Licence: corpus.LicenceArXiv, LicenceFrom: metadata.SourceAbs}}}
	plane := metadata.Plane{Root: root}
	if _, err := plane.Write("2407", []metadata.Record{rec}); err != nil {
		t.Fatal(err)
	}
	id, _ := axid.Parse(rec.ID)
	var m fetch.Manifest
	b := &batch{root: root, witness: &witness.Cache{Root: root}, plane: plane, m: &m, pace: time.Nanosecond, keep: true,
		fetcher: &fetch.Fetcher{SourceBase: s.URL + "/", Pace: time.Nanosecond, UserAgent: "arxiv-reader/test"}}

	if state, _, err := b.one(context.Background(), rec.ID); err != nil || state != "stopped: licence" {
		t.Fatalf("without -record the batch gave %q, %v", state, err)
	}
	b.record = true
	state, did, err := b.one(context.Background(), rec.ID)
	if err != nil || !did || state != "extracted" {
		t.Fatalf("with -record the batch gave %q, %v, %v", state, did, err)
	}
	if exists(corpus.StopPath(root, id, 1)) {
		t.Error("the stop record for the licence is still there")
	}
	if !exists(corpus.CensusPath(root, id, 1)) {
		t.Error("there is no census record")
	}
	if files, _ := filepath.Glob(filepath.Join(root, "work", "*", "*", "*")); len(files) > 0 {
		t.Errorf("the batch wrote %v, and a record paper is read in memory only", files)
	}
	if raw, err := os.ReadFile(corpus.AnalysisObjectsPath(root, id)); err != nil || !strings.Contains(string(raw), "thm:main") {
		t.Errorf("the object records do not keep the label of the theorem: %v\n%s", err, raw)
	}
	if e, ok := m.Find(rec.ID, 1, fetch.RouteSource); !ok || e.SHA256 != fetch.Digest(gz.Bytes()) || e.Holds != fetch.HoldsTeX {
		t.Errorf("the manifest has %+v", e)
	}
	err = filepath.WalkDir(filepath.Join(root, "analysis"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, text := range []string{"Main estimate", "at most", "$x$"} {
			if strings.Contains(string(raw), text) {
				t.Errorf("%s holds the text %q of a record paper", p, text)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if state, did, _ := b.one(context.Background(), rec.ID); state != "done" || did {
		t.Errorf("a second run gave %q, %v", state, did)
	}
}

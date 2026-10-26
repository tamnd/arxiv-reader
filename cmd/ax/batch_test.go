package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/analysis"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/licence"
)

func TestReadIDsTakesTheFirstFieldAndSkipsComments(t *testing.T) {
	file := filepath.Join(t.TempDir(), "x-v1.txt")
	body := "# gold set X v1\nmath/0003117\t1991-2000\t14\n\n2312.00752\tform\tcs.LG\n"
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readIDs(file)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"math/0003117", "2312.00752"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestAStopRecordNamesTheReason(t *testing.T) {
	root := t.TempDir()
	id, err := axid.Parse("math/0003117")
	if err != nil {
		t.Fatal(err)
	}
	b := &batch{root: root}
	state, did, err := b.stop(id, "math/0003117", 2, analysis.StopNoSource, "the e-print is a PDF")
	if err != nil || !did || state != "stopped: no-source" {
		t.Fatalf("got %q %v %v", state, did, err)
	}
	p := corpus.StopPath(root, id, 2)
	if want := filepath.Join(root, "analysis", "stopped", "0003", "math-0003117v2.json"); p != want {
		t.Errorf("the path is %s, want %s", p, want)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var s analysis.Stop
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.State != "stopped" || s.Reason != analysis.StopNoSource || s.By == "" || s.On == "" {
		t.Errorf("got %+v", s)
	}
}

func TestRemoveKeepsFilesOutsideWork(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "metadata.jsonl")
	gone := filepath.Join(root, "work", "source", "x.gz")
	for _, f := range []string{keep, gone} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b := &batch{root: root}
	b.remove(keep)
	b.remove(gone)
	if !exists(keep) {
		t.Error("remove took a file outside work/")
	}
	if exists(gone) {
		t.Error("remove left the download in work/")
	}
}

func TestTheQueueKeepsTheOrderOfAX26(t *testing.T) {
	q := []queued{
		{ID: "math/0101001", Month: "2001-01", Class: licence.ClassOpen, Old: true},
		{ID: "2001.00001", Month: "2020-01", Class: licence.ClassShareAlikeNC},
		{ID: "2001.00002", Month: "2020-01", Class: licence.ClassOpen},
		{ID: "2312.00001", Month: "2023-12", Class: licence.ClassOpen},
		{ID: "2312.00009", Month: "2023-12", Class: licence.ClassShareAlikeNC},
		{ID: "2312.00002", Month: "2023-12", Class: licence.ClassOpen},
	}
	orderQueue(q)
	var got []string
	for _, x := range q {
		got = append(got, x.ID)
	}
	want := "2312.00002 2312.00001 2001.00002 2312.00009 2001.00001 math/0101001"
	if s := strings.Join(got, " "); s != want {
		t.Errorf("got %s, want %s", s, want)
	}
}

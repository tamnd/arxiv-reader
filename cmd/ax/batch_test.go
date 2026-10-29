package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/analysis"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/fetch"
	"github.com/tamnd/arxiv-reader/licence"
	"github.com/tamnd/arxiv-reader/metadata"
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

func TestARedoKeepsTheCensusRecordWhenItStops(t *testing.T) {
	root := t.TempDir()
	id, err := axid.Parse("math/0003117")
	if err != nil {
		t.Fatal(err)
	}
	census := corpus.CensusPath(root, id, 2)
	if err := writeFile(census, []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	b := &batch{root: root, redo: true}
	state, did, err := b.stop(id, "math/0003117", 2, analysis.StopFailed, "the fetch failed")
	if err != nil || !did || state != "stopped: failed" {
		t.Fatalf("got %q %v %v", state, did, err)
	}
	if exists(corpus.StopPath(root, id, 2)) {
		t.Error("the redo wrote a stop record next to the census record")
	}
	if !exists(census) {
		t.Error("the redo removed the census record")
	}
}

func TestABatchGoesOnOnlyWhenTheSourceChangedAtArXiv(t *testing.T) {
	for _, c := range []struct {
		err  error
		want bool
	}{
		{&fetch.Changed{Where: "at the source"}, true},
		{fmt.Errorf("one: %w", &fetch.Changed{Where: "at the source"}), true},
		{&fetch.Changed{Where: "on disk"}, false},
		{errors.New("the network is down"), false},
		{nil, false},
	} {
		if got := changedAtSource(c.err); got != c.want {
			t.Errorf("%v: got %v, want %v", c.err, got, c.want)
		}
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

// A paper of the list with no record in the plane is a typed error, so the
// batch goes on and a redo list does not stop at it each run.
func TestAPaperOutsideThePlaneIsNotInPlane(t *testing.T) {
	id, err := axid.Parse("2106.09685")
	if err != nil {
		t.Fatal(err)
	}
	_, err = record(metadata.Plane{Root: t.TempDir()}, id)
	var missing *notInPlane
	if !errors.As(err, &missing) || missing.ID != "2106.09685" {
		t.Fatalf("got %v, want a paper not in the plane", err)
	}
}

func TestTheRenderSampleIsOneInEveryAndStable(t *testing.T) {
	n := 0
	for i := 0; i < 10000; i++ {
		id := fmt.Sprintf("2610.%05d", i)
		first := sampled(id, 10)
		if first {
			n++
		}
		if sampled(id, 10) != first || !sampled(id, 1) {
			t.Fatalf("the sample of %s changes", id)
		}
	}
	if n < 900 || n > 1100 {
		t.Errorf("one in 10 of 10000 papers is %d", n)
	}
}

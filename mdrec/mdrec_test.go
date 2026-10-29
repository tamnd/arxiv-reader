package mdrec

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func same(t *testing.T, a, b []byte) {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(x, y) {
		t.Errorf("the record\n%s\nreads back as\n%s", a, b)
	}
}

func TestRecordsReadBackTheSame(t *testing.T) {
	recs := [][]byte{
		[]byte(`{"local":"thm-1","number":"1","order":1,"score":0.25,"big":1e-05,"ok":true,"none":null,"labels":[],"span":{"file":"main.tex","from":3,"to":5},"body_md":"Each tree $T$ is a graph.\nIt has: no cycle.\n"}`),
		[]byte(`{"local":"proof-1","body_md":"<!-- end thm-1 -->\n# A head\n\n---\nyes: no\n"}`),
		[]byte(`{"local":"head-2","body_md":"a\n<!-- end head-2 -->\nb"}`),
		[]byte(`{"id":"math/9908107","title":"on: \"x\" & <y>","abstract":"  lead space\ttab é","body_md":""}`),
		[]byte(`{"local":"thm-1","body_md":"the same key again"}`),
		[]byte(`{"n":"007","yes":"yes","date":"2026-10-28","x":"null","e":{}}`),
	}
	data, err := Encode(recs, "body_md")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(data)
	if err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	if len(got) != len(recs) {
		t.Fatalf("%d records read back, want %d", len(got), len(recs))
	}
	for i := range recs {
		same(t, recs[i], got[i])
	}
	s := string(data)
	if !strings.Contains(s, "<!-- thm-1: body_md -->\nEach tree $T$ is a graph.\nIt has: no cycle.\n\n<!-- end thm-1 -->\n") {
		t.Errorf("the body of thm-1 is not in the Markdown:\n%s", s)
	}
	if strings.Count(s, "<!-- head-2: body_md -->") != 0 {
		t.Errorf("a text that holds its own end marker must stay in the header:\n%s", s)
	}
	// The keys stay in their order.
	if !bytes.HasPrefix(got[0], []byte(`{"local":"thm-1","number":"1","order":1,`)) {
		t.Errorf("the keys are out of order: %s", got[0])
	}
}

func TestNoRecordsIsAnEmptyList(t *testing.T) {
	data, err := Encode(nil, "body_md")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "---\nrecords: []\n---\n" {
		t.Errorf("the file is %q", data)
	}
	if got, err := Decode(data); err != nil || len(got) != 0 {
		t.Errorf("got %v, %v", got, err)
	}
}

// A corpus that has not moved to Markdown is read from its JSON Lines, and a
// write moves the file.
func TestAFileOfJSONLinesMoves(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "2501", "2501.00001"+Ext)
	old := strings.TrimSuffix(md, Ext) + Old
	if err := os.MkdirAll(filepath.Dir(md), 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"local":"thm-1","body_md":"Each tree is a graph."}`
	if err := os.WriteFile(old, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := Glob(filepath.Join(dir, "*", "*"+Ext))
	if err != nil || len(files) != 1 || files[0] != md {
		t.Fatalf("the files are %v (%v), and they must be %s", files, err, md)
	}
	recs, err := Read(md)
	if err != nil || len(recs) != 1 || string(recs[0]) != line {
		t.Fatalf("the records are %q (%v)", recs, err)
	}
	if err := Write(md, recs); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the JSON Lines file is still there (%v)", err)
	}
	b, err := os.ReadFile(md)
	if err != nil || !strings.Contains(string(b), "<!-- thm-1: body_md -->\nEach tree is a graph.\n") {
		t.Errorf("the file is %q (%v), and the text must be its body", b, err)
	}
	if files, _ := Glob(filepath.Join(dir, "*", "*"+Ext)); len(files) != 1 {
		t.Errorf("the files are %v, and there must be one", files)
	}
	if _, err := Read(filepath.Join(dir, "none"+Ext)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a file that is not there reads with %v", err)
	}
}

// TestAMovedCorpusReadsTheSame compares each JSON Lines file of the checkout
// MDREC_CORPUS with the Markdown file of the same name in MDREC_MOVED, a copy
// of it that ax md moved.
func TestAMovedCorpusReadsTheSame(t *testing.T) {
	root, moved := os.Getenv("MDREC_CORPUS"), os.Getenv("MDREC_MOVED")
	if root == "" || moved == "" {
		t.Skip("MDREC_CORPUS and MDREC_MOVED are not set")
	}
	n := 0
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, Old) || strings.Contains(p, "/.git/") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		data, _ := os.ReadFile(p)
		want := Lines(data)
		got, err := ReadFile(filepath.Join(moved, strings.TrimSuffix(rel, Old)+Ext))
		if err != nil || len(got) != len(want) {
			t.Errorf("%s: %v, %d of %d", rel, err, len(got), len(want))
			return nil
		}
		for i := range want {
			same(t, want[i], got[i])
		}
		n++
		return nil
	})
	t.Logf("%d files read the same", n)
}

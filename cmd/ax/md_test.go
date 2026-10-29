package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tamnd/arxiv-reader/mdrec"
)

// ax md move moves each file of records to Markdown, and the records read
// back the same.
func TestMoveToMarkdown(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ARXIV_CORPUS", root)
	files := map[string]string{
		"metadata/2106.jsonl":                    `{"id":"2106.00001","title":"A paper","abstract":"An abstract with <, > and &."}` + "\n",
		"analysis/objects/2106/2106.00001.jsonl": `{"local":"thm-1","kind":"statement","body_md":"Each tree is a graph."}` + "\n" + `{"local":"proof-1","kind":"proof"}` + "\n",
		"analysis/edges/2106/2106.00001.jsonl":   `{"from":"2106.00001#proof-1","to":"2106.00001#thm-1","predicate":"proves"}` + "\n",
		"manifests/not-a-plane/2106.00001.jsonl": `{"id":"x"}` + "\n",
	}
	for name, data := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := runMD([]string{"move"}); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		old := filepath.Join(root, name)
		md := old[:len(old)-len(mdrec.Old)] + mdrec.Ext
		if filepath.Dir(name) == "manifests/not-a-plane" {
			if _, err := os.Stat(old); err != nil {
				t.Errorf("%s is not a plane, and it moved", name)
			}
			continue
		}
		if _, err := os.Stat(old); !os.IsNotExist(err) {
			t.Errorf("%s is still there", name)
		}
		b, err := os.ReadFile(md)
		if err != nil || !mdrec.IsMarkdown(b) {
			t.Errorf("%s is not Markdown (%v)", md, err)
		}
		got, err := mdrec.ReadLines(md)
		if err != nil || string(got) != data {
			t.Errorf("%s reads back as %q (%v), and it must be %q", md, got, err, data)
		}
	}
}

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteFileReplacesWhole checks that writeFile puts the new content in
// place of the old and leaves no temporary file next to it.
func TestWriteFileReplacesWhole(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "0003", "math-0003117.jsonl")
	for _, body := range []string{"{\"a\":1}\n{\"a\":2}\n", "{\"a\":3}\n"} {
		if err := writeFile(file, []byte(body)); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != body {
			t.Fatalf("the file has %q, want %q", got, body)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(file))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the directory has %d entries, want the file alone", len(entries))
	}
	if fi, _ := os.Stat(file); fi.Mode().Perm() != 0o644 {
		t.Fatalf("the file has mode %v, want 0644", fi.Mode().Perm())
	}
}

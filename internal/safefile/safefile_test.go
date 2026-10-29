package safefile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAFailedWriteKeepsTheOldFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sources.yaml")
	if err := WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A directory in the place of the file makes the rename fail.
	bad := filepath.Join(dir, "sub")
	if err := os.Mkdir(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "x"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(bad, []byte("new"), 0o644); err == nil {
		t.Fatal("the write over a directory must fail")
	}
	if err := WriteFile(path, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "new" {
		t.Errorf("the file is %q", b)
	}
	es, _ := os.ReadDir(dir)
	if len(es) != 2 {
		t.Errorf("the directory has %d entries, and a failed write must leave no temporary file", len(es))
	}
}

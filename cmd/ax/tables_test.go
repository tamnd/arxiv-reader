package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/extract"
)

// Writing the same paper twice must leave the same bytes, because a
// re-extraction of a finished paper that rewrites forty files is a commit that
// says nothing.
func TestWritingTwiceChangesNothing(t *testing.T) {
	dir := t.TempDir()
	id := axid.ID{Canonical: "2501.00001", Year: 2025, Month: 1}
	found := []tbl{{tag: "1", rows: []extract.Row{
		{Cells: []extract.Cell{{Text: "a", Span: 1, Down: 1}, {Text: "b", Span: 1, Down: 1}}},
	}}}
	if err := writeTables(dir, id, found); err != nil {
		t.Fatal(err)
	}
	before := read(t, dir)
	if err := writeTables(dir, id, found); err != nil {
		t.Fatal(err)
	}
	if after := read(t, dir); after != before {
		t.Fatalf("the second run wrote different bytes:\n%s\n%s", before, after)
	}
	if !strings.Contains(before, "t01.md") || !strings.Contains(before, "t01.tex") {
		t.Fatalf("a table was not written twice:\n%s", before)
	}
}

// A corpus holding a table that was cut between versions is a corpus publishing
// something the paper does not say.
func TestATableThePaperNoLongerHasIsTakenAway(t *testing.T) {
	dir := t.TempDir()
	id := axid.ID{Canonical: "2501.00001", Year: 2025, Month: 1}
	one := tbl{tag: "1", rows: []extract.Row{{Cells: []extract.Cell{{Text: "a", Span: 1, Down: 1}}}}}
	two := tbl{tag: "2", rows: []extract.Row{{Cells: []extract.Cell{{Text: "b", Span: 1, Down: 1}}}}}
	if err := writeTables(dir, id, []tbl{one, two}); err != nil {
		t.Fatal(err)
	}
	if err := writeTables(dir, id, []tbl{one}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir); strings.Contains(got, "t02") {
		t.Fatalf("the second table is still there:\n%s", got)
	}
}

func read(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, e := range entries {
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString("=== " + e.Name() + "\n")
		b.Write(body)
	}
	return b.String()
}

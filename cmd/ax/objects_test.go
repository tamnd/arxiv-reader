package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tamnd/arxiv-reader/objects"
)

// A rebuild of a paper nobody has touched has to leave the file alone, because
// a corpus wide rebuild that rewrites every record is a commit that says
// nothing and a set of timestamps everything downstream then redoes.
func TestASecondBuildOfTheSamePaperWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "work", "objects", "2501", "2501.00001.jsonl")
	p := objects.Paper{Paper: "2501.00001", Version: "v1", Objects: []objects.Record{
		{Kind: "section", Paper: "2501.00001", Version: "v1", Local: "s1", File: "01.md", Path: "render", Confidence: "high"},
	}}
	changed, err := written(p, path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("the first build says it wrote nothing")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	changed, err = written(p, path)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("the second build of an unchanged paper rewrote the record")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("the second build wrote different bytes:\n%s\n%s", before, after)
	}
}

// The arguments this refuses are the ones where carrying on would do something
// other than what was asked.
func TestObjectsRefusesTheArgumentsItShouldRefuse(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"nonsense"},
		{"build"},
		{"count"},
		{"list"},
		{"list", "2501.00001", "2501.00002"},
		{"build", "not-an-id"},
	} {
		if err := runObjects(args); err == nil {
			t.Errorf("ax objects %v was accepted", args)
		}
	}
}

// A paper nobody has extracted is a paper with no objects, and saying so is
// better than writing an empty record that every reader downstream then has to
// tell apart from a paper that really is empty.
func TestAPaperThatHasNotBeenExtractedIsRefused(t *testing.T) {
	t.Setenv("ARXIV_CORPUS", t.TempDir())
	if err := runObjects([]string{"build", "2501.00001"}); err == nil {
		t.Fatal("a paper with no content files was accepted")
	}
}

func TestTheKindColumnCarriesWhatThePaperCalledIt(t *testing.T) {
	if got := kindOf(objects.Record{Kind: "statement", Subkind: "theorem"}); got != "statement/theorem" {
		t.Fatalf("a theorem prints as %q", got)
	}
	if got := kindOf(objects.Record{Kind: "figure"}); got != "figure" {
		t.Fatalf("a figure prints as %q", got)
	}
}

func TestTheHeadingColumnIsWhatThePaperPrinted(t *testing.T) {
	for _, c := range []struct{ number, title, want string }{
		{"1.1", "What Nothing Is", "1.1 What Nothing Is"},
		{"1", "", "1"},
		{"", "Introduction", "Introduction"},
		{"", "", ""},
	} {
		if got := heading(objects.Record{Number: c.number, Title: c.title}); got != c.want {
			t.Errorf("number %q and title %q print as %q", c.number, c.title, got)
		}
	}
}

func TestABodyIsFoldedOntoOneLine(t *testing.T) {
	if got := oneLine("Nothing at all,\nsaid over\n\ntwo paragraphs."); got != "Nothing at all, said over two paragraphs." {
		t.Fatalf("a body folded to %q", got)
	}
}

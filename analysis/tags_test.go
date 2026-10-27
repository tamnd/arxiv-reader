package analysis_test

import (
	"testing"

	"github.com/tamnd/arxiv-reader/analysis"
	"github.com/tamnd/arxiv-reader/objects"
	"github.com/tamnd/arxiv-reader/tags"
)

func rec(version, local, kind, label, hash string) objects.Record {
	return objects.Record{Paper: "2401.00001", Version: version, Local: local, File: "main.tex", Kind: kind, Label: label, Hash: hash, Path: "source"}
}

func TestEachObjectGetsATag(t *testing.T) {
	recs := []objects.Record{rec("1", "lemma-1", "statement", "l1", "h1"), rec("1", "proof-1", "proof", "", "h2")}
	got, err := analysis.AssignTags("2401.00001", 1, recs, nil, tags.Register{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Added != 2 || recs[0].Tag == "" || recs[1].Tag == "" || recs[0].Tag == recs[1].Tag {
		t.Fatalf("added %d, tags %q %q", got.Added, recs[0].Tag, recs[1].Tag)
	}
	if got.Register.Version != 1 {
		t.Fatalf("register version %d", got.Register.Version)
	}
}

func TestATagStaysWhenTheNumbersMove(t *testing.T) {
	v1 := []objects.Record{rec("1", "lemma-1", "statement", "", "a"), rec("1", "lemma-2", "statement", "", "b")}
	first, err := analysis.AssignTags("2401.00001", 1, v1, nil, tags.Register{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Version 2 puts a new lemma first, so the old lemmas are renumbered.
	v2 := []objects.Record{rec("2", "lemma-1", "statement", "", "new"), rec("2", "lemma-2", "statement", "", "a"), rec("2", "lemma-3", "statement", "", "b")}
	second, err := analysis.AssignTags("2401.00001", 2, v2, v1, first.Register, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v2[1].Tag != v1[0].Tag || v2[2].Tag != v1[1].Tag {
		t.Fatalf("tags moved: v1 %q %q, v2 %q %q", v1[0].Tag, v1[1].Tag, v2[1].Tag, v2[2].Tag)
	}
	if second.Kept != 2 || second.Added != 1 {
		t.Fatalf("kept %d, added %d", second.Kept, second.Added)
	}
}

func TestAnObjectThatIsGoneGetsATombstone(t *testing.T) {
	v1 := []objects.Record{rec("1", "lemma-1", "statement", "", "a"), rec("1", "lemma-2", "statement", "", "b")}
	first, err := analysis.AssignTags("2401.00001", 1, v1, nil, tags.Register{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	v2 := []objects.Record{rec("2", "lemma-1", "statement", "", "a")}
	second, err := analysis.AssignTags("2401.00001", 2, v2, v1, first.Register, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.Buried != 1 || v2[0].Tag != v1[0].Tag {
		t.Fatalf("buried %d, tag %q want %q", second.Buried, v2[0].Tag, v1[0].Tag)
	}
	for _, e := range second.Register.Live() {
		if e.Local == "lemma-2" {
			t.Fatal("the gone object still has a live tag")
		}
	}
}

func TestATagOfTheContentPlaneIsNotTaken(t *testing.T) {
	recs := []objects.Record{rec("1", "lemma-1", "statement", "", "a")}
	free, err := analysis.AssignTags("2401.00001", 1, recs, nil, tags.Register{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	recs = []objects.Record{rec("1", "lemma-1", "statement", "", "a")}
	if _, err := analysis.AssignTags("2401.00001", 1, recs, nil, tags.Register{}, []tags.Tag{tags.Tag(free.Register.Entries[0].Tag)}); err != nil {
		t.Fatal(err)
	}
	if recs[0].Tag == string(free.Register.Entries[0].Tag) {
		t.Fatalf("tag %q of the content plane was taken", recs[0].Tag)
	}
}

func TestAnOlderVersionIsRefused(t *testing.T) {
	if _, err := analysis.AssignTags("2401.00001", 1, []objects.Record{rec("1", "lemma-1", "statement", "", "a")}, nil, tags.Register{Version: 2, Entries: []tags.Entry{{Tag: "0A1B", Local: "lemma-1"}}}, nil); err == nil {
		t.Fatal("want an error")
	}
}

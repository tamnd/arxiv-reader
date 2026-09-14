package refs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestACitationManifestSurvivesBeingWrittenAndReadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "2311", "2311.05762.yaml")
	want := Cites{Paper: "2311.05762", Version: 2, Cites: []Cite{
		{Entry: "bib.bib12", Section: "01_introduction.md"},
		{Entry: "bib.bib7", Section: "02_proof.md", Kind: "lemma", Number: "3.4", Via: "after-comma", Text: "... \\[[7](#bib.bib7), Lemma 3.4\\] ..."},
	}}
	changed, err := want.Save(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("writing a manifest that was not there said nothing changed")
	}
	got, err := LoadCites(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Cites) != 2 || got.Paper != want.Paper || got.Version != want.Version {
		t.Fatalf("read back %+v", got)
	}
	if got.Cites[1] != want.Cites[1] {
		t.Fatalf("the located citation read back as %+v", got.Cites[1])
	}
}

func TestWritingTheSameCitationsTwiceChangesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "2311.05762.yaml")
	c := Cites{Paper: "2311.05762", Version: 2, Cites: []Cite{{Entry: "bib.bib12", Section: "01_introduction.md"}}}
	if _, err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	changed, err := c.Save(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("the second write of the same citations said the bytes changed")
	}
}

func TestAPaperNobodyHasReadHasNoCitationsAndThatIsNotAnError(t *testing.T) {
	got, err := LoadCites(filepath.Join(t.TempDir(), "nothing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Cites) != 0 {
		t.Fatalf("a file that is not there read as %+v", got)
	}
}

func TestACitationThatPointsAtNothingIsRefused(t *testing.T) {
	for _, c := range []struct{ name, yaml string }{
		{"no entry", "paper: \"2311.05762\"\ncites:\n    - section: 01_introduction.md\n"},
		{"no section", "paper: \"2311.05762\"\ncites:\n    - entry: bib.bib12\n"},
	} {
		path := filepath.Join(t.TempDir(), "c.yaml")
		if err := os.WriteFile(path, []byte(c.yaml), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadCites(path); err == nil {
			t.Errorf("a citation with %s was accepted", c.name)
		}
	}
}

// An anchor is only unique inside one version, so a citation pointing at an
// anchor the bibliography does not carry means the two were read from different
// versions and the number means something else now.
func TestACitationToAnAnchorTheBibliographyDoesNotCarryIsNotRecorded(t *testing.T) {
	m := Manifest{Entries: []Entry{{ID: "bib.bib1", Text: "N. Nobody. On nothing. 1999."}}}
	kept, orphan := m.Attach([]Cite{
		{Entry: "bib.bib1", Section: "a.md"},
		{Entry: "bib.bib9", Section: "a.md"},
		{Entry: "bib.bib9", Section: "b.md"},
	})
	if len(kept) != 1 || kept[0].Entry != "bib.bib1" {
		t.Fatalf("kept %+v", kept)
	}
	if len(orphan) != 1 || orphan[0] != "bib.bib9" {
		t.Fatalf("named %v, and one anchor cited twice is one thing to fix", orphan)
	}
}

func TestLocatedIsTheCitationsAUsesEdgeCanBeBuiltOn(t *testing.T) {
	c := Cites{Cites: []Cite{
		{Entry: "bib.bib12", Section: "a.md"},
		{Entry: "bib.bib7", Section: "a.md", Kind: "lemma", Number: "3.4"},
	}}
	got := c.Located()
	if len(got) != 1 || got[0].Entry != "bib.bib7" {
		t.Fatalf("came out as %+v", got)
	}
}

func TestTheManifestSaysWhatWroteItAndWhatItIsFor(t *testing.T) {
	b, err := Cites{Paper: "2311.05762"}.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "# Every citation one paper makes") {
		t.Fatal("the manifest does not say what it holds, and a file in a corpus nobody can read is a file somebody will edit by hand")
	}
}

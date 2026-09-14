package refs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sample() Manifest {
	return Manifest{
		Paper:   "2312.00752",
		Version: 2,
		Entries: []Entry{
			{ID: "bib.bibx1", Label: "Arjovsky et al. (2016)", Title: "Unitary Evolution Recurrent Neural Networks", Year: 2016, Text: "Martin Arjovsky et al, 2016"},
			{ID: "bib.bibx2", Label: "Gu et al. (2022)", ArXiv: "2111.00396", Year: 2022, Text: "Albert Gu et al, 2022"},
		},
	}
}

func TestAManifestSurvivesTheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "2312.00752.yaml")
	if _, err := sample().Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Paper != "2312.00752" || got.Version != 2 {
		t.Errorf("came back as %sv%d", got.Paper, got.Version)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("%d entries, want 2", len(got.Entries))
	}
	if got.Entries[0].Title != "Unitary Evolution Recurrent Neural Networks" {
		t.Errorf("first title is %q", got.Entries[0].Title)
	}
}

// The order the paper prints is the numbering in a numeric style, so it is
// information and not presentation, and nothing sorts it away.
func TestThePrintedOrderIsKept(t *testing.T) {
	m := sample()
	m.Entries[0], m.Entries[1] = m.Entries[1], m.Entries[0]
	path := filepath.Join(t.TempDir(), "2312.00752.yaml")
	if _, err := m.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Entries[0].ID != "bib.bibx2" {
		t.Errorf("the first entry is %q, and the paper printed it second", got.Entries[0].ID)
	}
}

func TestWritingTheSameManifestTwiceChangesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "2312.00752.yaml")
	changed, err := sample().Save(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("the first write reported no change")
	}
	changed, err = sample().Save(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("the second write reported a change, and nothing in the manifest moved")
	}
}

// A paper nobody has run ax refs over has no references, which is a fact and
// not a failure.
func TestAManifestThatIsNotThereIsEmpty(t *testing.T) {
	m, err := Load(filepath.Join(t.TempDir(), "nothing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Entries) != 0 {
		t.Errorf("%d entries came out of a file that does not exist", len(m.Entries))
	}
}

func TestAnEntryWithNoAnchorIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "2312.00752.yaml")
	body := "paper: \"2312.00752\"\nversion: 2\nentries:\n    - text: a line with nothing pointing at it\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "no id") {
		t.Errorf("the error is %v", err)
	}
}

func TestAnEntryWithNoTextIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "2312.00752.yaml")
	body := "paper: \"2312.00752\"\nversion: 2\nentries:\n    - id: bib.bibx1\n      title: a title and nothing printed\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "no text") {
		t.Errorf("the error is %v", err)
	}
}

func TestFind(t *testing.T) {
	m := sample()
	e, ok := m.Find("bib.bibx2")
	if !ok {
		t.Fatal("the anchor the paper points at was not found")
	}
	if e.ArXiv != "2111.00396" {
		t.Errorf("found %q", e.ArXiv)
	}
	if _, ok := m.Find("bib.bibx99"); ok {
		t.Error("an anchor no entry has was found")
	}
}

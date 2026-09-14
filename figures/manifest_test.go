package figures

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/arxiv-reader/corpus"
)

func entry(paper string, version int, source, file string) Figure {
	return Figure{
		Paper:   paper,
		Version: version,
		Number:  "1",
		Source:  source,
		File:    file,
		SHA256:  Digest([]byte(source)),
		Bytes:   1024,
		Format:  FormatPNG,
		Width:   400,
		Height:  300,
		Caption: "Nothing, drawn.",
		Licence: corpus.LicenceCCBY,
		Fetched: time.Now().UTC().Truncate(time.Second),
	}
}

func save(t *testing.T, m Manifest) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifests", "figures", "2501.yaml")
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}
	return path
}

// A file that is not there is an empty manifest, because the first paper of a
// month has nothing to read and a missing file is not a broken one.
func TestLoadingAManifestThatIsNotThereIsEmpty(t *testing.T) {
	m, err := Load(filepath.Join(t.TempDir(), "nothing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Figures) != 0 {
		t.Errorf("read %d figures out of a file that does not exist", len(m.Figures))
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	want := entry("2501.00001", 3, "2501.00001v3/one.png", "/figures/2501/2501.00001/one.png")
	path := save(t, Manifest{Figures: []Figure{want}})
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Figures) != 1 {
		t.Fatalf("read %d figures, want 1", len(m.Figures))
	}
	if got := m.Figures[0]; got != want {
		t.Errorf("read back %+v, want %+v", got, want)
	}
}

// The manifest is committed and everything about it is built for a git diff, so
// a run that decides the same things twice has to produce the same bytes twice
// whatever order the decisions arrived in.
func TestSaveSortsSoThatARerunHasNoDiff(t *testing.T) {
	a := entry("2501.00002", 1, "2501.00002v1/b.png", "/figures/2501/2501.00002/b.png")
	b := entry("2501.00001", 3, "2501.00001v3/z.png", "/figures/2501/2501.00001/z.png")
	c := entry("2501.00001", 3, "2501.00001v3/a.png", "/figures/2501/2501.00001/a.png")
	d := entry("2501.00001", 1, "2501.00001v1/a.png", "/figures/2501/2501.00001/a.png")

	one, err := os.ReadFile(save(t, Manifest{Figures: []Figure{a, b, c, d}}))
	if err != nil {
		t.Fatal(err)
	}
	two, err := os.ReadFile(save(t, Manifest{Figures: []Figure{d, c, b, a}}))
	if err != nil {
		t.Fatal(err)
	}
	if string(one) != string(two) {
		t.Fatalf("two orders of the same figures wrote two files:\n%s\n%s", one, two)
	}
	order := []string{"2501.00001v1/a.png", "2501.00001v3/a.png", "2501.00001v3/z.png", "2501.00002v1/b.png"}
	at := 0
	for _, want := range order {
		i := strings.Index(string(one), want)
		if i < at {
			t.Errorf("%s is out of order in\n%s", want, one)
		}
		at = i
	}
}

// Every one of these is a manifest that says less than nothing: it looks like a
// record of a decision and it cannot be used as one.
func TestLoadRefusesAnEntryThatSaysNothingUseful(t *testing.T) {
	for name, broken := range map[string]Figure{
		"no paper":   {Version: 1, Source: "a.png", SHA256: "x"},
		"no version": {Paper: "2501.00001", Source: "a.png", SHA256: "x"},
		"no source":  {Paper: "2501.00001", Version: 1, SHA256: "x"},
		"no hash":    {Paper: "2501.00001", Version: 1, Source: "a.png"},
		"no caption on a committed figure": {
			Paper: "2501.00001", Version: 1, Number: "1",
			Source: "a.png", SHA256: "x", File: "/figures/2501/2501.00001/a.png",
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := save(t, Manifest{Figures: []Figure{broken}})
			if _, err := Load(path); err == nil {
				t.Fatalf("loaded a manifest with %s", name)
			}
		})
	}
}

// A withheld figure has no caption requirement, because rule F07 is about what
// is published and a withheld figure is the one thing that is not.
func TestAWithheldEntryNeedsNoCaption(t *testing.T) {
	f := Figure{Paper: "2501.00001", Version: 1, Number: "1", Source: "a.png", SHA256: "x", Rule: "F09"}
	if _, err := Load(save(t, Manifest{Figures: []Figure{f}})); err != nil {
		t.Fatal(err)
	}
}

func TestPutReplacesTheSamePicture(t *testing.T) {
	var m Manifest
	m.Put(entry("2501.00001", 3, "a.png", "/figures/a.png"))
	m.Put(entry("2501.00001", 3, "a.png", ""))
	if len(m.Figures) != 1 {
		t.Fatalf("holding %d entries for one picture", len(m.Figures))
	}
	if m.Figures[0].Committed() {
		t.Error("the second decision did not replace the first")
	}
	// A different version of the same paper is a different picture, because the
	// licence belongs to the version and so does the figure.
	m.Put(entry("2501.00001", 4, "a.png", "/figures/a.png"))
	if len(m.Figures) != 2 {
		t.Fatalf("holding %d entries for two versions", len(m.Figures))
	}
}

func TestForgetDropsOneVersionAndLeavesTheRest(t *testing.T) {
	m := Manifest{Figures: []Figure{
		entry("2501.00001", 3, "a.png", "/figures/a.png"),
		entry("2501.00001", 4, "a.png", "/figures/a.png"),
		entry("2501.00002", 3, "a.png", "/figures/a.png"),
	}}
	m.Forget("2501.00001", 3)
	if len(m.Figures) != 2 {
		t.Fatalf("left %d entries, want 2", len(m.Figures))
	}
	for _, f := range m.Figures {
		if f.Paper == "2501.00001" && f.Version == 3 {
			t.Error("forgot the wrong things")
		}
	}
}

// The index is what the cross paper half of rule F09 reads. A withheld picture
// has no bytes in the corpus, so it has nothing to accuse anybody of, and
// letting it own a hash would withhold the same picture from every paper that
// legitimately has it.
func TestIndexIsBuiltFromCommittedFiguresOnly(t *testing.T) {
	committed := entry("2501.00001", 3, "a.png", "/figures/a.png")
	refused := entry("2501.00002", 1, "b.png", "")
	refused.SHA256 = "deadbeef"
	index := Manifest{Figures: []Figure{committed, refused}}.Index()
	if len(index) != 1 {
		t.Fatalf("the index holds %d hashes, want 1", len(index))
	}
	owner, ok := index[committed.SHA256]
	if !ok || owner.Paper != "2501.00001" || owner.Licence != string(corpus.LicenceCCBY) {
		t.Errorf("the owner is %+v", owner)
	}
	if _, found := index["deadbeef"]; found {
		t.Error("a withheld figure is claiming to own its bytes")
	}
}

// The round trip through Index and Elsewhere is the rule working, and it is
// worth one test because the two halves are written in different files and
// nothing else holds them together.
func TestTheIndexCatchesTheSamePictureUnderAnotherLicence(t *testing.T) {
	index := Manifest{Figures: []Figure{entry("2501.00001", 3, "a.png", "/figures/a.png")}}.Index()
	sum := Digest([]byte("a.png"))
	if s, _ := Elsewhere(sum, "2501.00009", "arxiv", index); s != Confirmed {
		t.Errorf("the same bytes under a different licence read as %q", s)
	}
	if s, _ := Elsewhere(sum, "2501.00009", string(corpus.LicenceCCBY), index); s != Owned {
		t.Errorf("the same bytes under the same licence read as %q", s)
	}
}

func TestRefIsTheVersionedReference(t *testing.T) {
	if got := entry("math/0211159", 2, "a.png", "").Ref(); got != "math/0211159v2" {
		t.Errorf("the reference is %q", got)
	}
}

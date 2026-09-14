package fetch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
)

func entry(id string, version int, sum string) Source {
	return Source{
		ID:          id,
		Version:     version,
		Route:       RouteRender,
		URL:         HTMLBase + id,
		Path:        "work/html/2312/" + id + ".html",
		SHA256:      sum,
		Bytes:       int64(len(sum)),
		Fetched:     time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC),
		Licence:     corpus.LicenceCCBY,
		LicenceFrom: metadata.SourceAbs,
	}
}

func TestLoadOfAFileThatIsNotThereIsAnEmptyManifest(t *testing.T) {
	m, err := Load(filepath.Join(t.TempDir(), "manifests", "sources.yaml"))
	if err != nil {
		t.Fatalf("a corpus with no manifest yet: %v", err)
	}
	if len(m.Sources) != 0 {
		t.Fatalf("got %d sources, want none", len(m.Sources))
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifests", "sources.yaml")
	want := Manifest{Sources: []Source{entry("2312.00752", 2, "abc123")}}
	if err := want.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sources) != 1 {
		t.Fatalf("got %d sources, want 1", len(got.Sources))
	}
	if got.Sources[0] != want.Sources[0] {
		t.Fatalf("got %+v, want %+v", got.Sources[0], want.Sources[0])
	}
}

func TestSaveWritesTheHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sources.yaml")
	if err := (Manifest{Sources: []Source{entry("2312.00752", 1, "a")}}).Save(path); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "# Every artefact") {
		t.Fatalf("the file does not say what it is:\n%s", b)
	}
	if !strings.Contains(string(b), "Do not edit by hand") {
		t.Fatalf("the file does not say who writes it:\n%s", b)
	}
}

func TestSaveSorts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sources.yaml")
	m := Manifest{Sources: []Source{
		entry("2404.19756", 1, "d"),
		entry("2312.00752", 2, "b"),
		entry("2312.00752", 1, "a"),
	}}
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2312.00752v1", "2312.00752v2", "2404.19756v1"}
	for i, w := range want {
		if got.Sources[i].Ref() != w {
			t.Fatalf("entry %d is %s, want %s", i, got.Sources[i].Ref(), w)
		}
	}
}

// Saving the same manifest twice has to produce the same bytes, because this
// file is committed and a re-save that reorders or re-times anything is a diff
// on a change that did not happen.
func TestSaveIsStable(t *testing.T) {
	dir := t.TempDir()
	m := Manifest{Sources: []Source{entry("2404.19756", 1, "d"), entry("2312.00752", 2, "b")}}
	first := filepath.Join(dir, "one.yaml")
	second := filepath.Join(dir, "two.yaml")
	if err := m.Save(first); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.Save(second); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(first)
	b, _ := os.ReadFile(second)
	if string(a) != string(b) {
		t.Fatalf("a round trip changed the file:\n%s\nand\n%s", a, b)
	}
}

func TestLoadRefusesAnEntryThatSaysNothing(t *testing.T) {
	cases := map[string]string{
		"no paper":   "sources:\n  - version: 1\n    sha256: abc\n",
		"no version": "sources:\n  - id: 2312.00752\n    sha256: abc\n",
		"no hash":    "sources:\n  - id: 2312.00752\n    version: 1\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sources.yaml")
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("loaded an entry that records nothing worth having")
			}
		})
	}
}

func TestLoadRefusesSomethingThatIsNotYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sources.yaml")
	if err := os.WriteFile(path, []byte("\tnot: [yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("loaded something that is not a manifest")
	}
}

func TestPutReplacesTheSameArtefact(t *testing.T) {
	var m Manifest
	m.Put(entry("2312.00752", 2, "first"))
	m.Put(entry("2312.00752", 2, "second"))
	if len(m.Sources) != 1 {
		t.Fatalf("got %d sources, want the second to have replaced the first", len(m.Sources))
	}
	if m.Sources[0].SHA256 != "second" {
		t.Fatalf("got %s, want second", m.Sources[0].SHA256)
	}
}

func TestPutKeepsTwoVersionsApart(t *testing.T) {
	var m Manifest
	m.Put(entry("2312.00752", 2, "two"))
	m.Put(entry("2312.00752", 1, "one"))
	if len(m.Sources) != 2 {
		t.Fatalf("got %d sources, want 2", len(m.Sources))
	}
	if m.Sources[0].Version != 1 {
		t.Fatalf("put left the manifest unsorted: %v", m.Sources)
	}
}

func TestFind(t *testing.T) {
	m := Manifest{Sources: []Source{entry("2312.00752", 2, "b")}}
	if _, ok := m.Find("2312.00752", 2, RouteRender); !ok {
		t.Fatal("did not find an entry that is there")
	}
	if _, ok := m.Find("2312.00752", 1, RouteRender); ok {
		t.Fatal("found a version that is not there")
	}
	if _, ok := m.Find("2312.00752", 2, Route("source")); ok {
		t.Fatal("found a route that is not there")
	}
}

func TestVerifyReadsTheDisk(t *testing.T) {
	root := t.TempDir()
	body := []byte("<html>a rendering</html>")
	ok := entry("2312.00752", 1, Digest(body))
	write(t, filepath.Join(root, filepath.FromSlash(ok.Path)), body)

	missing := entry("2312.00752", 2, Digest([]byte("nothing")))
	missing.Path = "work/html/2312/2312.00752v2.html"

	changed := entry("2404.19756", 5, Digest([]byte("what it was")))
	changed.Path = "work/html/2404/2404.19756v5.html"
	write(t, filepath.Join(root, filepath.FromSlash(changed.Path)), []byte("what it is now"))

	m := Manifest{Sources: []Source{ok, missing, changed}}
	got, err := m.Verify(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []State{StateOK, StateMissing, StateChanged}
	for i, w := range want {
		if got[i].State != w {
			t.Fatalf("entry %d is %s, want %s", i, got[i].State, w)
		}
	}
	if got[1].Got != "" {
		t.Fatalf("a missing file hashed to %q, and there is nothing there to hash", got[1].Got)
	}
	if got[2].Got == got[2].Source.SHA256 {
		t.Fatal("a changed file reported the hash the manifest holds rather than the one on disk")
	}

	counts := Count(got)
	if counts[StateOK] != 1 || counts[StateMissing] != 1 || counts[StateChanged] != 1 {
		t.Fatalf("counted %v", counts)
	}
}

// Count reports a zero for every state, so that a caller printing all three
// does not have to tell a state with no entries apart from a state it forgot.
func TestCountOfNothingIsThreeZeroes(t *testing.T) {
	counts := Count(nil)
	if len(counts) != 3 {
		t.Fatalf("got %v, want a zero for each of the three states", counts)
	}
}

func TestShort(t *testing.T) {
	if got := Short(Digest([]byte("a"))); len(got) != 12 {
		t.Fatalf("got %q, want twelve characters", got)
	}
	if got := Short("abc"); got != "abc" {
		t.Fatalf("got %q, want a short digest returned whole", got)
	}
}

func write(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

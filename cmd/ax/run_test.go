package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/fetch"
	"github.com/tamnd/arxiv-reader/selection"
)

// chosen is one row of a selection, with the evidence its reason needs so that it
// survives being written and read back.
func chosen(id string, version int, path selection.Path, status selection.Status) selection.Entry {
	e := selection.Entry{
		ID:      id,
		Version: version,
		Reason:  selection.Seed,
		By:      "tamnd",
		Added:   "2026-09-15",
		Status:  status,
	}
	if path != "" {
		e.Path, e.PathWhy = path, "because this is a test and somebody said so"
	}
	return e
}

// The vision path and the native path read the same PDF, because they are two ways
// of reading one file and not two downloads. A batch that fetched a second copy for
// the vision path would spend a request on a file it already had.
func TestTheFourPathsReadThreeSurfaces(t *testing.T) {
	for path, want := range map[selection.Path]fetch.Route{
		selection.PathRender: fetch.RouteRender,
		selection.PathSource: fetch.RouteSource,
		selection.PathNative: fetch.RouteNative,
		selection.PathVision: fetch.RouteNative,
	} {
		if got := routeFor(path); got != want {
			t.Errorf("the %s path reads the %s route, and it should read %s", path, got, want)
		}
	}
	for route, want := range map[fetch.Route]string{
		fetch.RouteRender: "rendering",
		fetch.RouteSource: "e-print",
		fetch.RouteNative: "PDF",
	} {
		if got := artefact(route); got != want {
			t.Errorf("the %s route brings back a %q", route, got)
		}
	}
}

func TestOnlyThePapersThisRunIsAboutAreQueued(t *testing.T) {
	var m selection.Manifest
	m.Put(chosen("2312.00752", 2, selection.PathRender, selection.StatusSelected))
	m.Put(chosen("2501.00001", 3, selection.PathVision, selection.StatusExtracted))
	m.Put(chosen("2501.00002", 1, "", selection.StatusSelected))

	all, err := runWanted(m, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("a run over everything queued %d of 3 papers", len(all))
	}
	// A paper already at the rung is queued and not filtered out, because a run that
	// says nothing about the papers it skipped is a run nobody can check.
	month, err := runWanted(m, nil, "2501", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(month) != 2 {
		t.Errorf("one month queued %d of 2 papers", len(month))
	}
	one, err := runWanted(m, nil, "", "vision")
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 || one[0].ID != "2501.00001" {
		t.Errorf("one path queued %v", one)
	}
	named, err := runWanted(m, []string{"2312.00752v2"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(named) != 1 || named[0].ID != "2312.00752" {
		t.Errorf("a named paper queued %v", named)
	}
	// A paper nobody chose is not a paper this run can do anything with, and it is
	// said rather than skipped, because somebody typed that reference on purpose.
	if _, err := runWanted(m, []string{"1706.03762v7"}, "", ""); err == nil {
		t.Error("a paper that is not in the selection was queued anyway")
	}
	if _, err := runWanted(m, nil, "", "ocr"); err == nil {
		t.Error("a path that does not exist was accepted as a filter")
	}
}

// A dry run is worth having only if it says what the real run would spend, so every
// case that costs something says so and every case that costs nothing says that.
func TestADryRunSaysWhatEachPaperWouldCost(t *testing.T) {
	root := t.TempDir()
	rel := filepath.Join("work", "html", "2312", "2312.00752v2.html")
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, rel), []byte("a rendering"), 0o644); err != nil {
		t.Fatal(err)
	}
	var sources fetch.Manifest
	sources.Put(fetch.Source{ID: "2312.00752", Version: 2, Route: fetch.RouteRender, Path: filepath.ToSlash(rel), SHA256: "0"})
	// The manifest has this one and the disk does not, which is every fresh clone,
	// because work/ is not committed.
	sources.Put(fetch.Source{ID: "2006.10256", Version: 1, Route: fetch.RouteSource, Path: "work/source/2006/2006.10256v1.gz", SHA256: "1"})
	r := &runner{root: root, sources: sources, target: selection.StatusExtracted}

	cases := []struct {
		name  string
		entry selection.Entry
		want  string
	}{
		{"a rendering that is already here", chosen("2312.00752", 2, selection.PathRender, selection.StatusSelected), "read it on the render path"},
		{"an e-print the manifest has and the disk does not", chosen("2006.10256", 1, selection.PathSource, selection.StatusFetched), "fetch the e-print again"},
		{"a paper nothing has been fetched for", chosen("2501.00009", 1, selection.PathNative, selection.StatusSelected), "fetch the PDF, then read it on the native path"},
		{"a paper with no path", chosen("2501.00002", 1, "", selection.StatusSelected), "until ax path decide says"},
		{"a paper on the path that costs money", chosen("2501.00003", 1, selection.PathVision, selection.StatusSelected), "until a reader is named"},
		{"a paper that is already there", chosen("2501.00004", 1, selection.PathRender, selection.StatusExtracted), ""},
	}
	for _, c := range cases {
		got := r.plan(c.entry)
		if c.want == "" {
			if got != "" {
				t.Errorf("%s: plans %q, and it should plan nothing", c.name, got)
			}
			continue
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: plans %q, which says nothing about %q", c.name, got, c.want)
		}
	}
	// And the tags are a rung, so a run going that far says so before it gets there.
	r.target = selection.StatusTagged
	if got := r.plan(chosen("2501.00004", 1, selection.PathRender, selection.StatusExtracted)); !strings.Contains(got, "assign its tags") {
		t.Errorf("a run to tagged plans %q for an extracted paper", got)
	}
}

// The selection is written per paper and not at the end, which is what makes a run
// that was interrupted after four hours worth the four hours. A paper that did not
// move is not written, so a run that changed nothing leaves no diff.
func TestOnlyAPaperThatMovedIsWrittenBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifests", "selected.yaml")
	before := chosen("2312.00752", 2, selection.PathRender, selection.StatusSelected)
	var m selection.Manifest
	m.Put(before)
	r := &runner{sel: m, selpath: path}

	if err := r.record(before, before); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a run that moved nothing wrote the selection anyway")
	}
	after := before
	after.Status = selection.StatusExtracted
	if err := r.record(after, before); err != nil {
		t.Fatal(err)
	}
	back, err := selection.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := back.Find("2312.00752")
	if !ok || e.Status != selection.StatusExtracted {
		t.Errorf("the paper came back at %q, %v", e.Status, ok)
	}
	// A demotion is a change worth writing as well, and it is the one change that is
	// not a rung.
	demoted := after
	demoted.Path, demoted.PathWhy = selection.PathSource, "the rendering holds errors the reject rule will not accept, so the TeX is compiled here"
	if err := r.record(demoted, after); err != nil {
		t.Fatal(err)
	}
	back, err = selection.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if e, _ := back.Find("2312.00752"); e.Path != selection.PathSource {
		t.Errorf("the demoted paper came back on the %s path", e.Path)
	}
}

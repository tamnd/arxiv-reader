package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
)

// touch writes an empty file and the directories above it.
func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEveryPaperWithARegisterIsReported(t *testing.T) {
	root := t.TempDir()
	touch(t, filepath.Join(root, "tags", "2305", "2305.18290.tags"))
	touch(t, filepath.Join(root, "tags", "2305", "2305.18290.runs"))
	touch(t, filepath.Join(root, "tags", "2106", "2106.09685.tags"))
	// An old style identifier writes its slash as a hyphen on disk, and it has to
	// come back as the identifier it was.
	touch(t, filepath.Join(root, "tags", "0704", "math-0701234.tags"))

	got, err := registered(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2106.09685", "2305.18290", "math/0701234"}
	if len(got) != len(want) {
		t.Fatalf("found %v and the registers are %v", got, want)
	}
	for i, id := range got {
		if id.Canonical != want[i] {
			t.Errorf("paper %d is %s and not %s, so the order is not the identifier's", i, id.Canonical, want[i])
		}
	}
}

// A corpus nobody has tagged is not an error. The report over it says so, which
// is the thing to print rather than a stack trace.
func TestACorpusWithNoRegistersIsEmptyAndNotAnError(t *testing.T) {
	got, err := registered(t.TempDir())
	if err != nil || len(got) != 0 {
		t.Fatalf("found %v with %v", got, err)
	}
}

// Either half of the cache is enough to read a version's objects out of, because
// paperAt takes whichever is there.
func TestAVersionIsCachedWhenEitherHalfOfTheCacheHasIt(t *testing.T) {
	root := t.TempDir()
	id := axid.ID{Canonical: "2305.18290", Year: 2023, Month: 5}
	touch(t, corpus.ConvertedPath(root, id, 1))
	touch(t, corpus.ConvertedPath(root, id, 2))
	touch(t, corpus.RenderPath(root, id, 2))
	touch(t, corpus.RenderPath(root, id, 4))
	// Another paper in the same month, whose versions are not this one's.
	other := axid.ID{Canonical: "2305.10601", Year: 2023, Month: 5}
	touch(t, corpus.RenderPath(root, other, 9))

	got := cached(root, id)
	want := []int{1, 2, 4}
	if len(got) != len(want) {
		t.Fatalf("cached %v and the versions are %v", got, want)
	}
	for i, v := range got {
		if v != want[i] {
			t.Fatalf("cached %v and the versions are %v", got, want)
		}
	}
}

func TestAPaperWithNothingInTheCacheHasNoVersions(t *testing.T) {
	id := axid.ID{Canonical: "2305.18290", Year: 2023, Month: 5}
	if got := cached(t.TempDir(), id); len(got) != 0 {
		t.Fatalf("cached %v out of an empty corpus", got)
	}
}

// The glob is built by putting the number back as a star, and a corpus living
// under a directory that spells the same thing would otherwise be globbed in the
// wrong place.
func TestTheGlobIsBuiltFromTheLastVersionInThePath(t *testing.T) {
	got := star("/home/v-1/corpus/work/html/2305/2305.18290v-1.html")
	want := "/home/v-1/corpus/work/html/2305/2305.18290v*.html"
	if got != want {
		t.Errorf("the glob is %s and not %s", got, want)
	}
}

package corpus

import (
	"strings"
	"testing"

	"github.com/tamnd/arxiv-cli/pkg/axid"
)

func mustParse(t *testing.T, ref string) axid.ID {
	t.Helper()
	id, err := axid.Parse(ref)
	if err != nil {
		t.Fatalf("axid.Parse(%q): %v", ref, err)
	}
	return id
}

func TestShard(t *testing.T) {
	cases := []struct{ ref, shard string }{
		{"2106.09685", "2106"},
		{"2106.09685v2", "2106"},
		{"1706.03762", "1706"},
		{"0704.0001", "0704"},
		{"hep-th/9711200", "9711"},
		{"math.GT/0309136", "0309"},
	}
	for _, c := range cases {
		if got := Shard(mustParse(t, c.ref)); got != c.shard {
			t.Errorf("Shard(%q) = %q, want %q", c.ref, got, c.shard)
		}
	}
}

// The old scheme ran from 1991 to 2007 and the new one started in 2007, so a
// two digit year is unambiguous across the whole corpus and the shard is four
// characters everywhere. This is worth pinning because the first person to
// reach for a four digit year will produce a second directory layout.
func TestShardIsFourCharacters(t *testing.T) {
	for _, ref := range []string{"9107.0001", "2106.09685", "hep-th/9711200"} {
		id, err := axid.Parse(ref)
		if err != nil {
			continue
		}
		if len(Shard(id)) != 4 {
			t.Errorf("Shard(%q) = %q, want four characters", ref, Shard(id))
		}
	}
}

func TestPathID(t *testing.T) {
	cases := []struct{ ref, want string }{
		{"2106.09685", "2106.09685"},
		{"hep-th/9711200", "hep-th-9711200"},
		{"math.GT/0309136", "math-0309136"},
	}
	for _, c := range cases {
		if got := PathID(mustParse(t, c.ref)); got != c.want {
			t.Errorf("PathID(%q) = %q, want %q", c.ref, got, c.want)
		}
	}
}

// A slash in an id is a directory separator, so the path form must not carry
// one. Nothing else about the id changes.
func TestPathIDHasNoSlash(t *testing.T) {
	id := mustParse(t, "hep-th/9711200")
	if strings.Contains(PathID(id), "/") {
		t.Fatalf("PathID left a slash in %q", PathID(id))
	}
	if !strings.Contains(id.Canonical, "/") {
		t.Fatal("the canonical id lost its slash, which only the path form may do")
	}
}

func TestPaths(t *testing.T) {
	id := mustParse(t, "2106.09685v2")
	cases := []struct{ got, want string }{
		{ContentDir("/c", "vi", id), "/c/content/vi/2106/2106.09685"},
		{FiguresDir("/c", id), "/c/figures/2106/2106.09685"},
		{TagsPath("/c", id), "/c/tags/2106/2106.09685.tags"},
		{RefsPath("/c", id), "/c/manifests/refs/2106/2106.09685.yaml"},
		{MetadataPath("/c", Shard(id)), "/c/metadata/2106.jsonl"},
		{GraphPath("/c", Shard(id)), "/c/graph/2106.jsonl"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

// A path holds the paper and never the version. The corpus publishes one named
// version of a paper and records which one in the front matter, so putting the
// version in the path would create a second directory for the same paper the
// first time anything re-extracts it.
func TestPathsDropTheVersion(t *testing.T) {
	with := ContentDir("/c", "en", mustParse(t, "2106.09685v2"))
	without := ContentDir("/c", "en", mustParse(t, "2106.09685"))
	if with != without {
		t.Fatalf("a versioned reference produced a different path: %q vs %q", with, without)
	}
}

// The cache path keeps the version, which is the one place in the layout that
// does. A rendering is of a version: v1 and v2 of the same paper are two
// different documents and caching one over the other would mean extracting the
// wrong one and never finding out.
func TestTheCachePathKeepsTheVersion(t *testing.T) {
	id := mustParse(t, "2312.00752v2")
	cases := []struct{ got, want string }{
		{RenderPath("/c", id, 2), "/c/work/html/2312/2312.00752v2.html"},
		{RenderPath("/c", id, 1), "/c/work/html/2312/2312.00752v1.html"},
		{RenderPath("", id, 2), "work/html/2312/2312.00752v2.html"},
		{RenderPath("/c", mustParse(t, "hep-th/9711200"), 3), "/c/work/html/9711/hep-th-9711200v3.html"},
		{EPrintPath("/c", id, 2), "/c/work/source/2312/2312.00752v2.gz"},
		{EPrintPath("", id, 1), "work/source/2312/2312.00752v1.gz"},
		{EPrintPath("/c", mustParse(t, "hep-th/9711200"), 3), "/c/work/source/9711/hep-th-9711200v3.gz"},
		{SourcesPath("/c"), "/c/manifests/sources.yaml"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

// The version in the path comes from the argument and never from the parsed
// reference. A reference that named no version would otherwise cache itself as
// v0, which is not a version of anything.
func TestTheCachePathTakesTheVersionFromTheCaller(t *testing.T) {
	bare := RenderPath("/c", mustParse(t, "2312.00752"), 2)
	versioned := RenderPath("/c", mustParse(t, "2312.00752v1"), 2)
	if bare != versioned {
		t.Fatalf("the reference decided the version: %q vs %q", bare, versioned)
	}
}

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tamnd/arxiv-reader/graph"
)

// The arguments this refuses are the ones where carrying on would do something
// other than what was asked.
func TestGraphRefusesTheArgumentsItShouldRefuse(t *testing.T) {
	t.Setenv("ARXIV_CORPUS", t.TempDir())
	for _, args := range [][]string{
		{},
		{"nonsense"},
		{"build"},
		{"build", "-all", "-shard", "2501"},
		{"build", "-all", "2501.00001"},
		{"build", "-shard", "2501", "2501.00001"},
		{"build", "-shard", "251"},
		{"build", "not-an-id"},
		{"node"},
		{"node", "2501.00001", "2501.00002"},
		{"node", "ax://surface/s10"},
		{"out"},
		{"out", "2501.00001", "-predicate", "cites"},
		{"in"},
	} {
		if err := runGraph(args); err == nil {
			t.Errorf("ax graph %v was accepted", args)
		}
	}
}

// The flags go in front of the id, because Go's flag package stops parsing at
// the first argument that is not one. Saying so in the usage line is the whole
// fix, and this is the test that keeps the usage line honest.
func TestAPredicateNobodyDefinedIsRefused(t *testing.T) {
	t.Setenv("ARXIV_CORPUS", t.TempDir())
	if err := runGraph([]string{"out", "-predicate", "depends-on", "2501.00001"}); err == nil {
		t.Fatal("a predicate nobody defined was accepted")
	}
	if err := runGraph([]string{"out", "-conf", "low", "2501.00001"}); err == nil {
		t.Fatal("a confidence of low was accepted")
	}
}

// What anybody types is an arXiv id, and making them write the scheme every
// time would be a tax on the common case.
func TestWhatSomebodyTypedBecomesANode(t *testing.T) {
	for _, c := range []struct{ typed, want string }{
		{"2312.00752", "ax://paper/2312.00752"},
		{"https://arxiv.org/abs/2312.00752v2", "ax://paper/2312.00752"},
		{"hep-th/9711200", "ax://paper/hep-th/9711200"},
		{"ax://paper/2312.00752#3KH2", "ax://paper/2312.00752#3KH2"},
		{"ax://concept/state-space-model", "ax://concept/state-space-model"},
	} {
		got, err := node(c.typed)
		if err != nil {
			t.Errorf("%q: %v", c.typed, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q is %q, want %q", c.typed, got, c.want)
		}
	}
	for _, typed := range []string{"", "nonsense", "ax://surface/s10", "ax://paper/2312.00752#v0"} {
		if got, err := node(typed); err == nil {
			t.Errorf("%q came out as %q", typed, got)
		}
	}
}

func TestConfidenceIsOneOfTheThree(t *testing.T) {
	for _, c := range []string{"certain", "high", "medium"} {
		if !confidence(c) {
			t.Errorf("%q is not a confidence", c)
		}
	}
	for _, c := range []string{"", "low", "Certain", "sure"} {
		if confidence(c) {
			t.Errorf("%q is a confidence", c)
		}
	}
}

func TestAnEdgeSetIsSummarisedInTableOrder(t *testing.T) {
	edges := []graph.Edge{
		{P: graph.AuthoredBy}, {P: graph.Cites}, {P: graph.Cites}, {P: graph.Uses},
	}
	if got := summary(edges); got != "2 cites, 1 uses, 1 authored-by" {
		t.Errorf("got %q", got)
	}
	if got := summary(nil); got != "nothing" {
		t.Errorf("an empty edge set is %q", got)
	}
}

// A corpus nobody has built a graph over says so, rather than answering every
// question with nothing found, which reads the same as a paper that really has
// no edges.
func TestACorpusWithNoEdgesSaysSo(t *testing.T) {
	if _, err := graphShards(t.TempDir()); err == nil {
		t.Fatal("a corpus with no graph directory answered")
	}
}

func TestOnlyShardFilesAreShards(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "graph")
	if err := os.MkdirAll(filepath.Join(dir, "2401"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"2501.jsonl", "2412.jsonl", "notes.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := graphShards(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "2412" || got[1] != "2501" {
		t.Errorf("got %v, want the two shard files in order", got)
	}
}

// A paper URI matches its objects too, so asking what a paper cites is
// everything the paper and everything inside it says.
func TestAQueryOnAPaperReadsWhatItsObjectsSaidToo(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "graph", "2501.jsonl")
	edges := []graph.Edge{
		{S: graph.Paper("2501.00001"), P: graph.Cites, O: graph.Paper("2401.01234"),
			Conf: graph.Certain, Via: "arxiv", Stage: "refs", At: "2026-10-14"},
		{S: graph.Object("2501.00001", "AAAA"), P: graph.Cites, O: graph.Paper("2401.01234"),
			Conf: graph.Certain, Via: "arxiv", Stage: "refs", At: "2026-10-14"},
		{S: graph.Paper("2501.00009"), P: graph.Cites, O: graph.Paper("2401.01234"),
			Conf: graph.Medium, Via: "title", Stage: "refs", At: "2026-10-14"},
	}
	if _, err := graph.Save(path, edges); err != nil {
		t.Fatal(err)
	}
	out, err := match(root, graph.Paper("2501.00001"), subject, filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Errorf("%d edges, want the paper's and its object's: %+v", len(out), out)
	}
	// Asking about the object alone is a different question.
	one, err := match(root, graph.Object("2501.00001", "AAAA"), subject, filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 {
		t.Errorf("%d edges for one object", len(one))
	}
	// What points at the cited paper is everybody, and that is the reverse pass.
	in, err := match(root, graph.Paper("2401.01234"), object, filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(in) != 3 {
		t.Errorf("%d edges point at the cited paper", len(in))
	}
}

func TestAQueryKeepsOnlyWhatWasAskedFor(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "graph", "2501.jsonl")
	edges := []graph.Edge{
		{S: graph.Paper("2501.00001"), P: graph.Cites, O: graph.Paper("2401.01234"),
			Conf: graph.Certain, Via: "arxiv", Stage: "refs", At: "2026-10-14"},
		{S: graph.Paper("2501.00001"), P: graph.Cites, O: graph.Paper("2303.00002"),
			Conf: graph.Medium, Via: "title", Stage: "refs", At: "2026-10-14"},
		{S: graph.Paper("2501.00001"), P: graph.InCategory, O: graph.Category("cs.LG"),
			Conf: graph.Certain, Via: "metadata", Stage: "harvest", At: "2026-10-14"},
	}
	if _, err := graph.Save(path, edges); err != nil {
		t.Fatal(err)
	}
	uri := graph.Paper("2501.00001")
	for _, c := range []struct {
		what string
		f    filter
		want int
	}{
		{"everything", filter{}, 3},
		{"one predicate", filter{predicate: graph.Cites}, 2},
		{"one confidence", filter{conf: map[string]bool{graph.Certain: true}}, 2},
		{"both", filter{predicate: graph.Cites, conf: map[string]bool{graph.Certain: true}}, 1},
		{"the top one", filter{top: 1}, 1},
	} {
		got, err := match(root, uri, subject, c.f)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != c.want {
			t.Errorf("%s gave %d edges, want %d", c.what, len(got), c.want)
		}
	}
}

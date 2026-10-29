package gold

import (
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/edges"
	"github.com/tamnd/arxiv-reader/objects"
)

func edgeObjects() []objects.Record {
	return []objects.Record{
		{Kind: "statement", Item: "statement/lemma", Paper: "p", Version: "1", Local: "lemma-1", Path: "source"},
		{Kind: "statement", Item: "statement/theorem", Paper: "p", Version: "1", Local: "theorem-1", Path: "source"},
		{Kind: "proof", Item: "proof", Paper: "p", Version: "1", Local: "proof-1", Proves: "theorem-1", Path: "source"},
	}
}

func edgeList() []edges.Edge {
	ev := edges.Evidence{Version: 1, Cue: "by"}
	return []edges.Edge{
		{From: "p#proof-1", To: "p#theorem-1", Predicate: edges.Proves, Method: edges.Attach, Confidence: edges.Certain, Evidence: ev},
		{From: "p#proof-1", To: "p#lemma-1", Predicate: edges.Uses, Method: edges.Ref, Confidence: edges.Certain, Evidence: ev},
		{From: "p#proof-1", To: "p#bib:Ser", Part: "Theorem 4.1", Predicate: edges.CitesResult, Method: edges.Cite, Confidence: edges.High, Evidence: ev},
		{From: "p#proof-1", To: "p#lemma-1", Predicate: edges.Uses, Method: edges.Term, Confidence: edges.Medium, Evidence: ev},
	}
}

func TestAProposalOfSetEHasTheUsesOfTheProofs(t *testing.T) {
	r := ProposeE("v1", "1991-2000", "ax/test", "p", 1, edgeObjects(), edgeList())
	if err := r.Check(); err != nil {
		t.Fatal(err)
	}
	if r.Set != "E" || len(r.Uses) != 2 || r.Uses[0].To != "lemma-1" || r.Uses[1].To != "bib:Ser" || r.Uses[1].Part != "Theorem 4.1" {
		t.Fatalf("uses %+v", r.Uses)
	}
}

func TestAUseOfAnUnknownMarkIsRefused(t *testing.T) {
	r := ProposeE("v1", "", "ax/test", "p", 1, edgeObjects(), nil)
	r.Uses = []Use{{Proof: "proof-1", To: "lemma-9"}}
	if err := r.Check(); err == nil || !strings.Contains(err.Error(), "lemma-9") {
		t.Fatalf("check %v", err)
	}
}

func TestSetEScoresEachMethod(t *testing.T) {
	g := ProposeE("v1", "", "ax/test", "p", 1, edgeObjects(), nil)
	g.State, g.By = Marked, "A. Person"
	// The person marks the lemma and a definition that no edge finds, and
	// not the citation.
	g.Marks = append(g.Marks, Mark{ID: "definition-1", Kind: "definition"})
	g.Uses = []Use{{Proof: "proof-1", To: "lemma-1"}, {Proof: "proof-1", To: "definition-1"}}
	var s EdgeScore
	s.Marked = 1
	s.AddPaper(g, edgeList())
	if r := s.Precision[edges.Ref]; r.Hit != 1 || r.Of != 1 {
		t.Errorf("ref %v", r)
	}
	if r := s.Precision[edges.Cite]; r.Hit != 0 || r.Of != 1 {
		t.Errorf("cite %v", r)
	}
	if s.Recall.Hit != 1 || s.Recall.Of != 2 {
		t.Errorf("recall %v", s.Recall)
	}
	if !strings.Contains(s.Markdown(), "proof-1 definition-1") {
		t.Errorf("the report does not list the missed use:\n%s", s.Markdown())
	}
}

func TestSetEIsDrawnFromSetX(t *testing.T) {
	var x []Pick
	for i := 0; i < 10; i++ {
		x = append(x, Pick{ID: "a" + string(rune('0'+i)), Group: Group1990s}, Pick{ID: "b" + string(rune('0'+i)), Group: GroupNow})
	}
	noProof := map[string]bool{"a1": true, "a2": true, "b3": true}
	picks := DrawE(x, func(id string) bool { return !noProof[id] }, "e-v1", 3)
	if len(picks) != 6 {
		t.Fatalf("picks %v", picks)
	}
	for _, p := range picks {
		if noProof[p.ID] {
			t.Errorf("%s has no proof and is drawn", p.ID)
		}
	}
	again := DrawE(x, func(id string) bool { return !noProof[id] }, "e-v1", 3)
	for i := range picks {
		if picks[i] != again[i] {
			t.Fatal("the same seed gives another draw")
		}
	}
}

// Seen in the gold pilot (set E): the marks of a pass that reads the source
// have names of its own, so the uses are set against the edges through the
// items that the marks match, and not through the names.
func TestTheUsesOfMarksWithOtherNamesAreScoredThroughTheItems(t *testing.T) {
	recs := edgeObjects()
	recs[0].Span = &objects.Span{File: "main.tex", From: 10, To: 14}
	recs[2].Span = &objects.Span{File: "main.tex", From: 30, To: 40}
	g := Record{Set: "E", Paper: "p", Version: 1, State: Marked, By: "a pass",
		Marks: []Mark{
			{ID: "l", Kind: "statement", File: "main.tex", From: 11, To: 13},
			{ID: "t", Kind: "statement", Number: "nine"},
			{ID: "pf", Kind: "proof", File: "main.tex", From: 30, To: 41, Proves: "l"},
		},
		Uses: []Use{{Proof: "pf", To: "l"}, {Proof: "pf", To: "t"}, {Proof: "pf", To: "bib:Ser"}},
	}
	in := InItems(g, recs)
	if in.Marks[0].ID != "lemma-1" || in.Marks[2].ID != "proof-1" || in.Marks[2].Proves != "lemma-1" {
		t.Fatalf("marks %+v", in.Marks)
	}
	if in.Uses[1].To != "gold-t" || in.Uses[2].To != "bib:Ser" || g.Uses[0].To != "l" {
		t.Fatalf("uses %+v, and the record given %+v", in.Uses, g.Uses)
	}
	var s EdgeScore
	s.AddPaper(in, edgeList())
	if r := s.Precision[edges.Ref]; r.Hit != 1 || r.Of != 1 {
		t.Errorf("ref %v", r)
	}
	if s.Recall.Hit != 2 || s.Recall.Of != 3 {
		t.Errorf("recall %v", s.Recall)
	}
}

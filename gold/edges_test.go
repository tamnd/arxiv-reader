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
	s.AddPaper(g, edgeObjects(), edgeList())
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

// Seen in the gold pilot (set E): the marks of a record are matched to the
// items by their place, so an item that the extractor names otherwise, or
// that a mark of set X alone found with another kind, is still the mark.
func TestSetEMatchesTheMarksByTheirPlace(t *testing.T) {
	recs := []objects.Record{
		{Kind: "statement", Item: "statement/lemma", Paper: "p", Version: "1", Local: "head-2", Path: "source", Span: &objects.Span{File: "a.tex", From: 10, To: 12}},
		{Kind: "statement", Item: "statement/lemma", Paper: "p", Version: "1", Local: "head-1", Path: "source", Span: &objects.Span{File: "a.tex", From: 20, To: 22}},
		{Kind: "proof", Item: "proof", Paper: "p", Version: "1", Local: "proof-1", Path: "source", Span: &objects.Span{File: "a.tex", From: 30, To: 40}},
	}
	g := Record{Set: "E", Paper: "p", Version: 1, State: Marked, By: "A. Person", Marks: []Mark{
		{ID: "head-1", Kind: "statement", File: "a.tex", From: 10, To: 12},
		{ID: "gold-lemma-2", Kind: "remark", File: "a.tex", From: 20, To: 22},
		{ID: "proof-1", Kind: "proof", File: "a.tex", From: 30, To: 40},
	}, Uses: []Use{{Proof: "proof-1", To: "gold-lemma-2"}}}
	ev := edges.Evidence{Version: 1, Cue: "by"}
	es := []edges.Edge{
		{From: "p#proof-1", To: "p#head-1", Predicate: edges.Uses, Method: edges.Ref, Confidence: edges.Certain, Evidence: ev},
	}
	var s EdgeScore
	s.AddPaper(g, recs, es)
	if r := s.Precision[edges.Ref]; r.Hit != 1 || r.Of != 1 {
		t.Errorf("ref %v, and the edge to head-1 is the use of the mark gold-lemma-2", r)
	}
	if s.Recall.Hit != 1 {
		t.Errorf("recall %v", s.Recall)
	}
}

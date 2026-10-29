package gold

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/objects"
)

func pool() []Candidate {
	var out []Candidate
	for y := 1992; y <= 2026; y++ {
		for i := 0; i < 400; i++ {
			code := []string{"14", "11", "35", "math.CO"}[i%4]
			out = append(out, Candidate{ID: fmt.Sprintf("%02d01.%05d", y%100, i), Year: y, Code: code})
		}
	}
	return out
}

func TestDrawGivesEachGroupItsSize(t *testing.T) {
	picks := Draw(pool(), PlanX, "x-v1")
	got := map[string]int{}
	seen := map[string]bool{}
	for _, p := range picks {
		got[p.Group]++
		if seen[p.ID] {
			t.Fatalf("%s is drawn twice", p.ID)
		}
		seen[p.ID] = true
	}
	for _, g := range PlanX.Groups {
		if got[g.Name] != g.Size {
			t.Errorf("group %s has %d papers, want %d", g.Name, got[g.Name], g.Size)
		}
	}
	again := Draw(pool(), PlanX, "x-v1")
	for i := range picks {
		if picks[i] != again[i] {
			t.Fatal("the same seed gave a different draw")
		}
	}
}

func TestDrawKeepsTheCodesInProportion(t *testing.T) {
	picks := Draw(pool(), PlanX, "x-v1")
	codes := map[string]int{}
	for _, p := range picks {
		if p.Group == GroupNow {
			codes[p.Code]++
		}
	}
	for code, n := range codes {
		if n != 100 {
			t.Errorf("code %s has %d papers of 400, want 100", code, n)
		}
	}
}

func TestCodeOf(t *testing.T) {
	r := metadata.Record{Categories: []string{"math.AG"}, MSCClass: "14J32 (Primary), 11G25"}
	if got := CodeOf(r); got != "14" {
		t.Errorf("CodeOf = %q, want 14", got)
	}
	r.MSCClass = ""
	if got := CodeOf(r); got != "math.AG" {
		t.Errorf("CodeOf with no MSC = %q, want math.AG", got)
	}
}

func objs() []objects.Record {
	return []objects.Record{
		{Kind: "statement", Subkind: "theorem", Paper: "2312.00752", Version: "2", Local: "theorem-1", Number: "1", Label: "thm:a", Path: "source", Item: "statement.theorem", Span: &objects.Span{File: "a.tex", From: 10, To: 14}, ProofStatus: "here"},
		{Kind: "proof", Paper: "2312.00752", Version: "2", Local: "proof-1", Path: "source", Item: "proof", Proves: "theorem-1", Span: &objects.Span{File: "a.tex", From: 15, To: 20}},
		{Kind: "remark", Paper: "2312.00752", Version: "2", Local: "remark-1", Number: "2", Path: "source", Item: "remark", Span: &objects.Span{File: "a.tex", From: 30, To: 31}},
	}
}

func TestScorePaper(t *testing.T) {
	g := Record{Set: "X", SetVersion: "v1", Paper: "2312.00752", Version: 2, State: Marked, By: "a person", Marks: []Mark{
		{ID: "m1", Kind: "statement", Number: "1", File: "a.tex", From: 10, To: 14, ProofStatus: "here"},
		{ID: "m2", Kind: "proof", File: "a.tex", From: 15, To: 20, Proves: "m1"},
		{ID: "m3", Kind: "definition", Number: "3", File: "a.tex", From: 40, To: 42},
	}}
	s := ScorePaper(g, objs())
	if s.Recall != (Ratio{2, 3}) || s.Precision != (Ratio{2, 3}) {
		t.Errorf("recall %v and precision %v, want 2 of 3 each", s.Recall, s.Precision)
	}
	if s.Attach != (Ratio{1, 1}) || s.Number != (Ratio{1, 1}) || s.Status != (Ratio{1, 1}) {
		t.Errorf("attach %v, number %v, status %v, want 1 of 1 each", s.Attach, s.Number, s.Status)
	}
	if len(s.Missed) != 1 || s.Missed[0] != "m3" || len(s.Extra) != 1 || s.Extra[0] != "remark-1" {
		t.Errorf("missed %v and extra %v", s.Missed, s.Extra)
	}
	if s.Decade != GroupNow {
		t.Errorf("decade %q, want %s", s.Decade, GroupNow)
	}
}

func TestScorePaperMatchesTheSpanFirst(t *testing.T) {
	// m1 agrees with the corollary only in its number; m2 is at its span. m1
	// comes first, and still m2 is the match of the corollary.
	g := Record{Set: "X", SetVersion: "v1", Paper: "2312.00752", Version: 2, State: Marked, By: "a person", Marks: []Mark{
		{ID: "m1", Kind: "statement", Number: "1", File: "a.tex", From: 90, To: 92},
		{ID: "m2", Kind: "statement", Number: "1", File: "a.tex", From: 10, To: 14},
	}}
	recs := []objects.Record{{Kind: "statement", Local: "corollary-1", Version: "2", Path: "source", Item: "statement/corollary", Number: "1", Span: &objects.Span{File: "a.tex", From: 10, To: 13}}}
	s := ScorePaper(g, recs)
	if len(s.Missed) != 1 || s.Missed[0] != "m1" {
		t.Errorf("missed %v, want m1", s.Missed)
	}
}

func TestProposeAndCheck(t *testing.T) {
	r := Propose("X", "v1", GroupNow, "ax/test", "2312.00752", 2, objs())
	if len(r.Marks) != 3 || r.Marks[1].Proves != "theorem-1" {
		t.Fatalf("the proposal is %+v", r.Marks)
	}
	path := filepath.Join(t.TempDir(), "r.json")
	if err := r.Write(path); err != nil {
		t.Fatal(err)
	}
	back, err := Read(path)
	if err != nil || back.State != Proposed {
		t.Fatalf("read back %v, %v", back.State, err)
	}
	r.State = Marked
	if err := r.Check(); err == nil {
		t.Error("a marked record with no person passed the check")
	}
	r.By = "a person"
	r.Marks[1].Proves = "nothing"
	if err := r.Check(); err == nil {
		t.Error("a proof of a mark that is not there passed the check")
	}
}

func TestListRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x-v1.txt")
	picks := []Pick{{"math/0002124", Group1990s, "14"}, {"2312.00752", GroupNow, "cs.LG"}}
	if err := WriteList(path, "a header", picks); err != nil {
		t.Fatal(err)
	}
	back, err := ReadList(path)
	if err != nil || len(back) != 2 || back[0] != picks[0] {
		t.Fatalf("read back %v, %v", back, err)
	}
}

// TestTheReportSaysWhoMarked checks that the report names who marked the
// records, so a score against marks made by a program is not read as a score
// against marks made by a person.
func TestTheReportSaysWhoMarked(t *testing.T) {
	s := Score{Set: "X", SetVersion: "v1", Extractor: "ax/test", Papers: 5, Marked: 4, Proposed: 1,
		MarkedBy: map[string]int{"Ann": 1, "claude-opus-5-5": 3}}
	md := s.Markdown()
	want := "4 have a marked record (3 by claude-opus-5-5, 1 by Ann), and 1 have a proposed record only."
	if !strings.Contains(md, want) {
		t.Fatalf("the report does not say %q:\n%s", want, md)
	}
}

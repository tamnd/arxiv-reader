package edges

import (
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/objects"
)

func TestTheReportCountsTheChecksOfAX67(t *testing.T) {
	objs := []objects.Record{
		{Kind: "statement", Local: "theorem-1"},
		{Kind: "statement", Local: "lemma-1"},
		{Kind: "statement", Local: "lemma-2"},
		{Kind: "proof", Local: "proof-1"},
		{Kind: "proof", Local: "proof-2"},
	}
	es := []Edge{
		{From: "p#proof-1", To: "p#theorem-1", Predicate: Proves, Method: Attach, Confidence: Certain},
		{From: "p#proof-1", To: "p#lemma-1", Predicate: Uses, Method: Ref, Confidence: Certain},
		{From: "p#remark-1", To: "p#lemma-2", Predicate: RefersTo, Method: Text, Confidence: High},
	}
	rp := NewReport()
	rp.Add("2012-01", "05", objs, es)
	a := rp.All
	if a.Statements != 3 || a.Used != 2 || a.Proofs != 2 || a.Bare != 1 || a.WithCycle != 0 {
		t.Fatalf("measures %+v", a)
	}
	md := rp.Markdown()
	for _, w := range []string{"66.7% of the statements", "50.0% of the proofs use nothing", "| 05 | 1 | 3 |"} {
		if !strings.Contains(md, w) {
			t.Errorf("the report has no %q:\n%s", w, md)
		}
	}
}

func TestAMonthChangeOfMoreThanFivePointsIsAFinding(t *testing.T) {
	rp := NewReport()
	used := []Edge{{From: "p#proof-1", To: "p#theorem-1", Predicate: Proves, Method: Attach, Confidence: Certain}}
	objs := []objects.Record{{Kind: "statement", Local: "theorem-1"}}
	for i := 0; i < minPapers; i++ {
		rp.Add("2012-01", "05", objs, used)
		rp.Add("2012-02", "05", objs, nil)
		rp.Add("2012-03", "05", objs, nil)
	}
	ch := rp.Changes()
	if len(ch) != 1 || ch[0].From != "2012-01" || ch[0].To != "2012-02" || ch[0].Measure != "statements used" {
		t.Fatalf("changes %+v", ch)
	}
}

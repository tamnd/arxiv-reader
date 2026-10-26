package analysis

import (
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/census"
	"github.com/tamnd/arxiv-reader/objects"
)

func TestMonthOf(t *testing.T) {
	for in, want := range map[string]string{"9506": "1995-06", "0003": "2000-03", "2312": "2023-12", "x": "x"} {
		if got := MonthOf(in); got != want {
			t.Errorf("MonthOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTheReportCountsTheMeasuresOfAX114(t *testing.T) {
	rp := NewReport("")
	strong := &census.Record{Paper: "2312.00001", Version: 1, Extractor: "ax/v1", Strength: "strong", Occurrences: map[string]int{"env": 2, "proof": 1}}
	gappy := &census.Record{Paper: "2312.00002", Version: 1, Extractor: "ax/v1", Strength: "weak", Occurrences: map[string]int{"env": 1},
		Gaps:     []census.Gap{{Occurrence: "env-1", Result: "missing", Pattern: "env/newtheorem/article"}},
		Findings: []census.Finding{{Rule: "D09", Message: "no object"}}}
	objs := []objects.Record{
		{Kind: "statement", ProofStatus: "here"},
		{Kind: "statement", ProofStatus: "none"},
		{Kind: "proof", Proves: "theorem-1"},
		{Kind: "proof"},
	}
	rp.Add("2312", "14", strong, objs)
	rp.Add("2312", "11", gappy, nil)
	rp.Add("9506", "14", &census.Record{Paper: "alg-geom/9506001", Version: 2, Extractor: "ax/v1", Strength: "normal"}, nil)
	rp.AddStop(Stop{Paper: "2312.00003", Version: 1, State: Stopped, Reason: StopNoSource})
	a := rp.All
	if a.Papers != 3 || a.WithGap != 1 || a.D09 != 1 || a.NoProof != 1 || a.Orphans != 1 || a.Occurrences != 4 {
		t.Errorf("got %+v", a)
	}
	md := rp.Markdown()
	for _, want := range []string{"3 versions have a census record, and 1 are stopped", "| 1995-06 | 1 |", "| env/newtheorem/article | 1 | 1 | 2312.00002v1 |", "| no-source | 1 |"} {
		if !strings.Contains(md, want) {
			t.Errorf("the report has no %q:\n%s", want, md)
		}
	}
	since := NewReport("2000-01")
	since.Add("9506", "14", &census.Record{Paper: "alg-geom/9506001", Version: 2, Strength: "normal"}, nil)
	if since.All.Papers != 0 {
		t.Error("a report since 2000 counted a paper from 1995")
	}
}

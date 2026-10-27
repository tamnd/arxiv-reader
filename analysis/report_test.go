package analysis

import (
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/census"
	"github.com/tamnd/arxiv-reader/objects"
	"github.com/tamnd/arxiv-reader/witness"
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
	rp.AddWitness("2312", "theoremgraph", gappy, witness.Record{Paper: "2312.00002", Version: 1, Count: 4})
	rp.AddWitness("2312", "theoremgraph", strong, witness.Record{Paper: "2312.00001", Version: 1, Count: 2})
	rp.AddWitness("2312", "theoremsearch", strong, witness.Record{Paper: "2312.00001", Version: 2, Count: 9})
	if w := rp.Witness["theoremgraph"]; w.Both != 2 || w.Same != 1 || w.More != 1 || len(w.Excess) != 1 || w.Excess[0].Census != 1 {
		t.Errorf("theoremgraph %+v", w)
	}
	if w := rp.Witness["theoremsearch"]; w.Both != 0 || w.Other != 1 {
		t.Errorf("a witness of another version is compared: %+v", w)
	}
	if md := rp.Markdown(); !strings.Contains(md, "| 2312.00002v1 | 1 | 4 |") {
		t.Errorf("the report has no census-diff row:\n%s", md)
	}
	since := NewReport("2000-01")
	since.Add("9506", "14", &census.Record{Paper: "alg-geom/9506001", Version: 2, Strength: "normal"}, nil)
	if since.All.Papers != 0 {
		t.Error("a report since 2000 counted a paper from 1995")
	}
}

func TestTheSummaryHasTheNumbersOfAX15(t *testing.T) {
	rp := NewReport("")
	rp.Add("2312", "14", &census.Record{Paper: "2312.00001", Version: 1, Strength: "strong"}, nil)
	rp.Add("2312", "11", &census.Record{Paper: "2312.00002", Version: 1, Strength: "normal", Gaps: []census.Gap{{Occurrence: "env-1"}, {Occurrence: "env-2"}}}, nil)
	rp.AddStop(Stop{Paper: "2312.00003", Version: 1, State: Stopped, Reason: StopLicence})
	rp.Scope, rp.Extracted, rp.Halted, rp.Closed, rp.ClosedOutside = 10, 2, 1, 5, 4
	s := rp.Summary(true)
	if s.NoState != 7 || s.Versions != 2 || s.Gaps != 2 || s.WithGap != 1 || s.Strength["strong"] != 1 || s.StopReasons[StopLicence] != 1 || s.ClosedOutside != 4 {
		t.Errorf("got %+v", s)
	}
	if s := rp.Summary(false); s.ScopeCounted || s.Scope != 0 || s.NoState != 0 || s.Versions != 2 {
		t.Errorf("a summary with no scope has %+v", s)
	}
}

func TestTheSummaryHasTheNumbersOfEachArea(t *testing.T) {
	rp := NewReport("")
	rp.Add("2312", "14", &census.Record{Paper: "2312.00001", Version: 1, Strength: "strong"}, nil)
	rp.Add("2312", "11", &census.Record{Paper: "2312.00002", Version: 1, Strength: "normal", Gaps: []census.Gap{{Occurrence: "env-1"}, {Occurrence: "env-2"}}}, nil)
	rp.AddScope("14", true, false)
	rp.AddScope("14", false, false)
	rp.AddScope("11", true, false)
	rp.AddScope("11", false, true)
	rp.AddScope("05", false, false)
	s := rp.Summary(true)
	if s.Scope != 5 || s.Extracted != 2 || s.Stopped != 1 || s.NoState != 2 {
		t.Errorf("got %+v", s)
	}
	want := map[string]AreaSummary{
		"14": {Scope: 2, Extracted: 1, NoState: 1, Versions: 1},
		"11": {Scope: 2, Extracted: 1, Stopped: 1, Versions: 1, Gaps: 2},
		"05": {Scope: 1, NoState: 1},
	}
	for code, w := range want {
		if s.Areas[code] != w {
			t.Errorf("the area %s has %+v, and it must have %+v", code, s.Areas[code], w)
		}
	}
	if s := rp.Summary(false); s.Areas["05"].Scope != 0 || s.Areas["11"].Gaps != 2 {
		t.Errorf("a summary with no scope has the areas %+v", s.Areas)
	}
}

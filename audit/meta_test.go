package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
)

func on(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// good is a record nothing is wrong with, which every test breaks one thing in.
//
// Modelled on 2106.09685, announced in June 2021 with a v1 from the seventeenth
// of that month.
func good() metadata.Record {
	return metadata.Record{
		ID:         "2106.09685",
		Title:      "LoRA: Low-Rank Adaptation of Large Language Models",
		Abstract:   "An important paradigm of natural language processing consists of large scale pre-training.",
		Authors:    []metadata.Author{{Surname: "Hu", Forename: "Edward J."}},
		Categories: []string{"cs.CL", "cs.AI", "cs.LG"},
		Versions: []metadata.Version{
			{Version: 1, Created: on("2021-06-17")},
			{Version: 2, Created: on("2021-10-16")},
		},
		Source:    metadata.SourceOAI,
		Harvested: "2026-09-14",
	}
}

// run writes the records into a temporary plane and audits it.
//
// Written to disk rather than held in memory, because the rules read what is
// committed and a test that checks a different path from the one CI runs is a
// test that will agree with the wrong thing.
func run(t *testing.T, shard string, recs ...metadata.Record) Report {
	t.Helper()
	plane := metadata.Plane{Root: t.TempDir()}
	if _, err := plane.Write(shard, recs); err != nil {
		t.Fatal(err)
	}
	return audit(t, plane)
}

func audit(t *testing.T, plane metadata.Plane) Report {
	t.Helper()
	shards, err := plane.Shards()
	if err != nil {
		t.Fatal(err)
	}
	m := Meta{
		Plane: plane,
		Now:   func() time.Time { return on("2026-09-14") },
	}
	report, err := m.Run(shards)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

// findings is what one rule found, by ID.
func findings(t *testing.T, r Report, rule string) []Finding {
	t.Helper()
	for _, res := range r.Results {
		if res.Rule.ID == rule {
			return res.Findings
		}
	}
	t.Fatalf("%s is not a rule this audit runs", rule)
	return nil
}

// A rule that has never been shown to fire is a rule nobody should trust, so
// every one of them gets a record built to break it.
//
// The writer takes all of these without complaint, because it validates the
// month it is handed and nothing about the records. That is the division of
// labour the audit exists for: writing stays cheap and the rules are where a
// bad record gets caught, whether it arrived from a harvest, a hand edit, a bad
// merge or an older version of this tool.
//
// The one thing the writer does do is sort, which is why S23 is tested
// separately below against a file written by hand.
func TestEveryRuleFires(t *testing.T) {
	for _, tc := range []struct {
		rule  string
		shard string
		recs  []metadata.Record
		want  string
	}{
		{
			rule: "S13", shard: "2106",
			recs: []metadata.Record{withID(good(), "2106.9685")},
			want: "2106.9685",
		},
		{
			rule: "S14", shard: "2106",
			recs: []metadata.Record{withID(good(), "1907.01743")},
			want: "belongs in 1907",
		},
		{
			rule: "S15", shard: "2106",
			recs: []metadata.Record{good(), good()},
			want: "is already on line 1",
		},
		{
			rule: "S16", shard: "2106",
			recs: []metadata.Record{func() metadata.Record { r := good(); r.Abstract = " "; return r }()},
			want: "has no abstract",
		},
		{
			rule: "S17", shard: "2106",
			recs: []metadata.Record{func() metadata.Record {
				r := good()
				r.Versions[1].Version = 3
				return r
			}()},
			want: "is numbered v1 v3",
		},
		{
			rule: "S18", shard: "2106",
			recs: []metadata.Record{func() metadata.Record {
				r := good()
				r.Versions[1].Created = on("2021-06-01")
				return r
			}()},
			want: "before v1",
		},
		{
			rule: "S19", shard: "2106",
			recs: []metadata.Record{func() metadata.Record {
				r := good()
				r.Versions[1].Created = on("2031-01-01")
				return r
			}()},
			want: "which has not happened",
		},
		{
			rule: "S20", shard: "2106",
			recs: []metadata.Record{func() metadata.Record {
				r := good()
				r.Versions[0].Created = on("2021-07-01")
				r.Versions[1].Created = on("2021-10-16")
				return r
			}()},
			want: "after the month 2106.09685 names",
		},
		{
			rule: "S05", shard: "2106",
			recs: []metadata.Record{func() metadata.Record {
				r := good()
				r.Versions[0].Licence = "cc-by-4.0"
				r.Versions[0].LicenceFrom = metadata.SourceOAI
				return r
			}()},
			want: "v1:",
		},
		{
			rule: "S21", shard: "2106",
			recs: []metadata.Record{func() metadata.Record { r := good(); r.Source = "arxiv.org"; return r }()},
			want: `read from "arxiv.org"`,
		},
		{
			rule: "S22", shard: "2106",
			recs: []metadata.Record{func() metadata.Record {
				r := good()
				r.Categories = []string{"cs.CL", "Machine Learning"}
				return r
			}()},
			want: "not the shape of a category",
		},
	} {
		t.Run(tc.rule, func(t *testing.T) {
			report := run(t, tc.shard, tc.recs...)
			found := findings(t, report, tc.rule)
			if len(found) == 0 {
				t.Fatalf("%s found nothing in a record built to break it", tc.rule)
			}
			if !strings.Contains(found[0].String(), tc.want) {
				t.Errorf("%s said %q, which does not mention %q", tc.rule, found[0], tc.want)
			}
			if !report.Failed() {
				t.Error("a hard rule found something and the report does not fail")
			}
		})
	}
}

// S23 needs the file out of order, and the plane's writer sorts on the way out,
// so this one is written by hand rather than through Write.
func TestS23Fires(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "metadata"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Two real records with the later identifier first.
	lines := `{"id":"2106.14604","title":"a","abstract":"a","authors":[],"categories":["cs.CL"],"versions":[{"version":1,"created":"2021-04-14T05:57:20Z"}],"source":"oai","harvested":"2026-09-14"}
{"id":"2106.09685","title":"b","abstract":"b","authors":[],"categories":["cs.CL"],"versions":[{"version":1,"created":"2021-06-17T00:00:00Z"}],"source":"oai","harvested":"2026-09-14"}
`
	if err := os.WriteFile(filepath.Join(root, "metadata", "2106.jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}

	report := audit(t, metadata.Plane{Root: root})
	found := findings(t, report, "S23")
	if len(found) == 0 {
		t.Fatal("S23 found nothing in a file written out of order")
	}
	if found[0].ID != "2106.09685" {
		t.Errorf("S23 blamed %s, want the line that is out of place", found[0].ID)
	}
	if found[0].Line != 2 {
		t.Errorf("S23 points at line %d, want 2", found[0].Line)
	}
}

// The one that matters most, because it is the one a rule written too tightly
// would get wrong. A paper held in moderation is announced under a later
// number than the month it was submitted in, and on a sample of five thousand
// that gap ran to four months.
func TestS20AllowsTheModerationGap(t *testing.T) {
	r := good()
	r.ID = "2609.09160"
	// Verbatim from the mirror: submitted in May, announced in September.
	r.Versions = []metadata.Version{{Version: 1, Created: on("2026-05-30")}}
	report := run(t, "2609", r)

	if found := findings(t, report, "S20"); len(found) != 0 {
		t.Errorf("S20 failed a paper held in moderation for four months: %s", found[0])
	}
}

func TestACleanPlanePasses(t *testing.T) {
	report := run(t, "2106", good())
	if report.Failed() {
		t.Fatalf("a clean plane failed: %s", report.Text())
	}
	if report.Findings() != 0 {
		t.Errorf("a clean plane produced %d findings", report.Findings())
	}
	for _, res := range report.Results {
		if res.State() != Pass {
			t.Errorf("%s is %s over a clean plane, want pass", res.Rule.ID, res.State())
		}
		if res.Checked != 1 {
			t.Errorf("%s checked %d records, want 1", res.Rule.ID, res.Checked)
		}
	}
}

// An empty plane is "not run" and not "pass". Reporting a clean sheet over
// nothing is how a corpus acquires a rule everybody believes is working.
func TestAnEmptyPlaneIsNotRunRatherThanPass(t *testing.T) {
	report := audit(t, metadata.Plane{Root: t.TempDir()})
	if report.Failed() {
		t.Error("an empty plane failed the build")
	}
	for _, res := range report.Results {
		if res.State() != NotRun {
			t.Errorf("%s is %s over an empty plane, want not run", res.Rule.ID, res.State())
		}
	}
}

// An empty licence is nobody having looked yet, which is the ordinary state of
// the field until the M2 census and is not a finding.
func TestAnEmptyLicenceIsNotAFinding(t *testing.T) {
	report := run(t, "2106", good())
	if found := findings(t, report, "S05"); len(found) != 0 {
		t.Errorf("S05 failed a record nobody has resolved yet: %s", found[0])
	}
}

func TestAResolvedLicencePasses(t *testing.T) {
	r := good()
	for i := range r.Versions {
		r.Versions[i].Licence = corpus.LicenceCCBY
		r.Versions[i].LicenceFrom = metadata.SourceOAI
	}
	report := run(t, "2106", r)
	if found := findings(t, report, "S05"); len(found) != 0 {
		t.Errorf("S05 failed a licence arXiv issues: %s", found[0])
	}
}

// A record whose identifier will not parse is reported once, by S13, rather
// than by every rule that needed the identifier.
func TestABadIDIsReportedOnce(t *testing.T) {
	report := run(t, "2106", withID(good(), "not-an-id"), good())
	for _, res := range report.Results {
		switch res.Rule.ID {
		case "S13":
			if res.Total != 1 {
				t.Errorf("S13 found %d, want 1", res.Total)
			}
		default:
			if res.Total != 0 {
				t.Errorf("%s also reported the bad id: %s", res.Rule.ID, res.Findings[0])
			}
		}
	}
}

// Findings are counted in full and kept up to the cap. The count says whether
// the rule is wrong or the corpus is, and the kept ones are the examples.
func TestTheCapKeepsTheCountAndDropsTheRest(t *testing.T) {
	var recs []metadata.Record
	for i := 0; i < 10; i++ {
		r := good()
		r.Source = "nowhere"
		r.ID = ids[i]
		recs = append(recs, r)
	}
	plane := metadata.Plane{Root: t.TempDir()}
	if _, err := plane.Write("2106", recs); err != nil {
		t.Fatal(err)
	}
	shards, err := plane.Shards()
	if err != nil {
		t.Fatal(err)
	}
	report, err := Meta{Plane: plane, Cap: 3, Now: func() time.Time { return on("2026-09-14") }}.Run(shards)
	if err != nil {
		t.Fatal(err)
	}

	for _, res := range report.Results {
		if res.Rule.ID != "S21" {
			continue
		}
		if res.Total != 10 {
			t.Errorf("S21 counted %d findings, want all 10", res.Total)
		}
		if len(res.Findings) != 3 {
			t.Errorf("S21 kept %d findings, want the cap of 3", len(res.Findings))
		}
		if res.Dropped() != 7 {
			t.Errorf("S21 says it dropped %d, want 7", res.Dropped())
		}
	}
}

var ids = []string{
	"2106.00001", "2106.00002", "2106.00003", "2106.00004", "2106.00005",
	"2106.00006", "2106.00007", "2106.00008", "2106.00009", "2106.00010",
}

func TestOnlyRunsTheRulesAskedFor(t *testing.T) {
	plane := metadata.Plane{Root: t.TempDir()}
	if _, err := plane.Write("2106", []metadata.Record{good()}); err != nil {
		t.Fatal(err)
	}
	shards, err := plane.Shards()
	if err != nil {
		t.Fatal(err)
	}
	report, err := Meta{Plane: plane, Only: []string{"S14", "S20"}}.Run(shards)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results) != 2 {
		t.Fatalf("ran %d rules, want 2", len(report.Results))
	}
	for i, want := range []string{"S14", "S20"} {
		if report.Results[i].Rule.ID != want {
			t.Errorf("rule %d is %s, want %s", i, report.Results[i].Rule.ID, want)
		}
	}
}

// Old style identifiers are the ones every tool that has got this wrong got
// wrong, so the whole set runs over one.
func TestAnOldStyleIdentifierPasses(t *testing.T) {
	r := good()
	r.ID = "math/9602216"
	r.Categories = []string{"math.LO"}
	r.Versions = []metadata.Version{
		{Version: 1, Created: on("1996-02-28")},
		{Version: 2, Created: on("1996-04-02")},
	}
	report := run(t, "9602", r)
	if report.Failed() {
		t.Fatalf("an old style identifier failed the audit: %s", report.Text())
	}
}

func TestMonthOf(t *testing.T) {
	for _, tc := range []struct{ shard, want string }{
		{"9602", "1996-02-01"},
		{"9112", "1991-12-01"},
		{"0704", "2007-04-01"},
		{"2106", "2021-06-01"},
	} {
		got, err := monthOf(tc.shard)
		if err != nil {
			t.Fatal(err)
		}
		if on(tc.want) != got {
			t.Errorf("%s is %s, want %s", tc.shard, got.Format("2006-01-02"), tc.want)
		}
	}
	if _, err := monthOf("2113"); err == nil {
		t.Error("a thirteenth month was accepted")
	}
}

func withID(r metadata.Record, id string) metadata.Record {
	r.ID = id
	return r
}

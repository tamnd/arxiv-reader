package audit

import (
	"testing"

	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/policy"
	"github.com/tamnd/arxiv-reader/selection"
)

func TestStateOfARuleThatFound(t *testing.T) {
	r := Result{Checked: 10, Total: 1}
	if got := r.State(); got != Fail {
		t.Fatalf("got %s", got)
	}
	// A rule that found something over the papers it was asked about failed,
	// whatever it was not asked about.
	r.Skipped = 40
	if got := r.State(); got != Fail {
		t.Fatalf("got %s", got)
	}
}

func TestStateOfARuleThatLooked(t *testing.T) {
	if got := (Result{Checked: 10}).State(); got != Pass {
		t.Fatalf("got %s", got)
	}
}

func TestStateOfARuleThatHadNothingToLookAt(t *testing.T) {
	if got := (Result{}).State(); got != NotRun {
		t.Fatalf("an empty corpus is not run, got %s", got)
	}
}

func TestStateOfARuleNobodyAsked(t *testing.T) {
	// The distinction the third state exists for. This rule was shown papers
	// and was not asked about them, which is a different thing from having had
	// nothing to look at, and reporting it as a pass is how a corpus acquires a
	// rule everybody believes is working.
	if got := (Result{Skipped: 40}).State(); got != NotApplicable {
		t.Fatalf("got %s", got)
	}
}

func TestARuleAskedOfSomePapersAndNotOthersPasses(t *testing.T) {
	// Not applicable is not a share. A rule that ran over the render papers and
	// was stepped over on the native ones passes on the ones it ran over, and
	// the share it was skipped for is a number reports/coverage.md carries.
	if got := (Result{Checked: 3, Skipped: 40}).State(); got != Pass {
		t.Fatalf("got %s", got)
	}
}

func TestPaperPathReadsTheFrontMatter(t *testing.T) {
	files := []content{
		{doc: extract.Document{Front: extract.Front{Path: "native"}}},
		{doc: extract.Document{Front: extract.Front{Path: "native"}}},
	}
	if got := paperPath(files); got != selection.PathNative {
		t.Fatalf("got %q", got)
	}
}

func TestPaperPathOfFilesThatDisagreeIsEmpty(t *testing.T) {
	// A paper whose files disagree about where they came from is a paper
	// somebody assembled by hand, and that one is worth asking every question
	// of rather than excusing half of them.
	files := []content{
		{doc: extract.Document{Front: extract.Front{Path: "native"}}},
		{doc: extract.Document{Front: extract.Front{Path: "render"}}},
	}
	if got := paperPath(files); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestPaperPathOfAFileThatSaysNothingIsEmpty(t *testing.T) {
	files := []content{{doc: extract.Document{Front: extract.Front{Path: "native"}}}, {}}
	if got := paperPath(files); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := paperPath(nil); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestTheMetadataPlaneAsksEveryRule(t *testing.T) {
	// A record has no extraction path, so nothing on that plane is ever skipped
	// and the collector's gate is off.
	c := &collector{cap: 50}
	c.checked("S14")
	if c.checks["S14"] != 1 || len(c.skips) != 0 {
		t.Fatalf("checks %v skips %v", c.checks, c.skips)
	}
}

func TestTheCollectorRefusesAFindingFromARuleNobodyAsked(t *testing.T) {
	// Refused rather than counted, because not applicable means the thing the
	// rule checks cannot exist here, so anything the rule thinks it found is an
	// artefact of a path that cannot produce the thing.
	pol := policy.Default().Audit
	c := &collector{cap: 50}
	c.expects = func(rule string) bool { return pol.Expects(selection.PathNative, rule) }
	c.checked("M03")
	c.add(Finding{Rule: "M03", ID: "2501.00001", What: "has \\alpha in the prose"})
	if c.skips["M03"] != 1 {
		t.Fatalf("the skip was not counted: %v", c.skips)
	}
	if c.totals["M03"] != 0 {
		t.Fatalf("the finding was kept: %v", c.findings)
	}
	// And a rule the same path does answer is counted as ever.
	c.checked("M05")
	c.add(Finding{Rule: "M05", ID: "2501.00001", What: "holds a replacement character"})
	if c.checks["M05"] != 1 || c.totals["M05"] != 1 {
		t.Fatalf("checks %v totals %v", c.checks, c.totals)
	}
}

func TestResultsCarryTheSkips(t *testing.T) {
	pol := policy.Default().Audit
	c := &collector{cap: 50}
	c.expects = func(rule string) bool { return pol.Expects(selection.PathNative, rule) }
	c.checked("M03")
	c.checked("M03")
	got := c.results([]Rule{{ID: "M03", Group: GroupMath}})
	if len(got) != 1 || got[0].Skipped != 2 || got[0].Checked != 0 {
		t.Fatalf("got %+v", got)
	}
	if got[0].State() != NotApplicable {
		t.Fatalf("got %s", got[0].State())
	}
}

func TestOnlyStillWinsOverThePolicy(t *testing.T) {
	// -rules asks for a rule and this says which papers it is asked of, so a
	// rule nobody asked for is neither checked nor skipped.
	pol := policy.Default().Audit
	c := &collector{cap: 50, only: []string{"M05"}}
	c.expects = func(rule string) bool { return pol.Expects(selection.PathNative, rule) }
	c.checked("M03")
	if len(c.skips) != 0 {
		t.Fatalf("a rule that was not asked for was counted as skipped: %v", c.skips)
	}
}

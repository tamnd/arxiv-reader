package audit

import (
	"strings"
	"testing"
)

func rule(id string, hard bool) Rule {
	return Rule{ID: id, Group: GroupSources, Hard: hard, Says: "every record names the surface it was read from"}
}

func clean() Report {
	return Report{
		Plane:   "meta",
		Records: 5000,
		Shards:  126,
		Results: []Result{
			{Rule: rule("S14", true), Checked: 5000},
			{Rule: rule("S21", true), Checked: 5000},
		},
	}
}

func dirty() Report {
	r := clean()
	r.Results[1].Total = 3
	r.Results[1].Findings = []Finding{
		{Rule: "S21", Shard: "2106", Line: 41, ID: "2106.09685", What: `is read from "arxiv.org"`},
		{Rule: "S21", Shard: "2106", Line: 42, ID: "2106.09686", What: `is read from "arxiv.org"`},
	}
	return r
}

// A clean run says nothing found and lists no findings, because a wall of green
// with an empty findings heading under it is how a person learns to stop
// reading the output.
func TestTextOnACleanRun(t *testing.T) {
	got := clean().Text()
	for _, want := range []string{"S14", "S21", "pass", "5000 records over 126 months, nothing found"} {
		if !strings.Contains(got, want) {
			t.Errorf("the report does not mention %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "every record names the surface") {
		t.Errorf("a clean run printed a rule's sentence, which only a finding needs:\n%s", got)
	}
}

func TestTextListsWhatWasFound(t *testing.T) {
	got := dirty().Text()
	for _, want := range []string{
		"3 findings, and the build fails",
		"S21 every record names the surface it was read from",
		"metadata/2106.jsonl:41: 2106.09685: is read from \"arxiv.org\"",
		"and 1 more finding",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report does not mention %q:\n%s", want, got)
		}
	}
}

// The count and the listing are different numbers and the report has to show
// both, because the count says whether the rule is wrong and the listing is the
// examples.
func TestTextCountsPastTheCap(t *testing.T) {
	r := dirty()
	if got := r.Results[1].Dropped(); got != 1 {
		t.Fatalf("dropped %d, want 1", got)
	}
	if n := strings.Count(r.Text(), "metadata/2106.jsonl"); n != 2 {
		t.Errorf("listed %d findings, want the 2 that were kept", n)
	}
}

func TestMarkdownHasAScoreboard(t *testing.T) {
	got := dirty().Markdown()
	for _, want := range []string{
		"# The audit",
		"5000 records over 126 months of the meta plane",
		"| Group | Rules | Pass | Fail | Not run | N/A | Findings |",
		"| Sources | 2 | 1 | 1 | 0 | 0 | 3 |",
		"## The rules",
		"## The findings",
		"### S21",
		"Every record names the surface it was read from.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the markdown does not have %q:\n%s", want, got)
		}
	}
}

func TestMarkdownDropsTheFindingsHeadingWhenThereAreNone(t *testing.T) {
	got := clean().Markdown()
	if strings.Contains(got, "## The findings") {
		t.Errorf("a clean run wrote an empty findings section:\n%s", got)
	}
	if !strings.Contains(got, "| Sources | 2 | 2 | 0 | 0 | 0 | 0 |") {
		t.Errorf("the scoreboard does not show two passes:\n%s", got)
	}
}

// A soft rule with findings is a report that reads badly if the summary says
// the build fails, because it does not.
func TestASoftFindingDoesNotFailTheBuild(t *testing.T) {
	r := clean()
	r.Results[1].Rule.Hard = false
	r.Results[1].Total = 2
	if r.Failed() {
		t.Error("a soft rule failed the build")
	}
	if got := r.Text(); !strings.Contains(got, "2 findings\n") || strings.Contains(got, "the build fails") {
		t.Errorf("the summary claims the build fails over a soft finding:\n%s", got)
	}
}

func TestNotRunIsNotPass(t *testing.T) {
	r := clean()
	for i := range r.Results {
		r.Results[i].Checked = 0
	}
	r.Records, r.Shards = 0, 0
	got := r.Markdown()
	if !strings.Contains(got, "| Sources | 2 | 0 | 0 | 2 | 0 | 0 |") {
		t.Errorf("an empty plane is not counted as not run:\n%s", got)
	}
	if !strings.Contains(got, "0 records over 0 months of the meta plane, nothing to check") {
		t.Errorf("an empty plane reads as a clean run rather than as an empty one:\n%s", got)
	}
}

func TestPlural(t *testing.T) {
	for _, tc := range []struct {
		n    int
		what string
		want string
	}{
		{0, "record", "0 records"},
		{1, "record", "1 record"},
		{2, "record", "2 records"},
		{1, "finding", "1 finding"},
	} {
		if got := plural(tc.n, tc.what); got != tc.want {
			t.Errorf("plural(%d, %q) is %q, want %q", tc.n, tc.what, got, tc.want)
		}
	}
}

// Findings come out in the order the file is in, because that is the order
// somebody fixing them will work through.
func TestFindingsAreInFileOrder(t *testing.T) {
	f := []Finding{
		{Shard: "2106", Line: 9},
		{Shard: "0704", Line: 2},
		{Shard: "2106", Line: 1},
	}
	sortFindings(f)
	want := []struct {
		shard string
		line  int
	}{{"0704", 2}, {"2106", 1}, {"2106", 9}}
	for i, w := range want {
		if f[i].Shard != w.shard || f[i].Line != w.line {
			t.Errorf("finding %d is %s:%d, want %s:%d", i, f[i].Shard, f[i].Line, w.shard, w.line)
		}
	}
}

func TestWhere(t *testing.T) {
	for _, tc := range []struct {
		f    Finding
		want string
	}{
		{Finding{Shard: "2106", Line: 41}, "metadata/2106.jsonl:41"},
		{Finding{Shard: "2106"}, "metadata/2106.jsonl"},
		{Finding{}, ""},
	} {
		if got := tc.f.Where(); got != tc.want {
			t.Errorf("Where is %q, want %q", got, tc.want)
		}
	}
}

// A finding about a file rather than a record reads without a stray colon
// where the paper's id would have been.
func TestAFindingAboutAFileReadsWithoutAnID(t *testing.T) {
	f := Finding{Rule: "S23", Shard: "2106", What: "is not in identifier order"}
	if got, want := f.String(), "metadata/2106.jsonl: is not in identifier order"; got != want {
		t.Errorf("the finding reads %q, want %q", got, want)
	}
}

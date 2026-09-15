package report

import (
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/audit"
	"github.com/tamnd/arxiv-reader/policy"
	"github.com/tamnd/arxiv-reader/selection"
)

// rules is a small rule set, because a test that reads audit.ContentRules is a
// test that changes every time somebody registers a rule.
var rules = []audit.Rule{
	{ID: "S01", Group: audit.GroupSources, Says: "the licence gate, read back off the file"},
	{ID: "M01", Group: audit.GroupMath, Says: "every math span is closed"},
	{ID: "M05", Group: audit.GroupMath, Says: "no replacement character"},
	{ID: "F10", Group: audit.GroupFigures, Says: "every figure the paper numbers is present"},
}

// skipping is a policy that names one path and leaves the other three alone, so
// both directions are in every test below.
func skipping(p selection.Path, ids ...string) policy.Audit {
	return policy.Audit{Skip: map[selection.Path][]string{p: ids}}
}

func TestCoverageCountsTheContentPlaneAndNotTheSelection(t *testing.T) {
	// The denominator for everything about rules is the papers a rule could
	// have been asked of, and a paper nothing has extracted is not one.
	m := selection.Manifest{Selected: []selection.Entry{
		entry("2501.00001", selection.PathNative, selection.StatusExtracted),
		entry("2501.00002", selection.PathNative, selection.StatusSelected),
		entry("2501.00003", selection.PathRender, selection.StatusFetched),
	}}
	c := BuildCoverage(m, skipping(selection.PathNative, "M01"), rules, "2026-09-15")
	if c.Papers != 3 || c.Read != 1 {
		t.Fatalf("papers %d read %d", c.Papers, c.Read)
	}
	if len(c.Paths) != 1 || c.Paths[0].Path != selection.PathNative || c.Paths[0].Papers != 1 {
		t.Fatalf("got %+v", c.Paths)
	}
	// The render paper is fetched and not extracted, so the render path has no
	// row at all rather than a row of noughts.
	for _, p := range c.Paths {
		if p.Path == selection.PathRender {
			t.Fatal("a path with nothing in the content plane got a row")
		}
	}
}

func TestCoverageCountsTheRulesEachPathIsAsked(t *testing.T) {
	m := selection.Manifest{Selected: []selection.Entry{
		entry("2501.00001", selection.PathNative, selection.StatusExtracted),
		entry("2501.00002", selection.PathRender, selection.StatusTagged),
	}}
	c := BuildCoverage(m, skipping(selection.PathNative, "M01", "F10"), rules, "2026-09-15")
	by := map[selection.Path]PathCoverage{}
	for _, p := range c.Paths {
		by[p.Path] = p
	}
	if got := by[selection.PathNative]; got.Asked != 2 || strings.Join(got.Skipped, ",") != "M01,F10" {
		t.Fatalf("native asked %d skipped %v", got.Asked, got.Skipped)
	}
	if got := by[selection.PathRender]; got.Asked != 4 || len(got.Skipped) != 0 {
		t.Fatalf("render asked %d skipped %v", got.Asked, got.Skipped)
	}
	// Tagged is past extracted, so that paper is in the content plane.
	if c.Read != 2 {
		t.Fatalf("read %d, and a tagged paper has been extracted", c.Read)
	}
	if got := by[selection.PathNative].AskedShare(c.Rules); got != 0.5 {
		t.Fatalf("got %v", got)
	}
}

func TestAQuietRuleIsOneMostOfTheCorpusNoLongerAnswers(t *testing.T) {
	// Four papers, three of them native, and M01 is off for native. Three in
	// four is over a fifth, so M01 is named with the path responsible.
	m := selection.Manifest{Selected: []selection.Entry{
		entry("2501.00001", selection.PathNative, selection.StatusExtracted),
		entry("2501.00002", selection.PathNative, selection.StatusExtracted),
		entry("2501.00003", selection.PathNative, selection.StatusExtracted),
		entry("2501.00004", selection.PathRender, selection.StatusExtracted),
	}}
	c := BuildCoverage(m, skipping(selection.PathNative, "M01"), rules, "2026-09-15")
	if len(c.Silent) != 1 {
		t.Fatalf("got %+v", c.Silent)
	}
	got := c.Silent[0]
	if got.Rule != "M01" || got.Papers != 3 || got.Share != 0.75 {
		t.Fatalf("got %+v", got)
	}
	if len(got.Paths) != 1 || got.Paths[0] != selection.PathNative {
		t.Fatalf("the path responsible is not named: %v", got.Paths)
	}
	// And it carries the rule's own sentence, because a report that names a
	// rule by number is a report nobody can argue with.
	if got.Says != "every math span is closed" {
		t.Fatalf("says %q", got.Says)
	}
}

func TestARuleOffForASmallCornerIsNotNamed(t *testing.T) {
	// One paper in five is a rule that does not fit a corner of the corpus,
	// which is ordinary and is what the policy file is for.
	var sel []selection.Entry
	sel = append(sel, entry("2501.00001", selection.PathNative, selection.StatusExtracted))
	for _, id := range []string{"2501.00002", "2501.00003", "2501.00004", "2501.00005"} {
		sel = append(sel, entry(id, selection.PathRender, selection.StatusExtracted))
	}
	c := BuildCoverage(selection.Manifest{Selected: sel}, skipping(selection.PathNative, "M01"), rules, "2026-09-15")
	if len(c.Silent) != 0 {
		t.Fatalf("a rule off for a fifth of the corpus was named: %+v", c.Silent)
	}
}

func TestQuietRulesAreOrderedByHowMuchOfTheCorpusTheyHaveStoppedReading(t *testing.T) {
	m := selection.Manifest{Selected: []selection.Entry{
		entry("2501.00001", selection.PathNative, selection.StatusExtracted),
		entry("2501.00002", selection.PathNative, selection.StatusExtracted),
		entry("2501.00003", selection.PathVision, selection.StatusExtracted),
	}}
	pol := policy.Audit{Skip: map[selection.Path][]string{
		selection.PathNative: {"M01", "F10"},
		selection.PathVision: {"F10"},
	}}
	c := BuildCoverage(m, pol, rules, "2026-09-15")
	if len(c.Silent) != 2 {
		t.Fatalf("got %+v", c.Silent)
	}
	// F10 is off for all three and M01 for two, so F10 leads.
	if c.Silent[0].Rule != "F10" || c.Silent[0].Papers != 3 {
		t.Fatalf("got %+v", c.Silent[0])
	}
	if c.Silent[1].Rule != "M01" || c.Silent[1].Papers != 2 {
		t.Fatalf("got %+v", c.Silent[1])
	}
	if len(c.Silent[0].Paths) != 2 {
		t.Fatalf("both paths responsible are not named: %v", c.Silent[0].Paths)
	}
}

func TestARuleThePolicyNamesAndThisProjectDoesNotHave(t *testing.T) {
	// The failure this catches is silent by construction. A typo excuses
	// nothing, so nothing changes, and the rule somebody meant to excuse goes
	// on firing over the path they wanted it off.
	m := selection.Manifest{Selected: []selection.Entry{
		entry("2501.00001", selection.PathNative, selection.StatusExtracted),
	}}
	c := BuildCoverage(m, skipping(selection.PathNative, "M01", "MO5"), rules, "2026-09-15")
	if len(c.Unknown) != 1 || c.Unknown[0] != "MO5" {
		t.Fatalf("got %v", c.Unknown)
	}
	if !strings.Contains(c.Markdown(), "Rules the policy names and this project does not have") {
		t.Fatal("the report does not say so")
	}
}

func TestAPolicyWithATypoIsNoticedOnAPathWithNoPapers(t *testing.T) {
	// The typo is in the file whether or not anything has been read down that
	// path yet, and a corpus that has not started on the vision path is exactly
	// when somebody would want to hear about it.
	m := selection.Manifest{Selected: []selection.Entry{
		entry("2501.00001", selection.PathNative, selection.StatusExtracted),
	}}
	c := BuildCoverage(m, skipping(selection.PathVision, "nope"), rules, "2026-09-15")
	if len(c.Unknown) != 1 || c.Unknown[0] != "nope" {
		t.Fatalf("got %v", c.Unknown)
	}
}

func TestEnglishIsTheStatusLadder(t *testing.T) {
	m := selection.Manifest{Selected: []selection.Entry{
		entry("2501.00001", selection.PathNative, selection.StatusExtracted),
		entry("2501.00002", selection.PathNative, selection.StatusPublished),
		entry("2501.00003", selection.PathRender, selection.StatusSelected),
	}}
	c := BuildCoverage(m, policy.Audit{}, rules, "2026-09-15")
	if len(c.Langs) != 1 || c.Langs[0].Lang != "en" {
		t.Fatalf("got %+v", c.Langs)
	}
	if c.Langs[0].Full != 2 || c.Langs[0].None != 1 {
		t.Fatalf("got %+v", c.Langs[0])
	}
}

func TestALanguageIsCountedOverTheWholeSelection(t *testing.T) {
	// Two of four have Vietnamese, so the row says two and two rather than two
	// and nothing, because the work left is the number somebody is after.
	m := selection.Manifest{Selected: []selection.Entry{
		entry("2501.00001", selection.PathRender, selection.StatusTranslated, "en", "vi"),
		entry("2501.00002", selection.PathRender, selection.StatusTranslated, "en", "vi", "ja"),
		entry("2501.00003", selection.PathRender, selection.StatusExtracted, "en"),
		entry("2501.00004", selection.PathRender, selection.StatusSelected),
	}}
	c := BuildCoverage(m, policy.Audit{}, rules, "2026-09-15")
	by := map[string]LangRow{}
	for _, l := range c.Langs {
		by[l.Lang] = l
	}
	if got := by["vi"]; got.Full != 2 || got.None != 2 {
		t.Fatalf("vi %+v", got)
	}
	if got := by["ja"]; got.Full != 1 || got.None != 3 {
		t.Fatalf("ja %+v", got)
	}
	// English is the ladder and not the Languages field, so the paper that is
	// only selected is not counted as having English because somebody wrote en
	// in a list.
	if got := by["en"]; got.Full != 3 || got.None != 1 {
		t.Fatalf("en %+v", got)
	}
	// English first, then the rest in a fixed order, because a report whose
	// rows move between runs has a diff nobody can read.
	if c.Langs[0].Lang != "en" || c.Langs[1].Lang != "ja" || c.Langs[2].Lang != "vi" {
		t.Fatalf("got %v %v %v", c.Langs[0].Lang, c.Langs[1].Lang, c.Langs[2].Lang)
	}
}

func TestAPaperWithNoPathIsCountedUnderUndecided(t *testing.T) {
	// A paper in the content plane with no path is a paper somebody extracted
	// by hand, and it is asked every rule, which is what the report should say
	// rather than leaving it out of the denominator.
	m := selection.Manifest{Selected: []selection.Entry{
		entry("2501.00001", "", selection.StatusExtracted),
	}}
	c := BuildCoverage(m, policy.Default().Audit, rules, "2026-09-15")
	if len(c.Paths) != 1 || c.Paths[0].Path != "" || c.Paths[0].Asked != len(rules) {
		t.Fatalf("got %+v", c.Paths)
	}
	if got := c.Markdown(); !strings.Contains(got, "| undecided |") {
		t.Fatalf("the report does not name it:\n%s", got)
	}
}

func TestAnEmptyContentPlaneSaysSoRatherThanDividingByNought(t *testing.T) {
	m := selection.Manifest{Selected: []selection.Entry{
		entry("2501.00001", selection.PathNative, selection.StatusSelected),
	}}
	c := BuildCoverage(m, policy.Default().Audit, rules, "2026-09-15")
	if c.Read != 0 || len(c.Paths) != 0 || len(c.Silent) != 0 {
		t.Fatalf("got %+v", c)
	}
	got := c.Markdown()
	if !strings.Contains(got, "No paper in the selection has been extracted yet") {
		t.Fatalf("does not say so:\n%s", got)
	}
	if !strings.Contains(got, "The content plane is empty") {
		t.Fatalf("does not say so:\n%s", got)
	}
}

func TestACorpusWhereEveryRuleStillAppliesSaysSo(t *testing.T) {
	m := selection.Manifest{Selected: []selection.Entry{
		entry("2501.00001", selection.PathRender, selection.StatusExtracted),
	}}
	c := BuildCoverage(m, policy.Default().Audit, rules, "2026-09-15")
	got := c.Markdown()
	if !strings.Contains(got, "None. Every one of the 4 rules is asked of more than 80.0%") {
		t.Fatalf("does not say so:\n%s", got)
	}
	if !strings.Contains(got, "none, every rule is asked") {
		t.Fatalf("the path row does not say so:\n%s", got)
	}
}

func TestTheRealPolicyOverTheRealRulesLeavesTheMarkupPathsWhole(t *testing.T) {
	// The one test that reads both real lists, because the pair of them is what
	// ships and a default that quietly excused a rule on the render path would
	// pass every test above.
	m := selection.Manifest{Selected: []selection.Entry{
		entry("2501.00001", selection.PathRender, selection.StatusExtracted),
		entry("2501.00002", selection.PathSource, selection.StatusExtracted),
		entry("2501.00003", selection.PathVision, selection.StatusExtracted),
	}}
	c := BuildCoverage(m, policy.Default().Audit, audit.ContentRules, "2026-09-15")
	for _, p := range c.Paths {
		if p.Asked != len(audit.ContentRules) {
			t.Errorf("%s is asked %d of %d rules", p.Path, p.Asked, len(audit.ContentRules))
		}
	}
	if len(c.Silent) != 0 || len(c.Unknown) != 0 {
		t.Fatalf("silent %+v unknown %v", c.Silent, c.Unknown)
	}
}

func TestTheTextFormIsTheSameNumbers(t *testing.T) {
	m := selection.Manifest{Selected: []selection.Entry{
		entry("2501.00001", selection.PathNative, selection.StatusExtracted),
		entry("2501.00002", selection.PathNative, selection.StatusSelected),
	}}
	c := BuildCoverage(m, skipping(selection.PathNative, "M01"), rules, "2026-09-15")
	got := c.Text()
	for _, want := range []string{"native\t1\t", "3 of 4 rules", "papers\t2", "extracted\t1", "quiet rules\t1"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
}

func TestARuleSentenceSurvivesTheRenderer(t *testing.T) {
	// F11's sentence names <n>.md, and a browser reads that as a tag it does
	// not know and shows nothing, which loses the half of the row somebody
	// arguing with it would read.
	set := []audit.Rule{{ID: "F11", Group: audit.GroupFigures, Says: "every table exists twice, as <n>.md and as <n>.tex"}}
	m := selection.Manifest{Selected: []selection.Entry{
		entry("2501.00001", selection.PathNative, selection.StatusExtracted),
	}}
	c := BuildCoverage(m, skipping(selection.PathNative, "F11"), set, "2026-09-15")
	got := c.Markdown()
	if strings.Contains(got, "<n>") {
		t.Fatalf("the angle brackets went in raw:\n%s", got)
	}
	if !strings.Contains(got, "as &lt;n>.md and as &lt;n>.tex") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestAPipeInARuleSentenceDoesNotBreakTheTable(t *testing.T) {
	set := []audit.Rule{{ID: "M11", Group: audit.GroupMath, Says: "written between dollars, never a | of any kind"}}
	m := selection.Manifest{Selected: []selection.Entry{
		entry("2501.00001", selection.PathNative, selection.StatusExtracted),
	}}
	c := BuildCoverage(m, skipping(selection.PathNative, "M11"), set, "2026-09-15")
	if !strings.Contains(c.Markdown(), `never a \| of any kind`) {
		t.Fatalf("got:\n%s", c.Markdown())
	}
}

package audit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/figures"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/refs"
	"github.com/tamnd/arxiv-reader/tags"
)

// paper writes a corpus with one paper in it and hands back the root.
//
// Every file goes through extract.Document.Bytes, so the content hash is the
// one the writer would have written and T03 is testing the rule rather than the
// fixture. The paper is tagged on the way out for the same reason: a paper in
// the corpus has been through ax tags assign, and a fixture that has not is a
// fixture the G rules would all have something to say about. A test that wants
// a register with something wrong in it breaks this one afterwards.
func paper(t *testing.T, docs ...extract.Document) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "content", "en", "2501", "2501.00001")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The last section is where the figure, the table and the citation go,
	// because a paper in this corpus has been through ax figures, ax tables and
	// ax refs as well, and a fixture that has not is a fixture the F and R rules
	// would all have something to say about. Before the objects are read rather
	// than after, so the figure is tagged like every other object and the G
	// rules have nothing to say either.
	docs[len(docs)-1].Body += "\n\n" + cited + "\n\n" + picture + "\n\n" + tableMD
	figured(t, root)
	tabled(t, root)
	bibbed(t, root, oneEntry())
	planed(t, root, oneRecord(), ownRecord())
	var objects []tags.Object
	for i, d := range docs {
		if d.Front.LocalID != "" && d.Front.Kind != "front" {
			objects = append(objects, tags.Object{File: fileNames[i], Local: d.Front.LocalID, Class: d.Front.Kind})
		}
		objects = append(objects, tags.Objects(fileNames[i], d.Body)...)
	}
	var plan tags.Plan
	if len(objects) > 0 {
		var err error
		plan, err = tags.Assign(fixture, objects, tags.Register{})
		if err != nil {
			t.Fatal(err)
		}
	}
	for i, d := range docs {
		d.Front.Paper = fixture
		d.Front.Version = "v1"
		d.Front.Lang = "en"
		d.Front.Access = string(corpus.AccessOpen)
		d.Front.Licence = corpus.LicenceCCBY.SPDX()
		d.Front.LicenceOfSource = string(corpus.LicenceCCBY)
		d.Front.LicenceFrom = string(metadata.SourceAbs)
		if tag, ok := plan.Assigned[d.Front.LocalID]; ok {
			d.Front.Tag = string(tag)
		}
		d.Body, _ = tags.Retag(d.Body, plan.Assigned)
		b, err := d.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		name := fileNames[i]
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := plan.Register.Save(registerPath(root)); err != nil {
		t.Fatal(err)
	}
	return root
}

// committed gives a fixture the history G08 reads.
//
// It is called by the tests that are about that rule rather than by paper,
// because starting a repository and making a commit costs more than everything
// else in a test here put together, and every other rule in the package reads
// the corpus on disk. A fixture with no history leaves G08 reporting not run,
// which is what a corpus nobody has committed deserves.
func committed(t *testing.T, root string) {
	t.Helper()
	gitIn(t, root, "init", "-q", "-b", "main")
	commit(t, root)
}

// commit puts whatever the fixture looks like now into its history.
func commit(t *testing.T, root string) {
	t.Helper()
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "-c", "gc.auto=0", "commit", "-q", "-m", "the fixture")
}

func gitIn(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	// The user's own configuration is kept out so a rule cannot pass on one
	// machine and fail on another, and the automatic repack is kept out because
	// it outlives the test and writes into a directory the test framework is
	// busy removing.
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=ax", "GIT_AUTHOR_EMAIL=ax@example.com",
		"GIT_COMMITTER_NAME=ax", "GIT_COMMITTER_EMAIL=ax@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

// picture is the image line the writer puts down for a committed figure, and
// tableMD is the Markdown one table is laid out as.
//
// Both are in the fixture rather than in the tests that need them because they
// are what a finished paper has. The pair on disk is the same table, written
// the way ax tables writes it, so F12 is comparing the two representations the
// tool produces rather than two the test made up.
const (
	// cited is the pair of links a rendered paper is full of: one into its own
	// bibliography, which is R02's, and one into another section of itself,
	// which is T12's.
	cited    = "The idea is not new [1](#bib.bibx1), and [Section 1](#s1) says why."
	picture  = "**Figure 1** {#fig-1 .figure}\n\n![A picture of nothing in particular.](/figures/2501/2501.00001/one.svg)"
	tableMD  = "| Model | Accuracy |\n| :--- | :---: |\n| Nothing | 0.0 |"
	tableTeX = `% Table 1 of 2501.00001, rebuilt from arXiv's own rendering.
\begin{tabular}{lc}
\textbf{Model} & \textbf{Accuracy} \\
\hline
Nothing & 0.0 \\
\end{tabular}
`
	drawing = `<svg xmlns="http://www.w3.org/2000/svg" width="800" height="600"></svg>` + "\n"
)

// figured writes the bytes of one committed figure and the manifest entry
// recording what was decided about it.
func figured(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "figures", "2501", "2501.00001")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "one.svg"), []byte(drawing), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (figures.Manifest{Figures: []figures.Figure{oneFigure()}}).Save(manifestPath(root)); err != nil {
		t.Fatal(err)
	}
}

// tabled writes the pair of files the one table in the fixture is kept as.
func tabled(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "tables", "2501", "2501.00001")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"t01.md": tableMD + "\n", "t01.tex": tableTeX} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// bibbed writes the bibliography the fixture cites into.
func bibbed(t *testing.T, root string, entries ...refs.Entry) {
	t.Helper()
	if _, err := (refs.Manifest{Paper: fixture, Version: 1, Entries: entries}).Save(refsPath(root)); err != nil {
		t.Fatal(err)
	}
}

// planed writes the metadata plane the reference and source rules ask their
// questions of.
//
// Two records: the fixture paper itself, which group S reads, and the paper its
// bibliography resolves to, which group R reads. Those two months are the whole
// of the plane on purpose, because R01 says nothing about a month the plane has
// not got and a fixture that stops somewhere is what proves it.
//
// Each record goes in its own month, so a test that moves one somewhere else
// moves the file it lives in with it.
func planed(t *testing.T, root string, recs ...metadata.Record) {
	t.Helper()
	byShard := map[string][]metadata.Record{}
	for _, rec := range recs {
		shard, err := rec.Shard()
		if err != nil {
			t.Fatal(err)
		}
		byShard[shard] = append(byShard[shard], rec)
	}
	for shard, in := range byShard {
		if _, err := (metadata.Plane{Root: root}).Write(shard, in); err != nil {
			t.Fatal(err)
		}
	}
}

func manifestPath(root string) string {
	return filepath.Join(root, "manifests", "figures", "2501.yaml")
}

func refsPath(root string) string {
	return filepath.Join(root, "manifests", "refs", "2501", "2501.00001.yaml")
}

// fixture is the paper every test in this package builds.
const fixture = "2501.00001"

var fileNames = []string{"00_front.md", "01_one.md", "02_two.md", "03_three.md"}

func front() extract.Document {
	return extract.Document{
		Front: extract.Front{Section: 0, SectionTitle: "Front matter", Kind: "front", LocalID: "front"},
		Body:  strings.Repeat("An abstract long enough to be an abstract. ", 10),
	}
}

func section(n int, title, body string) extract.Document {
	return extract.Document{
		Front: extract.Front{Section: n, SectionTitle: title, Kind: "section", LocalID: fmt.Sprintf("s%d", n)},
		Body:  body,
	}
}

func prose(n int) string {
	return strings.Repeat("Ordinary prose that says nothing in particular. ", n)
}

// audited audits the corpus at root and returns the results by rule id.
func audited(t *testing.T, root string) map[string]Result {
	t.Helper()
	c := Content{Root: root, Cap: 50}
	papers, err := c.Papers()
	if err != nil {
		t.Fatal(err)
	}
	report, err := c.Run(papers)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]Result{}
	for _, res := range report.Results {
		out[res.Rule.ID] = res
	}
	return out
}

// fires checks that one rule found something and that nothing else did, which
// is the half of a rule test that usually goes missing. A rule that fires on
// the right file and takes three others with it is not a working rule.
//
// also is the rules that cannot help going with it, which is one corpus and two
// true things said about it rather than a rule reaching too far. A paper the
// corpus may not publish the text of is the case: S01 says the text should not
// be there and F08 says the figure should not be either, and there is no way to
// break the one without the other.
func fires(t *testing.T, root, rule string, also ...string) Finding {
	t.Helper()
	got := audited(t, root)
	res, ok := got[rule]
	if !ok {
		t.Fatalf("%s is not a rule", rule)
	}
	if res.Total == 0 {
		t.Fatalf("%s found nothing", rule)
	}
	allowed := map[string]bool{rule: true}
	for _, id := range also {
		allowed[id] = true
		if got[id].Total == 0 {
			t.Errorf("%s was allowed to fire and did not", id)
		}
	}
	for id, other := range got {
		if !allowed[id] && other.Total > 0 {
			t.Errorf("%s also fired: %v", id, other.Findings)
		}
	}
	return res.Findings[0]
}

func TestACleanPaperPassesEveryRule(t *testing.T) {
	root := paper(t,
		front(),
		section(1, "One", "## A subsection\n\n"+prose(8)+"\n\n### Deeper\n\n"+prose(8)),
		section(2, "Two", theorem+prose(12)),
	)
	committed(t, root)
	for id, res := range audited(t, root) {
		if res.State() != Pass {
			t.Errorf("%s is %s with %v", id, res.State(), res.Findings)
		}
	}
}

func TestAnEmptyCorpusRunsNothingRatherThanPassingEverything(t *testing.T) {
	c := Content{Root: t.TempDir(), Cap: 50}
	papers, err := c.Papers()
	if err != nil {
		t.Fatal(err)
	}
	if len(papers) != 0 {
		t.Fatalf("found %v", papers)
	}
	report, err := c.Run(papers)
	if err != nil {
		t.Fatal(err)
	}
	for _, res := range report.Results {
		if res.State() != NotRun {
			t.Errorf("%s is %s over no papers", res.Rule.ID, res.State())
		}
	}
	if !strings.Contains(report.Text(), "0 files over 0 papers, nothing to check") {
		t.Errorf("the report reads %q", report.Text())
	}
}

func TestAFileWithNoFrontMatterIsReportedOnceAndSteppedOver(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	name := filepath.Join(root, "content", "en", "2501", "2501.00001", "01_one.md")
	if err := os.WriteFile(name, []byte("no front matter here at all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// T04 goes too, because the file that carried section 1 no longer says so.
	got := audited(t, root)
	if got["T01"].Total != 1 {
		t.Fatalf("T01 found %d", got["T01"].Total)
	}
	if got["T05"].Checked != 1 {
		t.Errorf("T05 looked at %d files, and it should have skipped the one that did not parse", got["T05"].Checked)
	}
}

func TestAFrontMatterFieldNobodyReadsOrNobodyCanReadIsReported(t *testing.T) {
	for _, c := range []struct{ name, from, to, want string }{
		{"a field this tool does not know", "kind: section", "kind: section\nsection_kind: appendix", "section_kind"},
		// YAML names the line and the value rather than the field, which is
		// what the reader needs anyway: the line is where they have to go.
		{"a field of the wrong type", "objects: 0", "objects: several", "`several` into int"},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := paper(t, front(), section(1, "One", prose(8)))
			name := filepath.Join(root, "content", "en", "2501", "2501.00001", "01_one.md")
			b, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), c.from) {
				t.Fatalf("the fixture has no %q in it", c.from)
			}
			b = []byte(strings.Replace(string(b), c.from, c.to, 1))
			if err := os.WriteFile(name, b, 0o644); err != nil {
				t.Fatal(err)
			}
			f := fires(t, root, "T02")
			if !strings.Contains(f.What, c.want) {
				t.Errorf("the finding reads %q", f.What)
			}
			// A report of a corpus is read by somebody looking at the corpus.
			// Naming a Go type sends them to read this package's source to find
			// out what their file did wrong.
			if strings.Contains(f.What, "extract.Front") {
				t.Errorf("the finding names a Go type: %q", f.What)
			}
			if strings.Contains(f.What, "\n") {
				t.Errorf("the finding is more than one line: %q", f.What)
			}
		})
	}
}

func TestABodyThatMovedAwayFromItsHashIsReported(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	name := filepath.Join(root, "content", "en", "2501", "2501.00001", "01_one.md")
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, append(b, []byte("a sentence somebody added by hand.\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	f := fires(t, root, "T03")
	if !strings.Contains(f.What, "ax split -accept") {
		t.Errorf("the finding does not say what to do about it: %q", f.What)
	}
}

func TestASectionNumberSkippedIsReported(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)), section(3, "Three", prose(8)))
	f := fires(t, root, "T04")
	if f.What != "numbers its sections 0 1 3" {
		t.Errorf("the finding reads %q", f.What)
	}
}

func TestAHeadingLevelSkippedIsReported(t *testing.T) {
	root := paper(t, front(), section(1, "One", "## A subsection\n\n"+prose(8)+"\n\n#### Far too deep\n\n"+prose(8)))
	f := fires(t, root, "T05")
	if f.What != "goes from ## to ####" {
		t.Errorf("the finding reads %q", f.What)
	}
}

func TestABodyThatOpensBelowTheSectionTitleIsReported(t *testing.T) {
	root := paper(t, front(), section(1, "One", "# The section title again\n\n"+prose(8)))
	if f := fires(t, root, "T05"); !strings.Contains(f.What, "opens at #") {
		t.Errorf("the finding reads %q", f.What)
	}
}

func TestAPaperWithNoFrontFileIsReported(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	if err := os.Remove(filepath.Join(root, "content", "en", "2501", "2501.00001", "00_front.md")); err != nil {
		t.Fatal(err)
	}
	got := audited(t, root)
	if got["T06"].Total != 1 {
		t.Fatalf("T06 found %d", got["T06"].Total)
	}
	if got["T06"].Findings[0].What != "has no 00_front.md" {
		t.Errorf("the finding reads %q", got["T06"].Findings[0].What)
	}
}

func TestAFrontFileWithNoAbstractIsReported(t *testing.T) {
	f := front()
	f.Body = ""
	root := paper(t, f, section(1, "One", prose(8)))
	if got := fires(t, root, "T06"); got.What != "has a 00_front.md with no abstract in it" {
		t.Errorf("the finding reads %q", got.What)
	}
}

func TestASectionAfterTheReferencesIsReported(t *testing.T) {
	root := paper(t, front(), section(1, "References", prose(8)), section(2, "Two", prose(8)))
	if f := fires(t, root, "T07"); f.What != "puts 02_two.md after its references" {
		t.Errorf("the finding reads %q", f.What)
	}
}

// An appendix after the references is where an appendix goes, so the rule has
// to let it through or it fails on the ordinary shape of a mathematics paper.
func TestAnAppendixAfterTheReferencesIsNotReported(t *testing.T) {
	appendix := section(2, "A proof", prose(8))
	appendix.Front.Kind = "appendix"
	root := paper(t, front(), section(1, "References", prose(8)), appendix)
	if got := audited(t, root); got["T07"].Total != 0 {
		t.Errorf("T07 found %v", got["T07"].Findings)
	}
}

func TestASectionTooShortAndOneTooLongAreReported(t *testing.T) {
	root := paper(t, front(), section(1, "One", "A stub."), section(2, "Two", prose(1200)))
	got := audited(t, root)
	if got["T08"].Total != 1 || got["T09"].Total != 1 {
		t.Fatalf("T08 found %d and T09 found %d", got["T08"].Total, got["T09"].Total)
	}
	if !strings.Contains(got["T08"].Findings[0].What, "7 characters") {
		t.Errorf("T08 reads %q", got["T08"].Findings[0].What)
	}
}

// The abstract is as long as its author made it, so the short body rule does
// not look at the front matter file and has to be able to say it did not.
func TestAShortAbstractIsNotAShortSection(t *testing.T) {
	f := front()
	f.Body = "Short."
	root := paper(t, f, section(1, "One", prose(8)))
	got := audited(t, root)
	if got["T08"].Total != 0 {
		t.Errorf("T08 found %v", got["T08"].Findings)
	}
	if got["T08"].Checked != 1 {
		t.Errorf("T08 looked at %d files, want the one section", got["T08"].Checked)
	}
}

func TestThePageFurnitureIsReported(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{"the stamp", prose(4) + "\n\narXiv:2501.00001v1 [math.NT] 2 Jan 2025\n\n" + prose(4), "stamp"},
		{"a folio", prose(4) + "\n\n17\n\n" + prose(4), "page number"},
		{"the margin", prose(4) + "\n\na\nr\nX\ni\nv\n:\n2\n\n" + prose(4), "down the margin"},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := paper(t, front(), section(1, "One", c.body))
			if f := fires(t, root, "T10"); !strings.Contains(f.What, c.want) {
				t.Errorf("the finding reads %q", f.What)
			}
		})
	}
}

// A paper that says arXiv:2501.00001 in a sentence is saying it in a sentence.
// The stamp is the one with the category and the date after it.
func TestAnIdentifierInASentenceIsNotAStamp(t *testing.T) {
	root := paper(t, front(), section(1, "One", "The earlier version is arXiv:2501.00001v1 and it is wrong. "+prose(8)))
	if got := audited(t, root); got["T10"].Total != 0 {
		t.Errorf("T10 found %v", got["T10"].Findings)
	}
}

func TestRawMarkupIsReported(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(4)+"\n\n<table><tr><td>1.0</td></tr></table>\n\n"+prose(4)))
	if f := fires(t, root, "T11"); !strings.Contains(f.What, "<table>") {
		t.Errorf("the finding reads %q", f.What)
	}
}

// Mathematics is full of angle brackets and a listing is allowed to show HTML,
// so both are masked out before the rules read a line. The masking keeps every
// line where it was, which is why the findings above carry line numbers at all.
func TestMathematicsAndCodeAreNotBody(t *testing.T) {
	body := "Let $a<b>c$ hold. " + prose(4) + "\n\n```html\n<table><tr><td>17</td></tr></table>\n17\n#### not a heading\n```\n\n" + prose(4)
	root := paper(t, front(), section(1, "One", body))
	got := audited(t, root)
	for _, id := range []string{"T05", "T10", "T11"} {
		if got[id].Total != 0 {
			t.Errorf("%s found %v", id, got[id].Findings)
		}
	}
}

func TestAWordBrokenAtAPageHyphenIsReported(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(4)+"\n\nthe measure is quasi-\nrandom and that is that. "+prose(4)))
	if f := fires(t, root, "T13"); !strings.Contains(f.What, "quasi- and the next line starts random") {
		t.Errorf("the finding reads %q", f.What)
	}
}

func TestOnlyTheRulesAskedForAreReported(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	c := Content{Root: root, Cap: 50, Only: []string{"T03", "T06"}}
	papers, err := c.Papers()
	if err != nil {
		t.Fatal(err)
	}
	report, err := c.Run(papers)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results) != 2 {
		t.Fatalf("reported %d rules", len(report.Results))
	}
	if report.Results[0].Rule.ID != "T03" || report.Results[1].Rule.ID != "T06" {
		t.Errorf("reported %s and %s", report.Results[0].Rule.ID, report.Results[1].Rule.ID)
	}
}

// The findings cap is what keeps a rule that is wrong about the whole corpus to
// one screen, and the count next to it is what says how wrong it was.
func TestTheCapKeepsTheCountAndDropsTheRestOfAContentRule(t *testing.T) {
	root := paper(t, front(),
		section(1, "One", "#### too deep\n\n"+prose(8)),
		section(2, "Two", "#### too deep\n\n"+prose(8)),
		section(3, "Three", "#### too deep\n\n"+prose(8)),
	)
	c := Content{Root: root, Cap: 2, Only: []string{"T05"}}
	papers, err := c.Papers()
	if err != nil {
		t.Fatal(err)
	}
	report, err := c.Run(papers)
	if err != nil {
		t.Fatal(err)
	}
	res := report.Results[0]
	if res.Total != 3 || len(res.Findings) != 2 || res.Dropped() != 1 {
		t.Fatalf("%d found, %d kept, %d dropped", res.Total, len(res.Findings), res.Dropped())
	}
}

// A content finding names its own file. The metadata plane has one file per
// month and says so from the shard alone, and a content finding that did the
// same would point at metadata/2501.jsonl, which is not where it is.
func TestAContentFindingNamesItsFile(t *testing.T) {
	root := paper(t, front(), section(1, "One", "#### too deep\n\n"+prose(8)))
	f := fires(t, root, "T05")
	if f.Where() != "content/en/2501/2501.00001/01_one.md:1" {
		t.Errorf("the finding is at %q", f.Where())
	}
}

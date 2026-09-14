package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/tags"
)

// theorem is an object in a body, with the attribute block the extractor writes
// and without the tag, which paper puts in the way ax tags assign would.
const theorem = "**Theorem 1** {#thm-1 .statement env=theorem}\n\nSomething is true.\n\n"

// tagPattern is the tag the tagger wrote into a block, which is what a test
// that wants an object carrying the wrong tag or none at all reaches for.
var tagPattern = regexp.MustCompile(`\s+tag=[0-9A-Z]{4}`)

func registerPath(root string) string {
	return filepath.Join(root, "tags", "2501", fixture+".tags")
}

// reg reads the fixture's register back, so a test can name a tag the
// derivation handed out rather than one somebody wrote down here.
func reg(t *testing.T, root string) tags.Register {
	t.Helper()
	r, err := tags.LoadRegister(registerPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Entries) == 0 {
		t.Fatal("the fixture has an empty register")
	}
	return r
}

// appendLines puts lines on the end of the register, which is the one edit that
// does not disturb anything already in it, and says which line the first of
// them landed on.
func appendLines(t *testing.T, root string, lines ...string) int {
	t.Helper()
	b, err := os.ReadFile(registerPath(root))
	if err != nil {
		t.Fatal(err)
	}
	at := strings.Count(string(b), "\n") + 1
	b = append(b, []byte(strings.Join(lines, "\n")+"\n")...)
	if err := os.WriteFile(registerPath(root), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return at
}

// rewrite edits one file of the paper and writes it back through the writer, so
// the content hash is restamped and T03 stays out of the test.
func rewrite(t *testing.T, root, name string, edit func(d *extract.Document)) {
	t.Helper()
	path := filepath.Join(root, "content", "en", "2501", fixture, name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d, err := extract.ParseDocument(b)
	if err != nil {
		t.Fatal(err)
	}
	edit(&d)
	out, err := d.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestATagThatIsNotATagIsReported(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	at := appendLines(t, root, "ABC,s9")
	f := fires(t, root, "G01")
	if !strings.Contains(f.What, `names "ABC"`) {
		t.Errorf("the finding reads %q", f.What)
	}
	if f.Where() != fmt.Sprintf("tags/2501/2501.00001.tags:%d", at) {
		t.Errorf("the finding is at %q, and the line it is about is %d", f.Where(), at)
	}
}

// A register line that is not a line is reported by the rule that reads the
// register, the same way a content file that does not cut in two is reported by
// T01 and not by the nine rules that come after it.
func TestARegisterLineThatIsNotALineIsReported(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	appendLines(t, root, "ABCD")
	if f := fires(t, root, "G01"); !strings.Contains(f.What, "has 1 field") {
		t.Errorf("the finding reads %q", f.What)
	}
}

func TestOneTagOnTwoObjectsIsReported(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	taken := reg(t, root).Entries[0]
	appendLines(t, root, fmt.Sprintf("%s,s9", taken.Tag))
	f := fires(t, root, "G03")
	if !strings.Contains(f.What, fmt.Sprintf("gives %s to s9", taken.Tag)) {
		t.Errorf("the finding reads %q", f.What)
	}
	if !strings.Contains(f.What, "gave it to "+taken.Local) {
		t.Errorf("the finding does not say what the other line was: %q", f.What)
	}
}

func TestOneObjectWithTwoTagsIsReported(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	taken := reg(t, root).Entries[0]
	appendLines(t, root, fmt.Sprintf("ZZZZ,%s", taken.Local))
	if f := fires(t, root, "G04"); !strings.Contains(f.What, fmt.Sprintf("gives %s a second tag, ZZZZ", taken.Local)) {
		t.Errorf("the finding reads %q", f.What)
	}
}

// The register is the record and the file is the copy, and the two ways they
// come apart are a tag the register never handed out and a tag it handed to
// something else.
func TestABodyThatDisagreesWithTheRegisterIsReported(t *testing.T) {
	t.Run("an object the register has never heard of", func(t *testing.T) {
		root := paper(t, front(), section(1, "One", theorem+prose(8)))
		rewrite(t, root, "01_one.md", func(d *extract.Document) {
			d.Body = strings.Replace(d.Body, "{#thm-1 ", "{#thm-9 ", 1)
		})
		if f := fires(t, root, "G05"); !strings.Contains(f.What, "which is not in this paper's register") {
			t.Errorf("the finding reads %q", f.What)
		}
	})
	t.Run("a tag the register gives to something else", func(t *testing.T) {
		root := paper(t, front(), section(1, "One", prose(8)), section(2, "Two", prose(8)))
		other := reg(t, root).ByLocal()["s2"]
		rewrite(t, root, "01_one.md", func(d *extract.Document) { d.Front.Tag = string(other) })
		if f := fires(t, root, "G05"); !strings.Contains(f.What, "s1 carries "+string(other)+" and the register gives it") {
			t.Errorf("the finding reads %q", f.What)
		}
	})
	t.Run("a tag in an attribute block", func(t *testing.T) {
		root := paper(t, front(), section(1, "One", theorem+prose(8)))
		rewrite(t, root, "01_one.md", func(d *extract.Document) {
			d.Body = tagPattern.ReplaceAllString(d.Body, " tag=ZZZZ")
		})
		f := fires(t, root, "G05")
		if !strings.Contains(f.What, "thm-1 carries ZZZZ") {
			t.Errorf("the finding reads %q", f.What)
		}
		if f.Line != 1 {
			t.Errorf("the finding is on line %d, and the block is on the first", f.Line)
		}
	})
}

func TestAnObjectWithNoTagIsReported(t *testing.T) {
	t.Run("a section, which carries its tag in the front matter", func(t *testing.T) {
		root := paper(t, front(), section(1, "One", prose(8)))
		rewrite(t, root, "01_one.md", func(d *extract.Document) { d.Front.Tag = "" })
		if f := fires(t, root, "G06"); !strings.Contains(f.What, "is the section s1 and its front matter carries no tag") {
			t.Errorf("the finding reads %q", f.What)
		}
	})
	t.Run("an object in a body", func(t *testing.T) {
		root := paper(t, front(), section(1, "One", theorem+prose(8)))
		rewrite(t, root, "01_one.md", func(d *extract.Document) {
			d.Body = tagPattern.ReplaceAllString(d.Body, "")
		})
		if f := fires(t, root, "G06"); f.What != "thm-1 carries no tag" {
			t.Errorf("the finding reads %q", f.What)
		}
	})
}

// A paper nobody has run ax tags assign over fails on every object it has, and
// the three register rules say they had nothing to read rather than passing.
// Content is committed tagged, so this is a state the audit has to be loud
// about rather than one it lets through.
func TestAPaperWithNoRegisterFailsOnItsObjectsAndRunsNoRegisterRule(t *testing.T) {
	root := paper(t, front(), section(1, "One", theorem+prose(8)))
	rewrite(t, root, "01_one.md", func(d *extract.Document) {
		d.Front.Tag = ""
		d.Body = tagPattern.ReplaceAllString(d.Body, "")
	})
	if err := os.Remove(registerPath(root)); err != nil {
		t.Fatal(err)
	}
	got := audited(t, root)
	// Three objects in that section: the section itself, the theorem and the
	// figure the fixture lays out at the end of its last one.
	if got["G06"].Total != 3 {
		t.Errorf("G06 found %v", got["G06"].Findings)
	}
	for _, id := range []string{"G01", "G03", "G04"} {
		if got[id].State() != NotRun {
			t.Errorf("%s is %s over a paper with no register", id, got[id].State())
		}
	}
}

func TestATagWrittenWithoutItsPaperIsReported(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{"a link", prose(4) + "\n\nSee [Theorem 1](#VNYA) for the rest.\n\n" + prose(4), "links to VNYA"},
		{"in prose", prose(4) + "\n\nThe statement is tagged A3QK in the corpus.\n\n" + prose(4), ""},
		{"after a hash", prose(4) + "\n\nThe statement is #A3QK and nothing else.\n\n" + prose(4), "names A3QK with no paper"},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := paper(t, front(), section(1, "One", c.body))
			if c.want == "" {
				if got := audited(t, root); got["G02"].Total != 0 {
					t.Errorf("G02 found %v", got["G02"].Findings)
				}
				return
			}
			if f := fires(t, root, "G02"); !strings.Contains(f.What, c.want) {
				t.Errorf("the finding reads %q", f.What)
			}
			// One reference is one finding. A link is a hash in a body as well
			// as a link, and reporting it under both shapes is how a report of
			// twelve references reads as twenty four.
			if got := audited(t, root)["G02"].Total; got != 1 {
				t.Errorf("one reference was reported %d times", got)
			}
		})
	}
}

// The reference is the paper and the tag, and that is the form the rule is
// there to require rather than one it should complain about. The other two are
// the false positives the shape has to stay away from: a hash in front of four
// digits is a footnote marker or somebody's issue number, and an attribute
// block is where an identifier is defined rather than referenced.
func TestAReferenceThatNamesItsPaperIsNotReported(t *testing.T) {
	body := "It follows from 2106.09685#03QK and from [LoRA](ax://paper/2106.09685#03QK).\n\n" +
		"That is issue #1234 in the tracker.\n\n" + theorem + prose(8)
	root := paper(t, front(), section(1, "One", body))
	got := audited(t, root)
	if got["G02"].Total != 0 {
		t.Errorf("G02 found %v", got["G02"].Findings)
	}
	if got["G02"].Checked != 2 {
		t.Errorf("G02 looked at %d files, want both", got["G02"].Checked)
	}
}

func TestAKindThatIsNotOneOfTheSixteenIsReported(t *testing.T) {
	t.Run("an object in a body", func(t *testing.T) {
		root := paper(t, front(), section(1, "One", theorem+prose(8)))
		rewrite(t, root, "01_one.md", func(d *extract.Document) {
			d.Body = strings.Replace(d.Body, ".statement", ".theorem", 1)
		})
		if f := fires(t, root, "X01"); !strings.Contains(f.What, `thm-1 is of kind "theorem"`) {
			t.Errorf("the finding reads %q", f.What)
		}
	})
	t.Run("a block with no class at all", func(t *testing.T) {
		root := paper(t, front(), section(1, "One", theorem+prose(8)))
		rewrite(t, root, "01_one.md", func(d *extract.Document) {
			d.Body = strings.Replace(d.Body, " .statement", "", 1)
		})
		if f := fires(t, root, "X01"); !strings.Contains(f.What, "thm-1 carries no class") {
			t.Errorf("the finding reads %q", f.What)
		}
	})
	t.Run("a file", func(t *testing.T) {
		root := paper(t, front(), section(1, "One", prose(8)))
		rewrite(t, root, "01_one.md", func(d *extract.Document) { d.Front.Kind = "chapter" })
		if f := fires(t, root, "X01"); !strings.Contains(f.What, `is of kind "chapter"`) {
			t.Errorf("the finding reads %q", f.What)
		}
	})
}

// The extractor writes an appendix and a references section as kinds of their
// own, and both are sections. A rule that fails the ordinary shape of a paper
// is a rule somebody switches off.
func TestAnAppendixIsAKindOfFile(t *testing.T) {
	appendix := section(2, "A proof", prose(8))
	appendix.Front.Kind = "appendix"
	root := paper(t, front(), section(1, "References", prose(8)), appendix)
	if got := audited(t, root); got["X01"].Total != 0 {
		t.Errorf("X01 found %v", got["X01"].Findings)
	}
}

// The line an object's block sits on and not the line something with the same
// prefix sits on. A wrong line number here sends somebody to the wrong theorem,
// which is worse than no line number at all.
func TestTheLineOfAnObjectIsItsOwn(t *testing.T) {
	body := "one\n**Theorem 12** {#thm-12 .statement}\ntwo\n**Theorem 1** {#thm-1 .statement}\n"
	if got := lineOf(body, "thm-1"); got != 4 {
		t.Errorf("thm-1 is on line %d", got)
	}
	if got := lineOf(body, "thm-12"); got != 2 {
		t.Errorf("thm-12 is on line %d", got)
	}
	if got := lineOf(body, "thm-3"); got != 0 {
		t.Errorf("an object that is not there is on line %d", got)
	}
}

// A tombstone is what a reader gets instead of the object they asked for, and
// the version is the one thing it has to say.
func TestATombstoneThatDoesNotSayWhenIsReported(t *testing.T) {
	for _, c := range []struct{ name, line, want string }{
		{"no version at all", `ZZZZ,eq-7,,"it went"`, `says ""`},
		{"a version that is not one", `ZZZZ,eq-7,gone:later,"it went"`, `says "gone:later"`},
		{"the version without the word", `ZZZZ,eq-7,v3,"it went"`, `says "v3"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := paper(t, front(), section(1, "One", prose(8)))
			appendLines(t, root, c.line)
			if f := fires(t, root, "G07"); !strings.Contains(f.What, c.want) {
				t.Errorf("the finding reads %q", f.What)
			}
		})
	}
}

// The ordinary tombstone, and the thing about it that used to read as a fault:
// the identifier it keeps is the one the next version of the paper hands to
// whatever was renumbered into its place.
func TestATombstoneAndTheLiveEntryThatTookItsIdentifierAreBothFine(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	live := reg(t, root).Entries[0]
	appendLines(t, root, fmt.Sprintf(`ZZZZ,%s,gone:v3,"removed when section 4 was rewritten"`, live.Local))
	got := audited(t, root)
	for _, id := range []string{"G04", "G05", "G07"} {
		if got[id].Total != 0 {
			t.Errorf("%s found %v", id, got[id].Findings)
		}
	}
}

// G08, and the reason it reads the repository. The line is gone from the file,
// so nothing the corpus holds can tell you it was ever there.
func TestATagTakenOutOfARegisterIsReported(t *testing.T) {
	root := paper(t, front(), section(1, "One", theorem+prose(8)))
	// An object that went in an earlier version, so the body does not carry it
	// and this is a test about the register and not about the copy.
	appendLines(t, root, `ZZZZ,eq-7,gone:v2,"removed when section 4 was rewritten"`)
	committed(t, root)
	drop(t, root, "ZZZZ")
	f := fires(t, root, "G08")
	if !strings.Contains(f.What, "had ZZZZ on eq-7") {
		t.Errorf("the finding reads %q", f.What)
	}
	if !strings.Contains(f.What, "wants a revert") {
		t.Errorf("the finding does not say what to do about it: %q", f.What)
	}
	if f.Where() != "tags/2501/2501.00001.tags" {
		t.Errorf("the finding is at %q", f.Where())
	}
	// And the same removal once it is history rather than a working tree.
	commit(t, root)
	if got := audited(t, root)["G08"].Total; got != 1 {
		t.Errorf("a committed removal was reported %d times", got)
	}
}

// Carrying a register onto a new version rewrites the identifier on a line, and
// a rule that read that as a removal would fire on every paper that has ever
// been revised.
func TestATagThatOnlyMovedIsNotReported(t *testing.T) {
	root := paper(t, front(), section(1, "One", theorem+prose(8)))
	committed(t, root)
	moved := reg(t, root).ByLocal()["thm-1"]
	b, err := os.ReadFile(registerPath(root))
	if err != nil {
		t.Fatal(err)
	}
	out := strings.Replace(string(b), string(moved)+",thm-1", string(moved)+",thm-2", 1)
	if err := os.WriteFile(registerPath(root), []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	commit(t, root)
	if got := audited(t, root)["G08"].Total; got != 0 {
		t.Errorf("G08 found %v", got)
	}
}

// A corpus nobody has committed has no history, and a rule about what used to
// be in a file has nothing to read. Saying it passed would be saying every tag
// ever handed out is still there.
func TestAPaperWithNoHistoryLeavesTheHistoryRuleNotRun(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(8)))
	if got := audited(t, root)["G08"]; got.State() != NotRun {
		t.Errorf("G08 is %s over a corpus with no history", got.State())
	}
}

// drop takes one tag's line out of the register, which is the edit the rule
// exists to catch and the one nothing else in the corpus records.
func drop(t *testing.T, root string, tag tags.Tag) {
	t.Helper()
	b, err := os.ReadFile(registerPath(root))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, string(tag)+",") {
			continue
		}
		out = append(out, line)
	}
	if err := os.WriteFile(registerPath(root), []byte(strings.Join(out, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

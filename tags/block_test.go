package tags

import (
	"strings"
	"testing"
)

const sample = `## Applications {#su1-1 .section}

**Theorem 1.2** {#thm-1-2 .statement env=theorem}

*Suppose that $A\subset\mathbf{F}_{2}^{n}$.*

{#eq-1-1 .equation}

**Figure 2** {#fig-2 .figure file=f02.svg}

See [Theorem 1.2](#thm-1-2) for the statement.
`

func TestEveryAttributeBlockIsFoundInReadingOrder(t *testing.T) {
	got := Objects("01_introduction.md", sample)
	want := []Object{
		{File: "01_introduction.md", Local: "su1-1", Class: "section"},
		{File: "01_introduction.md", Local: "thm-1-2", Class: "statement"},
		{File: "01_introduction.md", Local: "eq-1-1", Class: "equation"},
		{File: "01_introduction.md", Local: "fig-2", Class: "figure"},
	}
	if len(got) != len(want) {
		t.Fatalf("found %+v", got)
	}
	for i := range want {
		if got[i].File != want[i].File || got[i].Local != want[i].Local || got[i].Class != want[i].Class {
			t.Errorf("the %dth object came out %+v and not %+v", i, got[i], want[i])
		}
	}
}

// The text of an object is the stretch of the file between its own anchor and
// the next one, which is what pass three hashes and pass four aligns.
func TestAnObjectCarriesTheTextUnderIt(t *testing.T) {
	got := Objects("01_introduction.md", sample)
	if want := `*Suppose that $A\subset\mathbf{F}_{2}^{n}$.*`; got[1].Text != want {
		t.Errorf("the theorem's text came out %q and not %q", got[1].Text, want)
	}
	// The equation's anchor is on its own line and the figure's is on the line
	// after the next blank one, so there is nothing of the equation's own in
	// between.
	if got[2].Text != "" {
		t.Errorf("the equation picked up %q, which belongs to nothing", got[2].Text)
	}
	// The last object runs to the end of the file rather than to the next
	// anchor, because there is not one.
	if want := "See [Theorem 1.2](#thm-1-2) for the statement."; got[3].Text != want {
		t.Errorf("the last object's text came out %q and not %q", got[3].Text, want)
	}
}

// The label is the pair pass one matches on, so it is read off the block along
// with the anchor and the class.
func TestTheLabelIsReadOffTheBlock(t *testing.T) {
	body := "**Theorem 1** {#thm-1 .statement label=thm:main env=theorem}\n"
	got := Objects("a.md", body)
	if len(got) != 1 || got[0].Label != "thm:main" {
		t.Fatalf("read %+v", got)
	}
}

func TestABlockWithNoLabelHasNone(t *testing.T) {
	got := Objects("a.md", "**Theorem 1** {#thm-1 .statement env=theorem}\n")
	if len(got) != 1 || got[0].Label != "" {
		t.Fatalf("read %+v", got)
	}
}

// A top level section has no attribute block of its own, so this is the only
// text it has.
func TestLeadIsWhatComesBeforeTheFirstAnchor(t *testing.T) {
	if got := Lead(sample); got != "" {
		t.Errorf("the sample starts with an anchor and its lead came out %q", got)
	}
	body := "Some prose first.\n\n**Theorem 1** {#thm-1 .statement}\nThe statement.\n"
	if want := "Some prose first."; Lead(body) != want {
		t.Errorf("the lead came out %q and not %q", Lead(body), want)
	}
}

func TestLeadOfAFileWithNoAnchorsIsTheWholeOfIt(t *testing.T) {
	if want := "Nothing here is an object."; Lead("Nothing here is an object.\n") != want {
		t.Errorf("the lead came out %q", Lead("Nothing here is an object.\n"))
	}
}

// A link is not an object. It is the same anchor written the other way round and
// mistaking one for the other would put a tag inside a sentence.
func TestALinkToAnObjectIsNotAnObject(t *testing.T) {
	for _, o := range Objects("a.md", sample) {
		if o.Local == "thm-1-2" && o.Class == "" {
			t.Fatal("the link [Theorem 1.2](#thm-1-2) was read as an object")
		}
	}
}

// The one false positive this shape can have, and the one that would corrupt an
// equation rather than just adding a stray attribute.
func TestABraceInMathematicsIsNotAnAttributeBlock(t *testing.T) {
	body := "$$\\{#1 .foo\\}$$\n"
	if got := Objects("a.md", body); len(got) != 0 {
		t.Fatalf("mathematics read as %+v", got)
	}
}

func TestAFootnoteDoesNotGetATag(t *testing.T) {
	if got := Objects("a.md", "1. A footnote {#fn-3 .note}\n"); len(got) != 0 {
		t.Fatalf("a footnote read as %+v", got)
	}
}

func TestTheTagGoesAfterTheClassAndBeforeEveryOtherPair(t *testing.T) {
	got, n := Retag(sample, map[string]Tag{
		"su1-1":   "03QK",
		"thm-1-2": "0A3F",
		"eq-1-1":  "2X40",
		"fig-2":   "1B77",
	})
	if n != 4 {
		t.Fatalf("rewrote %d blocks", n)
	}
	for _, want := range []string{
		"## Applications {#su1-1 .section tag=03QK}",
		"**Theorem 1.2** {#thm-1-2 .statement tag=0A3F env=theorem}",
		"{#eq-1-1 .equation tag=2X40}",
		"**Figure 2** {#fig-2 .figure tag=1B77 file=f02.svg}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the body does not hold %q", want)
		}
	}
	if !strings.Contains(got, "See [Theorem 1.2](#thm-1-2) for the statement.") {
		t.Error("the sentence with the link in it was changed")
	}
}

func TestRewritingTheSameTagsTwiceChangesNothing(t *testing.T) {
	assigned := map[string]Tag{"su1-1": "03QK", "thm-1-2": "0A3F", "eq-1-1": "2X40", "fig-2": "1B77"}
	once, _ := Retag(sample, assigned)
	twice, _ := Retag(once, assigned)
	if once != twice {
		t.Fatal("a second rewrite changed the body, so every assignment run would churn every file")
	}
}

// A tag is never changed, but the corpus should not end up with two of them on
// one line if one ever is.
func TestATagThatChangedReplacesTheOneThatWasThere(t *testing.T) {
	once, _ := Retag(sample, map[string]Tag{"thm-1-2": "0A3F"})
	twice, _ := Retag(once, map[string]Tag{"thm-1-2": "ZZ09"})
	if strings.Contains(twice, "0A3F") {
		t.Fatal("the old tag is still on the line")
	}
	if !strings.Contains(twice, "**Theorem 1.2** {#thm-1-2 .statement tag=ZZ09 env=theorem}") {
		t.Fatalf("the line came out as %q", line(twice, "thm-1-2"))
	}
}

func TestAnObjectWithNoTagIsLeftAlone(t *testing.T) {
	got, n := Retag(sample, map[string]Tag{"thm-1-2": "0A3F"})
	if n != 1 {
		t.Fatalf("rewrote %d blocks", n)
	}
	if !strings.Contains(got, "## Applications {#su1-1 .section}") {
		t.Error("a block with no tag in the map was rewritten anyway")
	}
}

func TestTaggedReadsBackWhatRetagWrote(t *testing.T) {
	assigned := map[string]Tag{"su1-1": "03QK", "thm-1-2": "0A3F"}
	got, _ := Retag(sample, assigned)
	back := Tagged(got)
	if len(back) != 2 || back["su1-1"] != "03QK" || back["thm-1-2"] != "0A3F" {
		t.Fatalf("read back %+v", back)
	}
}

func line(body, id string) string {
	for _, l := range strings.Split(body, "\n") {
		if strings.Contains(l, "{#"+id+" ") {
			return l
		}
	}
	return ""
}

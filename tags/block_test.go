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
		if got[i] != want[i] {
			t.Errorf("the %dth object came out %+v and not %+v", i, got[i], want[i])
		}
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

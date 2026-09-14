package refs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// find is one citation's worth of context, split at the marker, which is how a
// test says where the citation sits without writing two arguments.
func find(t *testing.T, s string) Locator {
	t.Helper()
	before, after, ok := strings.Cut(s, "@")
	if !ok {
		t.Fatalf("the context %q has no @ marking where the citation is", s)
	}
	return DefaultLocators().Find(before, after)
}

func TestTheEmbeddedPatternsCompile(t *testing.T) {
	l := DefaultLocators()
	if got := len(l.Patterns()); got < 6 {
		t.Fatalf("the embedded set has %d patterns, and the six shapes in 08-graph.md section 5 are the minimum", got)
	}
	if got := len(l.Kinds()); got < 25 {
		t.Fatalf("the embedded set knows %d kinds, and every field has its own words for these", got)
	}
}

func TestALocatorInsideTheCitationIsRead(t *testing.T) {
	for _, c := range []struct {
		context string
		want    Locator
	}{
		{`shown in \[[12](#bib.bib12)@, Lemma 3.4\]`, Locator{Kind: "lemma", Number: "3.4", Via: "after-comma"}},
		{`(Smith, 2020@, Theorem 2)`, Locator{Kind: "theorem", Number: "2", Via: "after-comma"}},
		{`\[[7](#bib.bib7)@, Thm. 3.4\]`, Locator{Kind: "theorem", Number: "3.4", Via: "after-comma"}},
		{`\[[7](#bib.bib7)@, Prop 5\]`, Locator{Kind: "proposition", Number: "5", Via: "after-comma"}},
		{`\[[7](#bib.bib7)@, Cor. 1.2.3\]`, Locator{Kind: "corollary", Number: "1.2.3", Via: "after-comma"}},
		{`\[[7](#bib.bib7)@, p. 42\]`, Locator{Kind: "page", Number: "42", Via: "after-comma"}},
		{`\[[7](#bib.bib7)@, Appendix B\]`, Locator{Kind: "appendix", Number: "B", Via: "after-comma"}},
		{`\[[7](#bib.bib7)@, Theorem I\]`, Locator{Kind: "theorem", Number: "I", Via: "after-comma"}},
		{`\[[7](#bib.bib7)@, Theorem 2a\]`, Locator{Kind: "theorem", Number: "2a", Via: "after-comma"}},
	} {
		if got := find(t, c.context); got != c.want {
			t.Errorf("%s read as %+v, want %+v", c.context, got, c.want)
		}
	}
}

// The section sign is the one kind word that is not a word, and it is why the
// alternation is built in two halves.
func TestASectionSignIsRead(t *testing.T) {
	for _, c := range []string{`\[[7](#bib.bib7)@, §3\]`, `\[[7](#bib.bib7)@, § 3\]`} {
		got := find(t, c)
		if got.Kind != "section" || got.Number != "3" {
			t.Errorf("%s read as %+v, want section 3", c, got)
		}
	}
}

// An equation is cited as (2.4) and the brackets are the style's, not part of
// the number, so the graph should not have to know about them.
func TestAnEquationNumberLosesItsBrackets(t *testing.T) {
	got := find(t, `\[[7](#bib.bib7)@, eq. (2.4)\]`)
	if got.Kind != "equation" || got.Number != "2.4" {
		t.Fatalf("read as %+v, want equation 2.4", got)
	}
}

func TestALocatorBeforeTheCitationIsRead(t *testing.T) {
	for _, c := range []struct {
		context string
		want    Locator
	}{
		{`as proved by Theorem 2 of \[@[7](#bib.bib7)\]`, Locator{Kind: "theorem", Number: "2", Via: "before-of"}},
		{`see Lemma 3.4 in \[@[7](#bib.bib7)\]`, Locator{Kind: "lemma", Number: "3.4", Via: "before-of"}},
		{`the comments on page 8 of \[@[11](#bib.bib11)\]`, Locator{Kind: "page", Number: "8", Via: "before-of"}},
		{`by Theorem 2, \[@[7](#bib.bib7)\]`, Locator{Kind: "theorem", Number: "2", Via: "before-comma"}},
		{`Theorem 3.1 due to \[@[7](#bib.bib7)\]`, Locator{Kind: "theorem", Number: "3.1", Via: "before-credit"}},
	} {
		if got := find(t, c.context); got != c.want {
			t.Errorf("%s read as %+v, want %+v", c.context, got, c.want)
		}
	}
}

func TestALocatorAfterTheCitationWithNoCommaIsRead(t *testing.T) {
	got := find(t, `see \[[12](#bib.bib12)\]@ Theorem 3.4 for the general case`)
	if got.Kind != "theorem" || got.Number != "3.4" || got.Via != "after-bare" {
		t.Fatalf("read as %+v, want theorem 3.4 via after-bare", got)
	}
}

// "as shown in [12]. Theorem 3.4 below says" is this paper's own Theorem 3.4
// starting a new sentence, and the full stop is the only thing that says so.
func TestAFullStopAfterTheCitationStopsTheBarePattern(t *testing.T) {
	if got := find(t, `as shown in \[[12](#bib.bib12)\]@. Theorem 3.4 below says`); !got.Empty() {
		t.Fatalf("read %+v out of a new sentence", got)
	}
}

// The case that made the kind words carry word boundaries. Without one, p for
// page matches the P of PINNs and the I of the next word is a Roman numeral,
// and a citation to a paper about neural networks acquires a page number.
func TestAKindWordInsideALongerWordIsNotAKindWord(t *testing.T) {
	for _, c := range []string{
		`\[[100](#bib.bib100)@\], PINNs \[[38](#bib.bib38)\]`,
		`\[[7](#bib.bib7)@, Chapman and Hall, 2004\]`,
	} {
		if got := find(t, c); !got.Empty() {
			t.Errorf("%s read as %+v, and no locator is printed there", c, got)
		}
	}
}

func TestACitationWithNoLocatorReadsNothing(t *testing.T) {
	for _, c := range []string{
		`we follow \[[12](#bib.bib12)@\] throughout`,
		`\[[6](#bib.bib6)@, [7](#bib.bib7), [8](#bib.bib8)\]`,
		`(Vaswani et al., 2017@) introduced the transformer`,
	} {
		if got := find(t, c); !got.Empty() {
			t.Errorf("%s read as %+v, and there is no locator in it", c, got)
		}
	}
}

// A paper writes Theorem~2 and the tie comes out the other side as a character
// that is not a space, which would miss every locator in every paper with a
// house style.
func TestANonBreakingSpaceIsASpace(t *testing.T) {
	got := find(t, "as proved by Theorem 2 of \\[@[7](#bib.bib7)\\]")
	if got.Kind != "theorem" || got.Number != "2" {
		t.Fatalf("read as %+v, want theorem 2", got)
	}
}

func TestTheStringOfALocator(t *testing.T) {
	if got := (Locator{Kind: "theorem", Number: "2"}).String(); got != "theorem 2" {
		t.Fatalf("printed %q", got)
	}
	if got := (Locator{}).String(); got != "" {
		t.Fatalf("an empty locator printed %q", got)
	}
}

func TestAPatternSetThatIsNotUsableIsRefused(t *testing.T) {
	for _, c := range []struct{ name, yaml string }{
		{"no number", "locators:\n  kinds:\n    theorem: [theorem]\n  patterns:\n    - {name: a, where: after, match: '{kind}'}\n"},
		{"no kinds", "locators:\n  number: '[0-9]+'\n  patterns:\n    - {name: a, where: after, match: '{kind}'}\n"},
		{"no patterns", "locators:\n  number: '[0-9]+'\n  kinds:\n    theorem: [theorem]\n"},
		{"a pattern with no name", "locators:\n  number: '[0-9]+'\n  kinds:\n    theorem: [theorem]\n  patterns:\n    - {where: after, match: '{kind}'}\n"},
		{"a pattern that reads neither side", "locators:\n  number: '[0-9]+'\n  kinds:\n    theorem: [theorem]\n  patterns:\n    - {name: a, where: sideways, match: '{kind}'}\n"},
		{"a pattern that does not compile", "locators:\n  number: '[0-9]+'\n  kinds:\n    theorem: [theorem]\n  patterns:\n    - {name: a, where: after, match: '{kind}('}\n"},
		{"a word under two kinds", "locators:\n  number: '[0-9]+'\n  kinds:\n    theorem: [thm]\n    lemma: [thm]\n  patterns:\n    - {name: a, where: after, match: '{kind}'}\n"},
	} {
		if _, err := ParseLocators([]byte(c.yaml)); err == nil {
			t.Errorf("%s was accepted", c.name)
		}
	}
}

func TestACorpusWithNoPatternSetUsesTheEmbeddedOne(t *testing.T) {
	l, err := LoadLocators(filepath.Join(t.TempDir(), "graph.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Kinds()) != len(DefaultLocators().Kinds()) {
		t.Fatal("a corpus with no pattern set did not fall back to the embedded one")
	}
}

func TestACorpusPatternSetReplacesTheEmbeddedOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.yaml")
	body := "locators:\n  number: '[0-9]+'\n  kinds:\n    theorem: [satz]\n  patterns:\n    - {name: german, where: after, match: '^,\\s*{kind}\\s*{number}'}\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := LoadLocators(path)
	if err != nil {
		t.Fatal(err)
	}
	got := l.Find("", ", Satz 4")
	if got.Kind != "theorem" || got.Number != "4" || got.Via != "german" {
		t.Fatalf("read as %+v, want theorem 4 via german", got)
	}
	if !l.Find("", ", Theorem 4").Empty() {
		t.Fatal("the embedded words are still being matched, and a corpus file replaces the set rather than adding to it")
	}
}

package refs

import (
	"strings"
	"testing"
)

func scan(t *testing.T, body string) []Cite {
	t.Helper()
	return Scan("01_introduction.md", body, DefaultLocators())
}

func TestEveryCitationIsRecordedAndNotOnlyTheLocatedOnes(t *testing.T) {
	body := `We follow \[[12](#bib.bib12)\] throughout, and use \[[7](#bib.bib7), Lemma 3.4\] once.`
	got := scan(t, body)
	if len(got) != 2 {
		t.Fatalf("read %d citations out of two", len(got))
	}
	if got[0].Entry != "bib.bib12" || got[0].Kind != "" {
		t.Errorf("the first came out as %+v, and it carries no locator", got[0])
	}
	if got[1].Entry != "bib.bib7" || got[1].Kind != "lemma" || got[1].Number != "3.4" {
		t.Errorf("the second came out as %+v, want lemma 3.4 of bib.bib7", got[1])
	}
	for _, c := range got {
		if c.Section != "01_introduction.md" {
			t.Errorf("%s says it sits in %q", c.Entry, c.Section)
		}
	}
}

// A paper has a thousand citations and a handful carry a locator, so the prose
// around a citation is kept for the handful that somebody will check and not
// for the thousand that nobody will.
func TestOnlyALocatedCitationKeepsTheProseAroundIt(t *testing.T) {
	got := scan(t, `see \[[12](#bib.bib12)\] and \[[7](#bib.bib7), Lemma 3.4\]`)
	if got[0].Text != "" {
		t.Errorf("a citation with no locator kept %q", got[0].Text)
	}
	if !strings.Contains(got[1].Text, "Lemma 3.4") {
		t.Errorf("the context %q does not show the words the locator was read from", got[1].Text)
	}
	if !strings.Contains(got[1].Text, "#bib.bib7") {
		t.Errorf("the context %q is not centred on the citation", got[1].Text)
	}
}

// The commonest shape in a numeric style is a run of citations in one bracket,
// and each of them is followed by a comma and then another citation rather than
// by a locator.
func TestCitationsInARunDoNotBorrowEachOthersLocator(t *testing.T) {
	got := scan(t, `\[[6](#bib.bib6), [7](#bib.bib7), [8](#bib.bib8), Lemma 2\]`)
	if len(got) != 3 {
		t.Fatalf("read %d citations out of three", len(got))
	}
	for _, c := range got[:2] {
		if c.Kind != "" {
			t.Errorf("%s took %s %s off the citation after it", c.Entry, c.Kind, c.Number)
		}
	}
	if got[2].Kind != "lemma" {
		t.Errorf("the last one came out as %+v, and the locator is its own", got[2])
	}
}

func TestALinkThatIsNotACitationIsNotACitation(t *testing.T) {
	got := scan(t, `by [Theorem 2](#thm-2) of this paper, and see [the site](https://example.com)`)
	if len(got) != 0 {
		t.Fatalf("read %d citations out of a cross reference and a URL", len(got))
	}
}

func TestTheFrontMatterIsNotProse(t *testing.T) {
	body := "---\npaper: \"2311.05762\"\nsection_title: 'A Note on \\[[7](#bib.bib7)\\]'\n---\nWe follow \\[[12](#bib.bib12)\\].\n"
	got := scan(t, Body(body))
	if len(got) != 1 || got[0].Entry != "bib.bib12" {
		t.Fatalf("read %+v, and only the body cites anything", got)
	}
}

func TestASectionWithNoFrontMatterIsAllBody(t *testing.T) {
	if got := Body("We follow nobody.\n"); got != "We follow nobody.\n" {
		t.Fatalf("Body took something off a section that has no front matter: %q", got)
	}
}

func TestTheLocatorOfACite(t *testing.T) {
	c := Cite{Kind: "theorem", Number: "2", Via: "after-comma"}
	if got := c.Locator(); got != (Locator{Kind: "theorem", Number: "2", Via: "after-comma"}) {
		t.Fatalf("came out as %+v", got)
	}
}

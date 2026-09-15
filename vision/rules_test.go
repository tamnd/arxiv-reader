package vision

import (
	"fmt"
	"strings"
	"testing"
)

// page is a reading that every rule is happy with, which the tests break one way
// at a time.
//
// A real page rather than a sentence, because most of these rules measure
// something about a page and a two line reading would pass V06 and V09 for the
// wrong reason.
func page() string {
	var b strings.Builder
	b.WriteString("# 2 The argument\n\n")
	// Numbered so that no two lines are the same, because a page of a paper does
	// not print one sentence eight times and a fixture that did would trip V03 on
	// its own.
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&b, "The estimate follows from the bound in the previous section %d, and the constant is absolute although we have not tried to make it small.\n\n", i+1)
	}
	b.WriteString("$$\n\\sum_{n\\le N} \\Lambda(n) = N + O(N\\exp(-c\\sqrt{\\log N}))\n\\tag{7}\n$$\n")
	return b.String()
}

// asked is what the rules said, as a set of identifiers, so a test can say which
// rule fired without depending on how the sentence is worded.
func asked(t *testing.T, q Question) map[string]string {
	t.Helper()
	if q.Text == "" {
		q.Text = page()
	}
	out := map[string]string{}
	for _, r := range Ask(q) {
		out[r.Rule] = r.What
	}
	return out
}

func TestAGoodPageIsRefusedByNothing(t *testing.T) {
	if got := asked(t, Question{Page: 4, Prompt: DefaultPrompt}); len(got) != 0 {
		t.Errorf("an ordinary page was refused: %v", got)
	}
}

// Nine rules, and each one of them is watched firing. A rule nobody has seen fire
// is a rule nobody has tested, and this table is the only reason to believe any of
// them work.
func TestEveryRuleFires(t *testing.T) {
	long := strings.Repeat("the model has started writing rather than reading the page. ", 260)
	for _, c := range []struct {
		rule string
		q    Question
	}{
		{"V01", Question{Text: "   \n\n  "}},
		{"V02", Question{Text: "I cannot read this image.\n\n" + page()}},
		{"V03", Question{Text: strings.Repeat("the same line of the page over and over again\n", 5)}},
		{"V04", Question{Text: page() + "\n\nWrite out the text of this page of an academic paper exactly as it is printed.", Prompt: DefaultPrompt}},
		{"V05", Question{Text: page(), Before: page()}},
		{"V06", Question{Text: long}},
		{"V07", Question{Text: strings.Repeat("the p�ge is h�lf gone and every third letter with it. ", 40)}},
		{"V08", Question{Text: "$$\n\\sum_n a_n = 1\n\nand then the page carried on in prose"}},
		{"V09", Question{Text: page(), Layer: "an entirely different page about the classification of finite simple groups which nothing in the reading accounts for at all whatsoever"}},
	} {
		got := asked(t, c.q)
		if _, fired := got[c.rule]; !fired {
			t.Errorf("%s did not fire, and what fired was %v", c.rule, keys(got))
		}
	}
}

// V02 matches the opening of a reading and not the body of one. A paper about
// image models says "the image shows" in its prose, and a rule that refused the
// page for it would be a rule fighting the papers it was written for.
func TestTalkingAboutItselfIsOnlyTheOpening(t *testing.T) {
	text := "# 3 Results\n\nThe image shows a cat, and the model said so, which is the behaviour we set out to measure.\n\n" + page()
	if got := asked(t, Question{Text: text}); len(got) != 0 {
		t.Errorf("a paper that talks about images was refused: %v", got)
	}
}

// V03 allows a line three times over, because a table of a paper prints the same
// short row more than once and so does a list of citations to one author.
func TestRepetitionIsAllowedUpToAPoint(t *testing.T) {
	text := page() + strings.Repeat("\n| the same row of a table with a long label |", 3)
	if _, fired := asked(t, Question{Text: text})["V03"]; fired {
		t.Error("V03 fired on a line that came back three times, which a table does")
	}
}

// V08 counts dollars in the prose and not in the listings. A systems paper's page
// is mostly shell, and a shell listing is full of dollar signs.
func TestUnclosedSpansIgnoreWhatIsInsideAFence(t *testing.T) {
	text := "```\n$ ./configure && make\necho $PATH\n```\n\nand the page carried on in prose."
	if _, fired := asked(t, Question{Text: text})["V08"]; fired {
		t.Error("V08 fired on the dollars in a shell listing")
	}
}

// V09 says nothing about a page whose text layer is a running head, which is every
// page of a paper on this path. A rule that fired there would fire on every paper
// and be turned off within a week.
func TestTheTextLayerRuleWaitsForASentence(t *testing.T) {
	if _, fired := asked(t, Question{Text: page(), Layer: "J. Algebra 214 (1999) 33"})["V09"]; fired {
		t.Error("V09 fired on a text layer that is a running head and a page number")
	}
}

// And it accounts for the words rather than the order of them, because a two
// column page has a reading order the text layer does not share.
func TestTheTextLayerRuleAccountsForWordsAndNotOrder(t *testing.T) {
	q := Question{
		Text:  page(),
		Layer: "constant absolute the estimate follows from the bound in the previous section although we have not tried to make it small at all",
	}
	if _, fired := asked(t, q)["V09"]; fired {
		t.Error("V09 fired on a layer whose words are all in the reading in another order")
	}
}

// The ceiling is the audit's ceiling. A reading accepted here becomes a file the
// audit reads, so two different numbers would mean this path accepts pages the
// corpus then fails on.
func TestTheCeilingIsTheAuditsCeiling(t *testing.T) {
	if MostPerPage != 12000 {
		t.Errorf("V06 allows %d characters a page and audit rule S08 allows 12000", MostPerPage)
	}
}

func TestEveryRuleSaysSomethingAndCanBeFound(t *testing.T) {
	if len(Rules) != 9 {
		t.Errorf("there are %d rules and the path was designed around nine", len(Rules))
	}
	seen := map[string]bool{}
	for _, r := range Rules {
		switch {
		case r.Says == "":
			t.Errorf("%s says nothing, and a rule nobody can read is a rule nobody can argue with", r.ID)
		case seen[r.ID]:
			t.Errorf("%s is the identifier of two rules, and a rule number has to be citable", r.ID)
		case !strings.HasPrefix(r.ID, "V"):
			t.Errorf("%s is not in the V group", r.ID)
		}
		seen[r.ID] = true
		if _, ok := Find(r.ID); !ok {
			t.Errorf("%s cannot be found by its own identifier", r.ID)
		}
	}
	if _, ok := Find("V99"); ok {
		t.Error("a rule that does not exist was found")
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

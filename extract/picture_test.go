package extract

import (
	"strings"
	"testing"
)

// The three states are three different lines, so the test is three different
// lines. A flag on one line would be one assertion and would not say which of
// the three a reader actually gets.
func TestAPictureIsWrittenAccordingToWhatWasDecided(t *testing.T) {
	pics := Pictures{
		"2501.00001v3/nothing.svg": {File: "/figures/2501/2501.00001/nothing.svg"},
		"2501.00001v3/second.png": {
			Withheld: "the caption credits somebody else",
			At:       "https://arxiv.org/html/2501.00001v3/second.png",
		},
	}
	fs, err := Files(parse(t, "rendering.html"), Front{Paper: "2501.00001", Version: "v3", Lang: "en"}, pics)
	if err != nil {
		t.Fatal(err)
	}
	var body string
	for _, f := range fs {
		body += f.Doc.Body
	}

	if !strings.Contains(body, "![](/figures/2501/2501.00001/nothing.svg)") {
		t.Errorf("the committed figure does not point at the corpus:\n%s", body)
	}
	if strings.Contains(body, "](2501.00001v3/second.png)") {
		t.Errorf("the withheld figure is still served off arXiv as an image:\n%s", body)
	}
	for _, want := range []string{
		"The image is withheld because the caption credits somebody else.",
		"It is in the paper on arXiv at https://arxiv.org/html/2501.00001v3/second.png.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the withheld note does not say %q:\n%s", want, body)
		}
	}
	// A withheld picture keeps its number and its caption, because that is the
	// whole reason the corpus is allowed to publish the page at all.
	if !strings.Contains(body, "**Figure 3**") {
		t.Errorf("the withheld figure lost its number:\n%s", body)
	}
}

// Until ax figures has run there is nothing decided about any of a paper's
// pictures, and the honest thing to write is where the picture actually is.
func TestAPictureNothingHasDecidedAboutPointsAtArxiv(t *testing.T) {
	fs, err := Files(parse(t, "rendering.html"), Front{Paper: "2501.00001", Version: "v3", Lang: "en"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var body string
	for _, f := range fs {
		body += f.Doc.Body
	}
	if !strings.Contains(body, "](2501.00001v3/nothing.svg)") {
		t.Errorf("an undecided picture is not written as it stands:\n%s", body)
	}
}

// A figure withheld for a reason nobody wrote down still gets a sentence, and
// the sentence has to be a sentence rather than a dangling because.
func TestAWithheldNoteReadsWithNothingToSay(t *testing.T) {
	if got := withheld(Picture{}); got != "*The image is withheld.*" {
		t.Errorf("the note is %q", got)
	}
}

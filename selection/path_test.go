package selection

import (
	"errors"
	"strings"
	"testing"
)

// The whole decision tree, every combination of the three facts, including the ones
// nobody has gathered. The table is the spec's diagram written out, and it is here
// because a tree with nine leaves is a tree somebody will otherwise reason about
// from memory.
func TestTheDecisionTreeHasAnAnswerOrSaysWhatIsMissing(t *testing.T) {
	for _, c := range []struct {
		what  string
		facts Facts
		want  Path
		// missing is what the refusal should mention when there is no path.
		missing string
	}{
		{"TeX and a rendering is the cheap path", Facts{TeX: Yes, Rendering: Yes}, PathRender, ""},
		{"TeX and no rendering is compiled here", Facts{TeX: Yes, Rendering: No}, PathSource, ""},
		{"TeX and nobody has asked arXiv", Facts{TeX: Yes}, "", "whether it renders this version"},
		{"a PDF with text is read as text", Facts{TeX: No, TextLayer: Yes}, PathNative, ""},
		{"a PDF with no text is read as pictures", Facts{TeX: No, TextLayer: No}, PathVision, ""},
		{"a PDF nobody has opened", Facts{TeX: No}, "", "whether the PDF holds a text layer"},
		{"nobody has looked at the submission", Facts{}, "", "what the submission holds"},
		// The two facts that belong to the other branch are ignored rather than
		// tipping the answer, because a PDF only paper with a rendering is a state
		// that cannot happen and a tree that quietly acted on it would be hiding a
		// bug in whatever produced it.
		{"a rendering does not decide a PDF only paper", Facts{TeX: No, Rendering: Yes}, "", "whether the PDF holds a text layer"},
		{"a text layer does not decide a TeX paper", Facts{TeX: Yes, TextLayer: Yes}, "", "whether it renders this version"},
	} {
		got, why, err := Decide(c.facts)
		if c.missing != "" {
			var missing *Missing
			if !errors.As(err, &missing) {
				t.Errorf("%s: came back as %s, %v", c.what, got, err)
				continue
			}
			if !strings.Contains(err.Error(), c.missing) {
				t.Errorf("%s: the refusal is %q and should mention %q", c.what, err, c.missing)
			}
			if missing.How == "" {
				t.Errorf("%s: the refusal says nothing to run", c.what)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.what, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: came back as %s and should be %s", c.what, got, c.want)
		}
		// The sentence is not decoration. It is what goes in the manifest, and a
		// path with nothing behind it is the word with no evidence all over again.
		if why == "" {
			t.Errorf("%s: %s was decided and nothing says why", c.what, got)
		}
	}
}

func TestThereAreFourPathsAndEachOneSaysWhatItIs(t *testing.T) {
	if len(Paths) != 4 {
		t.Errorf("there are %d paths and the project was built around four", len(Paths))
	}
	for _, p := range Paths {
		if p.Says() == "" {
			t.Errorf("%s says nothing about what it is", p)
		}
		got, err := ParsePath(string(p))
		if err != nil || got != p {
			t.Errorf("%s came back as %s, %v", p, got, err)
		}
	}
	_, err := ParsePath("ocr")
	if err == nil {
		t.Fatal("a path that is not one of the four was accepted")
	}
	for _, p := range Paths {
		if !strings.Contains(err.Error(), string(p)) {
			t.Errorf("the refusal does not name %s: %v", p, err)
		}
	}
}

// The path and the sentence behind it are both or neither, for the same reason a
// reason and its evidence are.
func TestAPathWithNothingBehindItIsRefused(t *testing.T) {
	e := chosen()
	e.Path = PathVision
	if err := e.Check(); err == nil {
		t.Error("a paper was put on the vision path with nothing saying why")
	}
	e.PathWhy = "the submission is a PDF with no text layer worth reading"
	if err := e.Check(); err != nil {
		t.Errorf("a path with a sentence behind it was refused: %v", err)
	}
	e.Path = "ocr"
	if err := e.Check(); err == nil {
		t.Error("a fifth path was accepted")
	}
	e.Path = ""
	if err := e.Check(); err == nil {
		t.Error("an entry explaining a path it is not on was accepted")
	}
}

// Every path is in the count, and so are the papers nobody has decided, because the
// undecided number is the one that says whether the next stage can be planned.
func TestTheCountsHoldTheFourPathsAndTheUndecided(t *testing.T) {
	m := Manifest{}
	first := chosen()
	first.Path, first.PathWhy = PathRender, "arXiv serves a rendering of this version"
	m.Put(first)
	second := chosen()
	second.ID = "1706.03762"
	m.Put(second)
	counts := m.ByPath()
	if counts[PathRender] != 1 {
		t.Errorf("the render count is %d", counts[PathRender])
	}
	if counts[""] != 1 {
		t.Errorf("the undecided count is %d", counts[""])
	}
	if len(counts) != len(Paths)+1 {
		t.Errorf("the counts hold %d entries and there are four paths and the undecided", len(counts))
	}
}

package graph

import "testing"

func TestAPaperIsANodeWithoutItsVersion(t *testing.T) {
	if got := Paper("2312.00752"); got != "ax://paper/2312.00752" {
		t.Errorf("got %q", got)
	}
	if kind, ok := Kind(Paper("2312.00752")); !ok || kind != KindPaper {
		t.Errorf("got %q %v", kind, ok)
	}
}

// The old style id keeps its slash, which is arxiv-cli's rule and the reason the
// parse is "everything after ax://paper/ is the id" rather than a path split.
func TestAnOldStyleIDKeepsItsSlash(t *testing.T) {
	uri := Paper("hep-th/9711200")
	if uri != "ax://paper/hep-th/9711200" {
		t.Errorf("got %q", uri)
	}
	if kind, ok := Kind(uri); !ok || kind != KindPaper {
		t.Errorf("got %q %v", kind, ok)
	}
	if got := PaperOf(uri); got != "hep-th/9711200" {
		t.Errorf("got %q", got)
	}
}

func TestAnObjectIsItsPaperAndItsTag(t *testing.T) {
	uri := Object("2312.00752", "3KH2")
	if uri != "ax://paper/2312.00752#3KH2" {
		t.Errorf("got %q", uri)
	}
	kind, ok := Kind(uri)
	if !ok || kind != KindObject {
		t.Errorf("got %q %v", kind, ok)
	}
	if got := PaperOf(uri); got != "2312.00752" {
		t.Errorf("paper of an object is %q", got)
	}
	if got := TagOf(uri); got != "3KH2" {
		t.Errorf("tag of an object is %q", got)
	}
}

// The one thing that makes the two fragment spaces safe to share.
func TestAVersionAndAnObjectAreToldApartByTheirFragment(t *testing.T) {
	version, object := Version("2312.00752", 2), Object("2312.00752", "V2XX")
	if k, _ := Kind(version); k != KindVersion {
		t.Errorf("%s is %q", version, k)
	}
	if k, _ := Kind(object); k != KindObject {
		t.Errorf("%s is %q", object, k)
	}
	if got := TagOf(version); got != "" {
		t.Errorf("a version has tag %q, and a tag is four characters of digits and capitals", got)
	}
}

func TestANameIsFoldedAndACategoryIsNot(t *testing.T) {
	if got := Name("Aidan N. Gomez"); got != "ax://name/aidan-n-gomez" {
		t.Errorf("got %q", got)
	}
	if got := Category("cs.CL"); got != "ax://category/cs.CL" {
		t.Errorf("a category code is arXiv's own and is not folded, got %q", got)
	}
}

func TestAConceptAndAnArtefactAreMintedFromTheirName(t *testing.T) {
	if got := Concept("Low Rank Adaptation"); got != "ax://concept/low-rank-adaptation" {
		t.Errorf("got %q", got)
	}
	if got := Artefact("GLUE"); got != "ax://artefact/glue" {
		t.Errorf("got %q", got)
	}
	if got := Concept("   "); got != "" {
		t.Errorf("a name that folds to nothing is not a node, got %q", got)
	}
}

func TestWhatIsNotANodeSaysSo(t *testing.T) {
	for _, s := range []string{
		"",
		"2312.00752",
		"http://arxiv.org/abs/2312.00752",
		"ax://",
		"ax://paper/",
		"ax://paper/2312.00752#lowercase",
		"ax://paper/2312.00752#v0",
		"ax://paper/2312.00752#TOOLONG",
		"ax://surface/s10",
		"ax://author/Hu_Edward_J",
	} {
		if kind, ok := Kind(s); ok {
			t.Errorf("%q came out as %q, and it is not a node in this graph", s, kind)
		}
		if got := PaperOf(s); got != "" {
			t.Errorf("%q has paper %q", s, got)
		}
	}
}

// The author space is arXiv's registered identifiers and the corpus does not
// have them, so nothing here mints one. This is the test that says so out loud,
// because the difference between a name and a person is the sort of thing that
// gets quietly collapsed a year later.
func TestTheAuthorSpaceIsNotTheNameSpace(t *testing.T) {
	if Name("Edward J. Hu") == "ax://author/Hu_Edward_J" {
		t.Error("a name node and an author node are two different claims")
	}
	if kind, ok := Kind(Name("Edward J. Hu")); !ok || kind != KindName {
		t.Errorf("got %q %v", kind, ok)
	}
}

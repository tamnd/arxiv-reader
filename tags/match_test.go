package tags

import (
	"strings"
	"testing"
)

// obj is one object written the way the passes care about it.
func obj(local, class, label, text string) Object {
	return Object{File: "01_introduction.md", Local: local, Class: class, Label: label, Text: text}
}

// pass is the pass that matched one object of the new version, by identifier.
func pass(d Diff, local string) Pass {
	for _, p := range d.Pairs {
		if p.New.Local == local {
			return p.Pass
		}
	}
	return 0
}

// came is where an object of the new version came from.
func came(d Diff, local string) string {
	for _, p := range d.Pairs {
		if p.New.Local == local {
			return p.Old.Local
		}
	}
	return ""
}

// The pass every renumbering depends on. The author called it thm:main in both
// versions and it is Theorem 4 in one and Theorem 2 in the other, and it is the
// same theorem.
func TestTheAuthorsLabelSurvivesARenumbering(t *testing.T) {
	old := []Object{obj("thm-4", "statement", "thm:main", "Every finite group is finite.")}
	next := []Object{obj("thm-2", "statement", "thm:main", "Every finite group is finite, obviously.")}
	d := Match(old, next)
	if len(d.Pairs) != 1 || d.Pairs[0].Pass != ByLabel {
		t.Fatalf("matched %+v", d)
	}
	if came(d, "thm-2") != "thm-4" {
		t.Errorf("thm-2 came from %q", came(d, "thm-2"))
	}
}

// Two objects on one label is evidence of nothing, so pass one says nothing and
// the next pass gets its chance.
func TestALabelOnTwoObjectsIsLeftToTheNextPass(t *testing.T) {
	old := []Object{
		obj("thm-1", "statement", "thm:main", "The first one."),
		obj("thm-2", "statement", "thm:main", "The second one."),
	}
	next := []Object{
		obj("thm-1", "statement", "thm:main", "The first one."),
		obj("thm-2", "statement", "thm:main", "The second one."),
	}
	d := Match(old, next)
	for _, p := range d.Pairs {
		if p.Pass != ByNumber {
			t.Errorf("%s matched by %s and not by its number", p.New.Local, p.Pass)
		}
	}
}

// The ordinary case, which is a paper extracted again with nothing moved.
func TestNothingMovedIsAllNumbers(t *testing.T) {
	old := []Object{obj("thm-1", "statement", "", "One."), obj("fig-1", "figure", "", "A picture.")}
	d := Match(old, old)
	if d.Count(ByNumber) != 2 || len(d.Added) != 0 || len(d.Gone) != 0 {
		t.Fatalf("matched %+v", d)
	}
	if d.Fell() != 0 {
		t.Errorf("a paper that did not change fell through at %v", d.Fell())
	}
}

// A section moved wholesale, which 03-tags.md calls the most common real
// revision. Nothing has a label, every number changed, and the prose did not.
func TestAMovedSectionIsMatchedByItsProse(t *testing.T) {
	old := []Object{obj("thm-3-2", "statement", "", "Let $X$ be a scheme. Then $X$ is a scheme.")}
	next := []Object{obj("thm-5-4", "statement", "", "Let $Y$ be a scheme.  Then   $Y$ is a scheme.")}
	d := Match(old, next)
	if len(d.Pairs) != 1 || pass(d, "thm-5-4") != ByContent {
		t.Fatalf("matched %+v", d)
	}
}

// The mathematics is stripped before the hash, which is what lets the same
// statement retypeset by a better converter still hash the same.
func TestTheHashIgnoresTheMathematicsAndTheSpacing(t *testing.T) {
	a := Hash("Let $X$ be a scheme.")
	b := Hash("Let $\\mathcal{X}$   be a\nscheme.")
	if a == "" || a != b {
		t.Errorf("%q and %q hashed to %q and %q", "Let $X$...", "Let $\\mathcal{X}$...", a, b)
	}
	if Hash("Let $X$ be a ring.") == a {
		t.Error("two different statements hashed the same")
	}
}

// An equation is nothing but mathematics, so stripping the mathematics leaves it
// with nothing. It keeps the mathematics rather than having no hash at all.
func TestAnEquationHashesItsMathematics(t *testing.T) {
	h := Hash("$$\na = b\n$$")
	if h == "" {
		t.Fatal("an equation came out with no hash")
	}
	if h == Hash("$$\na = c\n$$") {
		t.Error("two different equations hashed the same")
	}
}

func TestAnObjectWithNoTextHasNoHash(t *testing.T) {
	if got := Hash("   \n  "); got != "" {
		t.Errorf("empty text hashed to %q", got)
	}
}

// Pass four, which is the only one that matches on being alike rather than on
// being the same. The wording was edited and the object did not move.
func TestLightlyEditedWordingIsMatchedByTheAlignment(t *testing.T) {
	old := []Object{obj("thm-1", "statement", "", "Suppose that the sequence converges to a limit in the unit ball.")}
	next := []Object{obj("thm-9", "statement", "", "Suppose that the sequence converges to a limit in the closed unit ball.")}
	d := Match(old, next)
	if len(d.Pairs) != 1 || d.Pairs[0].Pass != BySequence {
		t.Fatalf("matched %+v", d)
	}
}

// Being in the same place is not evidence, because that is what pass two already
// said and it said no.
func TestTheAlignmentDoesNotPairThingsThatReadNothingAlike(t *testing.T) {
	old := []Object{obj("thm-1", "statement", "", "Every finite group has a composition series.")}
	next := []Object{obj("thm-2", "statement", "", "The Fourier transform of a Gaussian is a Gaussian.")}
	d := Match(old, next)
	if len(d.Pairs) != 0 {
		t.Fatalf("paired %+v", d.Pairs)
	}
	if len(d.Added) != 1 || len(d.Gone) != 1 {
		t.Fatalf("came out %+v", d)
	}
}

// The silent failure this whole package exists to prevent. A paper gains a
// theorem in the middle, so the new one has the old one's number, and taking
// that pair would hand a permanent name to a theorem the author has just
// written.
func TestATheoremInsertedInTheMiddleDoesNotTakeItsNeighboursName(t *testing.T) {
	kept := "Nothing comes of nothing, and that is the whole of it."
	old := []Object{obj("thm-1", "statement", "", kept)}
	next := []Object{
		obj("thm-1", "statement", "", "Something comes of something, which is a different claim."),
		obj("thm-2", "statement", "", kept),
	}
	d := Match(old, next)
	if len(d.Pairs) != 1 {
		t.Fatalf("matched %+v", d.Pairs)
	}
	if d.Pairs[0].Old.Local != "thm-1" || d.Pairs[0].New.Local != "thm-2" || d.Pairs[0].Pass != ByContent {
		t.Fatalf("the old theorem came out as %+v", d.Pairs[0])
	}
	if len(d.Added) != 1 || d.Added[0].Local != "thm-1" {
		t.Fatalf("the new theorem came out as %+v", d.Added)
	}
}

// The same thing the other way round. The first theorem was removed and the
// second was renumbered into its place, so the one that is left keeps its own
// name and the one that went gets a tombstone.
func TestATheoremRemovedFromTheMiddleDoesNotTakeItsNeighboursName(t *testing.T) {
	kept := "Nothing comes of nothing, and that is the whole of it."
	old := []Object{
		obj("thm-1", "statement", "", "Something comes of something, which is a different claim."),
		obj("thm-2", "statement", "", kept),
	}
	next := []Object{obj("thm-1", "statement", "", kept)}
	d := Match(old, next)
	if len(d.Pairs) != 1 || d.Pairs[0].Old.Local != "thm-2" || d.Pairs[0].Pass != ByContent {
		t.Fatalf("matched %+v", d.Pairs)
	}
	if len(d.Gone) != 1 || d.Gone[0].Local != "thm-1" {
		t.Fatalf("gone came out %+v", d.Gone)
	}
}

// A theorem does not become a figure, whatever the two of them read like.
func TestNothingIsMatchedAcrossKinds(t *testing.T) {
	text := "The same words in both of them."
	old := []Object{obj("thm-1", "statement", "", text)}
	next := []Object{obj("fig-1", "figure", "", text)}
	d := Match(old, next)
	if len(d.Pairs) != 0 {
		t.Fatalf("paired a statement with a figure: %+v", d.Pairs)
	}
}

// The alignment cannot cross itself, which is the property that makes pass four
// worth trusting. Two objects that swapped places are not silently paired the
// wrong way round.
func TestTheAlignmentKeepsReadingOrder(t *testing.T) {
	a := "The first statement, about groups and their subgroups."
	b := "The second statement, about rings and their ideals."
	old := []Object{obj("thm-1", "statement", "", a+" More."), obj("thm-2", "statement", "", b+" More.")}
	next := []Object{obj("thm-1", "statement", "", b+" Extra."), obj("thm-2", "statement", "", a+" Extra.")}
	d := Match(old, next)
	// Pass two takes both of these on their identifiers, which is the answer
	// this case is really about: the alignment never sees them.
	for _, p := range d.Pairs {
		if p.Old.Local != p.New.Local {
			t.Errorf("%s was paired with %s", p.Old.Local, p.New.Local)
		}
	}
}

// A paper with nothing to compare against is a first extraction, and everything
// in it is new rather than being matched to something.
func TestAFirstExtractionIsAllNew(t *testing.T) {
	next := []Object{obj("thm-1", "statement", "thm:main", "One."), obj("fig-1", "figure", "", "Two.")}
	d := Match(nil, next)
	if len(d.Added) != 2 || len(d.Pairs) != 0 {
		t.Fatalf("came out %+v", d)
	}
	if d.Fell() != 1 {
		t.Errorf("a first extraction fell through at %v and not at all of it", d.Fell())
	}
}

// The fifth from 03-tags.md section 6, which is what stops a paper that changed
// too much from having its names moved without somebody reading the diff.
func TestTooMuchFallingThroughRefusesToBeWritten(t *testing.T) {
	var old, next []Object
	for i := range 10 {
		old = append(old, obj(local(i), "statement", "", "Statement number "+local(i)+" of the paper."))
		next = append(next, obj(local(i), "statement", "", "Statement number "+local(i)+" of the paper."))
	}
	d := Match(old, next)
	if err := d.Err(); err != nil {
		t.Fatalf("a paper that did not change was refused: %v", err)
	}
	next[3] = obj("thm-x", "statement", "", "Something else entirely, about the Riemann zeta function.")
	next[7] = obj("thm-y", "statement", "", "Another thing entirely, about elliptic curves over finite fields.")
	next[8] = obj("thm-z", "statement", "", "A third thing entirely, about the distribution of primes.")
	d = Match(old, next)
	err := d.Err()
	if err == nil {
		t.Fatalf("three objects in ten changed and it was not refused: %+v", d)
	}
	if !strings.Contains(err.Error(), "30 per cent") {
		t.Errorf("the refusal reads %q", err)
	}
}

func local(i int) string { return "thm-" + string(rune('1'+i)) }

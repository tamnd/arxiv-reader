package tags

import (
	"errors"
	"strings"
	"testing"
)

func objects(locals ...string) []Object {
	out := make([]Object, 0, len(locals))
	for _, l := range locals {
		out = append(out, Object{File: "01_introduction.md", Local: l, Class: "statement"})
	}
	return out
}

func TestAPaperNobodyHasTaggedGetsATagForEveryObject(t *testing.T) {
	p, err := Assign("2311.05762", objects("s1", "thm-1-1", "eq-1-1"), Register{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Added) != 3 || p.Kept != 0 {
		t.Fatalf("added %v and kept %d", p.Added, p.Kept)
	}
	if len(p.Register.Entries) != 3 {
		t.Fatalf("the register came out %+v", p.Register.Entries)
	}
	for _, o := range []string{"s1", "thm-1-1", "eq-1-1"} {
		if !p.Assigned[o].Valid() {
			t.Errorf("%s got %q", o, p.Assigned[o])
		}
	}
	if err := p.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestAPaperExtractedAgainKeepsEveryTagItHad(t *testing.T) {
	first, err := Assign("2311.05762", objects("s1", "thm-1-1"), Register{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Assign("2311.05762", objects("s1", "thm-1-1"), first.Register)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Added) != 0 || second.Kept != 2 {
		t.Fatalf("added %v and kept %d", second.Added, second.Kept)
	}
	if second.Run != nil {
		t.Fatal("a run that handed out nothing wrote a run line, and the runs file would grow on every CI push")
	}
	for _, o := range []string{"s1", "thm-1-1"} {
		if first.Assigned[o] != second.Assigned[o] {
			t.Errorf("%s was %s and is now %s", o, first.Assigned[o], second.Assigned[o])
		}
	}
}

func TestAnObjectAddedToAPaperGetsAFreshTagAndTheRestKeepTheirs(t *testing.T) {
	first, err := Assign("2311.05762", objects("s1", "thm-1-1"), Register{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Assign("2311.05762", objects("s1", "thm-1-1", "thm-1-2"), first.Register)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Added) != 1 || second.Added[0] != "thm-1-2" {
		t.Fatalf("added %v", second.Added)
	}
	if second.Run == nil || second.Run.First != second.Assigned["thm-1-2"] || second.Run.Last != second.Assigned["thm-1-2"] {
		t.Fatalf("the run came out %+v", second.Run)
	}
	if second.Assigned["s1"] != first.Assigned["s1"] {
		t.Error("an existing object was given a new tag when a new one was added")
	}
}

// The run is the stretch of the register one assignment wrote, so it has to name
// the ends of that stretch and everything between them has to be in it.
func TestARunNamesTheStretchOfTheRegisterItWrote(t *testing.T) {
	first, err := Assign("2311.05762", objects("s1"), Register{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Assign("2311.05762", objects("s1", "a", "b", "c"), first.Register)
	if err != nil {
		t.Fatal(err)
	}
	e := second.Register.Entries
	if second.Run.First != e[1].Tag || second.Run.Last != e[3].Tag {
		t.Fatalf("the run is %+v and the register is %+v", second.Run, e)
	}
}

// The dangerous case, and the one the four passes in M4 exist for. Guessing here
// would point a reference at the wrong theorem, which is worse than the
// reference breaking, because a broken reference is visible.
func TestAnObjectThatDisappearedStopsTheRun(t *testing.T) {
	first, err := Assign("2311.05762", objects("s1", "thm-1-1"), Register{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Assign("2311.05762", objects("s1"), first.Register)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Missing) != 1 || second.Missing[0] != "thm-1-1" {
		t.Fatalf("missing is %v", second.Missing)
	}
	err = second.Err()
	if err == nil {
		t.Fatal("an object that disappeared was written over")
	}
	if !strings.Contains(err.Error(), "thm-1-1") {
		t.Errorf("the error does not name what went: %v", err)
	}
}

// A tombstone is what pass four writes in M4, and once it is there the object is
// accounted for and the run should not stop again.
func TestATombstonedObjectIsNotMissing(t *testing.T) {
	r := Register{Entries: []Entry{
		{Tag: "03QK", Local: "s1"},
		{Tag: "0A3F", Local: "thm-1-1", Gone: "v3", Note: "merged into thm-1-2"},
	}}
	p, err := Assign("2311.05762", objects("s1"), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Missing) != 0 {
		t.Fatalf("missing is %v", p.Missing)
	}
	if err := p.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestTwoObjectsWithOneIdentifierIsRefused(t *testing.T) {
	_, err := Assign("2311.05762", objects("thm-1", "thm-1"), Register{})
	if err == nil {
		t.Fatal("one identifier on two objects was accepted, and one of them would silently get the other's tag")
	}
}

func TestAPaperWithNothingInItIsNotTagged(t *testing.T) {
	_, err := Assign("2311.05762", nil, Register{})
	if !errors.Is(err, ErrNoObjects) {
		t.Fatalf("came back %v", err)
	}
}

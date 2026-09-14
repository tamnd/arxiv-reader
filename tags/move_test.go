package tags

import (
	"strings"
	"testing"
)

// The whole point of the matching, which is that a tag written against v2 still
// names the same theorem in v3 after it was renumbered.
func TestATagFollowsItsObjectToItsNewNumber(t *testing.T) {
	old := Register{Entries: []Entry{{Tag: "2X40", Local: "thm-4"}, {Tag: "2X41", Local: "fig-1"}}}
	d := Match(
		[]Object{obj("thm-4", "statement", "thm:main", "One."), obj("fig-1", "figure", "", "A picture.")},
		[]Object{obj("thm-2", "statement", "thm:main", "One."), obj("fig-1", "figure", "", "A picture.")},
	)
	m := old.Move(d, 3)
	if len(m.Carried) != 1 || m.Kept != 1 || len(m.Buried) != 0 {
		t.Fatalf("moved %+v", m)
	}
	if got := m.Register.ByLocal()["thm-2"]; got != "2X40" {
		t.Errorf("thm-2 came out as %q and not as 2X40", got)
	}
	if _, ok := m.Register.ByLocal()["thm-4"]; ok {
		t.Error("the old identifier is still in the register, so two tags answer to one object")
	}
	if m.Register.Version != 3 {
		t.Errorf("the register came out belonging to v%d and not to v3", m.Register.Version)
	}
}

// Straight from the Stacks Project, and the reason the register has four fields.
// A reference that resolves to an explanation is a working reference.
func TestAnObjectThatIsGoneGetsATombstoneAndKeepsItsTag(t *testing.T) {
	old := Register{Entries: []Entry{{Tag: "2X40", Local: "eq-7"}}}
	d := Match([]Object{obj("eq-7", "equation", "", "$$\na = b\n$$")}, nil)
	m := old.Move(d, 3)
	if len(m.Buried) != 1 {
		t.Fatalf("buried %+v", m.Buried)
	}
	e := m.Register.Entries[0]
	if e.Tag != "2X40" || e.Local != "eq-7" {
		t.Errorf("the tombstone came out %+v, and a tombstone keeps both", e)
	}
	if e.Gone != "v3" {
		t.Errorf("the tombstone says %q and not v3", e.Gone)
	}
	if !strings.Contains(e.Note, "equation") || !strings.Contains(e.Note, "01_introduction.md") {
		t.Errorf("the note reads %q, and it is meant to say what was where", e.Note)
	}
	if len(m.Register.Live()) != 0 {
		t.Error("a tombstoned entry is still live")
	}
	if len(m.Register.Taken()) != 1 {
		t.Error("a tombstoned tag is no longer taken, so it could be handed out again")
	}
}

// The file has to survive being written and read again, because a tombstone is
// four fields and every other line is two.
func TestATombstoneRoundTripsThroughTheFile(t *testing.T) {
	old := Register{Entries: []Entry{{Tag: "2X40", Local: "eq-7"}, {Tag: "2X41", Local: "thm-1"}}}
	d := Match(
		[]Object{obj("eq-7", "equation", "", "$$\na=b\n$$"), obj("thm-1", "statement", "", "One.")},
		[]Object{obj("thm-1", "statement", "", "One.")},
	)
	b := old.Move(d, 3).Register.Bytes()
	if !strings.Contains(string(b), "2X40,eq-7,gone:v3,") {
		t.Fatalf("the register came out as\n%s", b)
	}
	back, err := ParseRegister(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Entries) != 2 || back.Entries[0].Gone != "v3" {
		t.Fatalf("it read back as %+v", back.Entries)
	}
}

// An identifier is reused the moment the theorem that had it is removed and the
// next one is renumbered into its place, so a tombstone and a live entry share
// one. The live entry is the one the content plane is asking about.
func TestATombstoneAndALiveEntryMayShareAnIdentifier(t *testing.T) {
	old := Register{Entries: []Entry{{Tag: "2X40", Local: "thm-1"}, {Tag: "2X41", Local: "thm-2"}}}
	// The first theorem was removed and the second was renumbered into its
	// place, which the label is what says.
	d := Match(
		[]Object{obj("thm-1", "statement", "", "The one that was removed."), obj("thm-2", "statement", "thm:main", "The one that stayed.")},
		[]Object{obj("thm-1", "statement", "thm:main", "The one that stayed.")},
	)
	m := old.Move(d, 3)
	b := m.Register.Bytes()
	back, err := ParseRegister(b)
	if err != nil {
		t.Fatalf("the register no longer reads: %v\n%s", err, b)
	}
	if got := back.ByLocal()["thm-1"]; got != "2X41" {
		t.Errorf("thm-1 came out as %q, and the live entry is the one that answers", got)
	}
}

// An entry naming something neither version has is a register and a corpus that
// have come apart, and guessing at it would be exactly the wrong answer.
func TestAnEntryNeitherVersionHasIsLeftAlone(t *testing.T) {
	old := Register{Entries: []Entry{{Tag: "2X40", Local: "thm-99"}}}
	d := Match([]Object{obj("thm-1", "statement", "", "One.")}, []Object{obj("thm-1", "statement", "", "One.")})
	m := old.Move(d, 3)
	if len(m.Stray) != 1 || m.Stray[0] != "thm-99" {
		t.Fatalf("came out %+v", m)
	}
	if m.Register.Entries[0].Gone != "" {
		t.Error("it was tombstoned rather than reported")
	}
}

// A tombstone is settled. This run is not the one to reopen it, and an object
// that came back gets its old tag through ByLocal rather than through here.
func TestAMoveLeavesAnOldTombstoneWhereItIs(t *testing.T) {
	old := Register{Entries: []Entry{{Tag: "2X40", Local: "eq-7", Gone: "v2", Note: "removed when section 4 was rewritten"}}}
	d := Match(nil, []Object{obj("thm-1", "statement", "", "One.")})
	m := old.Move(d, 3)
	if m.Register.Entries[0].Gone != "v2" || m.Register.Entries[0].Note != "removed when section 4 was rewritten" {
		t.Errorf("the old tombstone came out %+v", m.Register.Entries[0])
	}
	if len(m.Stray) != 0 || len(m.Buried) != 0 {
		t.Errorf("came out %+v", m)
	}
}

// Assignment matches on the identifier alone, so a register that has been
// carried over is a register whose next assignment is the ordinary case.
func TestAssignmentAfterAMoveKeepsEveryTag(t *testing.T) {
	old := Register{Entries: []Entry{{Tag: "2X40", Local: "thm-4"}}}
	next := []Object{obj("thm-2", "statement", "thm:main", "One."), obj("fig-1", "figure", "", "A picture.")}
	d := Match([]Object{obj("thm-4", "statement", "thm:main", "One.")}, next)
	m := old.Move(d, 3)
	plan, err := Assign("2106.09685", next, m.Register)
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Err(); err != nil {
		t.Fatalf("the assignment refused after a move: %v", err)
	}
	if plan.Kept != 1 || len(plan.Added) != 1 {
		t.Fatalf("the plan came out %+v", plan)
	}
	if plan.Assigned["thm-2"] != "2X40" {
		t.Errorf("thm-2 was assigned %q and not the tag it already had", plan.Assigned["thm-2"])
	}
}

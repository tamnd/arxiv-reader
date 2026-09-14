package tags

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestARegisterSurvivesBeingWrittenAndReadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "2311", "2311.05762.tags")
	want := Register{Entries: []Entry{
		{Tag: "03QK", Local: "s4-1"},
		{Tag: "0A3F", Local: "thm-1"},
		{Tag: "2X40", Local: "eq-7", Gone: "v3", Note: "removed when section 4 was rewritten"},
	}}
	changed, err := want.Save(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("writing a register that was not there said nothing changed")
	}
	got, err := LoadRegister(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 3 {
		t.Fatalf("read back %+v", got)
	}
	if got.Entries[2] != want.Entries[2] {
		t.Fatalf("the tombstone read back as %+v", got.Entries[2])
	}
}

func TestWritingTheSameRegisterTwiceChangesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "2311.05762.tags")
	r := Register{Entries: []Entry{{Tag: "03QK", Local: "s4-1"}}}
	if _, err := r.Save(path); err != nil {
		t.Fatal(err)
	}
	changed, err := r.Save(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("the second write of the same register said the bytes changed")
	}
}

func TestAPaperNobodyHasTaggedHasAnEmptyRegisterAndThatIsNotAnError(t *testing.T) {
	got, err := LoadRegister(filepath.Join(t.TempDir(), "nothing.tags"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 0 {
		t.Fatalf("a file that is not there read as %+v", got)
	}
}

func TestARegisterThatCannotBeTrustedIsRefused(t *testing.T) {
	for _, c := range []struct{ name, text string }{
		{"a tag that is not a tag", "03qk,s4-1\n"},
		{"a tag of the wrong length", "03QKA,s4-1\n"},
		{"no local identifier", "03QK,\n"},
		{"one field", "03QK\n"},
		{"five fields", "03QK,s4-1,gone:v3,why,extra\n"},
		{"one tag on two objects", "03QK,s4-1\n03QK,thm-1\n"},
		{"one object with two tags", "03QK,s4-1\n0A3F,s4-1\n"},
	} {
		if _, err := ParseRegister([]byte(c.text)); err == nil {
			t.Errorf("a register with %s was accepted", c.name)
		}
	}
}

func TestCommentsAreNotEntries(t *testing.T) {
	got, err := ParseRegister([]byte("# a comment\n03QK,s4-1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 {
		t.Fatalf("read %+v", got.Entries)
	}
}

// A tombstoned tag is still spoken for. Handing it out again would point every
// reference written against the old object at a new one, which is worse than the
// reference breaking, because a broken reference is visible.
func TestATombstonedTagIsStillTaken(t *testing.T) {
	r := Register{Entries: []Entry{
		{Tag: "03QK", Local: "s4-1"},
		{Tag: "2X40", Local: "eq-7", Gone: "v3"},
	}}
	if len(r.Taken()) != 2 {
		t.Fatalf("taken is %v", r.Taken())
	}
	if live := r.Live(); len(live) != 1 || live[0].Tag != "03QK" {
		t.Fatalf("live is %v", live)
	}
}

func TestTheRegisterSaysWhatWroteItAndWhatItIsFor(t *testing.T) {
	b := Register{Entries: []Entry{{Tag: "03QK", Local: "s4-1"}}}.Bytes()
	if !strings.HasPrefix(string(b), "# The permanent name of every object") {
		t.Fatal("the register does not say what it holds, and a file in a corpus nobody can read is a file somebody will edit by hand")
	}
}

// Which version the identifiers belong to is what tells an assignment that the
// paper moved under it, so it has to survive the file.
func TestTheVersionSurvivesBeingWrittenAndReadBack(t *testing.T) {
	b := Register{Version: 3, Entries: []Entry{{Tag: "03QK", Local: "s4-1"}}}.Bytes()
	if !strings.Contains(string(b), "\n# version: v3\n") {
		t.Fatalf("the register came out as\n%s", b)
	}
	got, err := ParseRegister(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 3 || len(got.Entries) != 1 {
		t.Fatalf("it read back as %+v", got)
	}
}

// Every register written before this was recorded has no such line, and it reads
// as not knowing rather than as version zero of anything.
func TestARegisterWithNoVersionLineComesBackAsZero(t *testing.T) {
	got, err := ParseRegister([]byte("03QK,s4-1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 0 {
		t.Fatalf("the version came out %d", got.Version)
	}
	if strings.Contains(string(got.Bytes()), "# version:") {
		t.Fatal("a register that does not know its version wrote a line saying which it is")
	}
}

func TestRunsSurviveBeingWrittenAndReadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "2311.05762.runs")
	want := []Run{{First: "03QK", Last: "0A3F"}, {First: "1B77", Last: "2X40"}}
	if _, err := SaveRuns(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadRuns(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("read back %+v", got)
	}
}

func TestARunThatNamesSomethingThatIsNotATagIsRefused(t *testing.T) {
	if _, err := ParseRuns([]byte("03QK,nope\n")); err == nil {
		t.Fatal("a run ending at something that is not a tag was accepted")
	}
}

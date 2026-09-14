package tags

import "fmt"

// Moved is what carrying a register from one version of a paper to the next did.
type Moved struct {
	// Register is the register as it would be after the move.
	Register Register
	// Carried is the entries whose object is somewhere else in the paper now,
	// which is the work this is for.
	Carried []Pair
	// Buried is the entries that got a tombstone.
	Buried []Entry
	// Kept is how many entries name the same identifier they did before.
	Kept int
	// Stray is entries naming an identifier the old version does not have
	// either, which is a register and a corpus that have come apart.
	Stray []string
}

// Move carries a register onto a new version of the paper.
//
// Every tag the passes matched is pointed at the identifier its object has now,
// and every tag whose object is not in the new version at all is tombstoned with
// the version it was last present in. Nothing is deleted and no tag is handed to
// a different object, which is the whole of the guarantee this package makes.
//
// This is what makes ax tags assign work after a revision. Assignment matches on
// the identifier alone, which is pass two, so a register that has been carried
// over is a register whose identifiers line up and whose assignment is then the
// ordinary case of a paper where nothing moved.
func (r Register) Move(d Diff, version int) Moved {
	to := fmt.Sprintf("v%d", version)
	carried := make(map[string]Pair, len(d.Pairs))
	for _, p := range d.Pairs {
		carried[p.Old.Local] = p
	}
	gone := make(map[string]Object, len(d.Gone))
	for _, o := range d.Gone {
		gone[o.Local] = o
	}
	m := Moved{Register: Register{Entries: make([]Entry, len(r.Entries)), Version: version}}
	copy(m.Register.Entries, r.Entries)
	for i, e := range m.Register.Entries {
		// A tombstone is already settled. The object it names came back or it
		// did not, and either way this run is not the one to reopen it.
		if e.Gone != "" {
			continue
		}
		if p, ok := carried[e.Local]; ok {
			if p.New.Local == e.Local {
				m.Kept++
				continue
			}
			m.Register.Entries[i].Local = p.New.Local
			m.Carried = append(m.Carried, p)
			continue
		}
		o, ok := gone[e.Local]
		if !ok {
			m.Stray = append(m.Stray, e.Local)
			continue
		}
		m.Register.Entries[i].Gone = to
		m.Register.Entries[i].Note = note(o, to)
		m.Buried = append(m.Buried, m.Register.Entries[i])
	}
	return m
}

// note is the sentence a tombstone carries when nobody has written a better one.
//
// It says what the object was and where it was, which is what somebody arriving
// at the tag a year from now needs in order to work out what happened. The note
// is prose and this is a placeholder for prose: a person who knows that section
// four was rewritten should say so in the register instead.
func note(o Object, to string) string {
	kind := o.Class
	if kind == "" {
		kind = "object"
	}
	return fmt.Sprintf("%s in %s, and nothing in %s matches it", kind, o.File, to)
}

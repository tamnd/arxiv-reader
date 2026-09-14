package tags

import (
	"fmt"
	"sort"

	"github.com/tamnd/arxiv-reader/internal/prose"
)

// Plan is what one assignment would do, worked out before anything is written.
//
// Assignment is the one operation in this project that cannot be undone. A tag
// handed out to the wrong object and committed is a wrong answer that every
// later reference inherits, so the decision is made whole, checked, and only
// then written.
type Plan struct {
	// Register is the register as it would be after the run.
	Register Register
	// Run is the stretch of it this assignment wrote, or nil when it handed out
	// nothing.
	Run *Run
	// Assigned is every object in the paper and the tag it now carries, kept
	// ones included, because that is what the content files are rewritten from.
	Assigned map[string]Tag
	// Added is the objects that got a tag for the first time, in the order they
	// got one.
	Added []string
	// Kept is how many objects already had one.
	Kept int
	// Missing is register entries with no object in the paper any more.
	Missing []string
}

// Assign works out the tags for one paper.
//
// It matches by local identifier, which is the same object in the same section
// with the same kind and the same printed number. That is pass two of the four
// in 03-tags.md section 6 and it is the one that catches the ordinary case,
// where a paper was extracted again and nothing moved.
//
// The other three passes, the author's own label, the normalised content hash
// and the sequence alignment, are what catch a paper whose objects moved between
// versions. They arrive with ax tags diff in M4, together with the tombstones
// they need. Until then an object that disappeared stops this run rather than
// being guessed at, because guessing is exactly the failure this whole mechanism
// exists to prevent: a reference that breaks is visible and a reference silently
// pointed at the wrong theorem is not.
func Assign(paper string, objects []Object, old Register) (Plan, error) {
	if len(objects) == 0 {
		return Plan{}, ErrNoObjects
	}
	byLocal := old.ByLocal()
	seen := map[string]bool{}
	p := Plan{Register: Register{Entries: old.Entries}, Assigned: map[string]Tag{}}
	space := NewSpace(paper, old.Taken())
	for _, o := range objects {
		if seen[o.Local] {
			return Plan{}, fmt.Errorf("tags: %s is the identifier of two objects in %s, the second of them in %s", o.Local, paper, o.File)
		}
		seen[o.Local] = true
		if t, ok := byLocal[o.Local]; ok {
			p.Assigned[o.Local] = t
			p.Kept++
			continue
		}
		t := space.Next()
		p.Assigned[o.Local] = t
		p.Added = append(p.Added, o.Local)
		p.Register.Entries = append(p.Register.Entries, Entry{Tag: t, Local: o.Local})
	}
	for _, e := range old.Live() {
		if !seen[e.Local] {
			p.Missing = append(p.Missing, e.Local)
		}
	}
	sort.Strings(p.Missing)
	if n := len(p.Added); n > 0 {
		first := p.Register.Entries[len(p.Register.Entries)-n].Tag
		last := p.Register.Entries[len(p.Register.Entries)-1].Tag
		p.Run = &Run{First: first, Last: last}
	}
	return p, nil
}

// Err is the reason an assignment must not be written, or nil.
//
// Separate from Assign because a caller doing a dry run wants the plan and the
// objection both, and a caller about to write wants to be stopped.
func (p Plan) Err() error {
	if len(p.Missing) == 0 {
		return nil
	}
	return fmt.Errorf("tags: the register names %s the paper no longer has, starting with %s, and matching them to what replaced them is ax tags diff in M4", prose.Count(len(p.Missing), "object"), p.Missing[0])
}

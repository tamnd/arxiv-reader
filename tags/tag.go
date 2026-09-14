// Package tags hands out the permanent names for the objects inside a paper.
//
// A tag is four characters of the Stacks Project's alphabet and it is assigned
// once and never changed. It is what lets a link written today survive the
// paper being extracted again next year by a better tool, and what lets the
// Vietnamese, Chinese and Japanese versions of a theorem point at the same
// theorem.
//
// The idea is the Stacks Project's, by way of tamnd/bourbaki. Numbering is not
// stable over time, LaTeX labels are human readable and therefore get edited,
// and a reference has to survive both.
package tags

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// Alphabet is the Stacks Project's, which is the digits and the capitals.
//
// Case is not folded and lowercase is not accepted, because a tag that reads
// the same as another tag is a tag two links disagree about.
const Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

// Width is how many characters a tag has, and Names is how many tags there are.
//
// Four characters is 1,679,616 names. Bourbaki and papers used four hex digits,
// which is 65,536, and that was enough for one treatise and for a hundred
// papers. It is not enough for a corpus, and the answer is not a longer tag, it
// is a tag with a smaller job: a tag is unique inside one paper and the arXiv id
// carries the other half of the name. A document that will never hold more than
// a few hundred objects drawing from 1.6 million names is not tight, it is
// absurdly loose, and that is what makes the next paragraph affordable.
const (
	Width = 4
	Names = 36 * 36 * 36 * 36
)

// Tag is one permanent name.
type Tag string

// Valid reports whether a string is a tag at all.
func (t Tag) Valid() bool {
	if len(t) != Width {
		return false
	}
	for i := 0; i < len(t); i++ {
		c := t[i]
		if !(c >= '0' && c <= '9') && !(c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}

// Space hands out the tags of one paper.
//
// Tags come out of it in an order that has nothing to do with the order of the
// objects they land on, which is deliberate. A sequential tag encodes where in
// the paper its object sits, and the first person to notice that will write code
// that sorts by it, which then breaks the day a section is inserted. There is no
// order to read into a tag here because there is none in it.
//
// The order is still the same on every run, which is what makes re-running an
// assignment write the same bytes. It is a hash of the paper and a counter, so
// two papers do not hand out their tags in the same order either, and a person
// looking at two papers side by side sees nothing in common between them.
type Space struct {
	paper string
	n     int
	taken map[Tag]bool
}

// NewSpace opens the space of one paper with the tags it has already used.
func NewSpace(paper string, taken []Tag) *Space {
	s := &Space{paper: paper, taken: make(map[Tag]bool, len(taken))}
	for _, t := range taken {
		s.taken[t] = true
	}
	return s
}

// Next is the next free tag.
//
// The loop is for collisions with tags already handed out. A paper of two
// hundred objects drawing from 1.6 million names collides about one time in
// eighty, so this runs a second time occasionally and a third time never.
func (s *Space) Next() Tag {
	for {
		t := derive(s.paper, s.n)
		s.n++
		if !s.taken[t] {
			s.taken[t] = true
			return t
		}
	}
}

// Taken reports whether a tag is already spoken for in this paper.
func (s *Space) Taken(t Tag) bool { return s.taken[t] }

// derive is the nth name in one paper's shuffled order.
func derive(paper string, n int) Tag {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s#%d", paper, n)))
	return encode(binary.BigEndian.Uint64(sum[:8]) % Names)
}

// encode writes a number in the Stacks alphabet, most significant first.
func encode(v uint64) Tag {
	b := make([]byte, Width)
	for i := Width - 1; i >= 0; i-- {
		b[i] = Alphabet[v%36]
		v /= 36
	}
	return Tag(b)
}

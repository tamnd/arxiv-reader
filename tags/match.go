package tags

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// Matching the objects of one version of a paper against the objects of the
// next is the hard problem of this whole package, and 03-tags.md section 6 is
// where it is argued out. An author posts v3 with a new section in the middle
// and two theorems renumbered, the corpus extracts it again, and every tag has
// to land on the same object it landed on before. A tag that lands on the wrong
// object is worse than a tag that lands on nothing, because a reference that
// breaks is visible and a reference quietly pointing at a different theorem is
// not.
//
// So the matching is four passes, strongest evidence first, and an object stops
// at the first pass that matches it. Nothing is matched on a guess that a later
// pass could have improved on, and everything that reaches the end unmatched is
// reported rather than resolved.

// Pass is which of the four passes matched a pair of objects.
type Pass int

const (
	// ByLabel is the author's own \label, which is pass one. Renumbering is
	// automatic and relabelling is manual, so a label is the most stable thing
	// an object has. It is on the source path only, because neither arXiv's own
	// HTML nor LaTeXML's carries it and this project reads it out of LaTeXML's
	// intermediate XML.
	ByLabel Pass = iota + 1
	// ByNumber is the same kind with the same number in the same section, which
	// is what the local identifier spells, and which is pass two. This is the
	// ordinary case where nothing moved.
	ByNumber
	// ByContent is the normalised content hash, which is pass three. An object
	// whose prose hashes the same is the same object even if it is now Lemma 4
	// in section 5 rather than Lemma 2 in section 3, which is what catches a
	// section being moved wholesale.
	ByContent
	// BySequence is the edit distance alignment of the two lists in reading
	// order, which is pass four. This catches a theorem whose wording was
	// lightly edited and which did not move, and it is the only pass that
	// matches on similarity rather than on equality.
	BySequence
)

// String is the name the report prints.
func (p Pass) String() string {
	switch p {
	case ByLabel:
		return "label"
	case ByNumber:
		return "number"
	case ByContent:
		return "content"
	case BySequence:
		return "sequence"
	}
	return "none"
}

// Pair is one decision: an object in the old version, the object in the new
// version it turned out to be, and the pass that put the two together.
type Pair struct {
	Old  Object
	New  Object
	Pass Pass
}

// Diff is the whole of one comparison, in a shape a person can argue with.
type Diff struct {
	// Pairs is every match, in the new version's reading order.
	Pairs []Pair
	// Added is the new version's objects that no pass could match, which are
	// the ones that get a fresh tag.
	Added []Object
	// Gone is the old version's objects that no pass could match, which are the
	// ones that get a tombstone.
	Gone []Object
	// Total is how many objects the new version has.
	Total int
}

// Match runs the four passes over two versions of one paper's objects.
//
// Both lists are in reading order, which pass four depends on and which the
// other three do not care about. The old list is the version the register was
// last assigned against and the new one is the version now in the corpus.
func Match(old, next []Object) Diff {
	d := Diff{Total: len(next)}
	usedOld := make([]bool, len(old))
	usedNew := make([]bool, len(next))
	pass := make([]Pass, len(next))
	from := make([]int, len(next))

	// Three passes on equality, which differ only in what they compare. Each one
	// pairs a key that exactly one unmatched object on each side carries: two
	// objects with the same key on one side are evidence of nothing, so they are
	// left for the next pass rather than paired arbitrarily.
	by := func(p Pass, key func(Object) string, veto func(i, j int) bool) {
		left := index(old, usedOld, key)
		right := index(next, usedNew, key)
		for k, is := range right {
			js := left[k]
			if len(is) != 1 || len(js) != 1 {
				continue
			}
			i, j := is[0], js[0]
			if veto != nil && veto(i, j) {
				continue
			}
			usedNew[i], usedOld[j] = true, true
			pass[i], from[i] = p, j
		}
	}
	by(ByLabel, func(o Object) string { return o.Label }, nil)
	// The class is part of the content key, because two objects of different
	// kinds that read the same are a table and its caption, not one object that
	// moved.
	content := func(o Object) string {
		h := Hash(o.Text)
		if h == "" {
			return ""
		}
		return o.Class + " " + h
	}
	// Pass two is the one place where a later pass can hold better evidence than
	// the pass in front of it, so it is the one place that looks ahead. A paper
	// that gains a theorem in the middle has a new Theorem 1 sitting on the old
	// Theorem 1's number, and taking that pair would hand a permanent name to a
	// theorem the author has just written. A number match is therefore declined
	// when the new object's prose is somewhere else in the old version, which is
	// a thing pass three can see and pass two cannot. It declines a deletion the
	// same way and for the same reason.
	wasAt := index(old, usedOld, content)
	isAt := index(next, usedNew, content)
	by(ByNumber, func(o Object) string { return o.Local }, func(i, j int) bool {
		if js := wasAt[content(next[i])]; len(js) == 1 && js[0] != j {
			return true
		}
		is := isAt[content(old[j])]
		return len(is) == 1 && is[0] != i
	})
	by(ByContent, content, nil)
	for _, l := range align(old, next, usedOld, usedNew) {
		usedOld[l.old], usedNew[l.next] = true, true
		pass[l.next], from[l.next] = BySequence, l.old
	}

	for i, o := range next {
		if !usedNew[i] {
			d.Added = append(d.Added, o)
			continue
		}
		d.Pairs = append(d.Pairs, Pair{Old: old[from[i]], New: o, Pass: pass[i]})
	}
	for j, o := range old {
		if !usedOld[j] {
			d.Gone = append(d.Gone, o)
		}
	}
	return d
}

// index groups the unmatched objects by key, dropping the ones with no key.
func index(objects []Object, used []bool, key func(Object) string) map[string][]int {
	out := map[string][]int{}
	for i, o := range objects {
		if used[i] {
			continue
		}
		if k := key(o); k != "" {
			out[k] = append(out[k], i)
		}
	}
	return out
}

// Count is how many objects one pass matched.
func (d Diff) Count(p Pass) int {
	n := 0
	for _, pair := range d.Pairs {
		if pair.Pass == p {
			n++
		}
	}
	return n
}

// Fell is the share of the new version's objects that the first three passes did
// not account for.
//
// So the ones pass four had to judge on similarity, plus the ones it could not
// judge at all. Both belong in the same number because both are the paper
// having changed enough that its identities are no longer being read off, and
// that is the thing worth refusing to write on.
func (d Diff) Fell() float64 {
	if d.Total == 0 {
		return 0
	}
	return float64(d.Count(BySequence)+len(d.Added)) / float64(d.Total)
}

// TooMuch is the share of a paper's objects that may fall through to pass four
// before a person has to look at the result.
//
// A fifth, from 03-tags.md section 6. A paper that changed that much is a paper
// somebody should read the diff of, and the cost of being wrong here is a
// permanent name on the wrong object.
const TooMuch = 0.2

// Err is the reason a diff must not be written, or nil.
//
// Separate from Match because a caller printing the decisions wants them all,
// and a caller about to move a register wants to be stopped.
func (d Diff) Err() error {
	if d.Fell() <= TooMuch {
		return nil
	}
	return fmt.Errorf("tags: %d per cent of the objects fell through to pass four or matched nothing at all, which is more than the fifth 03-tags.md allows, so read the passes above and pass -force if they are right", int(math.Round(d.Fell()*100)))
}

// display and inline are the two shapes the extractor writes mathematics in.
var (
	display = regexp.MustCompile(`(?s)\$\$.*?\$\$`)
	inline  = regexp.MustCompile(`(?s)\$[^$]*\$`)
)

// normalise is an object's text with the case, the mathematics and the spacing
// taken out of it.
//
// The mathematics goes because it is the part of a statement most likely to come
// out differently from a better converter next year, and a hash that changes
// when the converter improves is a hash that matches nothing. The case and the
// spacing go because they are Markdown's business and not the author's.
func normalise(text string) string {
	s := strings.ToLower(text)
	s = display.ReplaceAllString(s, " ")
	s = inline.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

// Hash is the normalised content hash of pass three.
//
// Empty for an object with no text, which is an object pass three cannot speak
// about.
func Hash(text string) string {
	s := strings.Join(strings.Fields(normalise(text)), "")
	if s == "" {
		// An object that is nothing but mathematics has nothing left once the
		// mathematics is stripped, and an equation is exactly that. Keeping the
		// mathematics is the only way such an object has a hash at all, and an
		// equation that is retypeset differently simply falls to pass four.
		s = strings.Join(strings.Fields(strings.ToLower(text)), "")
	}
	if s == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

// link is one pair the alignment made.
type link struct{ old, next int }

// similar is how alike two objects have to read before pass four will pair them.
//
// Half their word pairs in common. Below that the alignment is matching things
// because they are in the same place rather than because they are the same
// object, and being in the same place is what pass two already said.
const similar = 0.5

// most is how many unmatched objects on a side pass four will work on.
//
// The alignment is quadratic in both lists and a paper with thousands of
// unmatched objects on each side has been rewritten rather than revised. Such a
// paper comes back with everything added and everything gone, which is what the
// fifth in Err is there to catch, so the answer is the same and it arrives
// immediately.
const most = 500

// align pairs what is left in reading order by edit distance.
//
// A standard alignment: each list can be skipped over at a cost, two objects can
// be paired at a cost of how unalike they are, and the cheapest way through the
// two lists is the answer. Because the alignment cannot cross itself, an object
// can only be paired with something that comes after everything the objects
// before it were paired with, which is the property that makes the result worth
// trusting at all.
func align(old, next []Object, usedOld, usedNew []bool) []link {
	var a, b []int
	for i := range old {
		if !usedOld[i] {
			a = append(a, i)
		}
	}
	for i := range next {
		if !usedNew[i] {
			b = append(b, i)
		}
	}
	if len(a) == 0 || len(b) == 0 || len(a) > most || len(b) > most {
		return nil
	}
	left := make([]map[string]bool, len(a))
	for i, j := range a {
		left[i] = shingles(old[j].Text)
	}
	right := make([]map[string]bool, len(b))
	for i, j := range b {
		right[i] = shingles(next[j].Text)
	}

	// A skip costs one and a pair costs how unalike the two are, which is at
	// most a half by the time it is allowed at all, so a pair the threshold
	// permits is always cheaper than skipping both.
	const gap = 1.0
	const never = math.MaxFloat32
	pairCost := func(i, j int) float64 {
		if old[a[i]].Class != next[b[j]].Class {
			return never
		}
		s := jaccard(left[i], right[j])
		if s < similar {
			return never
		}
		return 1 - s
	}
	cost := make([][]float64, len(a)+1)
	for i := range cost {
		cost[i] = make([]float64, len(b)+1)
		cost[i][0] = float64(i) * gap
	}
	for j := 1; j <= len(b); j++ {
		cost[0][j] = float64(j) * gap
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			c := cost[i-1][j] + gap
			if x := cost[i][j-1] + gap; x < c {
				c = x
			}
			if p := pairCost(i-1, j-1); p < never {
				if x := cost[i-1][j-1] + p; x < c {
					c = x
				}
			}
			cost[i][j] = c
		}
	}
	var out []link
	for i, j := len(a), len(b); i > 0 && j > 0; {
		if p := pairCost(i-1, j-1); p < never && cost[i][j] == cost[i-1][j-1]+p {
			out = append(out, link{old: a[i-1], next: b[j-1]})
			i, j = i-1, j-1
			continue
		}
		if cost[i][j] == cost[i-1][j]+gap {
			i--
			continue
		}
		j--
	}
	// Backwards, because the walk starts at the end of both lists, and the
	// caller is owed reading order.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// shingles is an object's text as the set of word pairs in it.
//
// Pairs rather than words, because word counts call any two paragraphs of
// mathematical English similar, and pairs do not. A one word object keeps the
// word, which is the only thing it has.
func shingles(text string) map[string]bool {
	w := strings.Fields(normalise(text))
	out := map[string]bool{}
	if len(w) == 1 {
		out[w[0]] = true
	}
	for i := 0; i+1 < len(w); i++ {
		out[w[i]+" "+w[i+1]] = true
	}
	return out
}

// jaccard is the share of two sets that both of them hold.
func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	both := 0
	for k := range a {
		if b[k] {
			both++
		}
	}
	return float64(both) / float64(len(a)+len(b)-both)
}

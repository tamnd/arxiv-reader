// Package selection is the record of which papers enter the content plane and
// why.
//
// The metadata plane covers all of arXiv. The content plane is a selection out of
// it, it always will be, and the selection is the most consequential judgement in
// the project: it decides what gets read, translated and published, and what stays
// a record. So it is data with a reason on every row rather than a taste, and every
// paper in the content plane can say in one word why it is there.
//
// There are six reasons and no seventh. A paper nobody can put in one of the six is
// a paper somebody wants for a reason they have not written down, and the whole
// point of this package is that there is no row for that.
package selection

import "fmt"

// Reason is why one paper is in the content plane.
type Reason string

const (
	// Seed is the hand written list the corpus started from.
	//
	// It is licence first and subject second, which inverts how a reading list is
	// normally built, and the inversion is forced rather than chosen: there is no
	// point putting a paper on a seed list that the project may not publish.
	Seed Reason = "seed"
	// CitedByCorpus is a paper the corpus already cites, from at least three of
	// its own papers.
	CitedByCorpus Reason = "cited-by-corpus"
	// CitesCorpus is a paper citing at least three the corpus already holds.
	CitesCorpus Reason = "cites-corpus"
	// CategoryCanon is a paper near the top of its primary category for its year,
	// by citation count.
	CategoryCanon Reason = "category-canon"
	// Requested is a paper somebody asked for, by issue, with their name on it.
	Requested Reason = "requested"
	// Collection is a member of a named reading list.
	Collection Reason = "collection"
)

// Reasons are the six, in the order they were added.
//
// The order is kept because it is the order they will be argued about in, and
// because a report that lists them in a different order every run is a report
// nobody can diff.
var Reasons = []Reason{Seed, CitedByCorpus, CitesCorpus, CategoryCanon, Requested, Collection}

// Floor is how many papers inside the corpus have to cite a paper, or be cited by
// it, before the closure reasons apply.
//
// Three is the figure tamnd/papers used when it had a hundred papers to draw from.
// Here there are three million candidates, so three is a floor and not a working
// threshold: what actually gets run is a higher number, and this is the number
// below which the reason is not true at all.
const Floor = 3

// Says is the sentence behind the word.
//
// Every reason has one, because a reason nobody can read is a reason nobody can
// argue with, and the argument is the point of recording it.
func (r Reason) Says() string {
	switch r {
	case Seed:
		return "on the hand written seed list, which is licence first and subject second"
	case CitedByCorpus:
		return fmt.Sprintf("cited by at least %d papers already in the content plane", Floor)
	case CitesCorpus:
		return fmt.Sprintf("cites at least %d papers already in the content plane", Floor)
	case CategoryCanon:
		return "near the top by citation count for its primary category and year"
	case Requested:
		return "somebody asked for it, by issue, with their name on it"
	case Collection:
		return "a member of a named reading list in collections.yaml"
	}
	return ""
}

// ParseReason reads a reason off the command line.
func ParseReason(s string) (Reason, error) {
	for _, r := range Reasons {
		if string(r) == s {
			return r, nil
		}
	}
	return "", fmt.Errorf("selection: %q is not one of the six reasons, which are %s", s, ReasonNames())
}

// ReasonNames is the six, for a message.
func ReasonNames() string {
	out := ""
	for i, r := range Reasons {
		switch {
		case i == 0:
			out = string(r)
		case i == len(Reasons)-1:
			out += " and " + string(r)
		default:
			out += ", " + string(r)
		}
	}
	return out
}

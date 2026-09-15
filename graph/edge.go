package graph

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Edge is one claim about how the corpus connects.
//
// The field names are 2166-08 section 7's field names, which are short because
// this is the file the corpus has the most lines of by a wide margin: three
// million papers with five authors each is fifteen million authored-by edges
// before a single paper has been read. A three letter key on a hundred million
// lines is worth the terseness, and nothing but a program reads them.
type Edge struct {
	S string `json:"s"`
	P string `json:"p"`
	O string `json:"o"`
	// Conf is certain, high or medium, and never low. An edge that would be low
	// is not stored at all.
	Conf string `json:"conf"`
	// Via is how the claim was found, so metadata, arxiv, doi, title, ref, proof
	// or locator. It is what tells a step that is earning its place from one
	// that is firing on the wrong prose, and it is the first thing anybody
	// looking at a wrong edge wants.
	Via string `json:"via"`
	// Locator is the result the citation named, so "theorem 2", on the edges
	// that carry one. On a uses edge it is what was resolved. On a cites edge it
	// is what could not be, because the cited paper is not in the content plane,
	// and those are the strongest selection signal the corpus has: a paper whose
	// Theorem 2 is named by nine papers already here is a paper worth extracting.
	Locator string `json:"locator,omitempty"`
	// Stage is the part of the pipeline that asserted it, so harvest, refs or
	// objects.
	Stage string `json:"stage"`
	// At is the day the claim was first written, as YYYY-MM-DD.
	//
	// First written and not last rebuilt, which is why a merge carries it over.
	// A date that moved every time the graph was rebuilt would make every
	// rebuild a diff on every line of a file with a hundred million lines in it.
	At string `json:"at"`
}

// The confidences. Three of them, and they mean specific things.
const (
	// Certain is a fact arXiv published: an author, a category, a version, or a
	// citation resolved from an arXiv id printed in the reference itself.
	// Nothing inferred is ever certain, which is audit rule E05.
	Certain = "certain"
	// High is a structural match: a link the renderer resolved, a citation
	// matched on a DOI, an alias table hit that was exact.
	High = "high"
	// Medium is a match that could be wrong: a citation matched on title and
	// author similarity, a uses edge read off a locator, anything a model
	// proposed.
	Medium = "medium"
)

// Confidences is the three, strongest first.
var Confidences = []string{Certain, High, Medium}

// The predicates. Seven names over the nine rows of 2166-08 section 3, because
// cites and mentions each run between two pairs of kinds.
const (
	Cites      = "cites"
	Uses       = "uses"
	RefersTo   = "refers-to"
	Mentions   = "mentions"
	AuthoredBy = "authored-by"
	InCategory = "in-category"
	VersionOf  = "version-of"
)

// Predicate is one row of the table: what it means and what may be at each end.
type Predicate struct {
	Name string
	// From and To are the node kinds each end may be. A predicate whose ends are
	// not checked is a predicate that will eventually point a paper at a
	// category and nobody will notice for a year.
	From []string
	To   []string
	Help string
}

// Predicates is the whole table, in the order 2166-08 section 3 gives it.
var Predicates = []Predicate{
	{Cites, []string{KindPaper, KindObject}, []string{KindPaper},
		"the bibliography resolved to this paper, or this object cites it"},
	{Uses, []string{KindObject}, []string{KindObject},
		"this object depends on that one, which is the edge worth the most and the one that can be wrong"},
	{RefersTo, []string{KindObject}, []string{KindObject},
		"this object links to that one inside the same paper"},
	{Mentions, []string{KindObject}, []string{KindConcept, KindArtefact},
		"the concept layer or the artefact layer put this object next to that name"},
	{AuthoredBy, []string{KindPaper}, []string{KindName},
		"the metadata lists this author string on this paper"},
	{InCategory, []string{KindPaper}, []string{KindCategory},
		"the metadata puts this paper in this category, primary and cross listed alike"},
	{VersionOf, []string{KindVersion}, []string{KindPaper},
		"this version belongs to this paper"},
}

// byName is the table indexed, built once.
var byName = func() map[string]Predicate {
	m := make(map[string]Predicate, len(Predicates))
	for _, p := range Predicates {
		m[p.Name] = p
	}
	return m
}()

// Lookup finds a predicate by name.
func Lookup(name string) (Predicate, bool) {
	p, ok := byName[name]
	return p, ok
}

// Names is every predicate name, in table order, for a flag's enum and an
// error's list.
func Names() []string {
	out := make([]string, 0, len(Predicates))
	for _, p := range Predicates {
		out = append(out, p.Name)
	}
	return out
}

// day is the shape of the At field.
var day = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

// Validate checks one claim against the table.
//
// This runs on every edge before it is written rather than in a test only,
// because the failure it catches is silent: an edge pointing at the wrong kind
// of node joins to nothing, and missing joins look like missing data rather than
// like a fault.
func (e Edge) Validate() error {
	p, ok := Lookup(e.P)
	if !ok {
		return fmt.Errorf("%q is not a predicate, and the seven are %s", e.P, strings.Join(Names(), ", "))
	}
	from, ok := Kind(e.S)
	if !ok {
		return fmt.Errorf("%s: %q is not an ax:// uri", e.P, e.S)
	}
	to, ok := Kind(e.O)
	if !ok {
		return fmt.Errorf("%s: %q is not an ax:// uri", e.P, e.O)
	}
	if !allows(p.From, from) {
		return fmt.Errorf("%s runs from %s, not from %s", e.P, strings.Join(p.From, " or "), from)
	}
	if !allows(p.To, to) {
		return fmt.Errorf("%s runs to %s, not to %s", e.P, strings.Join(p.To, " or "), to)
	}
	if !allows(Confidences, e.Conf) {
		return fmt.Errorf("%s from %s is %q, and the three are %s", e.P, e.S, e.Conf, strings.Join(Confidences, ", "))
	}
	if e.Via == "" {
		return fmt.Errorf("%s from %s says nothing about how it was found", e.P, e.S)
	}
	if e.Stage == "" {
		return fmt.Errorf("%s from %s names no stage, and a claim nobody made is not a claim", e.P, e.S)
	}
	if !day.MatchString(e.At) {
		return fmt.Errorf("%s from %s is dated %q, which is not a day", e.P, e.S, e.At)
	}
	return nil
}

// allows reports whether a value is in a list.
func allows(values []string, v string) bool {
	for _, k := range values {
		if k == v {
			return true
		}
	}
	return false
}

// Key is the identity of a claim.
//
// The date is not in it, because a claim written twice is one claim and the
// second writing does not make it newer. The confidence is not in it either: a
// citation that resolved by title last month and by arXiv id this month is the
// same claim found a better way, and the way it was found is in the key so that
// the better answer replaces the worse one rather than sitting beside it.
func (e Edge) Key() string {
	return strings.Join([]string{e.S, e.P, e.O, e.Via, e.Locator}, "\x00")
}

// Sort puts the edges of a file in a stable order.
//
// By subject first, so one paper's claims sit together and a diff on the file
// reads as a diff on a paper. The date is not in the ordering, because two runs
// of the same build a day apart would otherwise write the same edges in a
// different order.
func Sort(edges []Edge) {
	sort.SliceStable(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
		switch {
		case a.S != b.S:
			return a.S < b.S
		case a.P != b.P:
			return order(a.P) < order(b.P)
		case a.O != b.O:
			return a.O < b.O
		case a.Via != b.Via:
			return a.Via < b.Via
		}
		return a.Locator < b.Locator
	})
}

// order is a predicate's place in the table, so that a sort by predicate is in
// the order 2166-08 lists them rather than alphabetical.
func order(name string) int {
	for i, p := range Predicates {
		if p.Name == name {
			return i
		}
	}
	return len(Predicates)
}

// Strongest is the better of two confidences.
//
// Two entries of one bibliography can resolve to the same paper, one by an
// arXiv id and one by a title, and the edge between the two papers is as good as
// the best reason for it rather than as bad as the worst.
func Strongest(a, b string) string {
	if rank(a) <= rank(b) {
		return a
	}
	return b
}

func rank(conf string) int {
	for i, c := range Confidences {
		if c == conf {
			return i
		}
	}
	return len(Confidences)
}

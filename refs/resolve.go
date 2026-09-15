package refs

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/metadata"
)

// The four steps of the ladder, in the order they are tried, and the value the
// via field carries when one of them hits.
const (
	ViaArXiv = "arxiv"
	ViaDOI   = "doi"
	ViaTitle = "title"
)

// Similar is the title similarity a match has to clear.
//
// 0.92, which is the threshold papers used, and it is high on purpose. Two
// different papers by the same group in the same year have titles that agree on
// far more than half of their characters, so a loose threshold here does not
// produce a few wrong edges, it produces a systematically wrong graph in
// exactly the neighbourhoods somebody would want to read.
const Similar = 0.92

// Near is the lower bound on a similarity worth reporting as a near miss.
//
// A title that scored 0.90 and was refused is a fact about the threshold. A
// title that scored 0.30 is two unrelated papers and saying so every time would
// bury the useful line.
const Near = 0.85

// Match is what the metadata plane offered for one entry.
type Match struct {
	// Paper is the arXiv id the entry resolved to, in canonical form.
	Paper string
	// Via is which step of the ladder found it.
	Via string
	// Score is the title similarity, and 1 for the two steps that match on an
	// identifier rather than on prose.
	Score float64
}

// Miss is a match that was found and then refused, kept for the report.
//
// The reason a threshold rejected something is the only way to tell a threshold
// that is doing its job from one that is set wrong, and a resolver that reports
// nothing but its successes cannot be tuned by anybody.
type Miss struct {
	Key   string
	Paper string
	Why   string
	Score float64
}

// Resolver matches bibliography entries against the metadata plane.
//
// It is built the other way round from an index. The plane holds three million
// records and a run holds a few hundred entries, so the entries are the thing
// that goes in a map and the plane is streamed past them once. That keeps the
// memory proportional to the bibliography rather than to arXiv, and it means a
// run that resolves fifty papers costs the same single pass as a run that
// resolves one.
type Resolver struct {
	byID    map[string][]*want
	byDOI   map[string][]*want
	byTitle map[string][]*want
	byYear  map[int][]*want
	found   map[string]Match
	// split is the keys that two different papers matched equally well. An
	// ambiguous match is dropped rather than decided, because a coin toss
	// between two papers is an edge that is wrong half the time and says
	// nothing about which half.
	split map[string]bool
	miss  map[string]Miss
}

// want is one entry, reduced to what the ladder compares against.
type want struct {
	key      string
	title    string
	year     int
	surnames map[string]bool
}

func NewResolver() *Resolver {
	return &Resolver{
		byID:    map[string][]*want{},
		byDOI:   map[string][]*want{},
		byTitle: map[string][]*want{},
		byYear:  map[int][]*want{},
		found:   map[string]Match{},
		split:   map[string]bool{},
		miss:    map[string]Miss{},
	}
}

// Want adds one entry to resolve, under a key the caller chooses.
//
// The key is the caller's, because an entry's anchor is only unique inside one
// paper and a run resolves many. What the command uses is the paper and the
// anchor together.
func (r *Resolver) Want(key string, e Entry) {
	if e.ArXiv != "" {
		if id, err := axid.Parse(e.ArXiv); err == nil {
			r.byID[id.Canonical] = append(r.byID[id.Canonical], &want{key: key})
		}
	}
	if e.DOI != "" {
		d := strings.ToLower(e.DOI)
		r.byDOI[d] = append(r.byDOI[d], &want{key: key})
	}
	// A title match needs a title, a year and a name. The year and the name are
	// the two guards that stop it, and an entry missing either of them cannot
	// be matched on prose at all, so it is not put in front of the plane.
	title := Normalise(e.Title)
	if title == "" || e.Year == 0 || len(e.Authors) == 0 {
		return
	}
	w := &want{key: key, title: title, year: e.Year, surnames: surnames(e.Authors)}
	r.byTitle[title] = append(r.byTitle[title], w)
	r.byYear[e.Year] = append(r.byYear[e.Year], w)
}

// Offer shows the resolver one record from the metadata plane.
func (r *Resolver) Offer(rec metadata.Record) {
	for _, w := range r.byID[rec.ID] {
		r.take(w.key, Match{Paper: rec.ID, Via: ViaArXiv, Score: 1})
	}
	if rec.DOI != "" {
		for _, w := range r.byDOI[strings.ToLower(rec.DOI)] {
			r.take(w.key, Match{Paper: rec.ID, Via: ViaDOI, Score: 1})
		}
	}
	year := recordYear(rec)
	if year == 0 || len(r.byTitle) == 0 {
		return
	}
	title := Normalise(rec.Title)
	if title == "" {
		return
	}
	// The exact hit first, which is what most title matches are once both sides
	// have been through the same normalisation, and then the expensive pass
	// over the entries whose year is within one of this record's.
	seen := map[*want]bool{}
	for _, w := range r.byTitle[title] {
		seen[w] = true
		r.title(w, rec, 1)
	}
	for y := year - 1; y <= year+1; y++ {
		for _, w := range r.byYear[y] {
			if seen[w] || !comparable(w.title, title) {
				continue
			}
			seen[w] = true
			if s := similarity(w.title, title); s >= Near {
				r.title(w, rec, s)
			}
		}
	}
}

// title applies the two guards a prose match has to clear and records what
// happened either way.
func (r *Resolver) title(w *want, rec metadata.Record, score float64) {
	switch {
	case score < Similar:
		r.near(Miss{Key: w.key, Paper: rec.ID, Why: fmt.Sprintf("the title similarity is under %.2f", Similar), Score: score})
	case abs(recordYear(rec)-w.year) > 1:
		r.near(Miss{Key: w.key, Paper: rec.ID, Why: fmt.Sprintf("the entry says %d and the record says %d", w.year, recordYear(rec)), Score: score})
	case !shares(w.surnames, rec.Authors):
		r.near(Miss{Key: w.key, Paper: rec.ID, Why: "no author of the entry is an author of the record", Score: score})
	default:
		r.take(w.key, Match{Paper: rec.ID, Via: ViaTitle, Score: score})
	}
}

// take keeps the better of two matches for the same entry.
func (r *Resolver) take(key string, m Match) {
	old, ok := r.found[key]
	if !ok {
		r.found[key] = m
		return
	}
	if old.Paper == m.Paper {
		// The same paper found twice. Whichever step is higher up the ladder
		// is the one the manifest should say it came from.
		if rank(m.Via) < rank(old.Via) {
			r.found[key] = m
		}
		return
	}
	switch {
	case rank(m.Via) < rank(old.Via):
		r.found[key] = m
		delete(r.split, key)
	case rank(m.Via) > rank(old.Via):
	case m.Score > old.Score:
		r.found[key] = m
		delete(r.split, key)
	case m.Score == old.Score:
		r.split[key] = true
	}
}

// near keeps the best near miss per entry, which bounds what a run remembers by
// the size of the bibliography rather than by the size of the plane.
func (r *Resolver) near(m Miss) {
	if old, ok := r.miss[m.Key]; ok && old.Score >= m.Score {
		return
	}
	r.miss[m.Key] = m
}

// Matches is what resolved, keyed the way the caller asked for it.
func (r *Resolver) Matches() map[string]Match {
	out := make(map[string]Match, len(r.found))
	for k, m := range r.found {
		if !r.split[k] {
			out[k] = m
		}
	}
	return out
}

// Misses is every near miss and every ambiguity, worst first, for the report.
func (r *Resolver) Misses() []Miss {
	var out []Miss
	for k := range r.split {
		m := r.found[k]
		out = append(out, Miss{Key: k, Paper: m.Paper, Why: "two papers match it equally well", Score: m.Score})
	}
	for k, m := range r.miss {
		if _, ok := r.found[k]; ok {
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// Confidence is how much an edge built on this match is worth, in the
// vocabulary 08-graph.md section 3 defines.
//
// The three steps get the three values and they are not the same. An arXiv id
// printed in the reference is certain, because the paper itself says which
// paper it means and arXiv published the id. A DOI is high rather than certain,
// because the DOI in a reference is matched against a DOI field the submitting
// author typed into arXiv, and a structural match against a field somebody typed
// is not a fact arXiv published. A title match is a reading of prose and is
// medium.
//
// It is derived rather than stored, because a stored confidence is a second
// field that can disagree with the first one.
func Confidence(via string) string {
	switch via {
	case ViaArXiv:
		return "certain"
	case ViaDOI:
		return "high"
	}
	return "medium"
}

func rank(via string) int {
	switch via {
	case ViaArXiv:
		return 0
	case ViaDOI:
		return 1
	}
	return 2
}

// recordYear is the year the paper was first announced.
//
// v1 and not the latest version, because the year a bibliography prints is the
// year the work came out and a paper revised five years later is still cited by
// its original year.
func recordYear(rec metadata.Record) int {
	if len(rec.Versions) == 0 {
		return 0
	}
	return rec.Versions[0].Created.Year()
}

// Normalise is the form two titles are compared in.
//
// Letters and digits, lowercased, one space between them. Everything a style
// file argues about goes: the capitalisation, the hyphens, the colon before a
// subtitle, and the dollar signs around a piece of mathematics, which one side
// prints as $O(n)$ and the other as O(n).
func Normalise(s string) string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for i, f := range fields {
		fields[i] = strings.ToLower(f)
	}
	return strings.Join(fields, " ")
}

// surnames is every word of an entry's author block worth matching a surname
// against.
//
// Every word and not the last word of each name, because a style prints Vaswani
// A. as readily as A. Vaswani and taking the last would find the initial in one
// of them. Matching a surname against a set that holds a forename costs a false
// positive on a paper whose author shares a surname with another author's
// forename, and missing the surname entirely costs every match in the list.
func surnames(authors []string) map[string]bool {
	out := map[string]bool{}
	for _, a := range authors {
		for _, w := range strings.Fields(Normalise(a)) {
			if len([]rune(w)) > 1 {
				out[w] = true
			}
		}
	}
	return out
}

// shares is the author guard: one name in common is enough.
func shares(entry map[string]bool, authors []metadata.Author) bool {
	for _, a := range authors {
		s := Normalise(a.Surname)
		if s == "" {
			continue
		}
		if entry[s] {
			return true
		}
		// A surname of several words, so van der Waals, is matched on its last
		// word too, because a bibliography prints the particles inconsistently
		// and the last word is the part that is always there.
		if f := strings.Fields(s); len(f) > 1 && entry[f[len(f)-1]] {
			return true
		}
	}
	return false
}

// comparable is the cheap guard in front of the expensive comparison.
//
// Two strings whose lengths differ by more than the threshold allows cannot
// clear it, because every character of the difference is an edit. This runs
// once per entry per record in the plane and the similarity does not, which is
// the whole reason it is here.
func comparable(a, b string) bool {
	la, lb := len(a), len(b)
	if la < lb {
		la, lb = lb, la
	}
	return float64(la-lb) <= (1-Near)*float64(la)
}

// similarity is one minus the edit distance over the longer string.
//
// Levenshtein over runes, two rows at a time. Words would be cheaper and are
// worse here: a title that differs by a hyphen becomes two different words and
// scores as a whole word wrong, which is most of a short title.
func similarity(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 || len(rb) == 0 {
		return 0
	}
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min(min(curr[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	longer := max(len(ra), len(rb))
	return 1 - float64(prev[len(rb)])/float64(longer)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

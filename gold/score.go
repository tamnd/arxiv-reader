package gold

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/objects"
)

// Targets are the targets of AX11.3 for set X.
var Targets = struct {
	Recall, RecallDecade, Precision, Number, Attach, Strength float64
}{0.995, 0.98, 0.99, 0.995, 0.98, 0.95}

// Ratio is a count of hits over a count of trials.
type Ratio struct{ Hit, Of int }

// Value is the ratio, and 1 when there were no trials.
func (r Ratio) Value() float64 {
	if r.Of == 0 {
		return 1
	}
	return float64(r.Hit) / float64(r.Of)
}

func (r *Ratio) add(o Ratio) { r.Hit += o.Hit; r.Of += o.Of }

func (r Ratio) String() string {
	if r.Of == 0 {
		return "none"
	}
	return fmt.Sprintf("%s (%s of %s)", prose.Percent(r.Value()), prose.Thousands(r.Hit), prose.Thousands(r.Of))
}

// PaperScore is the score of the extractor on one paper.
type PaperScore struct {
	Paper   string
	Version int
	Group   string
	Decade  string
	// Recall is the structural marks that an object accounts for, and
	// Precision is the objects that a structural mark accounts for.
	Recall, Precision Ratio
	// Prose is the recall of the prose marks.
	Prose Ratio
	// Number is the matched items whose number agrees, Attach the matched
	// proofs whose target agrees, and Status the matched statements whose
	// proof_status agrees.
	Number, Attach, Status Ratio
	// Missed and Extra are the ids of the marks and of the objects that
	// nothing accounts for.
	Missed, Extra []string
}

// ScorePaper scores the object records of a version against its gold record.
func ScorePaper(g Record, recs []objects.Record) PaperScore {
	s := PaperScore{Paper: g.Paper, Version: g.Version, Group: g.Group, Decade: DecadeOf(yearOf(g.Paper))}
	items := Items(recs, strconv.Itoa(g.Version))
	match, used := matchMarks(g.Marks, items)
	for _, m := range g.Marks {
		_, hit := match[m.ID]
		if m.Prose {
			s.Prose.Of++
		} else {
			s.Recall.Of++
		}
		if !hit {
			s.Missed = append(s.Missed, m.ID)
			continue
		}
		if m.Prose {
			s.Prose.Hit++
		} else {
			s.Recall.Hit++
		}
	}
	for i, o := range items {
		s.Precision.Of++
		if used[i] {
			s.Precision.Hit++
		} else {
			s.Extra = append(s.Extra, o.Local)
		}
	}
	for _, m := range g.Marks {
		i, ok := match[m.ID]
		if !ok {
			continue
		}
		o := items[i]
		if m.Number != "" || o.Number != "" {
			s.Number.Of++
			if m.Number == o.Number {
				s.Number.Hit++
			}
		}
		if m.Kind == "proof" && m.Proves != "" {
			s.Attach.Of++
			if t, ok := match[m.Proves]; ok && o.Proves == items[t].Local {
				s.Attach.Hit++
			}
		}
		if m.ProofStatus != "" {
			s.Status.Of++
			if m.ProofStatus == o.ProofStatus {
				s.Status.Hit++
			}
		}
	}
	return s
}

// matchMarks pairs each mark with at most one item of the same kind: one whose
// span overlaps the mark's, else one with the mark's label, else one with its
// number. The marks are taken in order and an item is paired once.
func matchMarks(marks []Mark, items []objects.Record) (map[string]int, []bool) {
	used := make([]bool, len(items))
	match := map[string]int{}
	// An item that starts on the line of a mark is its item first, so that a
	// mark nested in another (a "First proof" inside a proof) does not take
	// the item of a later mark that it overlaps.
	for _, m := range marks {
		for i, o := range items {
			if !used[i] && o.Kind == m.Kind && m.File != "" && o.Span != nil && o.Span.File == m.File && o.Span.From == m.From {
				used[i] = true
				match[m.ID] = i
				break
			}
		}
	}
	for _, m := range marks {
		if _, ok := match[m.ID]; ok {
			continue
		}
		best, bestScore := -1, 0
		for i, o := range items {
			if used[i] || o.Kind != m.Kind {
				continue
			}
			sc := 0
			switch {
			case m.File != "" && o.Span != nil && o.Span.File == m.File && o.Span.From <= m.To && m.From <= o.Span.To:
				sc = 3
			case m.Label != "" && (o.Label == m.Label || contains(o.Labels, m.Label)):
				sc = 2
			case m.Number != "" && o.Number == m.Number:
				sc = 1
			}
			if sc > bestScore {
				best, bestScore = i, sc
			}
		}
		if best >= 0 {
			used[best] = true
			match[m.ID] = best
		}
	}
	return match, used
}

// InItems gives the record with the marks named as the items they match, so
// that its uses can be set against edges, which name items. A mark that no
// item matches is named "gold-" and its id, which no item has. A person or a
// pass that marks a paper names the marks as it likes, and the names of the
// items are the extractor's.
func InItems(g Record, recs []objects.Record) Record {
	items := Items(recs, strconv.Itoa(g.Version))
	match, _ := matchMarks(g.Marks, items)
	name := func(id string) string {
		if i, ok := match[id]; ok {
			return items[i].Local
		}
		if strings.HasPrefix(id, "bib:") {
			return id
		}
		return "gold-" + id
	}
	out := g
	out.Marks = make([]Mark, len(g.Marks))
	for i, m := range g.Marks {
		m.ID = name(m.ID)
		if m.Proves != "" {
			m.Proves = name(m.Proves)
		}
		out.Marks[i] = m
	}
	out.Uses = make([]Use, len(g.Uses))
	for i, u := range g.Uses {
		u.Proof, u.To = name(u.Proof), name(u.To)
		out.Uses[i] = u
	}
	return out
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// yearOf is the year that an arXiv id names.
func yearOf(id string) int {
	s := id
	if i := strings.LastIndex(id, "/"); i >= 0 {
		s = id[i+1:]
	}
	if len(s) < 2 {
		return 0
	}
	yy, err := strconv.Atoi(s[:2])
	if err != nil {
		return 0
	}
	if yy >= 91 {
		return 1900 + yy
	}
	return 2000 + yy
}

// Score is the score of the extractor on a gold set.
type Score struct {
	Set, SetVersion, Extractor string
	// Papers is the papers of the set, Marked the papers with a marked
	// record, and Proposed the papers with a proposed record only.
	Papers, Marked, Proposed int
	// MarkedBy counts the marked records by who marked them.
	MarkedBy                 map[string]int
	Recall, Precision, Prose Ratio
	Number, Attach, Status   Ratio
	ByDecade                 map[string]Ratio
	// Strength is the papers with a census strength of strong or normal,
	// over the papers of the set with a census record.
	Strength Ratio
	Scores   []PaperScore
}

// Add adds the score of one paper.
func (s *Score) Add(p PaperScore) {
	s.Recall.add(p.Recall)
	s.Precision.add(p.Precision)
	s.Prose.add(p.Prose)
	s.Number.add(p.Number)
	s.Attach.add(p.Attach)
	s.Status.add(p.Status)
	if s.ByDecade == nil {
		s.ByDecade = map[string]Ratio{}
	}
	r := s.ByDecade[p.Decade]
	r.add(p.Recall)
	s.ByDecade[p.Decade] = r
	s.Scores = append(s.Scores, p)
}

// Met tells if each target of AX11.3 is met.
func (s Score) Met() bool {
	if s.Marked == 0 {
		return false
	}
	ok := s.Recall.Value() >= Targets.Recall && s.Precision.Value() >= Targets.Precision &&
		s.Number.Value() >= Targets.Number && s.Attach.Value() >= Targets.Attach &&
		s.Strength.Value() >= Targets.Strength
	for _, r := range s.ByDecade {
		ok = ok && r.Value() >= Targets.RecallDecade
	}
	return ok
}

// Markdown is the report of the score, as reports/gold-x.md.
func (s Score) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# The score of the extractor on gold set %s %s\n\n", s.Set, s.SetVersion)
	fmt.Fprintf(&b, "The set has %s. %s have a marked record%s, and %s have a proposed record only. Only the marked records are scored. The extractor is %s.\n\n",
		prose.Count(s.Papers, "paper"), prose.Thousands(s.Marked), markers(s.MarkedBy), prose.Thousands(s.Proposed), s.Extractor)
	if s.Marked == 0 {
		b.WriteString("No record is marked yet, so there is no score. A person must read each paper and decide each mark (AX11.1).\n\n")
	}
	b.WriteString("| Measure | Value | Target |\n| --- | ---: | ---: |\n")
	row := func(name string, r Ratio, target float64) {
		t := "measured"
		if target > 0 {
			t = prose.Percent(target)
		}
		fmt.Fprintf(&b, "| %s | %s | %s |\n", name, r, t)
	}
	row("structural recall", s.Recall, Targets.Recall)
	row("structural precision", s.Precision, Targets.Precision)
	row("prose recall", s.Prose, 0.90)
	row("number accuracy", s.Number, Targets.Number)
	row("attachment accuracy", s.Attach, Targets.Attach)
	row("proof_status accuracy", s.Status, 0)
	row("census strength strong or normal", s.Strength, Targets.Strength)
	if len(s.ByDecade) > 0 {
		b.WriteString("\n## Structural recall by decade\n\n| Decade | Recall | Target |\n| --- | ---: | ---: |\n")
		var ds []string
		for d := range s.ByDecade {
			ds = append(ds, d)
		}
		sort.Strings(ds)
		for _, d := range ds {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", d, s.ByDecade[d], prose.Percent(Targets.RecallDecade))
		}
	}
	var bad []PaperScore
	for _, p := range s.Scores {
		if len(p.Missed)+len(p.Extra) > 0 {
			bad = append(bad, p)
		}
	}
	if len(bad) > 0 {
		b.WriteString("\n## Papers with a missed or an extra item\n\nEach one is a pattern to find and fix (AX4.9).\n\n| Paper | Missed | Extra |\n| --- | --- | --- |\n")
		for _, p := range bad {
			fmt.Fprintf(&b, "| %sv%d | %s | %s |\n", p.Paper, p.Version, strings.Join(p.Missed, ", "), strings.Join(p.Extra, ", "))
		}
	}
	return b.String()
}

// markers says who marked the records, as " (12 by Ann, 3 by Bo)", in the
// order of the most records first. A reader of the score has to know whether
// a person or a program decided the marks it is measured against.
func markers(by map[string]int) string {
	if len(by) == 0 {
		return ""
	}
	names := make([]string, 0, len(by))
	for n := range by {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if by[names[i]] != by[names[j]] {
			return by[names[i]] > by[names[j]]
		}
		return names[i] < names[j]
	})
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = prose.Thousands(by[n]) + " by " + n
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

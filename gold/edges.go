package gold

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/arxiv-reader/edges"
	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/objects"
)

// Use is one mark of set E: a result, a definition or a hypothesis that a
// proof uses, and the step of the proof that uses it (AX11.2).
type Use struct {
	// Proof is the id of the mark of the proof.
	Proof string `json:"proof"`
	// To is the id of the mark that the proof uses, or bib:<key> for a work
	// that the paper cites.
	To string `json:"to"`
	// Part is the part of the target, as (b), or the locator of a citation.
	Part string `json:"part,omitempty"`
	// File and Line are the step of the proof that uses it.
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
}

// PerGroupE is how many papers set E takes from each group of set X. Set E
// version 1 is drawn from set X version 1, so its papers are already fetched
// and extracted: 60 papers from each of the five groups, 300 in all.
const PerGroupE = 60

// TargetsE are the precision targets of AX11.3 for each method. A method that
// is not here has its precision measured and no target.
var TargetsE = map[string]float64{
	edges.Ref: 0.98, edges.Text: 0.98,
	edges.Position: 0.85, edges.Name: 0.85, edges.Term: 0.85,
}

// DrawE draws set E from the papers of set X. In each group of X, the papers
// are taken in the order of the seed, and a paper with no proof object is not
// taken, because set E marks what the proofs use.
func DrawE(x []Pick, hasProof func(id string) bool, seed string, per int) []Pick {
	groups := map[string][]Pick{}
	for _, p := range x {
		groups[p.Group] = append(groups[p.Group], p)
	}
	var names []string
	for _, g := range PlanX.Groups {
		names = append(names, g.Name)
	}
	for g := range groups {
		if !contains(names, g) {
			names = append(names, g)
		}
	}
	var out []Pick
	for _, g := range names {
		in := groups[g]
		sort.Slice(in, func(i, j int) bool { return rank(seed, in[i].ID) < rank(seed, in[j].ID) })
		n := 0
		for _, p := range in {
			if n == per {
				break
			}
			if hasProof(p.ID) {
				out = append(out, p)
				n++
			}
		}
	}
	return out
}

// ProposeE makes a proposed record of set E from the object records and the
// edges of a version. The marks are the items, as in set X, and the uses are
// the uses and cites-result edges from the proofs. A person reads each proof,
// removes the wrong uses and adds the missing ones.
func ProposeE(setVersion, group, by string, paper string, version int, recs []objects.Record, es []edges.Edge) Record {
	r := Propose("E", setVersion, group, by, paper, version, recs)
	proofs := map[string]bool{}
	for _, m := range r.Marks {
		if m.Kind == "proof" {
			proofs[m.ID] = true
		}
	}
	seen := map[string]bool{}
	for _, e := range es {
		if e.Evidence.Version != version || (e.Predicate != edges.Uses && e.Predicate != edges.CitesResult) {
			continue
		}
		from, to := localOf(e.From), localOf(e.To)
		if !proofs[from] || seen[from+" "+to] {
			continue
		}
		seen[from+" "+to] = true
		r.Uses = append(r.Uses, Use{Proof: from, To: to, Part: e.Part, File: e.Evidence.File, Line: e.Evidence.Line})
	}
	return r
}

func localOf(id string) string {
	if _, l, ok := strings.Cut(id, "#"); ok {
		return l
	}
	return id
}

// EdgeScore is the score of the resolver on set E.
type EdgeScore struct {
	SetVersion, Extractor    string
	Papers, Marked, Proposed int
	// MarkedBy counts the marked records by who marked them.
	MarkedBy map[string]int
	// Precision is, for each method, the edges from a marked proof that a
	// use accounts for. Recall is the uses that an edge of the methods 1 to
	// 7 accounts for.
	Precision map[string]Ratio
	Recall    Ratio
	// Missed is the uses that no edge accounts for, by paper.
	Missed map[string][]string
}

// AddPaper scores the edges of one version against its marked record.
func (s *EdgeScore) AddPaper(g Record, es []edges.Edge) {
	if s.Precision == nil {
		s.Precision, s.Missed = map[string]Ratio{}, map[string][]string{}
	}
	proofs := map[string]bool{}
	for _, m := range g.Marks {
		if m.Kind == "proof" {
			proofs[m.ID] = true
		}
	}
	marked := map[string]bool{}
	for _, u := range g.Uses {
		marked[u.Proof+" "+u.To] = true
	}
	found := map[string]bool{}
	for _, e := range es {
		if e.Evidence.Version != g.Version || e.Method == edges.Attach || (e.Predicate != edges.Uses && e.Predicate != edges.CitesResult) {
			continue
		}
		from := localOf(e.From)
		if !proofs[from] {
			continue
		}
		k := from + " " + localOf(e.To)
		r := s.Precision[e.Method]
		r.Of++
		if marked[k] {
			r.Hit++
			found[k] = true
		}
		s.Precision[e.Method] = r
	}
	ref := fmt.Sprintf("%sv%d", g.Paper, g.Version)
	for _, u := range g.Uses {
		k := u.Proof + " " + u.To
		s.Recall.Of++
		if found[k] {
			s.Recall.Hit++
		} else {
			s.Missed[ref] = append(s.Missed[ref], k)
		}
	}
}

// Met tells if each precision target of AX11.3 is met.
func (s EdgeScore) Met() bool {
	if s.Marked == 0 {
		return false
	}
	for m, t := range TargetsE {
		if s.Precision[m].Value() < t {
			return false
		}
	}
	return true
}

// Markdown is the report of the score, as reports/gold-e.md.
func (s EdgeScore) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# The score of the edge resolver on gold set E %s\n\n", s.SetVersion)
	fmt.Fprintf(&b, "The set has %s. %s have a marked record%s, and %s have a proposed record only. Only the marked records are scored. The extractor is %s.\n\n",
		prose.Count(s.Papers, "paper"), prose.Thousands(s.Marked), markers(s.MarkedBy), prose.Thousands(s.Proposed), s.Extractor)
	if s.Marked == 0 {
		b.WriteString("No record is marked yet, so there is no score. A person must read each proof and decide what it uses (AX11.1).\n\n")
	}
	b.WriteString("| Method | Precision | Target |\n| --- | ---: | ---: |\n")
	for _, m := range edges.Methods {
		if m == edges.Attach {
			continue
		}
		t := "measured"
		if v, ok := TargetsE[m]; ok {
			t = prose.Percent(v)
		}
		fmt.Fprintf(&b, "| %s | %s | %s |\n", m, s.Precision[m], t)
	}
	fmt.Fprintf(&b, "\nThe recall of the methods 1 to 7 together is %s. The first version has no target for it.\n", s.Recall)
	if len(s.Missed) > 0 {
		b.WriteString("\n## The uses that no edge accounts for\n\n| Version | Proof and target |\n| --- | --- |\n")
		var refs []string
		for r := range s.Missed {
			refs = append(refs, r)
		}
		sort.Strings(refs)
		for _, r := range refs {
			fmt.Fprintf(&b, "| %s | %s |\n", r, strings.Join(s.Missed[r], ", "))
		}
	}
	return b.String()
}

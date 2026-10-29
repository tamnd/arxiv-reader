// Package edges resolves the edges between the items of a paper, and from the
// items to the works that the paper cites (AX06).
//
// The resolver reads the expanded text of the TeX front end and the items that
// the builder found. It runs on each item body, each proof body and each head.
// Text between the items is not read. The methods are ref, text, scope, cite,
// position and term of AX6.1. The methods name, outside and model need the
// inventory, the outside indexes and a model, and they are not here.
//
// An edge holds structure only: the two ends, the predicate, the method, the
// confidence and where the cue is. It holds no text of the paper, so the same
// record is correct for each licence class (AX6.4).
package edges

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/tamnd/arxiv-reader/objects"
	"github.com/tamnd/arxiv-reader/tex"
	"github.com/tamnd/arxiv-reader/theorems"
)

// The predicates of AX6.3.
const (
	Uses        = "uses"
	Proves      = "proves"
	RefersTo    = "refers-to"
	Restates    = "restates"
	CitesResult = "cites-result"
	Cites       = "cites"
	Generalizes = "generalizes"
	Specializes = "specializes"
)

// The methods of AX6.1. Attach is for the edges that the builder already
// decided: a proof and what it proves (AX3.6), and a restatement and what it
// restates (AX3.7).
const (
	Ref      = "ref"
	Text     = "text"
	Scope    = "scope"
	Cite     = "cite"
	Position = "position"
	Name     = "name"
	Term     = "term"
	Outside  = "outside"
	Model    = "model"
	Attach   = "attach"
)

// Methods is each method, in the order of AX6.1.
var Methods = []string{Ref, Text, Scope, Cite, Position, Name, Term, Outside, Model, Attach}

// The confidences.
const (
	Certain = "certain"
	High    = "high"
	Medium  = "medium"
	Low     = "low"
)

// Confidences is each confidence, from the most certain.
var Confidences = []string{Certain, High, Medium, Low}

// The flags of an edge.
const (
	// FlagTitleMismatch is a text reference whose title does not agree with
	// the kind of the item that has the number.
	FlagTitleMismatch = "title-mismatch"
	// FlagCycle is a uses edge in a cycle of the in-paper uses graph.
	FlagCycle = "cycle"
)

// Edge is one edge record of AX6.4.
type Edge struct {
	// From and To are object ids, as <paper>#<local>. A citation goes to
	// <paper>#bib:<key> until AX5.7 resolves the key to a record.
	From string `json:"from"`
	To   string `json:"to"`
	// Part is the part of the target, as (b), the label of an equation in
	// the target, or the locator of a citation.
	Part       string   `json:"part,omitempty"`
	Predicate  string   `json:"predicate"`
	Method     string   `json:"method"`
	Confidence string   `json:"confidence"`
	Flags      []string `json:"flags,omitempty"`
	Evidence   Evidence `json:"evidence"`
	Extractor  string   `json:"extractor"`
}

// Evidence is where the cue of an edge is. It holds no text of the paper.
type Evidence struct {
	Version int    `json:"version"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	Cue     string `json:"cue"`
}

// Item is what the resolver needs to know of one item.
type Item struct {
	Local  string
	Kind   string
	Title  string
	Number string
	Name   string
	Labels []string
	Parts  []objects.Part
	Proof  bool
	// Parent is the local of the enclosing item, and InProof is true when
	// the item is inside a proof.
	Parent  string
	InProof bool
	// Proves, ProvesPart and Restates are from the builder.
	Proves     string
	ProvesPart string
	Restates   string
	// Off, Inner, Close and End are the extent of the item in the expanded
	// text.
	Off, Inner, Close, End int
}

// Input is one main file of a version.
type Input struct {
	Paper     string
	Version   int
	Extractor string
	// Prefix is the prefix of the locals of the second and later main files.
	Prefix string
	Doc    *tex.Document
	Items  []Item
	Titles *theorems.Titles
}

// Result is the edges of one main file.
type Result struct {
	Edges []Edge
	// Dangling is the count of references that resolve to nothing.
	Dangling int
	// Cycles is each cycle of the in-paper uses graph, as the locals of the
	// statements in it.
	Cycles [][]string
}

// Resolve resolves the edges of one main file.
func Resolve(in Input) *Result {
	r := &resolver{in: in, res: &Result{}, best: map[key]int{}}
	if in.Doc == nil || in.Doc.Body == 0 || len(in.Items) == 0 {
		return r.res
	}
	r.index()
	r.attached()
	r.refs()
	r.texts()
	r.cites()
	r.positions()
	r.scopes()
	r.terms()
	r.cycles()
	sort.SliceStable(r.res.Edges, func(i, j int) bool {
		a, b := r.res.Edges[i], r.res.Edges[j]
		if r.order[a.From] != r.order[b.From] {
			return r.order[a.From] < r.order[b.From]
		}
		return a.Evidence.Line < b.Evidence.Line
	})
	return r.res
}

type key struct{ from, to, part, predicate string }

type target struct {
	local   string
	part    string
	section bool
	// forward is a reference by position to an item after it.
	forward bool
}

type resolver struct {
	in     Input
	res    *Result
	best   map[key]int
	byID   map[string]*Item
	order  map[string]int
	labels map[string]target
}

func (r *resolver) id(local string) string { return r.in.Paper + "#" + local }

func (r *resolver) index() {
	r.byID = map[string]*Item{}
	r.order = map[string]int{}
	r.labels = map[string]target{}
	for i := range r.in.Items {
		it := &r.in.Items[i]
		r.byID[it.Local] = it
		r.order[r.id(it.Local)] = i
		for _, l := range it.Labels {
			r.labels[l] = target{local: it.Local}
		}
		for _, p := range it.Parts {
			if p.Label != "" {
				r.labels[p.Label] = target{local: it.Local, part: p.Marker}
			}
		}
	}
	d := r.in.Doc
	body := d.Text[d.Body:d.End]
	for _, m := range labelCmd.FindAllSubmatchIndex(body, -1) {
		k := strings.TrimSpace(string(body[m[2]:m[3]]))
		off := d.Body + m[0]
		if t, ok := r.labels[k]; ok || k == "" {
			// The label of an equation in an item is a part of the item.
			if ok && t.part == "" && r.inMath(off) {
				t.part = k
				r.labels[k] = t
			}
			continue
		}
		if it := r.innermost(off); it != nil {
			r.labels[k] = target{local: it.Local, part: k}
			continue
		}
		if s := r.sectionAt(off); s != "" {
			r.labels[k] = target{local: s, section: true}
		}
	}
}

// mathEnvs are the environments of displayed equations.
var mathEnvs = map[string]bool{"equation": true, "align": true, "gather": true, "multline": true, "eqnarray": true, "flalign": true, "alignat": true, "subequations": true}

// inMath tells if the offset is in a displayed equation.
func (r *resolver) inMath(off int) bool {
	for _, n := range r.in.Doc.Tree.Nodes {
		if n.Kind == "env" && mathEnvs[strings.TrimSuffix(n.Name, "*")] && n.From <= off && off < n.To {
			return true
		}
	}
	return false
}

// sectionAt is the local of the section whose heading holds the offset.
func (r *resolver) sectionAt(off int) string {
	k := 0
	for _, n := range r.in.Doc.Tree.Nodes {
		if n.Kind != "section" {
			continue
		}
		k++
		if off >= n.From && off < n.Inner+200 && off < n.To {
			return fmt.Sprintf("%ssec-%d", r.in.Prefix, k)
		}
	}
	return ""
}

// innermost is the innermost item that holds the offset, or nil.
func (r *resolver) innermost(off int) *Item {
	var best *Item
	for i := range r.in.Items {
		it := &r.in.Items[i]
		if it.Off <= off && off < it.End && (best == nil || it.Off >= best.Off) {
			best = it
		}
	}
	return best
}

// inside tells if b is a or holds a.
func (r *resolver) inside(a, b *Item) bool {
	for x := a; x != nil; x = r.byID[x.Parent] {
		if x == b {
			return true
		}
		if x.Parent == "" {
			break
		}
	}
	return false
}

// add adds an edge, and keeps only the most certain edge for each pair of
// ends, part and predicate.
func (r *resolver) add(e Edge) {
	if e.From == e.To {
		return
	}
	e.Extractor = r.in.Extractor
	e.Evidence.Version = r.in.Version
	k := key{e.From, e.To, e.Part, e.Predicate}
	if i, ok := r.best[k]; ok {
		if rank(e.Confidence) < rank(r.res.Edges[i].Confidence) {
			r.res.Edges[i] = e
		}
		return
	}
	r.best[k] = len(r.res.Edges)
	r.res.Edges = append(r.res.Edges, e)
}

func rank(c string) int {
	for i, x := range Confidences {
		if x == c {
			return i
		}
	}
	return len(Confidences)
}

func (r *resolver) where(off int) (string, int) {
	p := r.in.Doc.Pos(off)
	return p.File, p.Line
}

// attached gives the edges that the builder decided.
func (r *resolver) attached() {
	for _, it := range r.in.Items {
		file, line := r.where(it.Off)
		if it.Proves != "" {
			r.add(Edge{From: r.id(it.Local), To: r.id(it.Proves), Part: it.ProvesPart, Predicate: Proves, Method: Attach, Confidence: Certain, Evidence: Evidence{File: file, Line: line, Cue: "attach"}})
		}
		if it.Restates != "" {
			r.add(Edge{From: r.id(it.Local), To: r.id(it.Restates), Predicate: Restates, Method: Attach, Confidence: Certain, Evidence: Evidence{File: file, Line: line, Cue: "restate"}})
		}
	}
}

var (
	labelCmd = regexp.MustCompile(`\\label\s*\{([^{}]+)\}`)
	refCmd   = regexp.MustCompile(`\\(ref|cref|Cref|autoref|Autoref|eqref|nameref|vref|Vref|labelcref|cpageref|pageref|hyperref|thmref|subref)\*?\s*(?:\[([^\]]*)\])?\s*\{([^{}]*)\}`)
)

// from is the item that a reference at the offset is from, or nil when the
// reference is not in an item, or is a reference of a proof to what it
// proves.
func (r *resolver) from(off int) *Item {
	return r.innermost(off)
}

// link adds an edge from the item at off to the target, with the predicate of
// the cue.
func (r *resolver) link(src *Item, off, end int, t target, method, confidence string, flags []string) {
	if t.local == src.Local {
		return
	}
	if dst := r.byID[t.local]; dst != nil {
		// A proof that names what it proves, an item that names an item that
		// holds it, and an item that names an item in it, as a proof that
		// names its own claim, are not edges.
		if r.provesOf(src) == dst.Local || r.inside(src, dst) || r.inside(dst, src) {
			return
		}
		// A label in a proof is an equation of the argument, and a proof is
		// not a result that another item uses. The head of an item, as "Case
		// 1", is not a reference to it.
		if dst.Proof || (off >= dst.Off && off < dst.Inner) {
			return
		}
	}
	pred, cue := r.predicate(src, off, end)
	if dst := r.byID[t.local]; pred == Uses && dst != nil && dst.Kind == "other/case" {
		// A case is a part of an argument, as "in Case 2", not a result.
		pred = RefersTo
	}
	if pred == Uses && t.forward && (cue == CueNone || cue == "gives" || givenBy.Match(r.in.Doc.Text[max(src.Off, off-40):off])) {
		// "We first prove the following lemma", "the result is given by the
		// following theorem" and "the following theorem gives" name what
		// comes next.
		pred = RefersTo
	}
	if pred == Uses && src.Proof && off < src.Inner && (cue == CueNone || cue == "since") {
		// The head of a proof, as "Proof of Propositions 2 and 3", names what
		// it proves; "Proof that Lemma 1 implies Theorem 2" and "Proof of Theorem 2
		// (using Lemma 1)" use Lemma 1.
		pred = RefersTo
	}
	if pred == Uses && slices.Contains(flags, FlagTitleMismatch) {
		// "Lemma 4.1" when the paper has no Lemma 4.1 but a Proposition 4.1 is
		// most often a result of another work, or a part, as "Condition 1".
		// The edge says that the text names the item, not that it uses it.
		pred = RefersTo
	}
	if t.section {
		pred = RefersTo
	}
	file, line := r.where(off)
	r.add(Edge{From: r.id(src.Local), To: r.id(t.local), Part: t.part, Predicate: pred, Method: method, Confidence: confidence, Flags: flags, Evidence: Evidence{File: file, Line: line, Cue: cue}})
}

// provesOf is what the item proves, or what the proof that holds it proves.
func (r *resolver) provesOf(it *Item) string {
	for x := it; x != nil; x = r.byID[x.Parent] {
		if x.Proves != "" {
			return x.Proves
		}
		if x.Parent == "" {
			break
		}
	}
	return ""
}

// refs is method 1: each reference command to a label.
func (r *resolver) refs() {
	d := r.in.Doc
	body := d.Text[d.Body:d.End]
	for _, m := range refCmd.FindAllSubmatchIndex(body, -1) {
		off := d.Body + m[0]
		src := r.from(off)
		if src == nil {
			continue
		}
		keys := string(body[m[6]:m[7]])
		if string(body[m[2]:m[3]]) == "hyperref" {
			if m[4] < 0 {
				continue
			}
			keys = string(body[m[4]:m[5]])
		}
		for _, k := range strings.Split(keys, ",") {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			t, ok := r.labels[k]
			if !ok {
				r.res.Dangling++
				continue
			}
			r.link(src, off, d.Body+m[1], t, Ref, Certain, nil)
		}
	}
	r.macroRefs()
}

// userCmd is the name of a command in the text, as \refl.
var userCmd = regexp.MustCompile(`\\[A-Za-z@]+`)

// macroRefs resolves the references that a command of the paper makes, as
// \refl{bfe1} for \newcommand{\refl}[1]{Lemma~\ref{l:#1}}. The call is the
// place of the reference.
func (r *resolver) macroRefs() {
	d := r.in.Doc
	if d.Macros == nil || len(d.Macros.Commands) == 0 {
		return
	}
	refMacros := map[string]*tex.Macro{}
	for name, mac := range d.Macros.Commands {
		if refCmd.MatchString(d.Macros.Expand(mac.Body)) {
			refMacros[name] = mac
		}
	}
	if len(refMacros) == 0 {
		return
	}
	body := d.Text[d.Body:d.End]
	for _, loc := range userCmd.FindAllIndex(body, -1) {
		if loc[0] > 0 && body[loc[0]-1] == '\\' {
			continue
		}
		mac, ok := refMacros[string(body[loc[0]:loc[1]])]
		if !ok {
			continue
		}
		off := d.Body + loc[0]
		src := r.from(off)
		if src == nil {
			continue
		}
		spec := strings.Repeat("m", mac.Args)
		if mac.HasDefault && mac.Args > 0 {
			spec = "o" + strings.Repeat("m", mac.Args-1)
		}
		_, end, ok := tex.ReadArgs(d.Text, d.Body+loc[1], spec)
		if !ok {
			continue
		}
		expanded := d.Macros.Expand(string(d.Text[off:end]))
		for _, m := range refCmd.FindAllStringSubmatch(expanded, -1) {
			keys := m[3]
			if m[1] == "hyperref" {
				keys = m[2]
			}
			for _, k := range strings.Split(keys, ",") {
				k = strings.TrimSpace(k)
				if k == "" {
					continue
				}
				t, ok := r.labels[k]
				if !ok {
					r.res.Dangling++
					continue
				}
				r.link(src, off, end, t, Ref, Certain, nil)
			}
		}
	}
}

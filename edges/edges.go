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
	// Counter is the counter that the environment of the item steps.
	Counter string
	// Parent is the local of the enclosing item, and InProof is true when
	// the item is inside a proof.
	Parent  string
	InProof bool
	// Nested is true for a manual head inside an environment item, as a
	// bold "Case 1." or "Proof of (i)." in a proof: its uses are the uses of
	// the item that holds it. Seen in the gold pilot (set E).
	Nested bool
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
	r.closed()
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
}

type resolver struct {
	in     Input
	res    *Result
	best   map[key]int
	byID   map[string]*Item
	order  map[string]int
	labels map[string]target
	// named is the items that the head of each proof names as what it
	// proves, and headUses is the part of the head that names what it uses
	// ("from Lemma 3").
	named    map[string]map[string]bool
	headUses map[string][2]int
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
		if it.Kind == "other/case" {
			// The labels of a case or a step go to the item that holds it,
			// below. Seen in the gold pilot (set E).
			continue
		}
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
		it := r.innermost(off)
		// An equation of a case or a step is an equation of the item that
		// holds it; a step of an algorithm is not an item of the paper.
		// Seen in the gold pilot (set E).
		for it != nil && it.Kind == "other/case" {
			it = r.byID[it.Parent]
		}
		if it != nil {
			r.labels[k] = target{local: it.Local, part: k}
			continue
		}
		if s := r.sectionAt(off); s != "" {
			r.labels[k] = target{local: s, section: true}
		}
	}
	r.named = map[string]map[string]bool{}
	r.headUses = map[string][2]int{}
	for i := range r.in.Items {
		it := &r.in.Items[i]
		if !it.Proof || it.Inner <= it.Off {
			continue
		}
		head := d.Text[it.Off:it.Inner]
		from, to := len(head), len(head)
		if m := headReason.FindSubmatchIndex(head); m != nil {
			if m[2] >= 0 {
				// "Proof that Proposition 2 implies Theorem 1": what comes
				// before the verb is used.
				from, to = m[2], m[3]
			} else {
				from = m[0]
			}
		}
		r.headUses[it.Local] = [2]int{it.Off + from, it.Off + to}
		for _, m := range refCmd.FindAllSubmatchIndex(head, -1) {
			if m[0] >= from && m[0] < to {
				continue
			}
			for _, k := range strings.Split(string(head[m[6]:m[7]]), ",") {
				if t, ok := r.labels[strings.TrimSpace(k)]; ok && !t.section {
					if r.named[it.Local] == nil {
						r.named[it.Local] = map[string]bool{}
					}
					r.named[it.Local][t.local] = true
				}
			}
		}
	}
}

// headReason is the word of a proof head after which the head names what the
// proof uses: "Proof of Theorem 1 from Lemma 2", "given", "assuming". With
// "that … implies", the head names the use before the verb.
var headReason = regexp.MustCompile(`(?i)\b(?:from|given|assuming|using|via|modulo|based on|(that\b.*?\bimplies))\b`)

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

// closingCues are the cues of a sentence that closes a proof: "This
// completes the proof of Lemma 3 and finishes the proof of part (a) of
// Theorem A".
var closingCues = []string{"proves", "also proves", "completes the proof of", "concludes the proof of", "finishes the proof of", "complete the proof of", "conclude the proof of", "finish the proof of"}

// closed drops the uses edges of a proof to an item that the proof says it
// completes the proof of: the proof proves that item too.
func (r *resolver) closed() {
	done := map[string]map[string]bool{}
	for _, e := range r.res.Edges {
		src := r.byID[strings.TrimPrefix(e.From, r.in.Paper+"#")]
		if e.Predicate != RefersTo || src == nil || !src.Proof || !in(closingCues, e.Evidence.Cue) {
			continue
		}
		if done[e.From] == nil {
			done[e.From] = map[string]bool{}
		}
		done[e.From][e.To] = true
	}
	if len(done) == 0 {
		return
	}
	kept := r.res.Edges[:0]
	for _, e := range r.res.Edges {
		if e.Predicate == Uses && done[e.From][e.To] {
			continue
		}
		kept = append(kept, e)
	}
	r.res.Edges = kept
	r.best = map[key]int{}
	for i, e := range r.res.Edges {
		r.best[key{e.From, e.To, e.Part, e.Predicate}] = i
	}
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
	labelCmd = regexp.MustCompile(`\\(?:th)?label\s*\{([^{}]+)\}`)
	refCmd   = regexp.MustCompile(`\\(ref|cref|Cref|autoref|Autoref|eqref|nameref|vref|Vref|labelcref|cpageref|pageref|hyperref|thmref|thref|subref)\*?\s*(?:\[([^\]]*)\])?\s*\{([^{}]*)\}`)
)

// from is the item that a reference at the offset is from, or nil when the
// reference is not in an item, or is a reference of a proof to what it
// proves.
func (r *resolver) from(off int) *Item {
	it := r.innermost(off)
	for it != nil && it.Nested && r.byID[it.Parent] != nil {
		it = r.byID[it.Parent]
	}
	return it
}

// link adds an edge from the item at off to the target, with the predicate of
// the cue.
func (r *resolver) link(src *Item, off, end int, t target, method, confidence string, flags []string) {
	if t.local == src.Local {
		return
	}
	// The head of a proof names what it proves, and a head that names two
	// items ("Proof of Lemmas 2 and 3") proves both: neither is a use. A
	// head can also name what the proof uses ("from Lemma 4").
	if u := r.headUses[src.Local]; src.Proof && off >= src.Off && off < src.Inner && (off < u[0] || off >= u[1]) {
		return
	}
	if dst := r.byID[t.local]; dst != nil {
		// A proof that names what it proves, and an item that names an item
		// that holds it, are not edges.
		if r.proved(src, dst.Local) || r.inside(src, dst) {
			return
		}
		// A case, a step or a claim of the item itself is a part of its own
		// argument, not a use (AX6.1).
		if r.inside(dst, src) {
			return
		}
		// An equation of another proof is a step of the proof of what that
		// proof proves, so the edge goes to what it proves (AX6.1).
		if dst.Proof && dst.Proves != "" {
			t.local = dst.Proves
			if t.local == src.Local || r.proved(src, t.local) {
				return
			}
			if to := r.byID[t.local]; to != nil && r.inside(src, to) {
				return
			}
		}
	}
	pred, cue := r.predicate(src, off, end)
	if t.section {
		pred = RefersTo
	}
	file, line := r.where(off)
	r.add(Edge{From: r.id(src.Local), To: r.id(t.local), Part: t.part, Predicate: pred, Method: method, Confidence: confidence, Flags: flags, Evidence: Evidence{File: file, Line: line, Cue: cue}})
}

// proved tells if the item, or the proof that holds it, proves the target:
// the target is what it proves, or an item that the head of the proof names
// ("Proof of Theorem 1.1 and Corollary 1.2").
func (r *resolver) proved(it *Item, local string) bool {
	if r.provesOf(it) == local {
		return true
	}
	for x := it; x != nil; x = r.byID[x.Parent] {
		if r.named[x.Local][local] {
			return true
		}
		if x.Parent == "" {
			break
		}
	}
	return false
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
		_, end, ok := tex.ReadArgs(d.Text, d.Body+loc[1], mac.Spec())
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

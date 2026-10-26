// Package census counts, with code, each place in the source of a paper where
// an item starts, and matches each place to an object record (AX04).
//
// The census does not use the object builder. It reads the expanded text of
// the TeX front end again, with its own scan, so that a bug in the builder
// shows as a gap and not as a silent loss. A gap is fixed in the extractor and
// never in the data.
package census

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/tamnd/arxiv-reader/tex"
	"github.com/tamnd/arxiv-reader/theorems"
)

// Occurrence types of AX4.2.
const (
	Env     = "env"
	Proof   = "proof"
	Head    = "head"
	Restate = "restate"
	Label   = "label"
	Textref = "textref"
)

// Match results of AX4.3.
const (
	Matched = "matched"
	Waived  = "waived"
	Missing = "missing"
	Double  = "double"
)

// Occurrence is one place in the expanded source.
type Occurrence struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	// K is the position of the occurrence among the occurrences of its type,
	// in the order of the expanded source, from 1.
	K int `json:"k"`
	// Env is the environment, or head for a manual head.
	Env   string `json:"env,omitempty"`
	Title string `json:"title,omitempty"`
	Item  string `json:"item,omitempty"`
	// Off is the offset of \begin or of the head, Inner the offset after its
	// arguments, Close the offset of \end, and End the offset after the item.
	Off   int `json:"-"`
	Inner int `json:"-"`
	Close int `json:"-"`
	End   int `json:"-"`
	// Opt is the optional argument, as the author wrote it.
	Opt    string `json:"-"`
	HasOpt bool   `json:"-"`
	// Counter is the counter that the occurrence steps, and Number its number
	// from the best witness. NumberFrom says which witness: aux, render,
	// counter or head.
	Counter    string `json:"counter,omitempty"`
	Number     string `json:"number,omitempty"`
	NumberFrom string `json:"number_from,omitempty"`
	// Model is the number that the counter model gives, Aux the number that
	// the .aux file gives, and Printed the number that the render printed.
	// They are kept apart for the check of agreement.
	Model   string `json:"-"`
	Aux     string `json:"-"`
	Printed string `json:"-"`
	// Labels is each \label of an env or head occurrence. For a label
	// occurrence, Target is the label key. For a textref, Target is the title
	// and number that the text names.
	Labels []string `json:"labels,omitempty"`
	Target string   `json:"target,omitempty"`
	// Parent is the id of the enclosing env, proof or head occurrence.
	Parent string `json:"parent,omitempty"`
	// Pos is the file and the first line, and Last is the last line.
	Pos  tex.Pos `json:"pos"`
	Last int     `json:"last,omitempty"`
	// Flags is conditional, appendix, unclosed.
	Flags []string `json:"flags,omitempty"`
	// Result is the match result of AX4.3.
	Result string `json:"result,omitempty"`
	// Waiver is the waiver reason when Result is waived.
	Waiver string `json:"waiver,omitempty"`
	// Confidence is for heads: high or medium (AX3.4).
	Confidence string `json:"confidence,omitempty"`
}

// HasFlag tells if the occurrence has a flag.
func (o *Occurrence) HasFlag(f string) bool {
	for _, x := range o.Flags {
		if x == f {
			return true
		}
	}
	return false
}

// Input is what the census reads for one main file of one version.
type Input struct {
	Paper     string
	Version   int
	Extractor string
	Run       string
	Doc       *tex.Document
	Set       *theorems.Set
	Titles    *theorems.Titles
	// Aux is each .aux file of the e-print, when the author included one.
	Aux [][]byte
	// Render is the second reading from the LaTeXML render, or nil.
	Render *Render
	// Outside is the counts of results that outside indexes give, by name.
	Outside map[string]int
}

// Census is the result of a census of one main file.
type Census struct {
	In          Input
	Occurrences []*Occurrence
	// Labels maps a label key to the id of the env, proof or head occurrence
	// that holds it.
	Labels map[string]string
	// AuxLabels is the number of each label that the .aux file gives.
	AuxLabels map[string]string
	// Findings is each finding of rules D07, D08 and D09.
	Findings []Finding
	// touched is each counter that the paper sets by hand, so that the check
	// of contiguity skips it.
	touched map[string]bool
	// Restatable is the macro of each restatable of thm-restate.
	Restatable map[string]bool
}

// Finding is one finding of the census checks.
type Finding struct {
	Rule string `json:"rule"`
	// Occurrence is the id that the finding is about, when there is one.
	Occurrence string `json:"occurrence,omitempty"`
	Message    string `json:"message"`
}

var (
	beginEnv   = regexp.MustCompile(`\\begin\s*\{([^{}]{1,64})\}`)
	labelCmd   = regexp.MustCompile(`\\label\s*\{([^{}]*)\}`)
	restateCmd = regexp.MustCompile(`\\([A-Za-z@]+)\*`)
)

// Run takes the census of one main file. It counts the occurrences, numbers
// them with the counter model, and runs the checks of AX4.4.
func Run(in Input) *Census {
	c := &Census{In: in, Labels: map[string]string{}, AuxLabels: map[string]string{}, Restatable: map[string]bool{}}
	d := in.Doc
	if d == nil || d.Body == 0 {
		return c
	}
	for _, a := range in.Aux {
		for k, v := range ParseAux(a) {
			c.AuxLabels[k] = v
		}
	}
	c.envs()
	c.heads()
	c.restates()
	sort.SliceStable(c.Occurrences, func(i, j int) bool { return c.Occurrences[i].Off < c.Occurrences[j].Off })
	c.parents()
	c.labels()
	c.number()
	c.textrefs()
	c.ids()
	c.checks()
	return c
}

// envs counts each \begin of a theorem-like or proof environment.
func (c *Census) envs() {
	d := c.In.Doc
	body := d.Text[d.Body:d.End]
	for _, m := range beginEnv.FindAllSubmatchIndex(body, -1) {
		off := d.Body + m[0]
		if off > 0 && d.Text[off-1] == '\\' {
			continue
		}
		name := strings.TrimSpace(string(body[m[2]:m[3]]))
		inner := d.Body + m[1]
		if name == "restatable" || name == "restatable*" {
			// \begin{restatable}[opt]{env}{macro} of thm-restate.
			a, end, ok := tex.ReadArgs(d.Text, inner, "omm")
			if !ok {
				continue
			}
			env := strings.TrimSpace(a.Values[1])
			decl := c.In.Set.Get(env)
			if decl == nil {
				continue
			}
			o := c.occ(Env, env, decl, off, end)
			o.Opt, o.HasOpt = a.Values[0], a.Has[0]
			o.Close, o.End = closeOf(d, name, end)
			c.Restatable[strings.TrimSpace(a.Values[2])] = true
			continue
		}
		decl := c.In.Set.Get(name)
		if decl == nil {
			continue
		}
		typ := Env
		if decl.Proof {
			typ = Proof
		}
		o := c.occ(typ, name, decl, off, inner)
		if decl.TitleArg {
			o.Title, o.Item, o.Inner = decl.TitleOf(d.Text, inner, c.In.Titles)
		}
		if j := skipSpace(d.Text, o.Inner); j < d.End && d.Text[j] == '[' {
			if in, e, ok := tex.Group(d.Text, j); ok && e-j < 2000 {
				o.Opt, o.HasOpt, o.Inner = string(d.Text[in:e-1]), true, e
			}
		}
		o.Close, o.End = closeOf(d, name, o.Inner)
		if o.Close == d.End {
			o.Flags = append(o.Flags, "unclosed")
		}
	}
}

func (c *Census) occ(typ, env string, decl *theorems.Declaration, off, inner int) *Occurrence {
	d := c.In.Doc
	o := &Occurrence{Type: typ, Env: env, Off: off, Inner: inner, Pos: d.Pos(off)}
	if decl != nil {
		o.Title, o.Item, o.Counter = decl.Title, decl.Item, decl.Counter
	}
	if tex.In(d.Conditional, off) {
		o.Flags = append(o.Flags, "conditional")
	}
	if d.Tree != nil && d.Tree.Appendix > 0 && off > d.Tree.Appendix {
		o.Flags = append(o.Flags, "appendix")
	}
	c.Occurrences = append(c.Occurrences, o)
	return o
}

// closeOf finds the \end of an environment that starts before from, with
// nesting of the same environment.
func closeOf(d *tex.Document, name string, from int) (close, end int) {
	re := regexp.MustCompile(`\\(begin|end)\s*\{` + regexp.QuoteMeta(name) + `\}`)
	depth := 1
	for _, m := range re.FindAllSubmatchIndex(d.Text[from:d.End], -1) {
		if string(d.Text[from+m[2]:from+m[3]]) == "begin" {
			depth++
			continue
		}
		depth--
		if depth == 0 {
			return from + m[0], from + m[1]
		}
	}
	return d.End, d.End
}

// heads counts each manual head of AX3.4 outside the env occurrences.
func (c *Census) heads() {
	var skip []tex.Region
	for _, o := range c.Occurrences {
		skip = append(skip, tex.Region{From: o.Off, To: o.End})
	}
	for _, h := range theorems.FindHeads(c.In.Doc, c.In.Titles, skip) {
		typ := Head
		if h.Proof {
			typ = Proof
		}
		o := c.occ(typ, "head", nil, h.Off, h.Inner)
		o.Title, o.Item, o.Close, o.End = h.Title, h.Item, h.End, h.End
		o.Number, o.Confidence = h.Number, h.Confidence()
		o.Opt = h.Name
		if h.Proof {
			o.Item = "proof"
			o.Opt = h.Target
			o.HasOpt = h.Target != ""
		}
		if h.Number != "" {
			o.NumberFrom = "head"
		}
		if h.Form == "section" {
			o.Flags = append(o.Flags, "section")
		}
	}
}

// restates counts each \macro* of a restatable, the restatements that
// thm-restate makes.
func (c *Census) restates() {
	if len(c.Restatable) == 0 {
		return
	}
	d := c.In.Doc
	body := d.Text[d.Body:d.End]
	for _, m := range restateCmd.FindAllSubmatchIndex(body, -1) {
		name := string(body[m[2]:m[3]])
		if _, ok := c.Restatable[name]; !ok {
			continue
		}
		o := c.occ(Restate, "restate", nil, d.Body+m[0], d.Body+m[1])
		o.End, o.Close = o.Inner, o.Inner
		o.Target = name
	}
}

// parents sets the enclosing env, proof or head of each occurrence.
func (c *Census) parents() {
	var stack []*Occurrence
	for _, o := range c.Occurrences {
		for len(stack) > 0 && stack[len(stack)-1].End <= o.Off {
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 {
			o.Parent = "#" + fmt.Sprint(indexOf(c.Occurrences, stack[len(stack)-1]))
		}
		if o.End > o.Inner {
			stack = append(stack, o)
		}
	}
}

func indexOf(os []*Occurrence, o *Occurrence) int {
	for i, x := range os {
		if x == o {
			return i
		}
	}
	return -1
}

// labels counts each \label inside an env or a head occurrence.
//
// The label belongs to the innermost occurrence that holds it. A label before
// the first paragraph break is a label of the item. A later one is the label
// of a part or of an equation, and stays a label occurrence of the item.
func (c *Census) labels() {
	d := c.In.Doc
	body := d.Text[d.Body:d.End]
	var found []*Occurrence
	for _, m := range labelCmd.FindAllSubmatchIndex(body, -1) {
		off := d.Body + m[0]
		key := strings.TrimSpace(string(body[m[2]:m[3]]))
		owner := c.innermost(off)
		if owner == nil {
			continue
		}
		if owner.Type != Restate {
			if !contains(owner.Labels, key) {
				owner.Labels = append(owner.Labels, key)
			}
			if _, ok := c.Labels[key]; !ok {
				c.Labels[key] = fmt.Sprint(indexOf(c.Occurrences, owner))
			}
		}
		o := &Occurrence{Type: Label, Off: off, Inner: d.Body + m[1], Target: key, Pos: d.Pos(off)}
		o.Parent = "#" + fmt.Sprint(indexOf(c.Occurrences, owner))
		found = append(found, o)
	}
	for _, o := range found {
		c.Occurrences = append(c.Occurrences, o)
	}
}

func (c *Census) innermost(off int) *Occurrence {
	var best *Occurrence
	for _, o := range c.Occurrences {
		if o.Type == Label || o.Type == Textref {
			continue
		}
		if o.Off <= off && off < o.End {
			if best == nil || o.Off >= best.Off {
				best = o
			}
		}
	}
	return best
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// ids gives each occurrence its id of AX4.2.
func (c *Census) ids() {
	// The parents were written as indexes into the list before the labels
	// were added, and the list order of those does not change.
	byIndex := make([]*Occurrence, len(c.Occurrences))
	copy(byIndex, c.Occurrences)
	sort.SliceStable(c.Occurrences, func(i, j int) bool {
		if c.Occurrences[i].Type != c.Occurrences[j].Type {
			return typeOrder(c.Occurrences[i].Type) < typeOrder(c.Occurrences[j].Type)
		}
		return c.Occurrences[i].Off < c.Occurrences[j].Off
	})
	k := map[string]int{}
	prefix := fmt.Sprintf("%sv%d", c.In.Paper, c.In.Version)
	for _, o := range c.Occurrences {
		k[o.Type]++
		o.K = k[o.Type]
		o.ID = fmt.Sprintf("%s#%s-%d", prefix, o.Type, o.K)
	}
	for _, o := range c.Occurrences {
		if strings.HasPrefix(o.Parent, "#") {
			var i int
			fmt.Sscan(o.Parent[1:], &i)
			if i >= 0 && i < len(byIndex) {
				o.Parent = byIndex[i].ID
			}
		}
	}
	for key, v := range c.Labels {
		var i int
		fmt.Sscan(v, &i)
		if i >= 0 && i < len(byIndex) {
			c.Labels[key] = byIndex[i].ID
		}
	}
	for _, o := range c.Occurrences {
		o.Last = c.In.Doc.Pos(max(o.End-1, o.Off)).Line
	}
}

func typeOrder(t string) int {
	switch t {
	case Env:
		return 0
	case Proof:
		return 1
	case Head:
		return 2
	case Restate:
		return 3
	case Label:
		return 4
	}
	return 5
}

// Items is the env, proof and head occurrences, in the order of the source.
func (c *Census) Items() []*Occurrence {
	var out []*Occurrence
	for _, o := range c.Occurrences {
		if o.Type == Env || o.Type == Proof || o.Type == Head || o.Type == Restate {
			out = append(out, o)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Off < out[j].Off })
	return out
}

// ByID is the occurrence with an id, or nil.
func (c *Census) ByID(id string) *Occurrence {
	for _, o := range c.Occurrences {
		if o.ID == id {
			return o
		}
	}
	return nil
}

// Count is the count of occurrences of a type.
func (c *Census) Count(typ string) int {
	n := 0
	for _, o := range c.Occurrences {
		if o.Type == typ {
			n++
		}
	}
	return n
}

func skipSpace(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\n' || b[i] == '\t' || b[i] == '\r') {
		i++
	}
	return i
}

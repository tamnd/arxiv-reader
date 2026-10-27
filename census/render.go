package census

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/tamnd/arxiv-reader/extract"
)

// Render is the second reading of a paper, from its LaTeXML render.
type Render struct {
	// Theorems is each ltx_theorem element, in the order of the page.
	Theorems []RenderItem
	// Proofs is the count of ltx_proof elements.
	Proofs int
}

// RenderItem is one theorem of the render.
type RenderItem struct {
	Env    string
	Number string
}

// FromRender reads the theorems and the proofs out of a parsed render.
func FromRender(p *extract.Paper) *Render {
	r := &Render{}
	var walk func([]extract.Block)
	walk = func(bs []extract.Block) {
		for _, b := range bs {
			switch {
			case b.Kind == extract.KindTheorem && strings.EqualFold(b.Env, "proof"):
				// A class that declares proof with \newtheorem renders each
				// proof as a theorem, and the source counts it as a proof.
				r.Proofs++
			case b.Kind == extract.KindTheorem:
				r.Theorems = append(r.Theorems, RenderItem{Env: b.Env, Number: strings.TrimSpace(b.Tag)})
			case b.Kind == extract.KindProof:
				r.Proofs++
			}
			walk(b.Blocks)
		}
	}
	var sections func([]extract.Section)
	sections = func(ss []extract.Section) {
		for _, s := range ss {
			walk(s.Blocks)
			sections(s.Sections)
		}
	}
	walk(p.Front())
	sections(p.Sections)
	return r
}

// alignRender gives the numbers of the render to the env occurrences.
//
// The k-th occurrence of an environment in the source is the k-th theorem of
// that environment in the render. When the counts of an environment differ,
// its numbers are not taken, and the check of AX4.5 reports the difference.
func (c *Census) alignRender() {
	byEnv := map[string][]RenderItem{}
	for _, t := range c.In.Render.Theorems {
		byEnv[t.Env] = append(byEnv[t.Env], t)
	}
	src := map[string][]*Occurrence{}
	for _, o := range c.Items() {
		if o.Type == Env {
			env := c.renderName(o.Env)
			src[env] = append(src[env], o)
		}
	}
	for env, os := range src {
		rs := byEnv[env]
		if len(rs) != len(os) {
			continue
		}
		for i, o := range os {
			o.Printed = strings.TrimSuffix(rs[i].Number, ".")
			if o.Aux == "" && o.Printed != "" {
				o.Number, o.NumberFrom = o.Printed, "render"
			}
		}
	}
}

// renderName is the name that LaTeXML gives to an environment of the source.
// A wrapper renders as the environment that it opens.
func (c *Census) renderName(env string) string {
	if c.In.Set != nil {
		if d := c.In.Set.Get(env); d != nil && d.Inner != "" {
			return d.Inner
		}
	}
	return env
}

// textWrapper tells if the environment is a wrapper that prints its own head
// and opens no declared environment, as
// \newenvironment{proof}{\noindent{\bf Proof:}}{}. LaTeXML runs the code of
// a wrapper of the paper or of a local .sty file. It does not read a local
// .cls file, and it uses its own binding of amsthm for such a class.
func (c *Census) textWrapper(env string) bool {
	if c.In.Set == nil || c.In.Doc == nil || c.In.Doc.Macros == nil {
		return false
	}
	d := c.In.Set.Get(env)
	e := c.In.Doc.Macros.Envs[env]
	return d != nil && d.From == "wrapper" && d.Inner == "" && e != nil && !strings.HasSuffix(strings.ToLower(e.File), ".cls")
}

// textProof tells if the paper defines a proof environment again with its own
// head, as \def\proof{{\it Proof.}\ } with \def\endproof, or as
// \renewenvironment{proof}{\par\noindent{\itshape Proof.}}{}. LaTeXML runs the
// definition of the paper or of a local .sty file, so the render has the head
// as text and no proof. A definition that opens another environment, or that
// calls a command that \let saved from the old one, still makes a proof.
func (c *Census) textProof(env string) bool {
	if c.In.Set == nil || c.In.Doc == nil || c.In.Doc.Macros == nil {
		return false
	}
	d := c.In.Set.Get(env)
	if d == nil || !d.Proof || d.From == "wrapper" {
		return false
	}
	m := c.In.Doc.Macros
	var begin, file string
	if e := m.Envs[env]; e != nil {
		begin, file = e.Begin, e.File
	} else if p, q := m.Commands[`\`+env], m.Commands[`\end`+env]; p != nil && q != nil && p.How == "def" {
		begin, file = p.Body, p.File
	} else {
		return false
	}
	if strings.HasSuffix(strings.ToLower(file), ".cls") || strings.Contains(begin, `\begin`) {
		return false
	}
	for _, call := range commandCall.FindAllString(begin, -1) {
		if call == `\`+env {
			return false
		}
		if l := m.Commands[call]; l != nil && l.How == "let" && l.Body == `\`+env {
			return false
		}
	}
	return true
}

var commandCall = regexp.MustCompile(`\\[A-Za-z@]+`)

// renderCounts compares the counts of the source and of the render (AX4.5).
func (c *Census) renderCounts() *RenderCount {
	if c.In.Render == nil {
		return &RenderCount{}
	}
	rc := &RenderCount{Theorems: len(c.In.Render.Theorems), Proofs: c.In.Render.Proofs}
	byEnv := map[string]*EnvCount{}
	count := func(env string) *EnvCount {
		e := byEnv[env]
		if e == nil {
			e = &EnvCount{Env: env}
			byEnv[env] = e
		}
		return e
	}
	for _, t := range c.In.Render.Theorems {
		count(t.Env).Render++
	}
	for _, o := range c.Items() {
		switch {
		case c.textWrapper(o.Env), o.Type == Proof && c.textProof(o.Env):
			// LaTeXML runs the code of the wrapper, so the render has the
			// head as text and no theorem or proof.
		case o.Type == Env:
			rc.SourceTheorems++
			count(c.renderName(o.Env)).Source++
		case o.Type == Proof && (o.Env != "head" || o.HasFlag("command")):
			// A head such as "Proof." is text in the render, but LaTeXML
			// renders the command \proof of amsthm as a proof.
			rc.SourceProofs++
		}
	}
	src := rc.SourceTheorems
	diff := abs(rc.Theorems - src)
	rc.Agree = diff <= 1 && float64(diff) <= 0.02*float64(max(src, 1))+1
	rc.Agree = rc.Agree && abs(rc.Proofs-rc.SourceProofs) <= 1
	if diff > 1 && float64(diff) > 0.02*float64(src) {
		c.Findings = append(c.Findings, Finding{Rule: "D08", Message: fmt.Sprintf("the source has %d theorem environments and the render has %d", src, rc.Theorems)})
	}
	if !rc.Agree {
		for _, e := range byEnv {
			if e.Source != e.Render {
				rc.Envs = append(rc.Envs, *e)
			}
		}
		sort.Slice(rc.Envs, func(i, j int) bool { return rc.Envs[i].Env < rc.Envs[j].Env })
	}
	return rc
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

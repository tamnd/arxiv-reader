package census

import (
	"fmt"
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
			switch b.Kind {
			case extract.KindTheorem:
				r.Theorems = append(r.Theorems, RenderItem{Env: b.Env, Number: strings.TrimSpace(b.Tag)})
			case extract.KindProof:
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
	walk(p.Abstract)
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
			src[o.Env] = append(src[o.Env], o)
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

// renderCounts compares the counts of the source and of the render (AX4.5).
func (c *Census) renderCounts() (theorems, proofs int, agree bool) {
	if c.In.Render == nil {
		return 0, 0, false
	}
	theorems, proofs = len(c.In.Render.Theorems), c.In.Render.Proofs
	src, srcProofs := 0, 0
	for _, o := range c.Items() {
		switch {
		case o.Type == Env:
			src++
		case o.Type == Proof && o.Env != "head":
			srcProofs++
		}
	}
	diff := abs(theorems - src)
	agree = diff <= 1 && float64(diff) <= 0.02*float64(max(src, 1))+1
	agree = agree && abs(proofs-srcProofs) <= 1
	if diff > 1 && float64(diff) > 0.02*float64(src) {
		c.Findings = append(c.Findings, Finding{Rule: "D08", Message: fmt.Sprintf("the source has %d theorem environments and the render has %d", src, theorems)})
	}
	return theorems, proofs, agree
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

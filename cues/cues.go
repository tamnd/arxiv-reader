// Package cues finds the candidate sentences of the prose items of AX7.4.
//
// A prose item is a definition, a conjecture, a problem, a claim or a
// hypothesis that no environment and no manual head marks. Code cannot find
// them all, so this package only marks the sentences with a cue of
// manifests/prose.yaml. The understand role reads the sections with the cues
// marked and gives the items (AX7.4 step 2).
package cues

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/tamnd/arxiv-reader/objects"
	"github.com/tamnd/arxiv-reader/tex"
	"gopkg.in/yaml.v3"
)

// Emphasis is the cue of a sentence with an emphasized term and "if" or
// "such that". It is not in the manifest, because it is not a phrase.
const Emphasis = "emphasis"

// gap is the largest number of words that three dots in a cue match.
const gap = 8

// Cues is the cue patterns of manifests/prose.yaml.
type Cues struct {
	kinds []kind
}

type kind struct {
	name string
	cues []cue
}

type cue struct {
	text  string
	parts [][]string // the words before, between and after the gaps
}

// Parse reads manifests/prose.yaml.
func Parse(b []byte) (*Cues, error) {
	var raw map[string][]string
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("cues: prose.yaml: %w", err)
	}
	c := &Cues{}
	for name, list := range raw {
		k := kind{name: name}
		for _, t := range list {
			var parts [][]string
			for p := range strings.SplitSeq(t, "...") {
				ws := strings.Fields(strings.ToLower(p))
				if len(ws) == 0 {
					return nil, fmt.Errorf("cues: prose.yaml: the cue %q of %s has an empty part", t, name)
				}
				parts = append(parts, ws)
			}
			k.cues = append(k.cues, cue{text: t, parts: parts})
		}
		c.kinds = append(c.kinds, k)
	}
	slices.SortFunc(c.kinds, func(a, b kind) int { return strings.Compare(a.name, b.name) })
	return c, nil
}

// Candidate is one sentence with a cue.
type Candidate struct {
	Kind string `json:"kind"`
	Cue  string `json:"cue"`
	// Section is the section of the sentence, as sec-3, in the form of the
	// object records.
	Section string       `json:"section,omitempty"`
	Span    objects.Span `json:"span"`
	// From and To are the offsets of the sentence in the expanded text.
	From int `json:"-"`
	To   int `json:"-"`
}

// Record is one candidate of a paper, as the batch writes it to
// analysis/prose/. It has no text, so it is the same for each licence.
type Record struct {
	Paper   string `json:"paper"`
	Version int    `json:"version"`
	Candidate
}

// skipped is the environments that hold no running text.
var skipped = map[string]bool{
	"equation": true, "align": true, "alignat": true, "gather": true, "multline": true, "flalign": true,
	"eqnarray": true, "displaymath": true, "math": true, "array": true, "split": true, "cases": true,
	"figure": true, "table": true, "tabular": true, "tabularx": true, "longtable": true, "tikzpicture": true,
	"picture": true, "pspicture": true, "xy": true, "tikzcd": true, "diagram": true, "verbatim": true,
	"lstlisting": true, "minted": true, "algorithm": true, "algorithmic": true, "thebibliography": true,
	"comment": true,
}

// Find gives the candidates of one main file. The items are the extents of
// the items that the builder found, as environments and manual heads, so the
// sentences in them are not prose. Prefix is the prefix of the section names
// of the main file, as m2- for the second main file.
func (c *Cues) Find(d *tex.Document, items []tex.Region, prefix string) []Candidate {
	var out []Candidate
	for _, s := range c.read(d, items, prefix) {
		for _, m := range s.Cues {
			cd := Candidate{Kind: m.Kind, Cue: m.Cue, Section: s.Section, From: s.From, To: s.To}
			p, last := d.Pos(s.From), d.Pos(s.To-1)
			cd.Span = objects.Span{File: p.File, From: p.Line, To: last.Line}
			out = append(out, cd)
		}
	}
	slices.SortStableFunc(out, func(a, b Candidate) int { return a.From - b.From })
	return out
}

// Mark is one cue of a sentence.
type Mark struct {
	Kind string `json:"kind"`
	Cue  string `json:"cue"`
}

// sentence is one sentence of the running text, with its cues.
type sentence struct {
	Section  string
	From, To int
	Cues     []Mark
}

// read gives each sentence of the running text of one main file, in order,
// with the cues that it has.
func (c *Cues) read(d *tex.Document, items []tex.Region, prefix string) []sentence {
	if d.End <= d.Body {
		return nil
	}
	text := d.Text
	mask := make([]bool, d.End-d.Body)
	hide := func(from, to int) {
		for i := max(from, d.Body); i < min(to, d.End); i++ {
			mask[i-d.Body] = true
		}
	}
	for _, r := range items {
		hide(r.From, r.To)
	}
	secIndex := map[int]int{}
	k := 0
	if d.Tree != nil {
		for i, n := range d.Tree.Nodes {
			switch {
			case n.Kind == "section":
				k++
				secIndex[i] = k
				// The heading has no period, so it would join the first sentence.
				hide(n.From, n.Inner)
			case skipped[strings.TrimSuffix(n.Name, "*")]:
				hide(n.From, n.To)
			}
		}
	}
	var out []sentence
	for _, s := range sentences(text, d.Body, d.End, mask) {
		st := sentence{From: s.From, To: s.To}
		if d.Tree != nil {
			if i := d.Tree.Section(s.From); i >= 0 {
				st.Section = fmt.Sprintf("%ssec-%d", prefix, secIndex[i])
			}
		}
		raw := text[s.From:s.To]
		ws := words(raw)
		definition := false
		for _, kd := range c.kinds {
			for _, cu := range kd.cues {
				if cu.match(ws) {
					st.Cues = append(st.Cues, Mark{Kind: kd.name, Cue: cu.text})
					definition = definition || kd.name == "definition"
					break
				}
			}
		}
		if !definition && emphasized.Match(raw) && (slices.Contains(ws, "if") || slices.Contains(ws, "iff") || has(ws, []string{"such", "that"}) >= 0) {
			st.Cues = append(st.Cues, Mark{Kind: "definition", Cue: Emphasis})
		}
		out = append(out, st)
	}
	return out
}

var emphasized = regexp.MustCompile(`\\(emph|textit|textbf|textsl|defn|term|dfn)\s*\{|\{\\(em|it|bf|sl)\b`)

// match tells if the words hold the cue. A gap between two parts matches one
// to eight words.
func (c cue) match(ws []string) bool {
	for start := 0; start < len(ws); start++ {
		i := has(ws[start:], c.parts[0])
		if i < 0 {
			return false
		}
		start += i
		if c.rest(ws[start+len(c.parts[0]):], 1) {
			return true
		}
	}
	return false
}

func (c cue) rest(ws []string, part int) bool {
	if part == len(c.parts) {
		return true
	}
	p := c.parts[part]
	for skip := 1; skip <= gap && skip+len(p) <= len(ws); skip++ {
		if slices.Equal(ws[skip:skip+len(p)], p) && c.rest(ws[skip+len(p):], part+1) {
			return true
		}
	}
	return false
}

// has returns the index of the first place where ws holds p, or -1.
func has(ws, p []string) int {
	for i := 0; i+len(p) <= len(ws); i++ {
		if slices.Equal(ws[i:i+len(p)], p) {
			return i
		}
	}
	return -1
}

// words splits TeX into lower-case words. A control word is dropped, and its
// arguments stay, so "is \emph{called}" gives "is called". Inline math is
// one word "$", so "denote by $X$ the" gives "denote by $ the".
func words(b []byte) []string {
	var ws []string
	var w strings.Builder
	flush := func() {
		if w.Len() > 0 {
			ws = append(ws, w.String())
			w.Reset()
		}
	}
	math := false
	for i := 0; i < len(b); i++ {
		ch := b[i]
		switch {
		case ch == '$':
			if !math {
				flush()
				ws = append(ws, "$")
			}
			math = !math
		case math:
		case ch == '\\':
			flush()
			j := i + 1
			for j < len(b) && (b[j] >= 'a' && b[j] <= 'z' || b[j] >= 'A' && b[j] <= 'Z') {
				j++
			}
			if j < len(b) && j == i+1 {
				if b[j] == '(' {
					// \( ... \) is inline math.
					end := strings.Index(string(b[j:]), `\)`)
					if end < 0 {
						return append(ws, "$")
					}
					ws = append(ws, "$")
					j += end + 1
				}
			} else {
				j--
			}
			i = j
		case ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch >= 0x80:
			w.WriteByte(ch)
		case ch >= 'A' && ch <= 'Z':
			w.WriteByte(ch + 'a' - 'A')
		case ch == '\'' || ch == '-':
			if w.Len() > 0 {
				w.WriteByte(ch)
			}
		default:
			flush()
		}
	}
	flush()
	return ws
}

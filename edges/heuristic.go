package edges

import (
	"regexp"
	"strings"

	"github.com/tamnd/arxiv-reader/theorems"
)

// positionWindow is how many items a reference by position may skip.
const positionWindow = 10

// positions is method 5: a reference by position, as "the previous lemma" or
// "the lemma above". "This theorem" is not read, because in a proof it is
// what the proof proves.
func (r *resolver) positions() {
	alts := r.titles()
	if len(alts) == 0 {
		return
	}
	t := strings.Join(alts, "|")
	before := regexp.MustCompile(`(?i)\b(?:the|that)\s+(previous|preceding|above|last|foregoing|following|next|subsequent)\s+(` + t + `)\b`)
	after := regexp.MustCompile(`(?i)\bthe\s+(` + t + `)\s+(above|below)\b`)
	d := r.in.Doc
	body := d.Text[d.Body:d.End]
	read := func(off, end int, word, title string) {
		src := r.from(off)
		if src == nil {
			return
		}
		back := true
		switch strings.ToLower(word) {
		case "following", "next", "subsequent", "below":
			back = false
		}
		dst := r.byPosition(src, off, title, back)
		if dst == nil {
			r.res.Dangling++
			return
		}
		r.link(src, off, end, target{local: dst.Local}, Position, Medium, nil)
	}
	for _, m := range before.FindAllSubmatchIndex(body, -1) {
		read(d.Body+m[0], d.Body+m[1], string(body[m[2]:m[3]]), string(body[m[4]:m[5]]))
	}
	for _, m := range after.FindAllSubmatchIndex(body, -1) {
		read(d.Body+m[0], d.Body+m[1], string(body[m[4]:m[5]]), string(body[m[2]:m[3]]))
	}
}

// byPosition is the nearest item of the title before or after the offset,
// within the window and in the same section or the section before.
func (r *resolver) byPosition(src *Item, off int, title string, back bool) *Item {
	kind := theorems.Unknown
	if r.in.Titles != nil {
		kind = r.in.Titles.Kind(title)
	}
	match := func(it *Item) bool {
		if it.Proof || it == src || r.inside(src, it) {
			return false
		}
		return theorems.Normalize(it.Title) == theorems.Normalize(title) || (kind != theorems.Unknown && it.Kind == kind)
	}
	items := r.in.Items
	seen := 0
	sec := r.in.Doc.Tree.Section(off)
	if back {
		for i := len(items) - 1; i >= 0; i-- {
			it := &items[i]
			if it.End > off || it.Proof || it.InProof {
				continue
			}
			if seen++; seen > positionWindow {
				return nil
			}
			if s := r.in.Doc.Tree.Section(it.Off); sec >= 0 && s >= 0 && !nextSection(r, s, sec) {
				return nil
			}
			if match(it) {
				return it
			}
		}
		return nil
	}
	for i := range items {
		it := &items[i]
		if it.Off <= off || it.Proof || it.InProof {
			continue
		}
		if seen++; seen > positionWindow {
			return nil
		}
		if match(it) {
			return it
		}
	}
	return nil
}

// nextSection tells if section s is sec or the section just before it.
func nextSection(r *resolver, s, sec int) bool {
	if s == sec {
		return true
	}
	nodes := r.in.Doc.Tree.Nodes
	for i := sec - 1; i >= 0; i-- {
		if nodes[i].Kind == "section" {
			return i == s
		}
	}
	return false
}

var (
	// scopeOpen is the start of a standing assumption.
	scopeOpen    = regexp.MustCompile(`(?i)^\W*(throughout|from now on|for the rest of|in the rest of|unless (?:otherwise )?(?:stated|specified|mentioned))`)
	scopeSection = regexp.MustCompile(`(?i)\bthis\s+(?:sub)*section\b`)
)

// scopes is method 3: a context item, as a standing assumption, whose scope
// holds an item. The scope is the section of the context item when its text
// says "this section", and the rest of the paper otherwise. Only the
// statements and the definitions get the edge. A proof has it through the
// statement that it proves.
func (r *resolver) scopes() {
	d := r.in.Doc
	for i := range r.in.Items {
		ctx := &r.in.Items[i]
		if ctx.Proof || ctx.InProof || ctx.Close <= ctx.Inner {
			continue
		}
		body := d.Text[ctx.Inner:ctx.Close]
		head, _, _ := strings.Cut(ctx.Kind, "/")
		if head != "context" && !scopeOpen.Match(body) {
			continue
		}
		end := d.End
		if scopeSection.Match(body) {
			if s := d.Tree.Section(ctx.Off); s >= 0 {
				end = d.Tree.Nodes[s].To
			}
		} else if head == "context" && !scopeOpen.Match(body) {
			// A setting with no word of scope holds its section.
			if s := d.Tree.Section(ctx.Off); s >= 0 {
				end = d.Tree.Nodes[s].To
			}
		}
		file, line := r.where(ctx.Off)
		for j := i + 1; j < len(r.in.Items); j++ {
			it := &r.in.Items[j]
			if it.Off >= end {
				break
			}
			if it.Off < ctx.End || it.Proof || it.InProof {
				continue
			}
			h, _, _ := strings.Cut(it.Kind, "/")
			if h != "statement" && h != "definition" {
				continue
			}
			r.add(Edge{From: r.id(it.Local), To: r.id(ctx.Local), Predicate: Uses, Method: Scope, Confidence: High, Evidence: Evidence{File: file, Line: line, Cue: "scope"}})
		}
	}
}

var (
	emphCmd   = regexp.MustCompile(`\\(?:emph|textit|textbf|textsl|defn|dfn|df|term|textsc|index)\s*\{([^{}$\\]{3,60})\}`)
	emphGroup = regexp.MustCompile(`\{\\(?:em|it|bf|sl)\s+([^{}$\\]{3,60})\}`)
	// mathSpan is the math of the text, where a term is not read.
	mathSpan = regexp.MustCompile(`(?s)\$\$.*?\$\$|\$[^$]*\$|\\\(.*?\\\)|\\\[.*?\\\]`)
)

// commonTerms are the words that a definition often emphasizes and that are
// used in each paper, so a use of one is no evidence of an edge.
var commonTerms = map[string]bool{
	"set": true, "map": true, "space": true, "group": true, "ring": true, "field": true, "function": true, "graph": true, "point": true, "line": true, "curve": true, "module": true, "algebra": true, "category": true, "measure": true, "metric": true, "norm": true, "order": true, "morphism": true, "sequence": true, "series": true, "operator": true, "matrix": true, "vector": true, "element": true, "subset": true, "number": true, "integer": true, "the": true, "and": true, "not": true, "for": true, "all": true, "iff": true, "if and only if": true, "only if": true, "i.e.": true, "e.g.": true, "we say": true, "called": true, "respectively": true, "said": true, "satisfies": true, "such that": true, "there exists": true, "for all": true, "any": true, "each": true, "every": true, "note": true, "definition": true,
}

// terms is method 7: a term that a definition of the paper defines, used in a
// later item. The term is what the definition emphasizes, and the name of the
// definition. A use goes to the nearest definition of the term before it.
func (r *resolver) terms() {
	d := r.in.Doc
	defs := map[string][]*Item{}
	addTerm := func(t string, def *Item) {
		t = strings.ToLower(strings.Join(strings.Fields(t), " "))
		t = strings.Trim(t, " .,;:")
		if len(t) < 3 || commonTerms[t] || len(strings.Fields(t)) > 5 {
			return
		}
		for _, x := range defs[t] {
			if x == def {
				return
			}
		}
		defs[t] = append(defs[t], def)
	}
	for i := range r.in.Items {
		it := &r.in.Items[i]
		if !strings.HasPrefix(it.Kind, "definition") || it.Proof || it.Close <= it.Inner {
			continue
		}
		body := d.Text[it.Inner:it.Close]
		for _, re := range []*regexp.Regexp{emphCmd, emphGroup} {
			for _, m := range re.FindAllSubmatch(body, -1) {
				addTerm(string(m[1]), it)
			}
		}
		if it.Name != "" && !strings.ContainsAny(it.Name, `\$`) {
			addTerm(it.Name, it)
		}
	}
	if len(defs) == 0 {
		return
	}
	var alts []string
	for t := range defs {
		alts = append(alts, regexp.QuoteMeta(t))
	}
	for i := 1; i < len(alts); i++ {
		for j := i; j > 0 && (len(alts[j]) > len(alts[j-1]) || len(alts[j]) == len(alts[j-1]) && alts[j] < alts[j-1]); j-- {
			alts[j], alts[j-1] = alts[j-1], alts[j]
		}
	}
	re := regexp.MustCompile(`(?i)\b(` + strings.Join(alts, "|") + `)(?:s|es)?\b`)
	text := append([]byte(nil), d.Text[d.Body:d.End]...)
	for _, m := range mathSpan.FindAllIndex(text, -1) {
		for k := m[0]; k < m[1]; k++ {
			if text[k] != '\n' {
				text[k] = ' '
			}
		}
	}
	for _, m := range re.FindAllSubmatchIndex(text, -1) {
		off := d.Body + m[0]
		src := r.from(off)
		if src == nil || off < src.Inner {
			continue
		}
		t := strings.ToLower(string(text[m[2]:m[3]]))
		var def *Item
		for _, x := range defs[t] {
			if x.Off < off && (def == nil || x.Off > def.Off) {
				def = x
			}
		}
		if def == nil || def == src || r.inside(src, def) {
			continue
		}
		// The definition that defines the term again is not a use of it.
		if def.End > off {
			continue
		}
		file, line := r.where(off)
		r.add(Edge{From: r.id(src.Local), To: r.id(def.Local), Predicate: Uses, Method: Term, Confidence: Medium, Evidence: Evidence{File: file, Line: line, Cue: "term"}})
	}
}

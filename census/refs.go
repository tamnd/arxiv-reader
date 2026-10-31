package census

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/tamnd/arxiv-reader/theorems"
)

// Text references of AX4.2: "Lemma 3.2", "Theorems 2.1 and 2.4", "Lemma
// \ref{lem:key}".

var (
	refNumber = `(?:\d+(?:\.\d+)*[a-z]?|[A-Z](?:\.\d+)*)\b`
	refLabel  = `\\(?:ref|cref|autoref)\s*\{([^{}]+)\}`
	// citeSpan is a cite with a locator: \cite[Theorem 2]{key}, or the form
	// \cite{key}*{Theorem 2} of amsrefs.
	citeSpan = regexp.MustCompile(`\\cite[a-zA-Z]*\*?\s*(?:\[[^\]]*\]\s*){1,2}\{[^{}]*\}|\\cite[a-zA-Z]*\s*\{[^{}]*\}\s*\*\s*\{[^{}]*\}`)
	// citeComma is a cite and a comma just before a reference, as in
	// (\cite{key}, Theorem 6).
	citeComma = regexp.MustCompile(`\\cite[a-zA-Z]*\*?\s*(?:\[[^\]]*\]\s*){0,2}\{[^{}]*\}\s*[,;]\s*(?:~|\s)*$`)
	afterCite = regexp.MustCompile(`^\s*(?:of|in|from|de|du|aus)\s*(?:~|\s)*(?:\\cite|\[|the\s+paper|\\citet|\\citep|ref\.)`)
	beforeRef = regexp.MustCompile(`(?:\[|\\cite[a-zA-Z]*\s*\[)[^\]]{0,30}$`)
)

// Textref is one text reference, read for the check of D09.
type textref struct {
	off    int
	title  string
	number string
	label  string
}

func (c *Census) textrefs() {
	d := c.In.Doc
	titles := map[string]bool{}
	for _, decl := range c.In.Set.List() {
		if decl.Proof || decl.Title == "" {
			continue
		}
		t := strings.TrimSpace(decl.Title)
		if strings.ContainsAny(t, `\{}$`) || len(t) > 40 {
			continue
		}
		titles[t] = true
	}
	for _, o := range c.Occurrences {
		if o.Type == Head && o.Title != "" {
			titles[o.Title] = true
		}
	}
	if len(titles) == 0 {
		return
	}
	var alts []string
	for t := range titles {
		alts = append(alts, regexp.QuoteMeta(t))
	}
	sortLongest(alts)
	re := regexp.MustCompile(`(?i:\b(` + strings.Join(alts, "|") + `)s?)\b\.?(?:~|\s)*(?:(` + refNumber + `(?:(?:\s*,\s*|\s+and\s+|\s*--?\s*|\s+or\s+)` + refNumber + `)*)|` + refLabel + `)`)
	one := regexp.MustCompile(refNumber)
	body := d.Text[d.Body:d.End]
	cites := citeSpan.FindAllIndex(body, -1)
	inCite := func(i int) bool {
		for _, s := range cites {
			if i >= s[0] && i < s[1] {
				return true
			}
		}
		return false
	}
	var refs []textref
	for _, m := range re.FindAllSubmatchIndex(body, -1) {
		if inCite(m[0]) || beforeRef.Match(body[max(0, m[0]-40):m[0]]) || citeComma.Match(body[max(0, m[0]-120):m[0]]) {
			continue
		}
		if afterCite.Match(body[m[1]:min(len(body), m[1]+40)]) {
			continue
		}
		// A head of an item is not a reference to it.
		if c.isHead(d.Body + m[0]) {
			continue
		}
		title := string(body[m[2]:m[3]])
		if m[6] >= 0 {
			refs = append(refs, textref{off: d.Body + m[0], title: title, label: strings.TrimSpace(string(body[m[6]:m[7]]))})
			continue
		}
		if m[4] < 0 {
			continue
		}
		list := string(body[m[4]:m[5]])
		nums := one.FindAllString(list, -1)
		// "Theorems 2.1--2.4" names a range. Only the two ends are read.
		for _, n := range nums {
			refs = append(refs, textref{off: d.Body + m[0], title: title, number: n})
		}
	}
	for _, r := range refs {
		o := &Occurrence{Type: Textref, Off: r.off, Inner: r.off, End: r.off, Pos: d.Pos(r.off), Title: r.title}
		if r.label != "" {
			o.Target = r.title + " " + `\ref{` + r.label + `}`
		} else {
			o.Target = r.title + " " + r.number
		}
		o.Result = c.resolveRef(r)
		c.Occurrences = append(c.Occurrences, o)
	}
}

// isHead tells if the offset is the head of an env or a head occurrence.
func (c *Census) isHead(off int) bool {
	for _, o := range c.Occurrences {
		if (o.Type == Env || o.Type == Head || o.Type == Proof) && off >= o.Off && off < o.Inner {
			return true
		}
	}
	return false
}

// resolveRef says how a text reference resolves: resolved, or one of the
// three reasons of AX4.4 for an unresolved one.
func (c *Census) resolveRef(r textref) string {
	if r.label != "" {
		for _, l := range strings.Split(r.label, ",") {
			if _, ok := c.Labels[strings.TrimSpace(l)]; !ok {
				if c.labelled(strings.TrimSpace(l)) {
					return "unresolved:not-an-item"
				}
				return "unresolved:no-label"
			}
		}
		return "resolved"
	}
	kind := c.In.Titles.Kind(r.title)
	anyNumber := false
	for _, o := range c.Occurrences {
		if o.Type != Env && o.Type != Head {
			continue
		}
		if o.Number == r.number {
			anyNumber = true
			if theorems.Normalize(o.Title) == theorems.Normalize(r.title) || o.Item == kind || sameHead(o.Item, kind) {
				return "resolved"
			}
		}
	}
	if anyNumber {
		// The number exists with another title: an author who wrote "Lemma 3"
		// for Proposition 3. Counted as resolved, with the reason kept.
		return "resolved:other-title"
	}
	if !c.numbered() {
		return "unresolved:no-numbers"
	}
	return "unresolved:no-item"
}

// sameHead tells if two item kinds have the same head, as statement/lemma and
// statement/theorem.
func sameHead(a, b string) bool {
	ah, _, _ := strings.Cut(a, "/")
	bh, _, _ := strings.Cut(b, "/")
	return ah == bh && ah != "" && ah != theorems.Unknown
}

// labelled tells if a \label with the key exists anywhere in the body.
func (c *Census) labelled(key string) bool {
	for _, o := range c.Occurrences {
		if o.Type == Label && o.Target == key {
			return true
		}
	}
	d := c.In.Doc
	return strings.Contains(string(d.Text[d.Body:d.End]), `\label{`+key+`}`)
}

// numbered tells if the model gave a number to the env occurrences.
func (c *Census) numbered() bool {
	for _, o := range c.Occurrences {
		if o.Type == Env && o.Number != "" {
			return true
		}
	}
	return false
}

func sortLongest(xs []string) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && (len(xs[j]) > len(xs[j-1]) || len(xs[j]) == len(xs[j-1]) && xs[j] < xs[j-1]); j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}

// sectionCounter is the counters of the section heads.
var sectionCounter = map[string]bool{"part": true, "chapter": true, "section": true, "subsection": true, "subsubsection": true, "paragraph": true, "subparagraph": true}

// checks runs the checks of AX4.4: agreement and contiguity, and the text
// references.
func (c *Census) checks() {
	// Contiguity: in each counter and each reset scope, 1, 2, 3 with no gap.
	type scope struct{ counter, prefix string }
	last := map[scope]int{}
	lastID := map[scope]string{}
	for _, o := range c.Items() {
		if o.Type != Env || o.Counter == "" || c.touched[o.Counter] || o.HasFlag("conditional") || sectionCounter[o.Counter] || o.Counter == "equation" {
			// The heads of a section counter step it too, as in a class that
			// numbers its theorems with the subsections, so a gap there is
			// a head. Seen in the gold pilot (set X). The equations step the
			// equation counter between the items that share it.
			continue
		}
		n := o.Number
		if n == "" {
			continue
		}
		prefix, tail := "", n
		if i := strings.LastIndexAny(n, ".-"); i >= 0 {
			prefix, tail = n[:i], n[i+1:]
		}
		v, ok := ordinal(tail)
		if !ok {
			continue
		}
		s := scope{o.Counter, prefix}
		if prev, seen := last[s]; seen && v != prev+1 && v != prev {
			c.Findings = append(c.Findings, Finding{Rule: "D07", Occurrence: o.ID, Message: fmt.Sprintf("%s %s comes after %s in counter %s", o.Title, n, lastID[s], o.Counter)})
		} else if !seen && v != 1 {
			c.Findings = append(c.Findings, Finding{Rule: "D07", Occurrence: o.ID, Message: fmt.Sprintf("%s %s is the first in its scope of counter %s", o.Title, n, o.Counter)})
		}
		last[s] = v
		lastID[s] = n
	}
	for _, o := range c.Occurrences {
		if o.Type == Textref && strings.HasPrefix(o.Result, "unresolved") {
			c.Findings = append(c.Findings, Finding{Rule: "D09", Occurrence: o.ID, Message: fmt.Sprintf("the text names %s and the census has no such item (%s)", o.Target, strings.TrimPrefix(o.Result, "unresolved:"))})
		}
	}
}

func ordinal(s string) (int, bool) {
	if v, err := strconv.Atoi(s); err == nil {
		return v, true
	}
	if len(s) == 1 && s[0] >= 'A' && s[0] <= 'Z' {
		return int(s[0]-'A') + 1, true
	}
	return 0, false
}

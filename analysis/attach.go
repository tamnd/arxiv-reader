package analysis

import (
	"regexp"
	"slices"
	"strings"

	"github.com/tamnd/arxiv-reader/tex"
	"github.com/tamnd/arxiv-reader/theorems"
)

var (
	headNumber = regexp.MustCompile(`(?i)^(?:the\s+)?([A-Za-z][A-Za-z -]{0,30}?)\s*~?\s*(\d+(?:\.\d+)*[a-z]?|(?-i:[A-Z])(?:\.\d+)*)\b`)
	mathRef    = regexp.MustCompile(`\$[^$]{1,30}\$`)
	mainName   = regexp.MustCompile(`(?i)^(?:the\s+)?main\s+(theorem|result|lemma|proposition)s?$`)
	otherProof = regexp.MustCompile(`(?i)\b(second|alternative|another|third|new)\s+proof\b|^\s*proof\s+(?:\d+|[ivx]+|[a-z])\s*[.:]?\s*$`)
	citeNear   = regexp.MustCompile(`\\cite[a-zA-Z]*\*?\s*(?:\[[^\]]*\]\s*){0,2}\{[^{}]*\}`)
	sentence   = regexp.MustCompile(`[.!?](?:\s|$)`)
)

// attach attaches each proof to what it proves, by the first rule of AX3.6
// that applies.
func (b *Builder) attach(d *tex.Document, items []*item) {
	byLabel := map[string]*item{}
	partLabel := map[string][2]string{}
	for _, it := range items {
		for _, l := range it.rec.Labels {
			if _, ok := byLabel[l]; !ok {
				byLabel[l] = it
			}
		}
		for _, p := range it.rec.Parts {
			if p.Label != "" {
				partLabel[p.Label] = [2]string{it.rec.Local, p.Marker}
			}
		}
	}
	for i, it := range items {
		if !it.proof {
			continue
		}
		r := &it.rec
		target := expandRefs(d.Macros, it.opt)
		// "Proof that Lemma 1 implies Theorem 2" proves Theorem 2.
		if m := impliesIn.FindStringIndex(target); m != nil && refIn.MatchString(target[m[1]:]) {
			target = target[m[1]:]
		}
		// Rule 1: a reference to a label in the head.
		m := refIn.FindStringSubmatch(target)
		if m == nil && it.wrapRef != "" {
			m = refIn.FindStringSubmatch(expandRefs(d.Macros, it.wrapRef))
		}
		if m != nil {
			key := strings.TrimSpace(strings.Split(m[1], ",")[0])
			if t, ok := byLabel[key]; ok && t != it {
				r.Proves, r.Attach, r.Confidence = t.rec.Local, 1, "certain"
				if p, ok := partLabel[key]; ok {
					r.ProvesPart = p[1]
				}
				continue
			}
			if p, ok := partLabel[key]; ok {
				r.Proves, r.ProvesPart, r.Attach, r.Confidence = p[0], p[1], 1, "certain"
				continue
			}
		}
		// Rule 2: a number or a name in the head. A label such as A1 names the
		// last claim with that label before the proof.
		if t := lastClaim(items, i, theorems.LabelKey(stripProofOf(target))); t != nil {
			r.Proves, r.Attach, r.Confidence = t.rec.Local, 2, "high"
			continue
		}
		if t := b.byNumber(items, it, target); t != nil && t != it {
			r.Proves, r.Attach, r.Confidence = t.rec.Local, 2, "high"
			continue
		}
		if t := b.theLast(items, i, target); t != nil {
			r.Proves, r.Attach, r.Confidence = t.rec.Local, 2, "medium"
			continue
		}
		// The sentence before the proof names what it proves, as "We are now
		// able to prove Theorem \ref{main}." or \renewcommand{\proofname}{Proof
		// of \prref{cat}}.
		if t := leadIn(d, items, i, byLabel); t != nil {
			r.Proves, r.Attach, r.Confidence = t.rec.Local, 2, "high"
			r.Flags = append(r.Flags, "lead-in")
			continue
		}
		prev := previous(items, i)
		// Rule 5: a part of the statement before it.
		if m := partOf.FindStringSubmatch(stripProofOf(target)); m != nil && prev != nil && isStatement(prev) {
			r.Proves, r.ProvesPart, r.Attach, r.Confidence = prev.rec.Local, m[1], 5, "high"
			continue
		}
		near, floats := false, false
		if prev != nil {
			near, floats = adjacent(d, prev, it)
		}
		if near {
			switch {
			case isStatement(prev):
				// Rule 3: right after a statement.
				r.Proves, r.Attach, r.Confidence = prev.rec.Local, 3, "certain"
				if floats {
					r.Confidence = "high"
				}
				continue
			case !prev.proof && remarkBetween(d, items, prev) != nil:
				// A remark between a statement and its proof, as "\\end{lemma}
				// \\begin{remark} We will later apply this ... \\end{remark}
				// \\begin{proof}". The proof proves the statement.
				t := remarkBetween(d, items, prev)
				r.Proves, r.Attach, r.Confidence = t.rec.Local, 3, "medium"
				r.Flags = append(r.Flags, "remark-between")
				continue
			case !prev.proof:
				// Rule 4: right after a definition, a remark or an example.
				r.Proves, r.Attach, r.Confidence = prev.rec.Local, 4, "high"
				r.Flags = append(r.Flags, "proves-non-statement")
				continue
			case prev.proof && otherProof.MatchString(target) && prev.rec.Proves != "":
				// A second proof of the same statement.
				r.Proves, r.Attach, r.Confidence = prev.rec.Proves, 3, "high"
				continue
			}
		}
		// A proof after a statement with a short text between them, as "In
		// particular, C(G) is not trivial.", proves the statement.
		if prev != nil && isStatement(prev) && shortGap(d, prev, it) {
			r.Proves, r.Attach, r.Confidence = prev.rec.Local, 3, "medium"
			r.Flags = append(r.Flags, "text-between")
			continue
		}
		// A proof after a statement, with paragraphs of remarks and no
		// environment or section between, proves it. A statement written by
		// hand ends where the paper does not say, and its end is a guess at
		// the first blank line, as "{\bf Proposition 9.2.3} ... (4) ... {\bf
		// Proof}: (1) ...".
		if prev != nil && isStatement(prev) && (prev.head == nil || !prev.head.EndMark) && longGap(d, prev, it) {
			r.Proves, r.Attach, r.Confidence = prev.rec.Local, 3, "medium"
			r.Flags = append(r.Flags, "text-between")
			continue
		}
		// A proof inside a statement, with no item before it there, proves
		// the statement, as a paper writes \begin{proof} before
		// \end{theorem}.
		if prev == nil && it.parent != nil && isStatement(it.parent) {
			r.Proves, r.Attach, r.Confidence = it.parent.rec.Local, 3, "high"
			continue
		}
		// A proof inside a remark or an example proves it, as a remark that
		// lists facts, each with its proof, and so does each proof after it
		// there.
		if it.parent != nil && !it.parent.proof && (prev == nil || prev.proof && prev.rec.Proves == it.parent.rec.Local) {
			r.Proves, r.Attach, r.Confidence = it.parent.rec.Local, 4, "medium"
			r.Flags = append(r.Flags, "proves-non-statement")
			continue
		}
		// Rule 6: an orphan. AX7.6 proposes a target.
		r.Flags = append(r.Flags, "orphan")
	}
}

// leadWords is a sentence that leads into a proof and names what it proves.
var leadWords = regexp.MustCompile(`(?i)\b(?:prove|proof|derive|establish)[a-z]*\b[^.;]{0,80}?\\ref\s*\{([^{}]+)\}`)

// leadIn gives the statement that the last paragraph before a proof names
// with a verb of proving and a reference, or nil. The paragraph is after the
// item before the proof, and at most 400 bytes long.
func leadIn(d *tex.Document, items []*item, i int, byLabel map[string]*item) *item {
	it := items[i]
	from := max(d.Body, it.off-400)
	for j := i - 1; j >= 0; j-- {
		if items[j].end <= it.off {
			from = max(from, items[j].end)
			break
		}
	}
	if from >= it.off {
		return nil
	}
	text := string(d.Text[from:it.off])
	if k := strings.LastIndex(strings.TrimRight(text, " \t\r\n"), "\n\n"); k >= 0 {
		text = text[k:]
	}
	ms := leadWords.FindAllStringSubmatch(expandRefs(d.Macros, text), -1)
	if ms == nil {
		return nil
	}
	key := strings.TrimSpace(strings.Split(ms[len(ms)-1][1], ",")[0])
	if t, ok := byLabel[key]; ok && t != it && isStatement(t) {
		return t
	}
	return nil
}

// refCall is a call of a command with one argument.
var refCall = regexp.MustCompile(`(\\[A-Za-z]+)\s*\{([^{}]*)\}`)

// expandRefs writes out each command of the paper that makes a reference, as
// \newcommand{\refmt}[1]{Theorem~\ref{mt:#1}} in math/9902147v1, so that the
// head names the label.
var impliesIn = regexp.MustCompile(`\bimpl(?:y|ies)\b`)

func expandRefs(m *tex.Macros, s string) string {
	if m == nil || !strings.Contains(s, `\`) {
		return s
	}
	return refCall.ReplaceAllStringFunc(s, func(call string) string {
		x := refCall.FindStringSubmatch(call)
		c := m.Commands[x[1]]
		if c == nil || c.Args != 1 || !refIn.MatchString(c.Body) {
			return call
		}
		return strings.ReplaceAll(c.Body, "#1", x[2])
	})
}

func stripProofOf(s string) string {
	s = strings.TrimSpace(s)
	low := strings.ToLower(s)
	for _, p := range []string{"proof of", "proof"} {
		if strings.HasPrefix(low, p) {
			return s[len(p):]
		}
	}
	return s
}

// nameKey is the name of a statement or the name in the head of a proof, in
// lower case and with no command, as "computational adequacy" for "Proof of
// computational adequacy \textnormal{(...)}".
func nameKey(s string) string {
	if i := strings.IndexAny(s, `\($`); i >= 0 {
		s = s[:i]
	}
	return strings.Join(strings.Fields(strings.Trim(strings.ToLower(s), " .:;,")), " ")
}

// byName gives the one statement whose name is the head of a proof, as
// \begin{proof}[Proof of computational adequacy] after
// \begin{therm}[Computational adequacy], or nil.
func byName(items []*item, h string) *item {
	k := nameKey(h)
	if len(k) < 6 {
		return nil
	}
	var found *item
	for _, it := range items {
		if it.proof || !it.hasOpt || !isStatement(it) || nameKey(it.opt) != k {
			continue
		}
		if found != nil {
			return nil
		}
		found = it
	}
	return found
}

// theHead is the head of a proof that names a kind and no number, as "Proof
// of the theorem".
var theHead = regexp.MustCompile(`(?i)^\s*(?:proof\s+)?of\s+the\s+([a-z]+)\s*[.:]?\s*$`)

// theLast gives the last statement of the kind that the head of items[i]
// names with no number, as "Proof of the theorem", when no proof before
// items[i] proves it, or nil.
func (b *Builder) theLast(items []*item, i int, head string) *item {
	m := theHead.FindStringSubmatch(head)
	if m == nil {
		return nil
	}
	kind := b.Titles.Kind(m[1])
	if kind == "" || !strings.HasPrefix(kind, "statement") {
		return nil
	}
	for j := i - 1; j >= 0; j-- {
		t := items[j]
		if t.proof || t.rec.Item != kind {
			continue
		}
		for _, p := range items[j+1 : i] {
			if p.proof && p.rec.Proves == t.rec.Local {
				return nil
			}
		}
		return t
	}
	return nil
}

// lastClaim is the last claim before items[i] whose number is label.
func lastClaim(items []*item, i int, label string) *item {
	if label == "" {
		return nil
	}
	for k := i - 1; k >= 0; k-- {
		if it := items[k]; !it.proof && it.rec.Item == "statement/claim" && it.rec.Number == label && it.head != nil && it.head.Form == "label" {
			return it
		}
	}
	return nil
}

// byNumber resolves "Lemma 3.2", "Theorem A" or "the Main Theorem" in a head
// to an item with that title and number. Of two items with the title and the
// number, the one in the item that holds the proof comes first, as the proof
// of Claim 1 in a proof proves the claim 1 of that proof.
func (b *Builder) byNumber(items []*item, proof *item, head string) *item {
	h := strings.TrimSpace(stripProofOf(head))
	h = strings.TrimPrefix(strings.TrimPrefix(h, "of "), "for ")
	if h == "" {
		return nil
	}
	// "Theorem $A$" names the head that prints Theorem A.
	h = mathRef.ReplaceAllStringFunc(h, theorems.MathNumber)
	if m := headNumber.FindStringSubmatch(h); m != nil {
		title, number := strings.TrimSpace(m[1]), m[2]
		kind := b.Titles.Kind(title)
		var same, any *item
		for _, it := range items {
			if it.proof || it.rec.Number != number {
				continue
			}
			if theorems.Normalize(it.rec.Title) == theorems.Normalize(title) || it.rec.Item == kind {
				if proof != nil && it.parent != nil && it.parent == proof.parent {
					return it
				}
				if same == nil {
					same = it
				}
				continue
			}
			if any == nil {
				any = it
			}
		}
		if same != nil {
			return same
		}
		return any
	}
	if t := byName(items, h); t != nil {
		return t
	}
	if mainName.MatchString(h) {
		var found *item
		for _, it := range items {
			if it.proof {
				continue
			}
			n := strings.ToLower(it.opt)
			if strings.Contains(n, "main") || strings.Contains(strings.ToLower(it.rec.Env), "main") {
				if found != nil {
					return nil
				}
				found = it
			}
		}
		return found
	}
	return nil
}

// remarkBetween gives the statement right before the remark prev, or nil.
func remarkBetween(d *tex.Document, items []*item, prev *item) *item {
	if !strings.HasPrefix(prev.rec.Item, "remark") {
		return nil
	}
	j := slices.Index(items, prev)
	if j < 0 {
		return nil
	}
	pp := previous(items, j)
	if pp == nil || !isStatement(pp) || citedStatement(d, pp) {
		return nil
	}
	if ok, _ := adjacent(d, pp, prev); !ok {
		return nil
	}
	return pp
}

// citedStatement tells if a statement is a result of another paper, which
// names its source in the head or in its first words, as "\begin{lem}
// \cite{dani} If ...".
func citedStatement(d *tex.Document, it *item) bool {
	if len(it.rec.CitesInHead) > 0 {
		return true
	}
	if it.close <= it.inner {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(string(d.Text[it.inner:it.close])), `\cite`)
}

// previous is the item before items[i] at the same depth, or nil.
func previous(items []*item, i int) *item {
	it := items[i]
	for j := i - 1; j >= 0; j-- {
		p := items[j]
		if p.rec.Origin == "restated" && p.end == p.inner {
			// A restatement is a statement that a proof can follow.
			if p.parent == it.parent {
				return p
			}
			continue
		}
		if p.parent == it.parent {
			return p
		}
	}
	return nil
}

// floatEnv is a float environment. TeX moves a float, so a float between a
// statement and a proof does not separate them.
var floatEnv = regexp.MustCompile(`(?s)\\begin\s*\{(figure|table|wrapfigure|wraptable|algorithm|marginfigure|SCfigure|longtable)(\*?)\}.*?\\end\s*\{(?:figure|table|wrapfigure|wraptable|algorithm|marginfigure|SCfigure|longtable)\*?\}`)

// adjacent tells if only spacing, labels and end marks are between two items.
// floats is true when a float is between them too.
func adjacent(d *tex.Document, a, b *item) (ok, floats bool) {
	if a.end > b.off {
		return false, false
	}
	gap := d.Text[a.end:b.off]
	if gapOK.Match(gap) {
		return true, false
	}
	if len(gap) > 20000 || !floatEnv.Match(gap) {
		return false, false
	}
	return gapOK.Match(floatEnv.ReplaceAll(gap, nil)), true
}

// gapBreak is what ends the run of a statement into its proof: an
// environment, a section or a display of the text.
var gapBreak = regexp.MustCompile(`\\(?:begin|section|subsection|subsubsection|chapter|paragraph)\b|\n[ \t\r]*\n[ \t\r]*\n`)

// shortGap tells if the text between two items is at most two short
// paragraphs with no environment and no section.
func shortGap(d *tex.Document, a, b *item) bool {
	if a.end > b.off || b.off-a.end > 600 {
		return false
	}
	gap := d.Text[a.end:b.off]
	if gapBreak.Match(gap) {
		return false
	}
	return strings.Count(strings.ReplaceAll(strings.TrimSpace(string(gap)), "\r", ""), "\n\n") <= 1
}

// partBreak is a break in the text that a statement written by hand does
// not run over: an environment or a section.
var partBreak = regexp.MustCompile(`\\(?:begin|section|subsection|subsubsection|chapter|paragraph)\b`)

// displayEnv is a display of mathematics, which is a part of a paragraph.
var displayEnv = regexp.MustCompile(`(?s)\\begin\s*\{(equation|align|gather|multline|eqnarray|displaymath|flalign|alignat)(\*?)\}.*?\\end\s*\{(?:equation|align|gather|multline|eqnarray|displaymath|flalign|alignat)\*?\}`)

// longGap tells if b follows a with up to a few paragraphs and no
// environment or section between them. A display of mathematics in the
// paragraphs does not break them, nor does a float.
func longGap(d *tex.Document, a, b *item) bool {
	if a.end > b.off || b.off-a.end > 4000 {
		return false
	}
	gap := displayEnv.ReplaceAll(d.Text[a.end:b.off], nil)
	return !partBreak.Match(floatEnv.ReplaceAll(gap, nil))
}

func isStatement(it *item) bool {
	return strings.HasPrefix(it.rec.Item, "statement") || it.rec.Kind == "statement"
}

// status sets the proof_status of each statement (AX3.6).
func (b *Builder) status(d *tex.Document, items []*item) {
	proved := map[string]bool{}
	byLocal := map[string]*item{}
	first := map[string]*item{}
	for _, it := range items {
		byLocal[it.rec.Local] = it
		if it.restate != "" && it.rec.Origin != "restated" {
			first[it.restate] = it
		}
	}
	for _, it := range items {
		if !it.proof || it.rec.Proves == "" {
			continue
		}
		proved[it.rec.Proves] = true
		// A proof of a restatement proves the first print of the item too.
		if t := byLocal[it.rec.Proves]; t != nil && t.rec.Origin == "restated" {
			if f := first[t.restate]; f != nil {
				proved[f.rec.Local] = true
			}
		}
	}
	// The sections whose title is a proof head, as "Proof of Theorem 1.2".
	elsewhere := map[string]bool{}
	byLabel := map[string]*item{}
	for _, it := range items {
		for _, l := range it.rec.Labels {
			byLabel[l] = it
		}
	}
	for _, h := range theorems.ProofSections(d, b.Titles) {
		if m := refIn.FindStringSubmatch(h.Target); m != nil {
			for k := range strings.SplitSeq(m[1], ",") {
				if t, ok := byLabel[strings.TrimSpace(k)]; ok {
					elsewhere[t.rec.Local] = true
				}
			}
			continue
		}
		if t := b.byNumber(items, nil, h.Target); t != nil {
			elsewhere[t.rec.Local] = true
		}
	}
	for _, it := range items {
		if !isStatement(it) && it.rec.Kind != "problem" {
			continue
		}
		r := &it.rec
		after := textAfter(d, it.end, 2)
		switch {
		case proved[r.Local]:
			r.ProofStatus = "here"
		case r.Origin == "restated":
			r.ProofStatus = "restated"
		case elsewhere[r.Local]:
			r.ProofStatus = "elsewhere"
		case len(r.CitesInHead) > 0 || citeNear.MatchString(after) && startsWithCite(after):
			r.ProofStatus = "cited"
		default:
			if _, ok := b.Phrases.Find("omitted", after); ok {
				r.ProofStatus = "omitted"
			} else if _, ok := b.Phrases.Find("elsewhere", after); ok {
				r.ProofStatus = "elsewhere"
			} else {
				r.ProofStatus = "none"
			}
		}
		if r.Kind == "problem" {
			// A conjecture or a problem has no proof by its nature. The value
			// says if the paper proves it here.
			if r.ProofStatus != "here" {
				r.ProofStatus = "none"
			}
		}
	}
}

// startsWithCite tells if the first sentence after an item cites a source for
// it, as "See [12, Thm 4]" or "This is due to \cite{S}".
func startsWithCite(after string) bool {
	first := after
	if m := sentence.FindStringIndex(after); m != nil {
		first = after[:m[1]]
	}
	low := strings.ToLower(first)
	if !citeNear.MatchString(first) {
		return false
	}
	for _, w := range []string{"see", "due to", "proved in", "proven in", "proof can be found", "for a proof", "for the proof", "cf.", "is from", "follows from"} {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

// textAfter is the text of the next n sentences after an offset.
func textAfter(d *tex.Document, off, n int) string {
	end := min(off+600, d.End)
	if off >= end {
		return ""
	}
	s := string(d.Text[off:end])
	at := 0
	for i := 0; i < n; i++ {
		m := sentence.FindStringIndex(s[at:])
		if m == nil {
			return s
		}
		at += m[1]
	}
	return s[:at]
}

// origin sets the origin of each item (AX3.7).
func (b *Builder) origin(items []*item) {
	byHash := map[string]*item{}
	byRestate := map[string]*item{}
	for _, it := range items {
		if it.restate != "" && it.rec.Origin != "restated" {
			byRestate[it.restate] = it
		}
	}
	for _, it := range items {
		r := &it.rec
		if it.proof {
			continue
		}
		switch {
		case r.Origin == "restated":
			if first, ok := byRestate[it.restate]; ok {
				r.Restates = first.rec.Local
			}
		case b.restatedHead(it.opt):
			r.Origin = "restated"
		case it.normLen >= 40 && byHash[r.Hash] != nil:
			r.Origin = "restated"
			r.Restates = byHash[r.Hash].rec.Local
		case len(r.CitesInHead) > 0 || r.ProofStatus == "cited":
			r.Origin = "cited"
		default:
			if _, ok := b.Phrases.Find("folklore", it.opt); ok {
				r.Origin = "folklore"
			} else {
				r.Origin = "own"
			}
		}
		if r.Origin == "restated" && r.ProofStatus != "" && r.ProofStatus != "here" {
			r.ProofStatus = "restated"
		}
		if it.normLen >= 40 && byHash[r.Hash] == nil {
			byHash[r.Hash] = it
		}
	}
}

func (b *Builder) restatedHead(opt string) bool {
	if opt == "" {
		return false
	}
	_, ok := b.Phrases.Find("restated", opt)
	return ok || regexp.MustCompile(`=\s*(?:Theorem|Lemma|Proposition|Corollary)\s`).MatchString(opt)
}

package analysis

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/tamnd/arxiv-reader/objects"
	"github.com/tamnd/arxiv-reader/tex"
	"github.com/tamnd/arxiv-reader/theorems"
)

var (
	headNumber = regexp.MustCompile(`(?i)^(?:the\s+)?([A-Za-z][A-Za-z -]{0,30}?)\s*~?\s*(\d+(?:\.\d+)*[a-z]?|(?-i:[A-Z])(?:\.\d+)*)\b`)
	mainName   = regexp.MustCompile(`(?i)^(?:the\s+)?main\s+(theorem|result|lemma|proposition)s?$`)
	// proofOrdinal is a head that numbers the proofs of one statement:
	// Proof 1, Proof II, First proof.
	proofOrdinal = regexp.MustCompile(`(?i)^\s*(?:proof\s+(\d|[ivx]{1,4})|(first|second|third)\s+proof)\s*[.:]?\s*$`)
	caseTarget   = regexp.MustCompile(`(?i)^(?:the\s+)?(?:sub)?case\b`)
	otherProof   = regexp.MustCompile(`(?i)\b(second|alternative|another|third|new)\s+proof\b`)
	citeNear     = regexp.MustCompile(`\\cite[a-zA-Z]*\*?\s*(?:\[[^\]]*\]\s*){0,2}\{[^{}]*\}`)
	sentence     = regexp.MustCompile(`[.!?](?:\s|$)`)
	// leadHead is a head in parentheses at the start of a proof body.
	// A font command may hold it, as \textit{(of Theorem 2)}.
	leadHead = regexp.MustCompile(`^\s*(?:\\(?:textit|textbf|textsl|emph|it|bf|em|sl)\b\s*\{?\s*)?\(\s*([^()]{1,80}?)\s*\)`)
	// fontHead is a head that a font command holds whole.
	fontHead = regexp.MustCompile(`^(?:\\(?:textbf|textit|textsl|textsc|emph|underline)\s*\{((?:[^{}]|\{[^{}]*\})*)\}|\{\s*\\(?:bf|it|em|sl|sc|bfseries|itshape)\b\s*((?:[^{}]|\{[^{}]*\})*)\})$`)
	// announce is a sentence or a section head that announces the proof
	// after it: We now prove Theorem~\ref{t}. \subsection{Proof of
	// Theorem~\ref{t}}.
	announce = regexp.MustCompile(`(?i)(?:proofs?\s+of|prove|proving|derive|deduce|establish)\s+(?:(?:now|next|here|first|finally)\s+)?(?:(?:the\s+)?[\w-]+\s+(?:part|half|direction)\s+of\s+)?(?:[A-Za-z]+\.?\s*)?(?:~|\s)*\\(?:ref|cref|Cref|autoref|thref)\*?\s*\{([^{}]+)\}(?:\s+(?:stated|given)\s+in\s+(?:the\s+)?[A-Za-z]+)?`)
	// proofHead is a section head that names what the proof after it
	// proves by a number: \paragraph{Proof of Lemma 1}.
	proofHead = regexp.MustCompile(`\\(?:(?:sub)*section|(?:sub)?paragraph)\*?\s*\{\s*(Proof\s+of\s+[^{}\\]{1,60}?)\s*[.:]?\s*\}`)
	// announceEnd is what may come between an announcement and the proof.
	whichClause = regexp.MustCompile(`^,\s*which\s`)
	announceEnd = regexp.MustCompile(`^(?:\s|[.:;,}]|\\label\s*\{[^{}]*\}|\\\\|\\(?:noindent|medskip|smallskip|bigskip|par)\b)*$`)
	// gapBlock is what ends the text about a statement: a section head or
	// an environment that is not display math.
	gapBlock    = regexp.MustCompile(`\\(?:part|chapter|(?:sub)*section|paragraph)\*?\s*[\[{]|\\begin\s*\{([^{}]*)\}`)
	impliesWord = regexp.MustCompile(`\b(?:implies|imply)\b`)
	displayMath = regexp.MustCompile(`^(?:equation|align|alignat|gather|multline|eqnarray|displaymath|flalign)\*?$`)
)

// attach attaches each proof to what it proves, by the first rule of AX3.6
// that applies.
func (b *Builder) attach(d *tex.Document, items []*item) {
	byLabel := map[string]*item{}
	// allLabel is each item of a label, for a label defined more than once.
	allLabel := map[string][]*item{}
	partLabel := map[string][2]string{}
	for _, it := range items {
		for _, l := range it.rec.Labels {
			if _, ok := byLabel[l]; !ok {
				byLabel[l] = it
			}
			allLabel[l] = append(allLabel[l], it)
		}
		for _, p := range it.rec.Parts {
			if p.Label != "" {
				partLabel[p.Label] = [2]string{it.rec.Local, p.Marker}
			}
		}
	}
	// proved is the items that an earlier proof proves.
	proved := map[string]bool{}
	var last *item
	for i, it := range items {
		if !it.proof {
			continue
		}
		if last != nil && last.rec.Proves != "" {
			proved[last.rec.Proves] = true
		}
		last = it
		r := &it.rec
		target := d.Macros.Expand(it.opt)
		// A head written in parentheses after \begin{proof}, as
		// \begin{proof}(of Theorem~\ref{t}), is the head of the proof.
		if target == "" && it.head == nil && it.close > it.inner {
			body := []byte(d.Macros.Expand(string(d.Text[it.inner:min(it.inner+200, it.close)])))
			if m := leadHead.FindSubmatch(body); m != nil {
				if lead := string(m[1]); refIn.MatchString(lead) || b.byNumber(items, lead) != nil {
					target = lead
				}
			}
		}
		// A proof wrapper that names what it proves out of its head.
		if provedRef(target) == "" && it.wrapRef != "" {
			target = strings.TrimSpace(target + " " + it.wrapRef)
		}
		// "Proof 2" or "First proof" names no item: it numbers the proofs of
		// the statement before it. Seen in the gold pilot (set E).
		second := otherProof.MatchString(target) || otherProof.MatchString(r.Title)
		if m := proofOrdinal.FindStringSubmatch(target); m != nil {
			second = second || m[1] != "1" && !strings.EqualFold(m[1], "i") && !strings.EqualFold(m[2], "first")
			target = ""
		}
		// Rule 1: a reference to a label in the head.
		if m := provedRef(target); m != "" {
			key := strings.TrimSpace(m)
			if _, ok := byLabel[key]; !ok {
				// \cref{a,b} names a first. A label can hold a comma
				// itself, as lem b^i_n,m. Seen in the gold pilot (set X).
				key = strings.TrimSpace(strings.Split(m, ",")[0])
			}
			if t, ok := byLabel[key]; ok && t != it {
				// A label defined twice, as a theorem of the introduction
				// restated with its label before its proof, is the last
				// item of the label before the proof.
				for _, x := range allLabel[key] {
					if x != it && !x.proof && x.off < it.off {
						t = x
					}
				}
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
		// Rule 2: a number or a name in the head.
		if t := b.byNumber(items, target); t != nil && t != it {
			r.Proves, r.Attach, r.Confidence = t.rec.Local, 2, "high"
			continue
		}
		// Rule 2 too: the label of a claim alone, as "Proof of A1", names the
		// last claim of the label before the proof. Seen in the gold pilot
		// (set X).
		if l := theorems.ClaimLabel(stripProofOf(target)); l != "" {
			var t *item
			for _, x := range items[:i] {
				if !x.proof && x.rec.Item == "statement/claim" && x.rec.Number == l {
					t = x
				}
			}
			if t != nil {
				r.Proves, r.Attach, r.Confidence = t.rec.Local, 2, "high"
				continue
			}
		}
		// "Proof of case (B1.i)" or "Proof of (ii)" inside a proof is a part
		// of that proof, and proves what it proves. Seen in the gold pilot
		// (set X).
		if (caseTarget.MatchString(stripProofOf(target)) || partOf.MatchString(stripProofOf(target))) && it.parent != nil && it.parent.proof && it.parent.rec.Proves != "" {
			r.Proves, r.Attach, r.Confidence = it.parent.rec.Proves, 2, "high"
			continue
		}
		prev := previous(items, i)
		// Rule 1 too: a sentence or a section head just before a proof with
		// no head announces what it proves.
		if strings.TrimSpace(stripProofOf(target)) == "" {
			if t := announced(d, prev, it, byLabel, func(h string) *item { return b.byNumber(items, h) }); t != nil && t != it {
				r.Proves, r.Attach, r.Confidence = t.rec.Local, 1, "high"
				continue
			}
		}
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
			case !prev.proof && betweenRemark(d, items, prev, proved) != nil:
				// Rule 4 too: a remark right after a statement that has no
				// proof yet, and the proof after the remark, proves the
				// statement.
				r.Proves, r.Attach, r.Confidence = betweenRemark(d, items, prev, proved).rec.Local, 4, "medium"
				continue
			case !prev.proof:
				// Rule 4: right after a definition, a remark or an example.
				r.Proves, r.Attach, r.Confidence = prev.rec.Local, 4, "high"
				r.Flags = append(r.Flags, "proves-non-statement")
				continue
			case prev.proof && second && prev.rec.Proves != "":
				// A second proof of the same statement.
				r.Proves, r.Attach, r.Confidence = prev.rec.Proves, 3, "high"
				continue
			}
		}
		// A proof written inside the environment of its statement, before
		// its \end, proves the statement that holds it.
		if prev == nil && it.parent != nil && isStatement(it.parent) {
			r.Proves, r.Attach, r.Confidence = it.parent.rec.Local, 3, "high"
			continue
		}
		// Proofs with no head inside an example, after each of its claims,
		// prove the example. Seen in the gold pilot (set X).
		if p := it.parent; p != nil && !p.proof && !isStatement(p) && strings.TrimSpace(stripProofOf(target)) == "" && (prev == nil || prev.off > p.off) {
			r.Proves, r.Attach, r.Confidence = p.rec.Local, 4, "high"
			r.Flags = append(r.Flags, "proves-non-statement")
			continue
		}
		// Rule 6: after a statement with one paragraph of text between
		// them, as a remark on the statement, and a head that names
		// nothing else.
		if prev != nil && isStatement(prev) && strings.TrimSpace(stripProofOf(target)) == "" && (oneParagraph(d.Text[prev.end:it.off]) || shortGap(d.Text[prev.end:it.off], prev.rec.Head == "manual")) {
			r.Proves, r.Attach, r.Confidence = prev.rec.Local, 6, "medium"
			continue
		}
		// Rule 7: an orphan. AX7.6 proposes a target.
		r.Flags = append(r.Flags, "orphan")
	}
}

// provedRef is the label of the reference in a head that names what the proof
// proves: the first, or in "Proposition~\ref{a} implies Theorem~\ref{b}" the
// one after "implies".
func provedRef(head string) string {
	ms := refIn.FindAllStringSubmatchIndex(head, -1)
	if len(ms) == 0 {
		return ""
	}
	for k := 1; k < len(ms); k++ {
		if impliesWord.MatchString(head[ms[k-1][1]:ms[k][0]]) {
			return head[ms[k][2]:ms[k][3]]
		}
	}
	return head[ms[0][2]:ms[0][3]]
}

func stripProofOf(s string) string {
	s = strings.TrimSpace(s)
	// A font command may hold the head, as
	// \begin{proof}[\textbf{Proof of Theorem 1.3.}]. Seen in the gold pilot
	// (set X).
	if m := fontHead.FindStringSubmatch(s); m != nil {
		s = strings.TrimSpace(m[1] + m[2])
	}
	low := strings.ToLower(s)
	for _, p := range []string{"proof of", "proof"} {
		if strings.HasPrefix(low, p) {
			return s[len(p):]
		}
	}
	return s
}

// byNumber resolves "Lemma 3.2", "Theorem A" or "the Main Theorem" in a head
// to an item with that title and number.
func (b *Builder) byNumber(items []*item, head string) *item {
	h := strings.TrimSpace(stripProofOf(head))
	h = strings.TrimPrefix(strings.TrimPrefix(h, "of "), "for ")
	if h == "" {
		return nil
	}
	if m := headNumber.FindStringSubmatch(h); m != nil {
		title, number := strings.TrimSpace(m[1]), m[2]
		kind := b.Titles.Kind(title)
		var any *item
		for _, it := range items {
			// \newtheorem*{thmA}{Theorem A}: the letter is in the title
			// of an item with no number.
			if !it.proof && it.rec.Number == "" && theorems.Normalize(it.rec.Title) == theorems.Normalize(title+" "+number) {
				return it
			}
			if it.proof || it.rec.Number != number {
				continue
			}
			if theorems.Normalize(it.rec.Title) == theorems.Normalize(title) || it.rec.Item == kind {
				return it
			}
			if any == nil {
				any = it
			}
		}
		return any
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
var floatEnv = regexp.MustCompile(`(?s)\\begin\s*\{(figure|table|wrapfigure|wraptable|algorithm|marginfigure|SCfigure)(\*?)\}.*?\\end\s*\{(?:figure|table|wrapfigure|wraptable|algorithm|marginfigure|SCfigure)\*?\}`)

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

// announced is the item of the label that the text just before a proof
// announces, after the item before the proof.
func announced(d *tex.Document, prev, it *item, byLabel map[string]*item, byNumber func(string) *item) *item {
	lo := max(0, it.off-400)
	if prev != nil && prev.end > lo {
		lo = prev.end
	}
	if lo >= it.off {
		return nil
	}
	tail := d.Macros.Expand(string(d.Text[lo:it.off]))
	ms := announce.FindAllStringSubmatchIndex(tail, -1)
	if len(ms) == 0 {
		// \paragraph{Proof of Lemma 1} with the proof after it: the head
		// names a number.
		hs := proofHead.FindAllStringSubmatchIndex(tail, -1)
		if len(hs) == 0 {
			// A class that defines \paragraph, as svjour3, expands the
			// head away: read it in the text as written. Seen in the gold
			// pilot (set X).
			tail = string(d.Text[lo:it.off])
			hs = proofHead.FindAllStringSubmatchIndex(tail, -1)
		}
		if len(hs) == 0 || byNumber == nil {
			return nil
		}
		h := hs[len(hs)-1]
		if !announceEnd.MatchString(tail[h[1]:]) {
			return nil
		}
		return byNumber(tail[h[2]:h[3]])
	}
	m := ms[len(ms)-1]
	if rest := tail[m[1]:]; !announceEnd.MatchString(rest) && !announceWhich(rest) {
		return nil
	}
	return byLabel[strings.TrimSpace(strings.Split(tail[m[2]:m[3]], ",")[0])]
}

// announceWhich tells if the rest of an announcing sentence is a clause on
// what it announces, as "Here we prove Lemma~\ref{a}, which will allow us to
// infer that ...", with no other reference and no paragraph between it and
// the proof. Seen in the gold pilot (set X).
func announceWhich(rest string) bool {
	m := whichClause.FindStringIndex(rest)
	if m == nil || refIn.MatchString(rest) {
		return false
	}
	end := strings.LastIndex(rest, ".")
	return end >= m[1] && !paraBreak.MatchString(rest[:end]) && announceEnd.MatchString(rest[end+1:])
}

// oneParagraph tells if a gap between two items is one paragraph of text,
// with no section head and no environment in it.
func oneParagraph(gap []byte) bool {
	if len(gap) > 3000 {
		return false
	}
	for _, m := range gapBlock.FindAllSubmatch(gap, -1) {
		if m[1] == nil || !displayMath.Match(m[1]) {
			return false
		}
	}
	n := 0
	for _, p := range paraBreak.Split(string(gap), -1) {
		if !gapOK.MatchString(p) {
			n++
		}
	}
	return n == 1
}

// shortGap tells if the text between a statement and a proof is a few lines
// with no section head and no environment but display math: 7 lines, or 20
// after a manual head, whose statement may run on past the end that the head
// reader gives it. On gold set E version 1, 32 of the 37 orphan proofs within
// these gaps proved the statement before it. Of the orphans that proved
// nothing, the nearest was 8 lines after an environment and 53 after a
// manual head.
func shortGap(gap []byte, manual bool) bool {
	limit := 7
	if manual {
		limit = 20
	}
	if bytes.Count(gap, []byte("\n")) > limit || len(gap) > 4000 {
		return false
	}
	for _, m := range gapBlock.FindAllSubmatch(gap, -1) {
		if m[1] == nil || !displayMath.Match(m[1]) {
			return false
		}
	}
	return true
}

// betweenRemark is the statement just before a remark or a notation, when
// nothing is between the two and the statement has no proof yet: the remark
// is on the statement, and a proof after the remark proves the statement.
// On gold set E, 22 of the 40 proofs that rule 4 gave to a remark proved the
// statement before it. An example is not read so: its proof proves it.
func betweenRemark(d *tex.Document, items []*item, rem *item, proved map[string]bool) *item {
	if rem.rec.Item != "remark" && rem.rec.Item != "definition/notation" {
		return nil
	}
	for j, x := range items {
		if x != rem {
			continue
		}
		st := previous(items, j)
		if st == nil || !isStatement(st) || proved[st.rec.Local] {
			return nil
		}
		if ok, _ := adjacent(d, st, rem); !ok {
			return nil
		}
		return st
	}
	return nil
}

// untitled makes a statement of an environment with no title at all in the
// plain style and a counter of its own that no section resets, as
// \newtheorem{point}{}, when its body is not blank: in the gold pilot (set X)
// the 45 of those were statements, and the untitled environments of the
// definition and remark styles, those that share the counter of the theorems
// or of the equations, and those numbered within the section, as
// \newtheorem{pkt}{}[section], numbered paragraphs. It runs before the proofs
// attach, so that a proof after one proves a statement.
func untitled(items []*item) {
	for _, it := range items {
		if d := it.decl; it.rec.Item == theorems.Unknown && d != nil && strings.TrimSpace(d.Title) == "" && !d.TitleArg && d.Numbered && (d.Style == "" || d.Style == "plain") && d.Shared == "" && !sectioning[d.Within] && !it.blank {
			it.rec.Item = "statement"
			it.rec.Kind, it.rec.Subkind = theorems.KindOf(it.rec.Item)
		}
	}
}

// sectioning is the counters of the section heads.
var sectioning = map[string]bool{"part": true, "chapter": true, "section": true, "subsection": true, "subsubsection": true, "paragraph": true}

// proven makes a statement of a section-like environment that a proof
// proves. \newtheorem{conclusion}[theorem]{Conclusion} is a summary in one
// paper and a corollary with its proof in another, and the proof is what tells
// them apart: in the gold pilot (set E) each of the 9 proved ones was marked a
// corollary, and 11 of the 14 without a proof a remark. An environment of no
// known title (\newtheorem{stat}{}[thm]) that a proof proves is a statement
// too.
func proven(items []*item) {
	byLocal := map[string]*item{}
	for _, it := range items {
		byLocal[it.rec.Local] = it
	}
	for _, it := range items {
		if !it.proof || it.rec.Proves == "" {
			continue
		}
		t := byLocal[it.rec.Proves]
		if t == nil {
			continue
		}
		switch t.rec.Item {
		case "other/section":
			t.rec.Item = "statement/corollary"
		case theorems.Unknown:
			t.rec.Item = "statement"
		default:
			continue
		}
		t.rec.Kind, t.rec.Subkind = theorems.KindOf(t.rec.Item)
	}
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
		if t := b.byNumber(items, h.Target); t != nil {
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

// onlyRef is an option that is only a reference, as [\ref{a}].
var onlyRef = regexp.MustCompile(`^\s*\(?\s*\\(?:ref|cref|Cref|autoref)\*?\s*\{([^{}]+)\}\s*\)?\s*$`)

// refHead is the earlier item of the same kind that the option of it names
// by its label alone, or nil.
func refHead(it *item, byLabel map[string]*item) *item {
	m := onlyRef.FindStringSubmatch(it.opt)
	if m == nil {
		return nil
	}
	f := byLabel[strings.TrimSpace(m[1])]
	if f == nil || f == it || f.proof || f.off >= it.off || f.rec.Item != it.rec.Item {
		return nil
	}
	return f
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
	byLabel := map[string]*item{}
	for _, it := range items {
		if it.restate != "" && it.rec.Origin != "restated" {
			byRestate[it.restate] = it
		}
		for _, l := range it.rec.Labels {
			if byLabel[l] == nil {
				byLabel[l] = it
			}
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
		case refHead(it, byLabel) != nil:
			// \begin{repthm}[\ref{a}]: an option that is only a reference
			// to an earlier item of the same kind restates it. Seen in the
			// gold pilot (set X).
			r.Origin, r.Restates = "restated", refHead(it, byLabel).rec.Local
		case it.normLen >= 40 && byHash[r.Hash] != nil && !otherNumber(r, &byHash[r.Hash].rec):
			// The same text under another printed number, as "Prop. 9.3.0"
			// and "Proposition 9.3.1", is two items. Seen in the gold pilot
			// (set X).
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
		if r.Origin == "restated" && r.Restates == "" {
			// \begin{repthm}[\ref{a}] names what it restates. Seen in the
			// gold pilot (set X).
			if m := refIn.FindStringSubmatch(it.opt); m != nil {
				if f := byLabel[strings.TrimSpace(m[1])]; f != nil && f != it && !f.proof && f.off < it.off {
					r.Restates = f.rec.Local
				}
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

// otherNumber tells if two records print two different numbers.
func otherNumber(a, b *objects.Record) bool {
	return a.Number != "" && b.Number != "" && a.Number != b.Number
}

func (b *Builder) restatedHead(opt string) bool {
	if opt == "" {
		return false
	}
	_, ok := b.Phrases.Find("restated", opt)
	return ok || regexp.MustCompile(`=\s*(?:Theorem|Lemma|Proposition|Corollary)\s`).MatchString(opt)
}

// Package analysis builds the object records of the analysis plane from the
// source of a paper, and takes the census of them (AX03 and AX04).
//
// The builder reads the tree of the TeX front end. The census reads the
// expanded text again with its own scan. An item that the builder misses is a
// gap in the census, and an object that the census cannot place is a phantom.
package analysis

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/tamnd/arxiv-reader/census"
	"github.com/tamnd/arxiv-reader/fingerprint"
	"github.com/tamnd/arxiv-reader/objects"
	"github.com/tamnd/arxiv-reader/tex"
	"github.com/tamnd/arxiv-reader/theorems"
)

// item is one item that the builder found, before it is a record.
type item struct {
	rec objects.Record
	// off is the offset of the \begin or of the head, inner the offset after
	// the head, close the offset of \end, and end the offset after the item.
	off, inner, close, end int
	opt                    string
	hasOpt                 bool
	decl                   *theorems.Declaration
	head                   *theorems.Head
	// restate is the macro of a restatable, or the macro that a restatement
	// calls.
	restate string
	parent  *item
	proof   bool
	// wrapLabels is the labels that the begin code of a wrapper sets, as
	// \label{l:#1} in \newenvironment{lemma}[1]{\begin{Lem}\label{l:#1}}.
	wrapLabels []string
	// wrapRef is a reference that the begin code of a wrapper makes out of
	// the head, as \newenvironment{PROOF}[2][\proofname.]{\begin{proof}[#1]
	// \renewcommand{\qedsymbol}{...#2...}} in math/9602216v3, whose use
	// \begin{PROOF}{\ref{X2.6}} names what the proof proves.
	wrapRef string
	// normLen is the length of the normalized body, so that two short or
	// empty bodies do not count as one item restated.
	normLen int
}

// Builder builds the object records of one main file.
type Builder struct {
	Paper     string
	Version   int
	Extractor string
	// Text is true when the licence class of the version allows text in the
	// analysis plane. When it is false, the records keep structure and
	// fingerprints only (rule D06).
	Text    bool
	Titles  *theorems.Titles
	Phrases theorems.Phrases
	// Prefix is put before each local identifier, for the second and later
	// main files of a multi-main paper.
	Prefix string
	// Numbers is the number that the census gives to the item that starts at
	// each offset. The rules of AX3.6 that read a number in a proof head use
	// it, so the census runs first.
	Numbers map[int]string
}

var (
	citeIn = regexp.MustCompile(`\\cite[a-zA-Z]*\*?\s*(?:\[([^\]]*)\])?\s*(?:\[([^\]]*)\])?\s*\{([^{}]*)\}`)
	refIn  = regexp.MustCompile(`\\(?:ref|cref|Cref|autoref|eqref|thref|Thref)\*?\s*\{([^{}]+)\}`)
	// leadTarget is a proof whose body opens by naming what it proves, as
	// "\begin{proof} (of Theorem \ref{a})" or "\begin{proof} \textit{(of Lemma \ref{b})}".
	leadTarget = regexp.MustCompile(`^\s*(?:\\textit\s*\{|\\emph\s*\{|\{\s*\\(?:it|em|bf)\b\s*)?\s*\(\s*(?:(?i:of|for)\s+)?((?:[A-Za-z]+\.?\s*~?\s*)?\\(?:ref|cref|Cref|autoref|thref)\*?\s*\{[^{}]+\})\s*\)`)
	labelIn    = regexp.MustCompile(`\\label\s*\{([^{}]*)\}`)
	paraBreak  = regexp.MustCompile(`\n[ \t\r]*\n|\\par\b`)
	gapOK      = regexp.MustCompile(`^(?:\s|\\label\s*\{[^{}]*\}|\\(?:qed|qedhere|medskip|smallskip|bigskip|noindent|par|vspace\*?\s*\{[^{}]*\}|vskip\s*[\d.]+\w*|newline|hfill|bigbreak|medbreak|smallbreak|goodbreak|nopagebreak|pagebreak|clearpage|newpage)\b|\\(?:begin|end)\s*\{sloppypar\}|\\\\)*$`)
	restateCmd = regexp.MustCompile(`\\([A-Za-z@]+)\*`)
	partStart  = regexp.MustCompile(`\\begin\s*\{(enumerate|enumerate\*|itemize|description|parts|romanenumerate|alphenumerate|enumeratei|enumi|thmenum|conditions)\}(\s*\[[^\]]*\])?`)
	itemCmd    = regexp.MustCompile(`\\item\b(\s*\[([^\]]*)\])?`)
	partOf     = regexp.MustCompile(`^\s*(?:of\s+)?(?:part\s+)?(\([a-zA-Z0-9]{1,5}\)|\([ivx]{1,5}\))\s*$`)
)

// Build builds the object records of a document. It also gives the extent of
// each record, for the match with the census and for the edges.
func (b *Builder) Build(d *tex.Document, set *theorems.Set) ([]objects.Record, []Extent) {
	if d == nil || d.Body == 0 || d.Tree == nil {
		return nil, nil
	}
	items := b.envItems(d, set)
	items = append(items, b.headItems(d, items)...)
	items = append(items, b.restateItems(d, items)...)
	sortItems(items)
	b.nest(items)
	b.place(d, items)
	b.local(items)
	b.fill(d, items)
	for _, it := range items {
		if n, ok := b.Numbers[it.off]; ok && n != "" && it.rec.Number == "" {
			it.rec.Number = n
		}
	}
	b.attach(d, items)
	b.status(d, items)
	b.origin(items)
	out := make([]objects.Record, len(items))
	ext := make([]Extent, len(items))
	for i, it := range items {
		it.rec.Order = i + 1
		out[i] = it.rec
		ext[i] = Extent{Off: it.off, Inner: it.inner, Close: it.close, End: it.end, Proof: it.proof}
	}
	return out, ext
}

// Extent is where an item is in the expanded text: the offset of the \begin
// or of the head, the offset after the head, the offset of the \end, and the
// offset after the item. Proof is true for a proof.
type Extent struct {
	Off, Inner, Close, End int
	Proof                  bool
}

func (b *Builder) envItems(d *tex.Document, set *theorems.Set) []*item {
	var out []*item
	for _, n := range d.Tree.Nodes {
		if n.Kind != "env" {
			continue
		}
		name := n.Name
		inner := n.Inner
		opt, hasOpt := n.Opt, n.HasOpt
		restate := ""
		if name == "restatable" || name == "restatable*" {
			// \begin{restatable}[opt]{env}{macro}: the tree read [opt] as the
			// option of the restatable.
			a, end, ok := tex.ReadArgs(d.Text, n.Inner, "mm")
			if !ok {
				continue
			}
			name, restate, inner = strings.TrimSpace(a.Values[0]), strings.TrimSpace(a.Values[1]), end
		}
		decl := set.Get(name)
		if decl == nil {
			continue
		}
		var wrapLabels []string
		wrapRef := ""
		if begin, after, ok := wrapperBegin(d, decl, inner, opt, hasOpt); ok {
			wrapRef = begin
			if m := innerOpt.FindStringSubmatch(begin); m != nil {
				opt, hasOpt = m[1], true
			}
			for _, m := range labelIn.FindAllStringSubmatch(begin, -1) {
				wrapLabels = append(wrapLabels, strings.TrimSpace(m[1]))
			}
			inner = after
		} else if a, ok := printedArg(d, decl, name, inner); ok && !hasOpt {
			// \newenvironment{proofof}[1]{{\bf #1.}}{\qed} prints its argument
			// as the head, as "Proof of Theorem \ref{a}".
			opt, hasOpt = a, true
		}
		it := &item{off: n.From, inner: inner, close: n.Close, end: n.To, opt: opt, hasOpt: hasOpt, decl: decl, restate: restate, proof: decl.Proof, wrapLabels: wrapLabels, wrapRef: wrapRef}
		it.rec.Env = name
		it.rec.Title, it.rec.Item, it.inner = decl.TitleOf(d.Text, inner, b.Titles)
		if decl.LeadOnly && it.rec.Item == theorems.Unknown {
			// A numbered heading, not an item. The census still counts
			// its steps.
			continue
		}
		it.rec.Head = "environment"
		it.rec.Counter = decl.Counter
		it.rec.Confidence = "certain"
		if !n.Closed {
			it.rec.Confidence = "high"
		}
		if decl.Proof {
			it.rec.Item = "proof"
		}
		if it.rec.Item == "proof" && !it.proof {
			// An environment with no title whose body leads with "{\bf Proof
			// of Theorem \ref{a}.}" is a proof, and the lead is its head.
			it.proof = true
			if !it.hasOpt {
				it.opt = it.rec.Title
			}
		}
		if it.proof && !it.hasOpt {
			if m := leadTarget.FindStringSubmatch(string(d.Text[it.inner:min(it.inner+200, it.close)])); m != nil {
				it.opt, it.hasOpt = "Proof of "+m[1], true
			}
		}
		out = append(out, it)
	}
	// \definition ... \enddefinition is an environment too.
	// So is a command that writes an environment around its argument.
	for _, e := range append(census.CommandEnvs(d, set), census.MacroEnvs(d, set)...) {
		it := &item{off: e.Off, inner: e.Inner, close: e.Close, end: e.End, opt: e.Opt, hasOpt: e.HasOpt, decl: e.Decl, proof: e.Decl.Proof, wrapLabels: e.Labels}
		it.rec.Env = e.Decl.Env
		it.rec.Title, it.rec.Item, it.inner = e.Decl.TitleOf(d.Text, e.Inner, b.Titles)
		it.rec.Head = "environment"
		it.rec.Counter = e.Decl.Counter
		it.rec.Confidence = "certain"
		if !e.Closed {
			it.rec.Confidence = "high"
		}
		if e.Decl.Proof {
			it.rec.Item = "proof"
		}
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].off < out[j].off })
	return out
}

var innerOpt = regexp.MustCompile(`\\begin\s*\{[^{}]+\}\s*\[([^\]]*)\]`)

// wrapperBegin gives the begin code of a wrapper that takes arguments, with
// the arguments of the use put in, and the offset after them. For
// \newenvironment{proofof}[1]{\begin{proof}[Proof of #1]}{\end{proof}}, the use
// \begin{proofof}{Theorem 1} gives \begin{proof}[Proof of Theorem 1], whose
// optional argument is the head, and for
// \newenvironment{lemma}[1]{\begin{Lem}\label{l:#1}}{\end{Lem}} the use
// \begin{lemma}{bfe1} gives the label l:bfe1.
//
// A wrapper whose first argument is optional, as
// \newenvironment{Proof}[1][\proofname]{\begin{proof}[#1]...}, takes the
// option of the use, which the tree read, or else the default.
func wrapperBegin(d *tex.Document, decl *theorems.Declaration, inner int, opt string, hasOpt bool) (string, int, bool) {
	if decl.Inner == "" || d.Macros == nil {
		return "", 0, false
	}
	e := d.Macros.Envs[decl.Env]
	if e == nil || e.Args < 1 || e.Args > 9 {
		return "", 0, false
	}
	if !innerOpt.MatchString(e.Begin) && !labelIn.MatchString(e.Begin) {
		return "", 0, false
	}
	var first []string
	n := e.Args
	if e.HasDefault {
		v := e.Default
		if hasOpt {
			v = opt
		}
		first, n = []string{v}, n-1
	}
	a, end, ok := tex.ReadArgs(d.Text, inner, strings.Repeat("m", n))
	if !ok {
		return "", 0, false
	}
	vals := append(first, a.Values...)
	begin := e.Begin
	for k := e.Args; k >= 1; k-- {
		begin = strings.ReplaceAll(begin, "#"+strconv.Itoa(k), vals[k-1])
	}
	return begin, end, true
}

// printedArg is the first argument of a use of a proof environment of the
// paper that prints it in its head.
func printedArg(d *tex.Document, decl *theorems.Declaration, name string, inner int) (string, bool) {
	if !decl.Proof || d.Macros == nil {
		return "", false
	}
	e := d.Macros.Envs[name]
	if e == nil || e.Args < 1 || e.HasDefault || !strings.Contains(e.Begin, "#1") {
		return "", false
	}
	a, _, ok := tex.ReadArgs(d.Text, inner, "m")
	if !ok {
		return "", false
	}
	return a.Values[0], true
}

func (b *Builder) headItems(d *tex.Document, envs []*item) []*item {
	var skip []tex.Region
	var stops []int
	for _, e := range envs {
		skip = append(skip, tex.Region{From: e.off, To: e.end, Part: !e.proof && theorems.PartOfProof(e.rec.Item), Statement: !e.proof && theorems.EndsAProof(e.rec.Item)})
		if !e.proof && theorems.EndsAProof(e.rec.Item) {
			stops = append(stops, e.off)
		}
	}
	var out []*item
	for _, h := range theorems.FindHeads(d, b.Titles, skip, stops...) {
		h := h
		it := &item{off: h.Off, inner: h.Inner, close: h.End, end: h.End, head: &h, proof: h.Proof}
		it.rec.Env = "head"
		it.rec.Title = h.Title
		it.rec.Item = h.Item
		it.rec.Head = "manual"
		if h.Form == "section" {
			it.rec.Head = "section"
		}
		it.rec.Number = h.Number
		if h.Number != "" {
			it.rec.NumberFrom = "head"
		}
		it.rec.Confidence = h.Confidence()
		if h.Proof {
			it.opt, it.hasOpt = h.Target, h.Target != ""
		} else {
			it.opt, it.hasOpt = h.Name, h.Name != ""
		}
		out = append(out, it)
	}
	return out
}

// restateItems finds the restatements of thm-restate: \macro* for the macro of
// a restatable.
func (b *Builder) restateItems(d *tex.Document, items []*item) []*item {
	macros := map[string]*item{}
	for _, it := range items {
		if it.restate != "" {
			macros[it.restate] = it
		}
	}
	if len(macros) == 0 {
		return nil
	}
	var out []*item
	body := d.Text[d.Body:d.End]
	for _, m := range restateCmd.FindAllSubmatchIndex(body, -1) {
		first, ok := macros[string(body[m[2]:m[3]])]
		if !ok {
			continue
		}
		off := d.Body + m[0]
		it := &item{off: off, inner: d.Body + m[1], close: d.Body + m[1], end: d.Body + m[1], decl: first.decl, restate: first.restate}
		it.rec = objects.Record{Env: first.rec.Env, Title: first.rec.Title, Item: first.rec.Item, Head: "environment", Counter: first.rec.Counter, Confidence: "certain", Origin: "restated"}
		it.parent = first // replaced in nest; kept for origin through restate
		out = append(out, it)
	}
	return out
}

func sortItems(items []*item) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].off < items[j-1].off; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

// nest sets the enclosing item of each item.
func (b *Builder) nest(items []*item) {
	var stack []*item
	for _, it := range items {
		for len(stack) > 0 && stack[len(stack)-1].end <= it.off {
			stack = stack[:len(stack)-1]
		}
		if it.rec.Origin == "restated" {
			// A restatement has no body of its own.
			if len(stack) > 0 {
				it.parent = stack[len(stack)-1]
			} else {
				it.parent = nil
			}
			continue
		}
		if len(stack) > 0 {
			it.parent = stack[len(stack)-1]
		}
		if it.end > it.inner {
			stack = append(stack, it)
		}
	}
}

// place sets the section, the span and the flags.
func (b *Builder) place(d *tex.Document, items []*item) {
	secIndex := map[int]int{}
	k := 0
	for i, n := range d.Tree.Nodes {
		if n.Kind == "section" {
			k++
			secIndex[i] = k
		}
	}
	for _, it := range items {
		if s := d.Tree.Section(it.off); s >= 0 {
			it.rec.Section = fmt.Sprintf("%ssec-%d", b.Prefix, secIndex[s])
		}
		p := d.Pos(it.off)
		last := d.Pos(max(it.end-1, it.off))
		it.rec.Span = &objects.Span{File: p.File, From: p.Line, To: last.Line}
		if tex.In(d.Conditional, it.off) {
			it.rec.Flags = append(it.rec.Flags, "conditional")
		}
		if d.Tree.Appendix > 0 && it.off > d.Tree.Appendix {
			it.rec.Flags = append(it.rec.Flags, "appendix")
		}
		if len(d.Mains) > 1 {
			it.rec.Flags = append(it.rec.Flags, "multi-main")
		}
	}
}

// local gives each item its local identifier: the environment and its
// position among the items of that environment, as lem-3.
func (b *Builder) local(items []*item) {
	n := map[string]int{}
	for _, it := range items {
		base := strings.ToLower(it.rec.Env)
		if it.rec.Origin == "restated" {
			base = "restate"
		}
		base = strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
				return r
			}
			return '-'
		}, base)
		n[base]++
		it.rec.Local = fmt.Sprintf("%s%s-%d", b.Prefix, base, n[base])
	}
	for _, it := range items {
		if it.parent != nil && it.rec.Origin != "restated" {
			it.rec.Parent = it.parent.rec.Local
			if it.parent.proof {
				it.rec.Scope = "local"
			}
		}
		if it.rec.Scope == "" {
			it.rec.Scope = "paper"
		}
	}
}

// fill sets the kind, the head text, the labels, the parts, the body and the
// fingerprints.
func (b *Builder) fill(d *tex.Document, items []*item) {
	for _, it := range items {
		r := &it.rec
		r.Paper, r.Version, r.Path, r.Extractor = b.Paper, fmt.Sprint(b.Version), "source", b.Extractor
		r.Kind, r.Subkind = theorems.KindOf(r.Item)
		if r.Item == theorems.Unknown {
			r.Item = theorems.Unknown
		}
		name, cites := splitHead(it.opt)
		r.CitesInHead = cites
		if b.Text {
			r.Name = name
		}
		if it.rec.Origin == "restated" {
			continue
		}
		body := ""
		if it.close > it.inner {
			body = string(d.Text[it.inner:it.close])
		}
		r.Labels, r.Parts = labels(body, it.inner)
		r.Labels = append(r.Labels, macroLabels(d.Macros, body)...)
		r.Labels = append(append([]string(nil), it.wrapLabels...), r.Labels...)
		if it.hasOpt {
			for _, m := range labelIn.FindAllStringSubmatch(it.opt, -1) {
				r.Labels = append(r.Labels, strings.TrimSpace(m[1]))
			}
		}
		if len(r.Labels) > 0 {
			r.Label = r.Labels[0]
		}
		norm := fingerprint.Normalize(innerOnly(body, items, it, d), d.Macros)
		it.normLen = len(norm)
		r.Hash = fingerprint.Hash(norm)
		r.MinHash = fingerprint.MinHash(norm)
		if b.Text {
			r.BodyMD = strings.TrimSpace(d.Macros.Expand(body))
		}
	}
}

// innerOnly is the body of an item without the bodies of the items nested
// in it, so that a proof with a claim in it and the claim do not share text.
func innerOnly(body string, items []*item, it *item, d *tex.Document) string {
	var b strings.Builder
	at := it.inner
	for _, o := range items {
		if o.parent != it || o.off < at || o.end > it.close {
			continue
		}
		b.Write(d.Text[at:o.off])
		at = o.end
	}
	if at < it.close {
		b.Write(d.Text[at:it.close])
	}
	if b.Len() == 0 {
		return body
	}
	return b.String()
}

// splitHead splits an optional argument into a name and its citations.
func splitHead(opt string) (string, []string) {
	var cites []string
	for _, m := range citeIn.FindAllStringSubmatch(opt, -1) {
		loc := strings.TrimSpace(m[2])
		if loc == "" {
			loc = strings.TrimSpace(m[1])
		}
		for k := range strings.SplitSeq(m[3], ",") {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			if loc != "" {
				k += "|" + loc
			}
			cites = append(cites, k)
		}
	}
	name := citeIn.ReplaceAllString(opt, "")
	name = labelIn.ReplaceAllString(name, "")
	return headName(name), cites
}

var (
	emptyGroup = regexp.MustCompile(`\{\s*\}`)
	headRef    = regexp.MustCompile(`\\(ref|eqref|cref|Cref|autoref|pageref|hyperref)\b`)
	controlIn  = regexp.MustCompile(`\\[a-zA-Z]+\*?`)
	// locator is a head that only points to an item of another text, as
	// "Theorem 1" or "Lemma 2.3 and 2.4". It is not a name.
	locator = regexp.MustCompile(`(?i)^(theorem|thm|lemma|lem|proposition|prop|corollary|cor|definition|def|remark|example|conjecture|claim|section|sec|chapter|ch|equation|eq|page|p|pp)s?\.?\s*[0-9a-z]+([.\-–][0-9a-z]+)*(\s*(,|and|&)\s*[0-9a-z]+([.\-–][0-9a-z]+)*)*$`)
)

// connectives are the words that join a citation to the head, as in "see
// [12]" or "cf. [3, Theorem 2]". When the citation goes, they are not part of
// the name.
var connectives = map[string]bool{
	"see": true, "cf": true, "cf.": true, "e.g.": true, "e.g": true, "eg": true, "compare": true, "also": true,
	"and": true, "in": true, "of": true, "from": true, "by": true, "after": true, "following": true,
	"with": true, "on": true, "i.e.": true, "resp.": true, "for": true, "to": true,
}

// headName gives the name of an item from its head text with the citations
// and the labels removed. A head that is only a cross-reference, a locator
// as "Theorem 1", the words that joined a citation, or one letter, as the
// "A" of "Theorem A", gives no name.
func headName(s string) string {
	for {
		t := emptyGroup.ReplaceAllString(s, "")
		if t == s {
			break
		}
		s = t
	}
	s = strings.Join(strings.Fields(strings.ReplaceAll(s, "~", " ")), " ")
	if headRef.MatchString(s) {
		return ""
	}
	for {
		t := strings.TrimSpace(strings.Trim(s, ",;:()[]{} "))
		ws := strings.Fields(t)
		if len(ws) > 0 && connectives[strings.ToLower(ws[0])] {
			ws = ws[1:]
		}
		if len(ws) > 0 && connectives[strings.ToLower(ws[len(ws)-1])] {
			ws = ws[:len(ws)-1]
		}
		t = strings.Join(ws, " ")
		if t == s {
			break
		}
		s = t
	}
	letters := 0
	for _, r := range controlIn.ReplaceAllString(s, "") {
		if unicode.IsLetter(r) {
			letters++
		}
	}
	if letters < 2 || locator.MatchString(s) {
		return ""
	}
	return s
}

// labels gives the labels of an item and its parts (AX3.5). A label before
// the first paragraph break and outside an enumeration is a label of the item.
// A label inside an \item of an enumeration is the label of a part.
func labels(body string, base int) ([]string, []objects.Part) {
	var own []string
	var parts []objects.Part
	lists := partStart.FindAllStringSubmatchIndex(body, -1)
	listEnd := func(from int, env string) int {
		re := regexp.MustCompile(`\\(begin|end)\s*\{` + regexp.QuoteMeta(env) + `\}`)
		depth := 1
		for _, m := range re.FindAllStringSubmatchIndex(body[from:], -1) {
			if body[from+m[2]:from+m[3]] == "begin" {
				depth++
				continue
			}
			depth--
			if depth == 0 {
				return from + m[0]
			}
		}
		return len(body)
	}
	var regions [][2]int
	first := true
	for _, l := range lists {
		if !first {
			break
		}
		env := body[l[2]:l[3]]
		if env == "itemize" || env == "description" {
			continue
		}
		first = false
		end := listEnd(l[1], env)
		regions = append(regions, [2]int{l[0], end})
		style := ""
		if l[4] >= 0 {
			style = body[l[4]:l[5]]
		}
		inner := body[l[1]:end]
		ms := itemCmd.FindAllStringSubmatchIndex(inner, -1)
		// Only the items of this list, not of a list inside it.
		depth := 0
		k := 0
		nestRe := regexp.MustCompile(`\\(begin|end)\s*\{(?:enumerate|itemize|description)\*?\}`)
		nests := nestRe.FindAllStringSubmatchIndex(inner, -1)
		for i, m := range ms {
			depth = 0
			for _, n := range nests {
				if n[0] < m[0] {
					if inner[n[2]:n[3]] == "begin" {
						depth++
					} else {
						depth--
					}
				}
			}
			if depth != 0 {
				continue
			}
			k++
			to := len(inner)
			if i+1 < len(ms) {
				to = ms[i+1][0]
			}
			marker := ""
			if m[4] >= 0 {
				marker = strings.TrimSpace(inner[m[4]:m[5]])
			} else {
				marker = autoMarker(style, k)
			}
			p := objects.Part{Marker: marker, From: base + l[1] + m[0], To: base + l[1] + to}
			if lm := labelIn.FindStringSubmatch(inner[m[1]:to]); lm != nil {
				p.Label = strings.TrimSpace(lm[1])
			}
			parts = append(parts, p)
		}
	}
	stop := len(body)
	if pb := paraBreak.FindStringIndex(body); pb != nil {
		stop = pb[0]
	}
	for _, m := range labelIn.FindAllStringSubmatchIndex(body, -1) {
		inList := false
		for _, r := range regions {
			if m[0] >= r[0] && m[0] < r[1] {
				inList = true
			}
		}
		if inList {
			continue
		}
		if m[0] < stop || len(own) == 0 && m[0] < 400 {
			own = append(own, strings.TrimSpace(body[m[2]:m[3]]))
		}
	}
	if len(parts) < 2 {
		parts = nil
	}
	return own, parts
}

// macroLabels gives the labels that a command of the paper sets in the first
// paragraph of a body, as \newcommand{\prlabel}[1]{\label{pr:#1}} in
// 0902.4012v5.
func macroLabels(m *tex.Macros, body string) []string {
	if m == nil {
		return nil
	}
	if pb := paraBreak.FindStringIndex(body); pb != nil {
		body = body[:pb[0]]
	}
	var out []string
	for _, x := range refCall.FindAllStringSubmatch(body, -1) {
		c := m.Commands[x[1]]
		if c == nil || c.Args != 1 || x[1] == `\label` {
			continue
		}
		if l := labelIn.FindStringSubmatch(c.Body); l != nil && strings.Contains(l[1], "#1") {
			out = append(out, strings.TrimSpace(strings.ReplaceAll(l[1], "#1", x[2])))
		}
	}
	return out
}

// autoMarker is the printed marker of the k-th item of an enumeration with an
// optional argument of the enumerate package or of enumitem.
func autoMarker(style string, k int) string {
	s := strings.Trim(strings.TrimSpace(style), "[]")
	if i := strings.Index(s, "label="); i >= 0 {
		s = s[i+len("label="):]
		if j := strings.IndexAny(s, ","); j >= 0 {
			s = s[:j]
		}
	}
	s = strings.Trim(s, "{}")
	switch {
	case strings.Contains(s, `\alph*`) || s == "(a)" || s == "a)" || s == "a." || s == "a":
		return wrap(s, string(rune('a'+k-1)), `\alph*`, "a")
	case strings.Contains(s, `\roman*`) || s == "(i)" || s == "i)" || s == "i." || s == "i":
		return wrap(s, strings.ToLower(romanOf(k)), `\roman*`, "i")
	case strings.Contains(s, `\Alph*`) || s == "(A)" || s == "A)" || s == "A.":
		return wrap(s, string(rune('A'+k-1)), `\Alph*`, "A")
	case strings.Contains(s, `\Roman*`) || s == "(I)" || s == "I)" || s == "I.":
		return wrap(s, romanOf(k), `\Roman*`, "I")
	case strings.Contains(s, `\arabic*`) || s == "(1)" || s == "1)" || s == "1.":
		return wrap(s, fmt.Sprint(k), `\arabic*`, "1")
	}
	return fmt.Sprintf("%d.", k)
}

func wrap(style, v, cmd, plain string) string {
	if strings.Contains(style, cmd) {
		return strings.Replace(style, cmd, v, 1)
	}
	return strings.Replace(style, plain, v, 1)
}

func romanOf(v int) string {
	vals := []int{10, 9, 5, 4, 1}
	syms := []string{"X", "IX", "V", "IV", "I"}
	var b strings.Builder
	for i, n := range vals {
		for v >= n {
			b.WriteString(syms[i])
			v -= n
		}
	}
	return b.String()
}

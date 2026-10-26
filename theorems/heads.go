package theorems

import (
	"regexp"
	"sort"
	"strings"

	"github.com/tamnd/arxiv-reader/tex"
)

// Head is one item that the author wrote by hand, with no environment.
type Head struct {
	// Off is the offset of the head, Inner is the offset after the head
	// text, and End is the offset where the item ends.
	Off, Inner, End int
	Title           string
	Item            string
	Number          string
	Name            string
	Proof           bool
	// Target is the title and number that a proof head names, as in "Proof
	// of Theorem 2.1". It is empty for other heads.
	Target string
	// Form is how the head is written: bold, italic, smallcaps, plain,
	// paragraph or section.
	Form string
	// EndMark is true when an end-of-proof mark or the end of an italic group
	// ends the item.
	EndMark bool
}

// Confidence is high when the head has a number and an end mark, and medium
// otherwise (AX3.4).
func (h Head) Confidence() string {
	if (h.Number != "" || h.Proof) && h.EndMark {
		return "high"
	}
	return "medium"
}

var (
	// layout is the commands that can come before a head at the start of a
	// paragraph.
	layout = regexp.MustCompile(`^(?:\s+|\\(?:noindent|indent|par|medskip|bigskip|smallskip|newline|linebreak|leavevmode|vfill|goodbreak|nopagebreak|pagebreak)\b|\\(?:vskip|hskip|kern)\s*-?[\d.]+\s*(?:pt|em|ex|mm|cm|in|bp|truecm|truept|mu)?(?:\s*plus\s*[\d.]+\s*\w*)?(?:\s*minus\s*[\d.]+\s*\w*)?|\\(?:vspace|hspace)\*?\s*\{[^{}]*\}|\\\\(?:\[[^\]]*\])?)`)

	fontGroup  = regexp.MustCompile(`^\{\s*\\(bf|bfseries|sc|scshape|it|itshape|sl|slshape|em|textbf)\b\s*`)
	fontArg    = regexp.MustCompile(`^\\(textbf|textsc|textit|emph|textsl|underline|paragraph\*?|subparagraph\*?|bfseries|itshape)\s*\{`)
	fontSwitch = regexp.MustCompile(`^\\(bf|bfseries|sc|it|itshape|sl)\b\s*`)
	plainProof = regexp.MustCompile(`^(Proof|PROOF|Proof of [^.:\n]{1,80}|Sketch of proof|Sketch of the proof|D[ée]monstration|Beweis)\s*[.:]`)

	headNumber = regexp.MustCompile(`^\s*~?\s*((?:\d+(?:\.\d+)*[a-z]?)|(?:[A-Z](?:\.\d+)*)|(?:[IVXLC]+(?:\.\d+)*)|(?:\\ref\s*\{[^}]*\}))(?:\s|\.|:|\(|$|\))`)
	headName   = regexp.MustCompile(`^\s*(\(([^()]|\([^()]*\)){0,200}\)|\[[^\]]{0,200}\])`)
	headEnd    = regexp.MustCompile(`^\s*[.:]`)

	qedMark    = regexp.MustCompile(`\\(qed|qedsymbol|qedhere|QED|endproof|BOX|Box|blacksquare|square|qquad\s*\\Box|hfill\s*\$\s*\\(?:Box|square|blacksquare)\s*\$|endpf|eop)\b|\$\s*\\(?:Box|square|blacksquare)\s*\$`)
	paraBreak  = regexp.MustCompile(`\n[ \t]*\n|\\par\b`)
	itemCmd    = regexp.MustCompile(`\\item\b(\s*\[[^\]]*\])?`)
	proofOfRef = regexp.MustCompile(`(?i)^(?:the\s+)?(?:proof|sketch of (?:the )?proof|outline of (?:the )?proof|démonstration|beweis)(?:\s+of|\s+for|\s+de|\s+du|\s+von)?\s*(.*)$`)
)

// matchTitle reads a title of the map at the start of s, as words.
//
// It returns the title as the author wrote it and the rest of s.
func matchTitle(s string, t *Titles) (title, rest string, ok bool) {
	words := strings.Fields(s)
	if len(words) == 0 {
		return "", "", false
	}
	// Try the longest run of words first, up to four.
	max := 4
	if len(words) < max {
		max = len(words)
	}
	for n := max; n >= 1; n-- {
		cand := strings.Join(words[:n], " ")
		cand = strings.TrimRight(cand, ".:")
		if _, ok := t.kinds[Normalize(cand)]; ok {
			idx := indexAfterWords(s, n)
			rest = s[idx:]
			// Remove the period that the trim took from the title.
			return cand, rest, true
		}
	}
	return "", "", false
}

// indexAfterWords is the offset in s after its first n words, before any
// period or colon that ends the last word.
func indexAfterWords(s string, n int) int {
	i := 0
	for w := 0; w < n; w++ {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '~') {
			i++
		}
		for i < len(s) && s[i] != ' ' && s[i] != '\t' && s[i] != '\n' && s[i] != '~' {
			i++
		}
	}
	for i > 0 && (s[i-1] == '.' || s[i-1] == ':') {
		i--
	}
	return i
}

// parseHead reads "Title Number (Name)." from the text of a head.
func parseHead(inner string, t *Titles) (Head, bool) {
	inner = strings.TrimSpace(inner)
	title, rest, ok := matchTitle(inner, t)
	if !ok {
		return Head{}, false
	}
	h := Head{Title: title, Item: t.Kind(title)}
	if strings.HasPrefix(h.Item, "other/") {
		// Case and step heads are parts of a proof, not items.
		return Head{}, false
	}
	if h.Item == "proof" {
		h.Proof = true
		if m := proofOfRef.FindStringSubmatch(strings.TrimRight(inner, ".: ")); m != nil {
			h.Target = strings.TrimSpace(m[1])
		}
		return h, true
	}
	if m := headNumber.FindStringSubmatch(rest + " "); m != nil {
		h.Number = m[1]
		rest = rest[len(m[0])-1:]
		if len(rest) > 0 && rest[0] == ' ' {
			rest = rest[1:]
		}
	}
	if m := headName.FindStringSubmatch(rest); m != nil {
		h.Name = strings.Trim(strings.TrimSpace(m[1]), "()[]")
		rest = rest[len(m[0]):]
	}
	rest = strings.TrimSpace(strings.Trim(strings.TrimSpace(rest), ".:"))
	if rest != "" {
		// Text after the head inside the group: this is a sentence in bold,
		// not a head.
		return Head{}, false
	}
	return h, true
}

// FirstHead finds a head in the begin code of an environment, and returns its
// title.
func FirstHead(code string, t *Titles) (string, bool) {
	for i := 0; i < len(code); i++ {
		if code[i] != '{' && code[i] != '\\' {
			continue
		}
		h, _, ok := headAt([]byte(code), i, t)
		if ok {
			return h.Title, true
		}
	}
	return "", false
}

// headAt reads a head at offset i. It returns the head and the offset after
// the head text.
func headAt(text []byte, i int, t *Titles) (Head, int, bool) {
	rest := text[i:]
	if len(rest) > 400 {
		rest = rest[:400]
	}
	if m := fontGroup.FindSubmatchIndex(rest); m != nil {
		_, end, ok := tex.Group(text, i)
		if !ok {
			return Head{}, 0, false
		}
		inner := string(text[i+m[1] : end-1])
		inner = stripFonts(inner)
		h, ok := parseHead(inner, t)
		if !ok {
			return Head{}, 0, false
		}
		h.Form = formOf(string(rest[m[2]:m[3]]))
		return h, afterName(text, end, &h), true
	}
	if m := fontArg.FindSubmatchIndex(rest); m != nil {
		_, end, ok := tex.Group(text, i+m[1]-1)
		if !ok {
			return Head{}, 0, false
		}
		inner := stripFonts(string(text[i+m[1] : end-1]))
		h, ok := parseHead(inner, t)
		if !ok {
			return Head{}, 0, false
		}
		h.Form = formOf(string(rest[m[2]:m[3]]))
		return h, afterName(text, end, &h), true
	}
	if m := fontSwitch.FindSubmatchIndex(rest); m != nil {
		// \bf Theorem 1. Text: the head runs to the first period or colon.
		s := string(rest[m[1]:])
		stop := strings.IndexAny(s, ".:\n")
		if stop < 0 || stop > 120 {
			return Head{}, 0, false
		}
		h, ok := parseHead(s[:stop], t)
		if !ok {
			return Head{}, 0, false
		}
		h.Form = formOf(string(rest[m[2]:m[3]]))
		return h, i + m[1] + stop + 1, true
	}
	if m := plainProof.FindSubmatchIndex(rest); m != nil {
		h, ok := parseHead(string(rest[m[2]:m[3]]), t)
		if !ok || !h.Proof {
			return Head{}, 0, false
		}
		h.Form = "plain"
		return h, i + m[1], true
	}
	return Head{}, 0, false
}

// EndMarks is the regexp of the end marks of a proof in one paper: the marks
// of qedMark, and each command of the paper whose body writes one of them, as
// \def\qeds{\qed\par\medskip}. A command that calls such a command is a
// mark too.
func EndMarks(m *tex.Macros) *regexp.Regexp {
	if m == nil {
		return qedMark
	}
	marks := map[string]bool{}
	for round := 0; round < 3; round++ {
		grew := false
		for name, c := range m.Commands {
			if marks[name] || c.Args > 0 || !strings.HasPrefix(name, `\`) {
				continue
			}
			ok := qedMark.MatchString(c.Body)
			for other := range marks {
				if !ok && callsCommand(c.Body, other) {
					ok = true
				}
			}
			if ok {
				marks[name], grew = true, true
			}
		}
		if !grew {
			break
		}
	}
	if len(marks) == 0 {
		return qedMark
	}
	var alts []string
	for name := range marks {
		alts = append(alts, regexp.QuoteMeta(name[1:]))
	}
	sort.Strings(alts)
	return regexp.MustCompile(qedMark.String() + `|\\(?:` + strings.Join(alts, "|") + `)\b`)
}

// callsCommand tells if code calls the command name, with name as \qeds.
func callsCommand(code, name string) bool {
	for i := strings.Index(code, name); i >= 0; {
		j := i + len(name)
		if j >= len(code) || !isLetter(code[j]) {
			return true
		}
		k := strings.Index(code[j:], name)
		if k < 0 {
			return false
		}
		i = j + k
	}
	return false
}

func isLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '@'
}

// fontBody is the body of a command that only writes its argument in a font,
// after layout: {\bf #1}, \textbf{#1} or \paragraph{#1}.
var fontBody = regexp.MustCompile(`^(?:\{\s*\\(bf|bfseries|sc|scshape|it|itshape|sl|em)\b\s*#1\s*\}|\\(textbf|textsc|textit|emph|paragraph\*?)\s*\{\s*#1\s*\})\s*$`)

// fontMacros is each command of the paper that takes one argument and writes
// it in a font, as \def\parbf#1{\smallskip\noindent{\bf #1}}. The value is
// the font command.
func fontMacros(m *tex.Macros) map[string]string {
	out := map[string]string{}
	if m == nil {
		return out
	}
	for name, c := range m.Commands {
		if c.Args != 1 {
			continue
		}
		body := []byte(c.Body)
		i := skipLayout(body, 0, len(body))
		if f := fontBody.FindSubmatch(body[i:]); f != nil {
			out[name] = string(f[1]) + string(f[2])
		}
	}
	return out
}

var macroCall = regexp.MustCompile(`^\\([a-zA-Z@]+)\s*\{`)

// macroHeadAt reads a head written with one of the font commands of the
// paper, as \parbf{Theorem 2.} or \parit{Proof.}.
func macroHeadAt(text []byte, i int, macros map[string]string, t *Titles) (Head, int, bool) {
	if len(macros) == 0 {
		return Head{}, 0, false
	}
	m := macroCall.FindSubmatchIndex(text[i:min(len(text), i+80)])
	if m == nil {
		return Head{}, 0, false
	}
	font, ok := macros[`\`+string(text[i+m[2]:i+m[3]])]
	if !ok {
		return Head{}, 0, false
	}
	_, end, ok := tex.Group(text, i+m[1]-1)
	if !ok || end-i > 400 {
		return Head{}, 0, false
	}
	h, ok := parseHead(stripFonts(string(text[i+m[1]:end-1])), t)
	if !ok {
		return Head{}, 0, false
	}
	h.Form = formOf(font)
	return h, afterName(text, end, &h), true
}

// stripFonts removes font commands inside a head, as in {\bf Theorem \it 2}.
func stripFonts(s string) string {
	s = regexp.MustCompile(`\\(bf|bfseries|sc|scshape|it|itshape|sl|em|rm|upshape|normalfont|textbf|textsc|textit|emph|textrm|textup)\b\s*`).ReplaceAllString(s, "")
	return strings.NewReplacer("{", "", "}", "", "~", " ").Replace(s)
}

// afterName reads a name or a period after the head group, as in
// \textbf{Theorem 2.1} (Hahn-Banach).
func afterName(text []byte, end int, h *Head) int {
	rest := text[end:min(end+300, len(text))]
	if m := headName.FindSubmatchIndex(rest); m != nil && h.Name == "" && !h.Proof {
		h.Name = strings.Trim(strings.TrimSpace(string(rest[m[2]:m[3]])), "()[]")
		end += m[1]
		rest = text[end:min(end+300, len(text))]
	}
	if m := headEnd.FindIndex(rest); m != nil {
		end += m[1]
	}
	return end
}

func formOf(cmd string) string {
	switch strings.TrimSuffix(cmd, "*") {
	case "bf", "bfseries", "textbf":
		return "bold"
	case "sc", "scshape", "textsc":
		return "smallcaps"
	case "paragraph", "subparagraph":
		return "paragraph"
	}
	return "italic"
}

// starts is each offset in text[from:to] where a paragraph or an \item starts.
func starts(text []byte, from, to int) []int {
	out := []int{from}
	body := text[from:to]
	for _, m := range paraBreak.FindAllIndex(body, -1) {
		out = append(out, from+m[1])
	}
	for _, m := range itemCmd.FindAllIndex(body, -1) {
		out = append(out, from+m[1])
	}
	return out
}

// skipLayout moves past the layout commands at offset i.
func skipLayout(text []byte, i, to int) int {
	for i < to {
		m := layout.FindIndex(text[i:min(to, i+200)])
		if m == nil || m[1] == 0 {
			return i
		}
		i += m[1]
	}
	return i
}

// ProofSections is each section whose title is a proof head, as in "Proof of
// Theorem 1.2". The section is the locator of proof_status elsewhere.
func ProofSections(d *tex.Document, t *Titles) []Head {
	var out []Head
	if d.Tree == nil {
		return nil
	}
	for _, n := range d.Tree.Nodes {
		if n.Kind != "section" {
			continue
		}
		h, ok := parseHead(stripFonts(n.Title), t)
		if !ok || !h.Proof {
			continue
		}
		h.Off, h.Inner, h.End, h.Form = n.From, n.Inner, n.To, "section"
		out = append(out, h)
	}
	return out
}

// FindHeads finds the manual heads of the body of a document.
//
// It looks at the start of each paragraph and each \item, and at each
// section whose title is a head. A head inside a region in skip is ignored:
// the census passes the declared environments that it has already counted,
// so that the head of a theorem environment is not counted twice.
func FindHeads(d *tex.Document, t *Titles, skip []tex.Region) []Head {
	if d.Body == 0 {
		return nil
	}
	var out []Head
	seen := map[int]bool{}
	macros := fontMacros(d.Macros)
	marks := EndMarks(d.Macros)
	for _, p := range starts(d.Text, d.Body, d.End) {
		i := skipLayout(d.Text, p, d.End)
		if i >= d.End || seen[i] || inSkip(skip, i) {
			continue
		}
		h, inner, ok := headAt(d.Text, i, t)
		if !ok {
			h, inner, ok = macroHeadAt(d.Text, i, macros, t)
		}
		if !ok {
			continue
		}
		seen[i] = true
		h.Off, h.Inner = i, inner
		out = append(out, h)
	}
	for _, n := range d.Tree.Nodes {
		if n.Kind != "section" || inSkip(skip, n.From) || seen[n.From] {
			// A \paragraph head is found once at the start of its paragraph.
			continue
		}
		h, ok := parseHead(stripFonts(n.Title), t)
		if !ok || h.Proof || h.Number == "" {
			// A section "Proof of Theorem 2" is a locator for proof_status
			// elsewhere (AX3.6), and a section "Notation" is a section. Only a
			// section that names a numbered item is a head.
			continue
		}
		h.Off, h.Inner, h.Form = n.From, n.Inner, "section"
		out = append(out, h)
	}
	sortHeads(out)
	for k := range out {
		out[k].End, out[k].EndMark = itemEnd(d, out, k, marks)
	}
	return out
}

func inSkip(rs []tex.Region, off int) bool {
	for _, r := range rs {
		if off > r.From && off < r.To {
			return true
		}
	}
	return false
}

func sortHeads(hs []Head) {
	for i := 1; i < len(hs); i++ {
		for j := i; j > 0 && hs[j].Off < hs[j-1].Off; j-- {
			hs[j], hs[j-1] = hs[j-1], hs[j]
		}
	}
}

// itemEnd is where the item of head k ends (AX3.4): the next head, the next
// section, an end-of-proof mark for a proof, or the end of the paragraph or
// of the italic group for a statement.
func itemEnd(d *tex.Document, hs []Head, k int, marks *regexp.Regexp) (int, bool) {
	h := hs[k]
	end := d.End
	if k+1 < len(hs) && hs[k+1].Off < end {
		end = hs[k+1].Off
	}
	if s := nextSection(d, h.Inner); s > 0 && s < end {
		end = s
	}
	if end < h.Inner {
		end = h.Inner
	}
	if h.Form == "section" {
		return end, false
	}
	if h.Proof {
		if m := marks.FindIndex(d.Text[h.Inner:end]); m != nil {
			return h.Inner + m[1], true
		}
		return end, false
	}
	j := skipSpaces(d.Text, h.Inner)
	if j < end && d.Text[j] == '{' {
		if _, e, ok := tex.Group(d.Text, j); ok && e <= end {
			return e, true
		}
	}
	if m := paraBreak.FindIndex(d.Text[h.Inner:end]); m != nil {
		return h.Inner + m[0], false
	}
	return end, false
}

func skipSpaces(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\n' || b[i] == '\t' || b[i] == '\r') {
		i++
	}
	return i
}

func nextSection(d *tex.Document, off int) int {
	for _, n := range d.Tree.Nodes {
		if n.Kind == "section" && n.From > off {
			return n.From
		}
	}
	return 0
}

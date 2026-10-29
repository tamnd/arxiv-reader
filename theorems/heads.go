package theorems

import (
	"bytes"
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
	// Form is how the head is written: bold, italic, smallcaps, sans, plain,
	// paragraph or section.
	Form string
	// EndMark is true when an end-of-proof mark or the end of an italic group
	// ends the item.
	EndMark bool
	// Code is the body of the command of the paper that writes the head, as
	// \theorem, whose counter commands give the number.
	Code string
	// Cited is the key of the work whose item the head restates, as AB13 for
	// "\cite[Assumption 2.4]{AB13}".
	Cited string
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
	layout = regexp.MustCompile(`^(?:\s+|\\(?:noindent|indent|par|smallbreak|medbreak|bigbreak|medskip|bigskip|smallskip|newline|linebreak|leavevmode|vfill|goodbreak|nopagebreak|pagebreak)\b|\\(?:vskip|hskip|kern)\s*-?[\d.]+\s*(?:pt|em|ex|mm|cm|in|bp|truecm|truept|mu)?(?:\s*plus\s*[\d.]+\s*\w*)?(?:\s*minus\s*[\d.]+\s*\w*)?|\\(?:vspace|hspace)\*?\s*\{[^{}]*\}|\\(?:fbox|framebox)\s*(?:\[[^\]]*\]\s*)*\{|\\parbox\s*(?:\[[^\]]*\]\s*)*\{[^{}]*\}\s*\{|\\!|\{[ \t]*\n|\\\\(?:\[[^\]]*\])?)`)

	fontGroup  = regexp.MustCompile(`^\{\s*(?:\\noindent\s*)?\\(bf|bfseries|sc|scshape|it|itshape|sl|slshape|em|sf|sffamily|textbf|underline)\b\s*`)
	fontArg    = regexp.MustCompile(`^\\(textbf|textsc|textit|emph|textsl|textsf|underline|paragraph\*?|subparagraph\*?|bfseries|itshape)\s*\{`)
	fontSwitch = regexp.MustCompile(`^\\(bf|bfseries|sc|it|itshape|sl)\b\s*`)
	// proofCmd is a proof that starts with the command of the proof
	// environment and no \begin, as in "\proof We show ... \qed". amsthm
	// and amsart define \proof, and old papers use it so.
	proofCmd   = regexp.MustCompile(`^\\(?:proof|pf)\*?(?:\s*\[([^\]]{1,120})\])?(?:[^a-zA-Z@]|$)`)
	plainProof = regexp.MustCompile(`^(Proof|PROOF|Proof of [^.:\n]{1,80}|Sketch of proof|Sketch of the proof|D[ée]monstration|Beweis)\s*[.:]`)

	headNumber = regexp.MustCompile(`^\s*~?\s*((?:\d+(?:\.\d+)*(?:[a-z]|[A-Z])?'*)|(?:[A-Z]{1,3}\d+(?:\.\d+)*[a-z]?'*)|(?:One|Two|Three|Four|Five|Six|Seven|Eight|Nine|Ten)|(?:[A-Z](?:\.\d+)*'*)|(?:[IVXLC]+(?:\.\d+)*)|(?:\\ref\s*\{[^}]*\}(?:[A-Za-z]'*)?)|(?:[A-Z]?\$[^$]{1,30}\$(?:\^\+)?))(?:\s|\.|:|\(|\[|$|\))`)
	headName   = regexp.MustCompile(`^\s*(\(([^()]|\([^()]*\)){0,200}\)|\[[^\]]{0,200}\])`)
	headEnd    = regexp.MustCompile(`^\s*[.:]`)
	headCite   = regexp.MustCompile(`^\s*(?:\$\s*\^\s*\{?\s*\[[^\]$]{1,40}\]\s*\}?\s*\$)`)

	qedMark    = regexp.MustCompile(`\\(qed|qedsymbol|qedhere|QED|endproof|BOX|Box|blacksquare|square|qquad\s*\\Box|hfill\s*\$\s*\\(?:Box|square|blacksquare)\s*\$|endpf|eop)\b|\$\s*\\(?:Box|square|blacksquare)\s*\$|\bQ\.\s?E\.\s?D\.`)
	envEnd     = regexp.MustCompile(`\\end\s*\{[^{}]{1,64}\}[ \t\r]*\n?`)
	paraBreak  = regexp.MustCompile(`\n[ \t\r]*\n|\\(?:par|smallbreak|medbreak|bigbreak)\b`)
	itemCmd    = regexp.MustCompile(`\\item\b(\s*\[[^\]]*\])?`)
	proofOfRef = regexp.MustCompile(`(?i)^(?:(?:the|an|a)\s+)?(?:(?:alternative|another|second|new)\s+proof|proof|sketch of (?:the )?proof|outline of (?:the )?proof|démonstration|beweis)(?:\s+of|\s+for|\s+de|\s+du|\s+von)?\s*(.*)$`)
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
	// "Proposision 9.2.7", "Thoerem B": a long title with one letter wrong
	// or two letters swapped. The title is then the right one.
	// "Main Thoerem B" has a qualifier before the typo.
	for k := 0; k < min(2, len(words)); k++ {
		if k == 1 && !qualifier[strings.ToLower(words[0])] {
			break
		}
		w := strings.TrimRight(words[k], ".:")
		for _, title := range typoTitles {
			if len(w) >= 7 && oneEdit(strings.ToLower(w), strings.ToLower(title)) {
				return title, s[indexAfterWords(s, k+1):], true
			}
		}
	}
	return "", "", false
}

// qualifier is a word that can come before a title with a typo.
var qualifier = map[string]bool{"main": true, "key": true, "principal": true, "technical": true}

// typoTitles is the titles that matchTitle reads with a typo.
var typoTitles = []string{"Theorem", "Proposition", "Corollary", "Definition", "Conjecture"}

// oneEdit is whether a and b differ by one letter changed, added or removed,
// or by two letters next to each other swapped.
func oneEdit(a, b string) bool {
	if a == b {
		return false
	}
	if len(a) == len(b) {
		var diff []int
		for i := range a {
			if a[i] != b[i] {
				diff = append(diff, i)
			}
		}
		return len(diff) == 1 || len(diff) == 2 && diff[1] == diff[0]+1 && a[diff[0]] == b[diff[1]] && a[diff[1]] == b[diff[0]]
	}
	if len(a) < len(b) {
		a, b = b, a
	}
	if len(a) != len(b)+1 {
		return false
	}
	for i := range b {
		if a[i] != b[i] {
			return a[i+1:] == b[i:]
		}
	}
	return true
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

var (
	// numberGroup is a group with only a number in it.
	numberGroup = regexp.MustCompile(`^\s*\{\s*(\d+(?:\.\d+)*[a-z]?)\s*\}`)
	// caseNumber is the number of a case in parentheses, as (1.1) or (ii).
	caseNumber = regexp.MustCompile(`^(?:\d+(?:\.\d+)*[a-z]?|[ivx]+|[a-z])$`)
)

// lastTitle is each title that a head can end with after a name, as
// "Cancelation axiom." or "Cylindrical case:".
var lastTitle = map[string]bool{"axiom": true, "case": true, "example": true, "examples": true, "theorem": true, "lemma": true, "proposition": true, "corollary": true, "conjecture": true, "problem": true, "question": true, "remark": true, "remarks": true, "definition": true, "claim": true, "criterion": true, "principle": true}

// namedHead reads a head of up to four words whose last word is a title, as
// "Cancelation axiom." The words before it are its name.
func namedHead(inner string, t *Titles) (Head, bool) {
	s := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(inner), ".:"))
	words := strings.Fields(s)
	if len(words) < 2 || len(words) > 4 || strings.ContainsAny(s, `\{}$`) {
		return Head{}, false
	}
	last := strings.ToLower(words[len(words)-1])
	if !lastTitle[last] {
		return Head{}, false
	}
	item := t.Kind(last)
	if item == Unknown || item == "proof" {
		return Head{}, false
	}
	return Head{Title: words[len(words)-1], Item: item, Name: strings.Join(words[:len(words)-1], " ")}, true
}

// citedHead is a head that cites an item of another work, as
// "\cite[Assumption 2.4]{AB13}: Growth."
// stripFonts has taken the braces off the key.
var citedHead = regexp.MustCompile(`^\\cite[tp]?\*?\s*\[([^\[\]{}]{1,60})\]\s*\{?([^\s{}]{1,80}?)[.:]?\}?(?:\s+[^$\\]{0,80})?$`)

// citedItem reads a head that restates an item of a cited work. The item
// has no number of its own in the paper; its name is the citation, as
// "[AB13, Assumption 2.4]".
func citedItem(m []string, t *Titles) (Head, bool) {
	h, ok := parseHead(m[1], t)
	if !ok || h.Proof || h.Number == "" {
		return Head{}, false
	}
	h.Name, h.Number, h.Cited = "["+m[2]+", "+strings.TrimSpace(m[1])+"]", "", m[2]
	return h, true
}

// parseHead reads "Title Number (Name)." from the text of a head.
func parseHead(inner string, t *Titles) (Head, bool) {
	inner = strings.TrimSpace(inner)
	if m := citedHead.FindStringSubmatch(inner); m != nil {
		return citedItem(m, t)
	}
	title, rest, ok := matchTitle(inner, t)
	if !ok {
		// "The main Theorem A", "An alternative proof": the article is not
		// part of the title.
		if a := article.FindString(inner); a != "" {
			title, rest, ok = matchTitle(inner[len(a):], t)
		}
		if !ok {
			return namedHead(inner, t)
		}
	}
	// "Prop. 9.3.0": the period ends the abbreviation, not the head.
	if strings.HasPrefix(rest, ".") && headNumber.MatchString(rest[1:]+" ") && strings.TrimSpace(rest[1:]) != "" {
		rest = rest[1:]
	}
	h := Head{Title: title, Item: t.Kind(title)}
	if strings.HasPrefix(h.Item, "other/") && h.Item != "other/case" {
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
		h.Number = MathNumber(m[1])
		rest = rest[len(m[0])-1:]
		if len(rest) > 0 && rest[0] == ' ' {
			rest = rest[1:]
		}
	}
	if m := headName.FindStringSubmatch(rest); m != nil {
		h.Name = strings.Trim(strings.TrimSpace(m[1]), "()[]")
		rest = rest[len(m[0]):]
		// "Case (1.1)." numbers the case in parentheses.
		if h.Number == "" && h.Item == "other/case" && caseNumber.MatchString(h.Name) {
			h.Number, h.Name = h.Name, ""
		}
	}
	// "{\bf Case (a): $H=P\times B$ for some ...}" writes the case in bold
	// after its head.
	if h.Item == "other/case" && h.Number != "" && strings.HasPrefix(strings.TrimSpace(rest), ":") {
		return h, true
	}
	rest = strings.TrimSpace(strings.Trim(strings.TrimSpace(rest), ".:"))
	// "Definition 2.1.3.$^{[22]}$" cites the source of the item after its
	// number.
	rest = strings.TrimSpace(strings.Trim(headCite.ReplaceAllString(rest, ""), ".: "))
	if rest != "" {
		// Text after the head inside the group: this is a sentence in bold,
		// not a head.
		return Head{}, false
	}
	return h, true
}

// inProof is the items whose manual heads are read inside a proof too.
var inProof = map[string]bool{"other/case": true, "statement/claim": true, "statement/fact": true}

// PartOfProof tells if an item is a part of a proof, as a claim, so that a
// proof head can follow it inside the proof.
func PartOfProof(item string) bool { return inProof[item] }

var partGap = regexp.MustCompile(`^(?:\s|\\label\s*\{[^{}]*\})*$`)

// partBefore tells if a region of a part of a proof ends just before off,
// with only spaces and labels between.
func partBefore(text []byte, rs []tex.Region, off int) bool {
	for _, r := range rs {
		if r.Part && r.To <= off && off-r.To < 200 && partGap.Match(text[r.To:off]) {
			return true
		}
	}
	return false
}

var (
	noindent    = regexp.MustCompile(`\\noindent\b`)
	displayLine = regexp.MustCompile(`(?:\$\$[^$]{1,1000}\$\$|\\\])[ \t\r]*\n`)
)

var breakNoindent = regexp.MustCompile(`\\\\(?:\[[^\]]*\])?\s*\\noindent\b`)

var article = regexp.MustCompile(`^(?i:the|an|a)\s+`)

// greek is the letters that a number such as Fact $(\alpha)$ writes.
var greek = strings.NewReplacer(`\alpha`, "α", `\beta`, "β", `\gamma`, "γ", `\delta`, "δ", `\epsilon`, "ε", `\varepsilon`, "ε", `\eps`, "ε", `\zeta`, "ζ", `\eta`, "η", `\theta`, "θ", `\iota`, "ι", `\kappa`, "κ", `\lambda`, "λ", `\mu`, "μ", `\nu`, "ν", `\xi`, "ξ", `\pi`, "π", `\rho`, "ρ", `\sigma`, "σ", `\tau`, "τ", `\phi`, "φ", `\chi`, "χ", `\psi`, "ψ", `\omega`, "ω", `\star`, "⋆", `\ast`, "*", `\prime`, "'", `\,`, "", `\;`, "", `\ `, "", "{", "", "}", "", " ", "")

// MathNumber writes a number in math, as D$(\alpha)$, as the PDF prints
// it: D(α).
func MathNumber(n string) string {
	if !strings.Contains(n, "$") {
		return n
	}
	return greek.Replace(strings.ReplaceAll(n, "$", ""))
}

// numberFirst is a counter and a space written before the title of a head.
var numberFirst = regexp.MustCompile(`(\{\s*\\(?:bf|bfseries|sc|scshape|it|itshape|sl)\b\s*)\\the[a-zA-Z]+\s*[.:]?\s*(?:\\hspace\*?\s*\{[^{}]*\}|\\quad|\\qquad|\\enspace|~|\\ )?\s*`)

// FirstHead finds a head in the begin code of an environment, and returns its
// title.
func FirstHead(code string, t *Titles) (string, bool) {
	// The number that the code writes before the title, as in {\bf
	// \theitem.\hspace{1em}Lemma.}, is not part of the title.
	code = numberFirst.ReplaceAllString(code, "$1")
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
	if m := proofCmd.FindSubmatchIndex(rest); m != nil {
		h, ok := Head{}, false
		if m[2] >= 0 {
			h, ok = parseHead(stripFonts(string(rest[m[2]:m[3]])), t)
		}
		if !ok || !h.Proof {
			if h, ok = parseHead("Proof", t); !ok {
				return Head{}, 0, false
			}
		}
		end := m[1]
		if m[1] > 0 && !isLetter(rest[m[1]-1]) && rest[m[1]-1] != ']' {
			end--
		}
		h.Form = "command"
		return h, i + end, true
	}
	if m := plainProof.FindSubmatchIndex(rest); m != nil {
		h, ok := parseHead(string(rest[m[2]:m[3]]), t)
		if !ok || !h.Proof {
			return Head{}, 0, false
		}
		h.Form = "plain"
		return h, i + m[1], true
	}
	// "{{\emph{Remark 1:}}} In the high region": a group holds the head and
	// nothing else.
	if text[i] == '{' {
		if _, end, ok := tex.Group(text, i); ok {
			if j := skipSpaces(text, i+1); j < end && (text[j] == '{' || text[j] == '\\') {
				if h, after, ok := headAt(text, j, t); ok && strings.TrimSpace(string(text[after:end-1])) == "" {
					return h, end, true
				}
			}
		}
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

var (
	stepsCounter = regexp.MustCompile(`\\(?:refstepcounter|stepcounter)\s*\{`)
	theCounter   = regexp.MustCompile(`\\the[a-zA-Z]+`)
	counterRep   = regexp.MustCompile(`\\(?:the[a-zA-Z]+|arabic|roman|Roman|alph|Alph|fnsymbol)\b(?:\s*\{[^{}]*\})?`)
	commandName  = regexp.MustCompile(`^\\([a-zA-Z@]+)(?:[^a-zA-Z@]|$)`)
)

// headCommands is each command of the paper with no argument that steps a
// counter and writes a head, as
//
//	\renewcommand{\thetheorem}{{Theorem \thesection.\arabic{theorem}}}
//	\newcommand{\theorem}{\refstepcounter{theorem}{\bf \thetheorem:\ }}
//
// used as "\theorem Each graph is finite." The head of the value has no
// number: the counter model gives it from the code of the command.
func headCommands(m *tex.Macros, t *Titles) map[string]Head {
	out := map[string]Head{}
	if m == nil {
		return out
	}
	for name, c := range m.Commands {
		if c.Args != 0 {
			continue
		}
		if !stepsCounter.MatchString(c.Body) {
			if h, ok := plainHeadCommand(c.Body, t); ok {
				out[name] = h
			}
			continue
		}
		body := c.Body
		// \the<counter> of the paper can write the title, so it is put in
		// before the counters are.
		for range 2 {
			body = theCounter.ReplaceAllStringFunc(body, func(the string) string {
				if d, ok := m.Commands[the]; ok && d.Args == 0 && !strings.Contains(d.Body, the) {
					return d.Body
				}
				return the
			})
		}
		body = counterRep.ReplaceAllString(body, "1")
		body = strings.ReplaceAll(body, `\ `, " ")
		b := []byte(body)
		for i := 0; i < len(b); i++ {
			if b[i] != '{' && b[i] != '\\' {
				continue
			}
			if h, _, ok := headAt(b, i, t); ok {
				h.Number, h.Name, h.Target, h.Code = "", "", "", c.Body
				out[name] = h
				break
			}
		}
	}
	return out
}

// plainFont is a font command in the body of a head command.
var plainFont = regexp.MustCompile(`\\(bf|bfseries|sc|scshape|it|itshape|sl|em|textbf|textsc|textit|emph|underline)\b`)

var headStop = regexp.MustCompile(`[A-Za-z]\}?\s*[.:]\s*\}?\s*(?:\\ |~)?\s*\}?$`)

var paraLayout = regexp.MustCompile(`\\(?:noindent|par|smallskip|medskip|bigskip|smallbreak|medbreak|bigbreak|vskip|vspace)\b`)

// plainHeadCommand reads the head of a command with no argument that writes
// a head in a font and steps no counter, as
//
//	\newcommand{\claim}{{\noindent \bf Claim }}
//
// used as "\claim The set is open." The head has no number.
func plainHeadCommand(body string, t *Titles) (Head, bool) {
	// The command starts a paragraph itself, with \noindent or a skip, or
	// it ends the head with a period, as {\em Proof}. : a command \Thm for
	// \textbf{Theorem} is a word used in a sentence.
	f := plainFont.FindStringSubmatch(body)
	if f == nil || !paraLayout.MatchString(body) && !headStop.MatchString(body) || strings.Contains(body, "#") || strings.Contains(body, `\begin`) || strings.Contains(body, `\end`) {
		return Head{}, false
	}
	b := []byte(body)
	var s strings.Builder
	for i := 0; i < len(b); {
		if j := skipLayout(b, i, len(b)); j > i {
			s.WriteByte(' ')
			i = j
			continue
		}
		s.WriteByte(b[i])
		i++
	}
	text := strings.ReplaceAll(stripFonts(strings.ReplaceAll(s.String(), `\underline`, "")), `\ `, " ")
	h, ok := parseHead(text, t)
	if !ok || h.Number != "" {
		return Head{}, false
	}
	h.Form = formOf(f[1])
	return h, true
}

// commandStarts is each offset where a head command of the paper whose body
// starts a paragraph is used, as \def\Proof{\medbreak\noindent{\bf Proof.}}
// on the line after a lemma: the command starts a paragraph wherever it is.
func commandStarts(d *tex.Document, heads map[string]Head) []int {
	var out []int
	for name := range heads {
		c, ok := d.Macros.Commands[name]
		if !ok || !paraLayout.MatchString(c.Body) {
			continue
		}
		at := regexp.MustCompile(regexp.QuoteMeta(name) + `(?:[^a-zA-Z@]|$)`)
		for _, m := range at.FindAllIndex(d.Text[d.Body:d.End], -1) {
			out = append(out, d.Body+m[0])
		}
	}
	return out
}

// commandHeadAt reads a head written with one of the head commands of the
// paper.
func commandHeadAt(text []byte, i int, heads map[string]Head) (Head, int, bool) {
	if len(heads) == 0 {
		return Head{}, 0, false
	}
	m := commandName.FindSubmatchIndex(text[i:min(len(text), i+80)])
	if m == nil {
		return Head{}, 0, false
	}
	h, ok := heads[`\`+string(text[i+m[2]:i+m[3]])]
	if !ok {
		return Head{}, 0, false
	}
	return h, i + m[3], true
}

// stripFonts removes font commands inside a head, as in {\bf Theorem \it 2}.
func stripFonts(s string) string {
	s = regexp.MustCompile(`\\(bf|bfseries|sc|scshape|it|itshape|sl|em|sf|sffamily|rm|upshape|normalfont|textbf|textsc|textit|textsf|emph|textrm|textup)\b\s*`).ReplaceAllString(s, "")
	// The braces of a reference stay, so that "Proof of Theorem \ref{T2}"
	// keeps the label.
	refs := refArg.FindAllStringIndex(s, -1)
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		in := false
		for _, r := range refs {
			if i >= r[0] && i < r[1] {
				in = true
			}
		}
		switch {
		case in:
			b.WriteByte(s[i])
		case s[i] == '{' || s[i] == '}':
		case s[i] == '~':
			b.WriteByte(' ')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// refArg is a reference command and its argument, as \ref{T2}, \cref{a,b},
// or a command of the paper such as \refmt{main}.
var refArg = regexp.MustCompile(`\\[A-Za-z]*ref[A-Za-z]*\*?\s*\{[^{}]*\}`)

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
	case "sf", "sffamily", "textsf":
		return "sans"
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
	// A head on the line after the end of an environment starts an item, as
	// a proof head after \end{lemma}, on the same line or the next.
	for _, m := range envEnd.FindAllIndex(body, -1) {
		out = append(out, from+m[1])
	}
	// "...\\[5pt] \noindent \textbf{Claim 1.}": a line break and \noindent
	// start a line as a paragraph does.
	for _, m := range breakNoindent.FindAllIndex(body, -1) {
		out = append(out, from+m[1])
	}
	// "...subcases; \noindent \textbf{Case 1:} If": the author starts a
	// head with \noindent inside a paragraph.
	for _, m := range noindent.FindAllIndex(body, -1) {
		// {\noindent\bf Proof.} is read at the group.
		if b := bytes.TrimRight(body[:m[0]], " \t\r\n"); len(b) > 0 && b[len(b)-1] == '{' {
			continue
		}
		out = append(out, from+m[0])
	}
	// A head on the line after a display, as "$$ ... $$\n{\bf Case I}.",
	// starts an item.
	for _, m := range displayLine.FindAllIndex(body, -1) {
		out = append(out, from+m[1])
	}
	return out
}

// sentenceLines is the start of each line whose line before ends a sentence,
// as "{\bf Case II}. Suppose ..." after "... follows from (2.1)." in one
// paragraph.
func sentenceLines(text []byte, from, to int) []int {
	var out []int
	for _, m := range sentenceEnd.FindAllIndex(text[from:to], -1) {
		out = append(out, from+m[1])
	}
	return out
}

var sentenceEnd = regexp.MustCompile(`(?:[.$]|\\\\)[ \t\r]*\n`)

// skipLayout moves past the layout commands at offset i, and past the
// commands of the paper in own, as \sn for \smallskip\noindent.
func skipLayout(text []byte, i, to int, own ...map[string]bool) int {
	for i < to {
		m := layout.FindIndex(text[i:min(to, i+200)])
		if m == nil || m[1] == 0 {
			if len(own) == 0 || !own[0][commandAt(text, i, to)] {
				return i
			}
			m = []int{0, len(commandAt(text, i, to))}
		}
		i += m[1]
	}
	return i
}

var layoutName = regexp.MustCompile(`^\\[a-zA-Z@]+`)

// commandAt is the command name at offset i, with its backslash, or "".
func commandAt(text []byte, i, to int) string {
	return string(layoutName.Find(text[i:min(to, i+40)]))
}

// layoutCommands is each command of the paper with no argument whose body is
// layout only, as \def\sn{\smallskip\noindent}. A head can follow one at the
// start of a paragraph.
func layoutCommands(m *tex.Macros) map[string]bool {
	out := map[string]bool{}
	if m == nil {
		return out
	}
	for name, c := range m.Commands {
		body := []byte(strings.TrimSpace(c.Body))
		if c.Args == 0 && len(body) > 0 && skipLayout(body, 0, len(body)) == len(body) {
			out[name] = true
		}
	}
	// A command can call another one, as \def\mn{\sn\medskip}.
	for changed := true; changed; {
		changed = false
		for name, c := range m.Commands {
			body := []byte(strings.TrimSpace(c.Body))
			if !out[name] && c.Args == 0 && len(body) > 0 && skipLayout(body, 0, len(body), out) == len(body) {
				out[name], changed = true, true
			}
		}
	}
	return out
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

var (
	cmdWord   = regexp.MustCompile(`\\[A-Za-z]+`)
	numberDef = regexp.MustCompile(`^\s*(\d+(?:\.\d+)*[a-z]?)\s*$`)
)

// numberCommands is each command of the paper with no argument whose body is
// a number, as \def\ROOTlem{1.6}.
func numberCommands(m *tex.Macros) map[string]string {
	out := map[string]string{}
	if m == nil {
		return out
	}
	for name, mac := range m.Commands {
		if mac.Args == 0 && mac.How == "def" {
			if n := numberDef.FindStringSubmatch(mac.Body); n != nil {
				out[name] = n[1]
			}
		}
	}
	return out
}

// FindHeads finds the manual heads of the body of a document.
//
// It looks at the start of each paragraph and each \item, and at each
// section whose title is a head. A head inside a region in skip is ignored:
// the census passes the declared environments that it has already counted,
// so that the head of a theorem environment is not counted twice.
//
// stops is the offset of each statement or definition environment. A manual
// proof with no end mark ends before the next one that no other environment
// holds, as a paper writes \proof Clear. and then the next lemma.
func FindHeads(d *tex.Document, t *Titles, skip []tex.Region, stops ...int) []Head {
	if d.Body == 0 {
		return nil
	}
	var out []Head
	seen := map[int]bool{}
	macros := fontMacros(d.Macros)
	heads := headCommands(d.Macros, t)
	marks := EndMarks(d.Macros)
	own := layoutCommands(d.Macros)
	nums := numberCommands(d.Macros)
	ps := append(starts(d.Text, d.Body, d.End), commandStarts(d, heads)...)
	nPara := len(ps)
	ps = append(ps, sentenceLines(d.Text, d.Body, d.End)...)
	for k, p := range ps {
		i := skipLayout(d.Text, p, d.End, own)
		if i >= d.End || seen[i] {
			continue
		}
		h, inner, ok := headAt(d.Text, i, t)
		if !ok {
			h, inner, ok = macroHeadAt(d.Text, i, macros, t)
		}
		if !ok {
			h, inner, ok = commandHeadAt(d.Text, i, heads)
		}
		// "\newline\textit{Proof of Claim 1.}" proves the claim just before it.
		proofOfPart := ok && h.Proof && h.Target != "" && len(out) > 0 && out[len(out)-1].Item == "statement/claim"
		if k >= nPara && !proofOfPart && (!ok || (h.Item != "other/case" && h.Item != "statement/claim") || h.Number == "") {
			// A line inside a paragraph starts only a numbered case or claim.
			continue
		}
		// A case head, a claim or a fact is a part of a proof, as "Subfact
		// D(α)" in a stage of a proof, so it is read inside one too, and so is
		// a proof of a part, as "Proof of A1" in the proof of a lemma.
		// A proof head right after one of these heads or items is the proof
		// of it.
		afterPart := h.Proof && (len(out) > 0 && inProof[out[len(out)-1].Item] || partBefore(d.Text, skip, p))
		if !ok || !inProof[h.Item] && !(h.Proof && (h.Target != "" || afterPart || k < nPara && inStatement(skip, i))) && inSkip(skip, i) {
			continue
		}
		seen[i] = true
		// A head command with no number takes the number in the group after
		// it, as \newcommand{\claim}{{\noindent \bf Claim }} and \claim{2}.
		if h.Number == "" && !h.Proof {
			if m := numberGroup.FindSubmatch(d.Text[inner:min(inner+40, d.End)]); m != nil {
				h.Number, inner = string(m[1]), inner+len(m[0])
			}
		}
		// A paper that sets its numbers by hand writes them as commands, as
		// \def\ROOTlem{1.6} and {\bf Lemma \ROOTlem.}, in Plain TeX.
		if h.Number == "" && !h.Proof && len(nums) > 0 {
			for _, c := range cmdWord.FindAll(d.Text[i:inner], -1) {
				if n, ok := nums[string(c)]; ok {
					h.Number = n
					break
				}
			}
		}
		h.Off, h.Inner = i, inner
		out = append(out, h)
	}
	out = append(out, labelClaims(d, out, seen, own)...)
	for _, n := range d.Tree.Nodes {
		if n.Kind != "section" || inSkip(skip, n.From) || seen[n.From] {
			// A \paragraph head is found once at the start of its paragraph.
			continue
		}
		h, ok := parseHead(stripFonts(n.Title), t)
		if !ok || h.Proof || h.Number == "" && h.Cited == "" {
			// A section "Proof of Theorem 2" is a locator for proof_status
			// elsewhere (AX3.6), and a section "Notation" is a section. Only a
			// section that names a numbered item is a head.
			continue
		}
		if h.Item == "other/case" && n.Level <= 3 {
			// "\subsection{Part (a): The expansion}" divides the paper; a case
			// is a \paragraph at most.
			continue
		}
		h.Off, h.Inner, h.Form = n.From, n.Inner, "section"
		out = append(out, h)
	}
	sortHeads(out)
	var top []int
	for _, s := range stops {
		if !inSkip(skip, s) {
			top = append(top, s)
		}
	}
	sort.Ints(top)
	for k := range out {
		out[k].End, out[k].EndMark = itemEnd(d, out, k, marks, top)
	}
	return out
}

// labelClaims is each label that a proof head of the paper names, as
// "\item[{\bf A1.}] ..." or "{\bf A1.} ..." with "{\it Proof of A1}." later:
// the paper states the label as a claim with its own proof.
func labelClaims(d *tex.Document, hs []Head, seen map[int]bool, own map[string]bool) []Head {
	targets := map[string]bool{}
	for _, h := range hs {
		if h.Proof && h.Target != "" {
			if l := LabelKey(h.Target); claimLabel.MatchString(l) {
				targets[l] = true
			}
		}
	}
	if len(targets) == 0 {
		return nil
	}
	var out []Head
	body := d.Text[d.Body:d.End]
	add := func(off, inner int, label string) {
		l := LabelKey(label)
		if !targets[l] || seen[off] {
			return
		}
		seen[off] = true
		out = append(out, Head{Off: off, Inner: inner, Item: "statement/claim", Number: l, Form: "label"})
	}
	for _, m := range itemCmd.FindAllSubmatchIndex(body, -1) {
		if m[2] >= 0 {
			add(d.Body+m[0], d.Body+m[1], string(body[m[2]:m[3]]))
		}
	}
	for _, p := range starts(d.Text, d.Body, d.End) {
		i := skipLayout(d.Text, p, d.End, own)
		if i >= d.End || d.Text[i] != '{' || !fontGroup.Match(d.Text[i:min(i+20, d.End)]) {
			continue
		}
		if _, end, ok := tex.Group(d.Text, i); ok && end-i < 40 {
			add(i, end, string(d.Text[i:end]))
		}
	}
	return out
}

// claimLabel is the label of a claim: a capital and digits, as A1 or B12.
var claimLabel = regexp.MustCompile(`^[A-Z][0-9]{1,3}[a-z']?$`)

// LabelKey is a label without fonts, brackets, spaces and the period.
func LabelKey(s string) string {
	s = stripFonts(s)
	s = strings.NewReplacer(`\,`, "", `\ `, "", `\;`, "", "~", "", "[", "", "]", "").Replace(s)
	return strings.Trim(strings.TrimSpace(s), ".:) (")
}

// EndsAProof tells if an environment of the item ends a manual proof before
// it: a statement other than a claim, or a definition. A claim is often a
// step of the proof.
func EndsAProof(item string) bool {
	return strings.HasPrefix(item, "statement/") && item != "statement/claim" || item == "definition" || strings.HasPrefix(item, "definition/")
}

// inStatement tells if each region that holds off is a statement, and that
// off is not at the start of one.
func inStatement(rs []tex.Region, off int) bool {
	in := false
	for _, r := range rs {
		if off > r.From && off < r.To {
			if !r.Statement || off-r.From < 20 {
				return false
			}
			in = true
		}
	}
	return in
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
func itemEnd(d *tex.Document, hs []Head, k int, marks *regexp.Regexp, stops []int) (int, bool) {
	h := hs[k]
	end := d.End
	// A case head is a part of the item it is in, so it does not end it.
	next := k + 1
	for h.Item != "other/case" && next < len(hs) && hs[next].Item == "other/case" {
		next++
	}
	if next < len(hs) && hs[next].Off < end {
		end = hs[next].Off
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
		for _, s := range stops {
			if s > h.Inner && s < end {
				end = s
				break
			}
		}
		if m := marks.FindIndex(d.Text[h.Inner:end]); m != nil {
			return h.Inner + m[1], true
		}
		// A proof with no mark at its end ends where the environment that
		// holds it does, as "\end{subsection}".
		if e := outerEnd(d.Text, h.Inner, end); e >= 0 {
			return e, false
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

var beginEnd = regexp.MustCompile(`\\(begin|end)\s*\{([^{}]{1,64})\}`)

// outerEnd is the offset of the first "\end" in text[from:to] that closes an
// environment opened before from, or -1.
func outerEnd(text []byte, from, to int) int {
	open := map[string]int{}
	for _, m := range beginEnd.FindAllSubmatchIndex(text[from:to], -1) {
		name := string(text[from+m[4] : from+m[5]])
		if text[from+m[2]] == 'b' {
			open[name]++
		} else if open[name] > 0 {
			open[name]--
		} else {
			return from + m[0]
		}
	}
	return -1
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

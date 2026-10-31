package theorems

import (
	"bytes"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"

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
	// Counter is the counter that the command of a head steps, and Code the
	// body of the command, as \refstepcounter{lemma}{\bf \thelemma:\ }.
	Counter, Code string
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
	// paragraph, and the opening of a box around the head, as
	// \fbox{\parbox{5in}{{\bf Theorem 1:} ...}}, and a space in math, as
	// "$\qquad$ {\bf Step 1:}". Seen in the gold pilot (set X).
	layout = regexp.MustCompile(`^(?:\s+|\$\s*\\(?:bullet|cdot|circ|diamond|quad|qquad)\s*\$|\\(?:textbullet|quad|qquad)\b|\\[,;:!]|~|\\(?:noindent|indent|normalfont|par|medskip|bigskip|smallskip|bigbreak|medbreak|smallbreak|newline|linebreak|leavevmode|vfill|goodbreak|nopagebreak|pagebreak)\b|\\(?:vskip|hskip|kern)\s*-?[\d.]+\s*(?:pt|em|ex|mm|cm|in|bp|truecm|truept|mu)?(?:\s*plus\s*[\d.]+\s*\w*)?(?:\s*minus\s*[\d.]+\s*\w*)?|\\(?:vspace|hspace)\*?\s*\{[^{}]*\}|\\\\(?:\[[^\]]*\])?|\\(?:fbox|framebox)\s*\{|\\parbox\s*(?:\[[^\]]*\]\s*)*\{[^{}]*\}\s*\{)`)

	fontGroup  = regexp.MustCompile(`^\{\s*(?:\\(?:flushleft|raggedright|centering|noindent)\b\s*)*\\(bf|bfseries|sc|scshape|it|itshape|sl|slshape|em|textbf)\b\s*`)
	fontArg    = regexp.MustCompile(`^\\(textbf|textsc|textit|emph|textsl|textsf|underline|ul|uline|paragraph\*?|subparagraph\*?|bfseries|itshape)\s*\{`)
	fontSwitch = regexp.MustCompile(`^\\(bf|bfseries|sc|it|itshape|sl)\b\s*`)
	// proofCmd is a proof that starts with the command of the proof
	// environment and no \begin, as in "\proof We show ... \qed". amsthm
	// and amsart define \proof, and old papers use it so.
	proofCmd   = regexp.MustCompile(`^\\(?:proof|pf)\*?(?:\s*\[([^\]]{1,120})\])?(?:[^a-zA-Z@]|$)`)
	plainProof = regexp.MustCompile(`^(Proof|PROOF|Proof of [^.:\n]{1,80}|Sketch of proof|Sketch of the proof|D[ée]monstration|Beweis)\s*[.:]`)

	// headNumber is the number of a head, as "Theorem BI1", and a prime
	// after it, as "Theorem 1B'", or a letter after a reference, as
	// "Theorem \ref{main}A", a number of a count, as "Case 1/2", and a
	// number in a word, as "Theorem One". Seen in the gold pilot (set X).
	headNumber  = regexp.MustCompile(`^\s*~?\s*((?:(?:One|Two|Three|Four|Five|Six|Seven|Eight|Nine|Ten)|(?:\d+(?:\.\d+)*[a-zA-Z]?(?:/\d+)?)|(?:[A-Z](?:\.\d+)*)|(?:[A-Z]{1,3}\d+[a-z]?)|(?:[IVXLC]+(?:\.\d+)*)|(?:\\ref\s*\{[^}]*\}[A-Z]?))'{0,2})(?:\s|\.|:|\(|$|\)|\[)`)
	headName    = regexp.MustCompile(`^\s*(\(([^()]|\([^()]*\)){0,200}\)|\[[^\]]{0,200}\])`)
	texorpdf    = regexp.MustCompile(`\\texorpdfstring\s*\{((?:[^{}]|\{[^{}]*\})*)\}\s*\{(?:[^{}]|\{[^{}]*\})*\}`)
	numberAfter = regexp.MustCompile(`^[ \t~]*(\d+(?:\.\d+)*|[A-Z]\d*)[ \t]*[.:]`)
	headEnd     = regexp.MustCompile(`^\s*[.:]`)
	citedHead   = regexp.MustCompile(`^\\cite[a-z]*\s*\[([^\]]{1,60})\]\s*\{?[^{}\s]{1,80}\}?\s*[.:]?\s*([^.:]{0,80}?)\s*[.:]?$`)
	headCite    = regexp.MustCompile(`^(?:\$\s*\^\s*\{?\s*\[[^\[\]{}$]{1,40}\]\s*\}?\s*\$|\\cite[a-z]*\s*(?:\[[^\]]*\])?\s*\{?[^{}\s]{1,80}\}?)\s*[.:]?$`)

	qedMark = regexp.MustCompile(`\\(qed|qedsymbol|qedhere|QED|endproof|BOX|Box|blacksquare|square|qquad\s*\\Box|hfill\s*\$\s*\\(?:Box|square|blacksquare)\s*\$|endpf|eop)\b|\$\s*\\(?:Box|square|blacksquare)\s*\$|\b(?:Q\.\s?E\.\s?D|q\.\s?e\.\s?d)\.`)
	envEnd  = regexp.MustCompile(`\\end\s*\{[^{}]{1,64}\}[ \t]*\n`)
	// envEndInline is the end of an environment and more text on its line.
	envEndInline = regexp.MustCompile(`\\end\s*\{[^{}]{1,64}\}[ \t]*[^\s%]`)
	// paraBreak is a blank line, which is \r\n\r\n in a file with CRLF line
	// ends. Seen in the gold pilot (set X).
	paraBreak = regexp.MustCompile(`\n[ \t\r]*\n|\\par\b`)
	itemCmd   = regexp.MustCompile(`\\item\b(\s*\[[^\]]*\])?`)
	// caseFormulaName is a formula and a name in parentheses after "Case".
	caseFormulaName = regexp.MustCompile(`^\$[^$]+\$\s*\([^()$]{1,60}\)\s*[.:]?$`)
	// mathHead is a head in bold math, and the period after it, inside or
	// after the math.
	mathHead = regexp.MustCompile(`^\$\s*\\(?:mathbf|boldsymbol)\s*\{([^{}$]{1,60})\}\s*[.:]?\s*\$\s*[.:]?`)
	// trailCite is a cite at the end of a head.
	trailCite = regexp.MustCompile(`\s*\\cite[a-z]*\s*(?:\[[^\]]*\])?\s*\{?[^{}\s]+\}?$`)
	// numCite is a cite by number in brackets after the number or the name
	// of a head, as "Lemma 1[6,9]" or "Theorem (Schaefer's Theorem) [9]".
	// Seen in the gold pilot (set X).
	numCite = regexp.MustCompile(`^\s*\[\d+(?:\s*[,–-]\s*\d+)*\]`)
	// onName is the name of a head after "on", as "Remark on Horseshoe".
	onName = regexp.MustCompile(`^on\s+([^.:]{1,60})$`)
	// caseOf is "of" and a formula after "Case", as "Case of $X=B$".
	caseOf = regexp.MustCompile(`^of\s+\$[^$]+\$[.:]?$`)
	// caseWord is a number word after "Case" or "Step", and a colon or a
	// period after it, as "Step one:".
	caseWord = regexp.MustCompile(`(?i)^(?:one|two|three|four|five|six|seven|eight|nine|ten|first|second|third|fourth|fifth|final|last)\s*[.:]`)
	// namedHead is a head of capitalized words and a title, with no number.
	// The words after the first one can be in lower case, as "Analytic
	// continuous fundamental theorem". Seen in the gold pilot (set X).
	namedHead  = regexp.MustCompile(`^(?:The\s+)?([A-Z]\S*\s+(?:\S+\s+){0,3})((?i:theorem|lemma|conjecture|principle|inequality|axiom|criterion|construction))$`)
	proofOfRef = regexp.MustCompile(`(?i)^(?:the\s+)?(?:(?:an? )?(?:alternative|another|second) proof|proof|sketch of (?:the )?proof|outline of (?:the )?proof|démonstration|beweis)(?:\s+of|\s+for|\s+de|\s+du|\s+von)?\s*(.*)$`)
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

// misspelledTitle reads a title of one long word that the paper misspells by
// one letter, as "Defnition", "Definiton", "Proposision" or "Thoerem", at the
// start of s. It returns the word as the paper writes it, the kind of the
// title, and the rest of s. A misspelled head must have a number. Seen in the
// gold pilot (set X).
func misspelledTitle(s string, t *Titles) (title, kind, rest string, ok bool) {
	words := strings.Fields(s)
	if len(words) == 0 {
		return "", "", "", false
	}
	w := strings.TrimRight(words[0], ".:")
	if len(w) < 7 || strings.ContainsFunc(w, func(r rune) bool { return !unicode.IsLetter(r) }) {
		return "", "", "", false
	}
	low := strings.ToLower(w)
	for n, k := range t.kinds {
		if len(n) >= 7 && !strings.Contains(n, " ") && n != low && oneEdit(low, n) {
			if kind != "" && kind != k {
				return "", "", "", false
			}
			kind = k
		}
	}
	if kind == "" || kind == "proof" {
		return "", "", "", false
	}
	return w, kind, s[indexAfterWords(s, 1):], true
}

// oneEdit tells whether a and b differ by one insertion, deletion,
// substitution or swap of two letters next to each other.
func oneEdit(a, b string) bool {
	if len(a) > len(b) {
		a, b = b, a
	}
	switch len(b) - len(a) {
	case 0:
		var diff []int
		for i := range len(a) {
			if a[i] != b[i] {
				diff = append(diff, i)
			}
		}
		return len(diff) == 1 || len(diff) == 2 && diff[1] == diff[0]+1 && a[diff[0]] == b[diff[1]] && a[diff[1]] == b[diff[0]]
	case 1:
		i := 0
		for i < len(a) && a[i] == b[i] {
			i++
		}
		return a[i:] == b[i+1:]
	}
	return false
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
// caseOrdinal is a case or a step head that gives its number as a word, as
// "First case" or "Final step", and caseGlued one that writes its number
// glued to the title or in math, as "STEP-1", "Step1" or "Step $1$". Seen in
// the gold pilot (set X).
var (
	caseOrdinal = regexp.MustCompile(`^(?i:(first|second|third|fourth|fifth|sixth|seventh|eighth|ninth|tenth|final|last)\s+(case|step))\b`)
	caseNamed   = regexp.MustCompile(`^((?:[A-Za-z][A-Za-z-]*\s+){1,3})(?i:(case|step))\b`)
	caseGlued   = regexp.MustCompile(`^(?i:(case|step|phase|stage))(?:\s*-\s*|\s*\$\s*|\s*)(\d+)(?:\s*\$)?(\))?`)
	ordinals    = map[string]string{"first": "1", "second": "2", "third": "3", "fourth": "4", "fifth": "5", "sixth": "6", "seventh": "7", "eighth": "8", "ninth": "9", "tenth": "10"}
	notNames    = map[string]bool{"in": true, "this": true, "that": true, "each": true, "either": true, "every": true, "any": true, "which": true, "our": true, "its": true, "one": true, "for": true, "of": true, "a": true, "an": true, "general": true, "special": true, "proof": true}
	headLetter  = regexp.MustCompile(`^[A-Z]\d?'?$`)
	caseLetter  = regexp.MustCompile(`^[a-z0-9]{1,3}$|^[A-Z]$|^(?:[ivx]{1,4}|[IVX]{1,4})$`)
)

// greekNumber is a Greek letter in parentheses, in math or not, as
// $(\alpha)$ or $(\beta)^+$, and greek the letter that each command prints.
var (
	greekNumber = regexp.MustCompile(`^\s*(?:\$\s*\(\s*\\(` + greekNames + `)\s*\)\s*(\^\s*(?:\+|\{\s*\+\s*\}))?\s*\$|\(\s*\$?\s*\\(` + greekNames + `)\s*\$?\s*\))`)
	greek       = map[string]string{"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ", "epsilon": "ε", "varepsilon": "ε", "eps": "ε", "zeta": "ζ", "eta": "η", "theta": "θ", "iota": "ι", "kappa": "κ", "lambda": "λ", "mu": "μ", "nu": "ν", "xi": "ξ", "pi": "π", "rho": "ρ", "sigma": "σ", "tau": "τ", "phi": "φ", "varphi": "φ", "chi": "χ", "psi": "ψ", "omega": "ω"}
)

const greekNames = `alpha|beta|gamma|delta|epsilon|varepsilon|eps|zeta|eta|theta|iota|kappa|lambda|mu|nu|xi|pi|rho|sigma|tau|phi|varphi|chi|psi|omega`

// caseForm writes the head of a case or a step in the form "Case 1" that
// parseHead reads.
func caseForm(s string) string {
	if m := theCase.FindStringSubmatch(s); m != nil {
		// "The case $1\le |x|$." is "Case $1\le |x|$.". Seen in the gold
		// pilot (set X).
		s = strings.ToUpper(m[1][:1]) + strings.ToLower(m[1][1:]) + s[len(m[0]):]
	}
	if m := caseOrdinal.FindStringSubmatch(s); m != nil {
		title := strings.ToUpper(m[2][:1]) + strings.ToLower(m[2][1:])
		if n, ok := ordinals[strings.ToLower(m[1])]; ok {
			title += " " + n
		}
		return title + s[len(m[0]):]
	}
	if m := caseGlued.FindStringSubmatch(s); m != nil && strings.Count(m[0], "$") != 1 {
		// "Case $1$" glues the number in math, but in "Case $1\le |x|$" the
		// digit starts a formula. Seen in the gold pilot (set X).
		return strings.ToUpper(m[1][:1]) + strings.ToLower(m[1][1:]) + " " + m[2] + s[len(m[0]):]
	}
	if m := caseNamed.FindStringSubmatch(s); m != nil && !notNames[strings.ToLower(strings.Fields(m[1])[0])] {
		// "Odd case." or "Surface-of-revolution case:": the words before
		// the title name the case. Seen in the gold pilot (set X).
		title := strings.ToUpper(m[2][:1]) + strings.ToLower(m[2][1:])
		return title + " (" + strings.TrimSpace(m[1]) + ")" + s[len(m[0]):]
	}
	return s
}

var (
	// theCase is "The case" or "The step" before the rest of a head.
	theCase = regexp.MustCompile(`^(?i:the)\s+((?i:case|step))\b`)
	// mathSpan is a formula in dollars.
	mathSpan = regexp.MustCompile(`\$[^$]+\$`)
	// mathJoin is what is left of a name of formulas joined by words.
	mathJoin = regexp.MustCompile(`^(?:\s*(?:and|or|with|,)\s*)+$`)
)

// andTitle is a second title after the first, as " and Terminology".
var andTitle = regexp.MustCompile(`^\s+(?:and|&|\\&)\s+([\p{L}]+)`)

// ofName is the item that a statement follows from, after its title, as
// " of Family $S_1$".
var ofName = regexp.MustCompile(`^\s+((?:of|to)\s+\S.{0,60}?)[.:]?\s*$`)

// mathLabel is a short label in math that is all a head has after its
// title, as $\Omega_1$ or $MK_2$.
var mathLabel = regexp.MustCompile(`^\s*(\$\\?[A-Za-z]+\s*(?:_\s*\{?[A-Za-z0-9]{1,3}\}?)?\$)\s*[.:]?\s*$`)

func parseHead(inner string, t *Titles) (Head, bool) {
	inner = caseForm(strings.TrimSpace(inner))
	if m := citedHead.FindStringSubmatch(inner); m != nil {
		// {\bf \cite[Assumption 4.1]{key}.}: the head states an item of the
		// cited paper again. It is an item of the kind of the title, and the
		// number is the number in the cited paper, not one of this paper.
		// The words after the cite, as in "\cite[Assumption 4.2]{key}
		// Relation between coefficients.", name it. Seen in the gold pilot
		// (set X).
		if h, ok := parseHead(m[1], t); ok && !h.Proof && h.Item != "other/case" {
			h.Name, h.Number = strings.TrimSpace(m[1]), ""
			if m[2] != "" {
				h.Name = m[2]
			}
			return h, true
		}
	}
	title, rest, ok := matchTitle(inner, t)
	kind, misspelled := "", false
	if !ok {
		title, kind, rest, ok = misspelledTitle(inner, t)
		misspelled = ok
	}
	if !ok {
		// "Vizing's conjecture \cite{key}.": a cite after the name is the
		// source. Seen in the gold pilot (set X).
		named := strings.TrimRight(trailCite.ReplaceAllString(strings.TrimRight(inner, ".: "), ""), ".: ")
		if m := namedHead.FindStringSubmatch(named); m != nil && !notNames[strings.ToLower(strings.Fields(m[1])[0])] && (strings.HasPrefix(t.Kind(m[2]), "statement/") || t.Kind(m[2]) == "definition/construction" || t.Kind(m[2]) == "conjecture") {
			// "The Decomposition Theorem", "A'Campo's Theorem", "Cancelation
			// axiom", "Geometric construction" or "Vizing's conjecture": the
			// words before the title name the item. Seen in the gold pilot
			// (set X).
			return Head{Title: m[2], Item: t.Kind(m[2]), Name: strings.TrimSpace(m[1])}, true
		}
		return Head{}, false
	}
	if !misspelled {
		kind = t.Kind(title)
	}
	if m := andTitle.FindStringSubmatch(rest); m != nil && t.Kind(m[1]) != "" {
		// "Notation and Terminology." or "Terminologies and Notations": a
		// second title after "and" is a second name of one item, of the kind
		// of the first. Seen in the gold pilot (set X).
		rest = rest[len(m[0]):]
	}
	// A case or a step head in a font is an item too, of the kind
	// other/case (AX3.4). Seen in the gold pilot (set E).
	h := Head{Title: title, Item: kind}
	if h.Item == "proof" {
		h.Proof = true
		if m := proofOfRef.FindStringSubmatch(strings.TrimRight(inner, ".: ")); m != nil {
			h.Target = strings.TrimSpace(m[1])
		}
		return h, true
	}
	if strings.HasPrefix(rest, ".") && headNumber.MatchString(rest[1:]+" ") {
		// "Prop. 9.3.0": the number comes after the period of an
		// abbreviated title. Seen in the gold pilot (set X).
		rest = rest[1:]
	}
	if m := headNumber.FindStringSubmatch(rest + " "); m != nil {
		h.Number = m[1]
		rest = rest[len(m[0])-1:]
		if len(rest) > 0 && rest[0] == ' ' {
			rest = rest[1:]
		}
	}
	if misspelled && h.Number == "" {
		return Head{}, false
	}
	letter := ""
	if r := strings.TrimLeft(rest, " ~"); h.Number == "" && len(r) > 1 && r[0] >= 'A' && r[0] <= 'Z' && greekNumber.MatchString(r[1:]) {
		// "Subfact D$(\alpha)$:": a capital letter and a Greek letter in
		// parentheses number the head together, as "D(α)". Seen in the gold
		// pilot (set X).
		letter, rest = r[:1], r[1:]
	}
	if m := greekNumber.FindStringSubmatch(rest); m != nil && h.Number == "" {
		// "Fact $(\alpha)$: ...": a Greek letter in parentheses numbers the
		// head, and the number is the letter that the paper prints. Seen in
		// the gold pilot (set X).
		h.Number, rest = letter+"("+greek[m[1]+m[3]]+")", rest[len(m[0]):]
		if m[2] != "" {
			h.Number += "+"
		}
	}
	if m := numCite.FindString(rest); m != "" {
		rest = rest[len(m):]
	}
	rawName := ""
	if strings.HasPrefix(rest, ".") && h.Number != "" && headName.MatchString(rest[1:]) {
		// "Theorem 1.1. (Main Theorem)": a period after the number, and then
		// the name. Seen in the gold pilot (set X).
		rest = rest[1:]
	}
	if m := headName.FindStringSubmatch(rest); m != nil {
		rawName = strings.TrimSpace(m[1])
		h.Name = strings.Trim(rawName, "()[]")
		rest = rest[len(m[0]):]
	}
	if m := ofName.FindStringSubmatch(rest); m != nil && h.Number == "" && strings.HasPrefix(h.Item, "statement/") {
		// "Corollaries of Family $S_1$" or "Corollary to Theorem B": the
		// item that a statement follows from names it. Seen in the gold
		// pilot (set X).
		h.Name, rest = strings.TrimSpace(m[1]), ""
	}
	if m := mathLabel.FindStringSubmatch(rest); m != nil && h.Number == "" && h.Name == "" && h.Item != "other/case" {
		// "Conditions $\Omega_1$:" or "Assumption $A$.": a short label in
		// math after the title names the item, which has no number. Seen in
		// the gold pilot (set X).
		h.Name, rest = m[1], ""
	}
	if h.Item == "other/case" && h.Number == "" && caseLetter.MatchString(h.Name) {
		// "Case (a): the set is bounded": the letter in parentheses numbers
		// the case, and the number keeps the parentheses as the paper
		// writes them. Seen in the gold pilot (set X).
		h.Number, h.Name = rawName, ""
	}
	if h.Item != "other/case" && h.Number == "" && strings.HasPrefix(rawName, "(") && headLetter.MatchString(h.Name) {
		// "Theorem (A). Let ...": a capital letter in parentheses numbers
		// the head. Seen in the gold pilot (set X).
		h.Number, h.Name = h.Name, ""
	}
	if h.Item == "other/case" && h.Name == "" {
		// "Step 1: Bound the size of the core set" or "Case: $w=0$": the
		// words after the number of a case or a step name it. Seen in the
		// gold pilot (set X).
		if r := strings.TrimSpace(rest); len(r) > 1 && (r[0] == ':' || h.Number != "" && r[0] == '.') {
			h.Name, rest = strings.TrimRight(strings.TrimSpace(r[1:]), ". "), ""
		}
	}
	if h.Item == "other/case" {
		r := strings.TrimSpace(rest)
		switch {
		case h.Name != "" && len(r) > 1 && r[0] == ':':
			// "Case B1 (i): $r \leq C$.": the words after the colon go
			// with the name. Seen in the gold pilot (set X).
			h.Name, rest = h.Name+": "+strings.TrimRight(strings.TrimSpace(r[1:]), ". "), ""
		case h.Name == "" && h.Number != "" && strings.HasPrefix(r, "-") && len(strings.Trim(r, "-.: ")) > 0:
			// "Case 1 -- $r(x)\neq 0$:": the words after the dash name the
			// case. Seen in the gold pilot (set X).
			h.Name, rest = strings.TrimSpace(strings.Trim(r, "-.: ")), ""
		case h.Name == "" && h.Number == "" && caseWord.MatchString(r):
			// "Step one: uniformly continuous data.": a number word, which
			// is not a number of the head, and the words after it name the
			// step. Seen in the gold pilot (set X).
			h.Name, rest = strings.TrimSpace(strings.TrimRight(r, ".: ")), ""
		case h.Name == "" && caseFormulaName.MatchString(r):
			// "Case $-\infty<A<B<\infty$ (Ellipsoid).": a formula and a name in
			// parentheses. Seen in the gold pilot (set X).
			h.Name, rest = strings.TrimSpace(strings.TrimRight(r, ".: ")), ""
		case h.Name == "" && caseOf.MatchString(r):
			// "Case of $X=B$": the formula after "of" names the case. Seen
			// in the gold pilot (set X).
			h.Name, rest = strings.TrimSpace(strings.TrimRight(r[2:], ".: ")), ""
		case h.Name == "" && strings.HasPrefix(r, "$") && strings.HasSuffix(strings.TrimRight(r, ".: "), "$") && mathJoin.MatchString(mathSpan.ReplaceAllString(strings.TrimRight(r, ".: "), "")):
			// "Case $s \neq 1$ and $K=K^{rbf}$.": formulas that "and", "or"
			// or "with" join name the case. Seen in the gold pilot (set X).
			h.Name, rest = strings.TrimSpace(strings.TrimRight(r, ".: ")), ""
		case h.Name == "" && strings.HasPrefix(r, "$") && (strings.HasSuffix(r, ":") || strings.Count(r, "$") == 2 && strings.HasSuffix(strings.TrimRight(r, ".: "), "$")):
			// "Case $p = y_2$:", "Case $u=\epsilon_1.$" or "Case I $\eta=1$:":
			// the formula names the case. Seen in the gold pilot (set X).
			h.Name, rest = strings.TrimSpace(strings.TrimRight(r, ".: ")), ""
		}
	}
	rest = strings.TrimSpace(strings.Trim(strings.TrimSpace(rest), ".:"))
	if m := onName.FindStringSubmatch(rest); m != nil && h.Name == "" && h.Item != "other/case" {
		// "Remark on Horseshoe:": the words after "on" name the head. Seen
		// in the gold pilot (set X).
		h.Name, rest = m[1], ""
	}
	if m := numCite.FindString(rest); m != "" && strings.TrimSpace(rest[len(m):]) == "" {
		rest = ""
	}
	if headCite.MatchString(rest) {
		// "\textbf{Definition 2.1.3.$^{[22]}$}": a citation of the source
		// after the number. Seen in the gold pilot (set X).
		rest = ""
	}
	if rest != "" {
		// Text after the head inside the group: this is a sentence in bold,
		// not a head.
		return Head{}, false
	}
	return h, true
}

var (
	// leadNumber is the value of a counter and its period before the title of
	// a head.
	leadNumber = regexp.MustCompile(`(?:\\the[a-zA-Z]+\b|\\(?:arabic|roman|Roman|alph|Alph)\s*\{[^{}]*\})(?:\s*[.:])?`)
	headSpace  = regexp.MustCompile(`\\hspace\*?\s*\{[^{}]*\}|\\(?:quad|qquad|enspace)\b|\\[,;! ]`)
)

// FirstHead finds a head in the begin code of an environment, and returns its
// title.
func FirstHead(code string, t *Titles) (string, bool) {
	// A head can print the number first, as {\bf \theitem.\hspace{1em}Lemma.},
	// and the title is read after it. Seen in the gold pilot (set E).
	code = headSpace.ReplaceAllString(leadNumber.ReplaceAllString(code, ""), " ")
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

// upright is a font switch that sets the body upright, which can come before
// a head, as \rm.
var upright = regexp.MustCompile(`^\\(?:rm|upshape|normalfont)\b`)

// StartHead reads a head in a font at the start of the body text[i:to] of an
// environment, after layout, labels and upright font switches, as
// "\begin{pkt}\rm \label{a} {\bf Definition.}". An environment with no
// title takes the kind of the head. Seen in the gold pilot (set X).
func StartHead(text []byte, i, to int, t *Titles) (Head, bool) {
	for {
		j := skipLabels(text, skipLayout(text, i, to), to)
		if m := upright.FindIndex(text[j:min(to, j+20)]); m != nil {
			j += m[1]
		}
		if j == i {
			break
		}
		i = j
	}
	if i >= to {
		return Head{}, false
	}
	h, inner, ok := headAt(text, i, t)
	if !ok || inner > to {
		return Head{}, false
	}
	return h, true
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
		// "\bf {Theorem A}.": the title can be in a group. Seen in the gold
		// pilot (set X).
		h, ok := parseHead(stripFonts(s[:stop]), t)
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
	if m := mathHead.FindSubmatchIndex(rest); m != nil {
		// "$\mathbf{Case \ 1}.$": a head in bold math. Seen in the gold
		// pilot (set X).
		h, ok := parseHead(strings.NewReplacer(`\ `, " ", "~", " ").Replace(string(rest[m[2]:m[3]])), t)
		if !ok {
			return Head{}, 0, false
		}
		h.Form = "bold"
		return h, i + m[1], true
	}
	if len(rest) > 0 && rest[0] == '{' {
		// {{\emph{Remark 1:}}}: a group that holds only a head is the head.
		// Seen in the gold pilot (set X).
		if _, end, ok := tex.Group(text, i); ok {
			j := skipSpaces(text, i+1)
			// {\flushleft{\emph{Proof:} Refer to ...}}: after an alignment,
			// the group holds the head and the text. Seen in the gold pilot
			// (set X).
			m := alignCmd.FindIndex(text[j:min(end, j+20)])
			if m != nil {
				j = skipSpaces(text, j+m[1])
				if j < end && text[j] == '{' {
					j = skipSpaces(text, j+1)
				}
			}
			if j < end-1 {
				if h, e, ok := headAt(text, j, t); ok && skipSpaces(text, e) == end-1 {
					return h, afterName(text, end, &h), true
				} else if ok && m != nil && e < end {
					return h, e, true
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
var fontBody = regexp.MustCompile(`^(?:\{\s*\\(bf|bfseries|sc|scshape|it|itshape|sl|em)\b\s*#1\s*[.:]?\s*\}|\\(textbf|textsc|textit|emph|paragraph\*?)\s*\{\s*#1\s*[.:]?\s*\})\s*[.:]?\s*$`)

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

// headStep is the step of the counter of a head command.
var headStep = regexp.MustCompile(`\\refstepcounter\s*\{([^{}]+)\}`)

var macroCall = regexp.MustCompile(`^\\([a-zA-Z@]+)\s*\{`)

var (
	// counterCmd is a command that sets or steps a counter, which a head
	// command does before it writes the head.
	counterCmd = regexp.MustCompile(`\\(?:setcounter|addtocounter)\s*\{[^{}]*\}\s*\{(?:[^{}]|\{[^{}]*\})*\}|\\(?:refstepcounter|stepcounter)\s*\{[^{}]*\}`)
	// counterValue is the value of a counter in the text of a head, as
	// \thesection.\arabic{lemma}.
	counterValue = regexp.MustCompile(`\\(?:the[a-zA-Z]+|arabic|roman|Roman|alph|Alph|fnsymbol)\b(?:\s*\{[^{}]*\})?`)
	headFont     = regexp.MustCompile(`\\(bf|bfseries|sc|scshape|it|itshape|sl|em|textbf|textsc|textit|emph)\b`)
	headCall     = regexp.MustCompile(`^\\([a-zA-Z@]+)`)
)

// commandHead is the head that a head command writes. A command with
// arguments writes its first argument as the number, as
// \newcommand{\proofpart}[2]{\emph{Step #1: #2}}.
type commandHead struct {
	Head
	mac *tex.Macro
}

// argNumber is a head whose number is the first argument of its command.
var argNumber = regexp.MustCompile(`^([A-Za-z][A-Za-z ]{0,30}?)\s*#1(?:[^0-9]|$)`)

// commandHeads is each command of the paper whose body writes a head in a
// font, as \newcommand{\p}{{\em Proof}. }, or
// \newcommand{\lemma}{\refstepcounter{lemma}{\bf \thelemma:\ }} with
// \thelemma written "Lemma \thesection.\arabic{lemma}". The paper then
// starts each item with the command and no environment. A head of a command
// with no arguments has no number of its own: the census numbers it with the
// counter that the command steps. A
// command with arguments writes the head word and its first argument, as
// \proofpart{1}{The bound}, and the argument is the number. Seen in the gold
// pilot (set E).
func commandHeads(m *tex.Macros, t *Titles) map[string]commandHead {
	out := map[string]commandHead{}
	if m == nil {
		return out
	}
	for name, c := range m.Commands {
		if len(name) < 2 || strings.Contains(c.Body, `\begin`) || strings.Contains(c.Body, `\end`) {
			continue
		}
		body := m.Expand(c.Body)
		f := headFont.FindStringSubmatchIndex(body)
		if f == nil {
			continue
		}
		font := body[f[2]:f[3]]
		if c.Args > 0 {
			// The head is in the font, after the layout of the command.
			body = body[f[0]:]
		}
		s := counterValue.ReplaceAllString(counterCmd.ReplaceAllString(body, ""), "")
		b := []byte(s)
		s = stripFonts(string(b[skipLayout(b, 0, len(b)):]))
		// {\noindent \bf \thelemma:\ } puts the layout in the group of
		// the font. Seen in the gold pilot (set X).
		b = []byte(s)
		s = string(b[skipLayout(b, 0, len(b)):])
		s = strings.TrimRight(strings.TrimSpace(strings.ReplaceAll(s, `\ `, " ")), " .:")
		if c.Args > 0 {
			a := argNumber.FindStringSubmatch(s)
			if a == nil {
				continue
			}
			s = a[1] + " 1"
		}
		if strings.ContainsAny(s, `\$#`) {
			continue
		}
		h, ok := parseHead(s, t)
		if !ok {
			continue
		}
		h.Number, h.Form = "", formOf(font)
		out[name] = commandHead{Head: h, mac: c}
	}
	return out
}

// commandHeadAt reads a head written with one of the head commands of the
// paper.
func commandHeadAt(text []byte, i int, heads map[string]commandHead) (Head, int, bool) {
	if len(heads) == 0 {
		return Head{}, 0, false
	}
	m := headCall.FindSubmatchIndex(text[i:min(len(text), i+80)])
	if m == nil {
		return Head{}, 0, false
	}
	c, ok := heads[`\`+string(text[i+m[2]:i+m[3]])]
	if !ok {
		return Head{}, 0, false
	}
	h, end := c.Head, i+m[1]
	if c.mac.Args == 0 {
		if x := headStep.FindStringSubmatch(c.mac.Body); x != nil {
			h.Counter, h.Code = strings.TrimSpace(x[1]), c.mac.Body
		}
	}
	if c.mac.Args > 0 {
		a, e, ok := tex.ReadArgs(text, end, c.mac.Spec())
		if !ok || e-i > 400 {
			return Head{}, 0, false
		}
		h.Number, end = strings.TrimSpace(a.Values[0]), e
	}
	return h, afterName(text, end, &h), true
}

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

// proofOfTitle is the title of a proof of an item, as "Proof of Lemma 2".
var proofOfTitle = regexp.MustCompile(`(?i)^(?:\w+\s+)?proofs?\s+of\b`)

// stepMacros is each font command of the paper that only heads the steps
// of proofs, as \newcommand{\st}[1]{\noindent{\bf #1}} written at the start
// of a line of a proof each time, and at least twice.
func stepMacros(d *tex.Document, macros map[string]string, envs []EnvSpan) map[string]string {
	out := map[string]string{}
	for name, font := range macros {
		call := regexp.MustCompile(regexp.QuoteMeta(name) + `\s*\{`)
		uses := call.FindAllIndex(d.Text[d.Body:d.End], -1)
		ok := len(uses) >= 2
		for _, u := range uses {
			i := d.Body + u[0]
			j := i
			for j > d.Body && (d.Text[j-1] == ' ' || d.Text[j-1] == '\t') {
				j--
			}
			e, in := enclosing(envs, i)
			if !in || !e.Proof || j > d.Body && d.Text[j-1] != '\n' {
				ok = false
				break
			}
		}
		if ok {
			out[name] = font
		}
	}
	return out
}

// pfStepCall is a step of the pf style of proofs, \step{label}{statement}.
var pfStepCall = regexp.MustCompile(`^\\step\s*\{([^{}]*)\}\s*\{`)

// pfStepAt reads a step of the pf style of proofs, \step{step:fill}{If ...},
// which cheatpf2 defines: the first argument labels the step, and the second
// states it. Seen in the gold pilot (set X).
func pfStepAt(text []byte, i int) (Head, int, bool) {
	m := pfStepCall.FindSubmatchIndex(text[i:min(len(text), i+200)])
	if m == nil {
		return Head{}, 0, false
	}
	return Head{Title: "Step", Item: "other/case", Form: "command"}, i + m[1], true
}

// stepHeadAt reads a step of a proof headed by one of the step commands of
// the paper, as \st{(i)} or \st{Rough estimates}: the argument names the
// step. Seen in the gold pilot (set X).
func stepHeadAt(text []byte, i int, steps map[string]string) (Head, int, bool) {
	if len(steps) == 0 {
		return Head{}, 0, false
	}
	m := macroCall.FindSubmatchIndex(text[i:min(len(text), i+80)])
	if m == nil {
		return Head{}, 0, false
	}
	font, ok := steps[`\`+string(text[i+m[2]:i+m[3]])]
	if !ok {
		return Head{}, 0, false
	}
	_, end, ok := tex.Group(text, i+m[1]-1)
	if !ok || end-i > 200 {
		return Head{}, 0, false
	}
	name := strings.TrimRight(strings.TrimSpace(stripFonts(string(text[i+m[1]:end-1]))), ".:")
	if name == "" {
		return Head{}, 0, false
	}
	return Head{Item: "other/case", Name: name, Form: formOf(font)}, end, true
}

// stripFonts removes font commands inside a head, as in {\bf Theorem \it 2}.
func stripFonts(s string) string {
	s = fontCmd.ReplaceAllString(s, "")
	s = skipCmd.ReplaceAllString(s, "")
	// Keep the braces of a reference, as in "Proof of Theorem~\ref{main}",
	// so that rule 1 of AX3.6 can read the label.
	var b strings.Builder
	keep := map[int]bool{}
	for _, m := range refCmd.FindAllStringIndex(s, -1) {
		keep[m[1]-1] = true
		if j := strings.IndexByte(s[m[1]:], '}'); j >= 0 {
			keep[m[1]+j] = true
		}
	}
	for i := 0; i < len(s); i++ {
		switch {
		case (s[i] == '{' || s[i] == '}') && !keep[i]:
		case s[i] == '~':
			b.WriteByte(' ')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

var (
	// skipCmd is a space at the start of a head, as {\it \hskip2em First Case.}
	skipCmd = regexp.MustCompile(`\\(?:hskip\s*-?[\d.]+\s*(?:pt|em|ex|mm|cm|in)?|hspace\*?\s*\{[^{}]*\})\s*`)
	fontCmd = regexp.MustCompile(`\\(bf|bfseries|sc|scshape|it|itshape|sl|em|rm|upshape|normalfont|textbf|textsc|textit|emph|textrm|textup|underline|uline|ul|textsl|textsf|sf|sffamily|slshape)\b\s*`)
	refCmd  = regexp.MustCompile(`\\(?:ref|cref|Cref|autoref|eqref)\*?\s*\{`)
)

// afterName reads a name or a period after the head group, as in
// \textbf{Theorem 2.1} (Hahn-Banach).
func afterName(text []byte, end int, h *Head) int {
	rest := text[end:min(end+300, len(text))]
	if m := numberAfter.FindSubmatchIndex(rest); m != nil && h.Item == "other/case" && h.Number == "" && h.Name == "" {
		// "{\it Case} 1: $j=i+1$.": the number of a case after the group
		// of its title. Seen in the gold pilot (set X).
		h.Number = string(rest[m[2]:m[3]])
		end += m[3]
		rest = text[end:min(end+300, len(text))]
	}
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
	// A head on the line after the end of an environment starts an item, as
	// a proof head after \end{lemma}.
	for _, m := range envEnd.FindAllIndex(body, -1) {
		out = append(out, from+m[1])
	}
	// So does a head on the line after a display, as {\bf Proof:} after \].
	// Seen in the gold pilot (set X).
	for _, m := range displayEnd.FindAllIndex(body, -1) {
		out = append(out, from+m[1])
	}
	return out
}

// alignCmd is an alignment at the start of a group, as {\flushleft ...}.
var alignCmd = regexp.MustCompile(`^\\(?:flushleft|raggedright|centering)\b`)

// displayEnd is the end of a display math at the end of a line.
var displayEnd = regexp.MustCompile(`\\\][ \t\r]*\n`)

// sectionLabel is a label right after a section head.
var sectionLabel = regexp.MustCompile(`^\s*\\label\s*\{[^{}]*\}`)

// skipLabels moves past the labels at offset i.
func skipLabels(text []byte, i, to int) int {
	for i < to {
		m := sectionLabel.FindIndex(text[i:min(to, i+200)])
		if m == nil {
			return i
		}
		i += m[1]
	}
	return i
}

// lineBreak is a forced line break at the end of a line, as "\\" or
// "\\[5pt] \noindent", and a line of only \noindent, which starts a
// paragraph on the next line. Seen in the gold pilot (set X).
var lineBreak = regexp.MustCompile(`\\\\(?:\[[^\]]{0,20}\])?(?:[ \t]*\\(?:noindent|smallskip|medskip|bigskip)\b)*[ \t\r]*\n$|(?:^|\n)[ \t]*\\noindent[ \t\r]*\n$`)

// breakBefore reports whether the line that starts at p comes after a line
// that ends in a forced line break.
func breakBefore(text []byte, p, from int) bool {
	// So does a line that starts with the break, as "\newline\textit{Proof
	// of Claim 1.}". Seen in the gold pilot (set X).
	return lineBreak.Match(text[max(from, p-40):p]) || lineStartBreak.Match(text[p:min(len(text), p+20)])
}

// lineStartBreak is a forced line break at the start of a line.
var lineStartBreak = regexp.MustCompile(`^[ \t]*(?:\\newline\b|\\\\)`)

// lineStarts is each offset in text[from:to] where a line starts.
func lineStarts(text []byte, from, to int) []int {
	var out []int
	for i := from; i < to; i++ {
		if text[i] == '\n' && i+1 < to {
			out = append(out, i+1)
		}
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

// layoutMacros is the commands of the paper with no arguments whose bodies
// are layout only, as \newcommand{\sn}{\smallskip\noindent}. Seen in the
// gold pilot (set X).
func layoutMacros(m *tex.Macros) map[string]bool {
	out := map[string]bool{}
	if m == nil {
		return out
	}
	for name, c := range m.Commands {
		body := []byte(strings.TrimSpace(c.Body))
		if c.Args == 0 && len(body) > 0 && len(name) > 1 && skipLayout(body, 0, len(body)) == len(body) {
			out[name] = true
		}
	}
	return out
}

// skipUserLayout moves past the layout commands at offset i, and past the
// commands of the paper that are layout only.
func skipUserLayout(text []byte, i, to int, user map[string]bool) int {
	for {
		i = skipLayout(text, i, to)
		m := headCall.FindIndex(text[i:min(to, i+64)])
		if m == nil || !user[string(text[i:i+m[1]])] || letterAt(text, i+m[1]) {
			return i
		}
		i += m[1]
	}
}

// letterAt tells if text[i] is a letter.
func letterAt(text []byte, i int) bool {
	return i < len(text) && isLetter(text[i])
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

// proofEnvNext is the start of a proof environment after a proof head, with
// only spacing, punctuation, braces and labels between them.
var proofEnvNext = regexp.MustCompile(`^(?:\s|[.:}]|\\label\s*\{[^{}]*\}|\\(?:noindent|par|medskip|smallskip)\b)*\\begin\s*\{[A-Za-z]*(?:proof|Proof|pf|prf)\*?\}`)

// An EnvSpan is the span of one environment item: Off is the offset of its
// \begin, Inner the offset after its arguments, and Close the offset of its
// \end.
type EnvSpan struct {
	Off, Inner, Close int
	// Proof is true for a proof environment.
	Proof bool
}

var labelCmd = regexp.MustCompile(`^\s*\\label\s*\{[^{}]*\}`)

// enclosing is the innermost environment of envs that holds off.
func enclosing(envs []EnvSpan, off int) (EnvSpan, bool) {
	var best EnvSpan
	found := false
	for _, e := range envs {
		if off > e.Off && off < e.Close && (!found || e.Off > best.Off) {
			best, found = e, true
		}
	}
	return best, found
}

// atStart tells if off is at the start of the body of e, after layout and
// labels only: a head there is the head of the environment, written again.
func atStart(text []byte, e EnvSpan, off int) bool {
	if off < e.Inner {
		return true
	}
	i := e.Inner
	for {
		i = skipLayout(text, i, off)
		m := labelCmd.FindIndex(text[i:off])
		if m == nil {
			break
		}
		i += m[1]
	}
	return i >= off
}

// FindHeads finds the manual heads of the body of a document.
//
// It looks at the start of each paragraph and each \item, and at each
// section whose title is a head. The census passes the declared environments
// that it has already counted. A head at the start of one of them is the head
// of the environment, written again, and is ignored. A head inside one, as a
// bold "Case 1." or "Claim." in a proof, is an item nested in it (AX3.5), and
// it ends by the \end of the environment at the latest. Seen in the gold
// pilot (set E).
func FindHeads(d *tex.Document, t *Titles, envs []EnvSpan) []Head {
	if d.Body == 0 {
		return nil
	}
	var out []Head
	seen := map[int]bool{}
	macros := fontMacros(d.Macros)
	cmds := commandHeads(d.Macros, t)
	steps := stepMacros(d, macros, envs)
	pfStep := false
	if d.Macros != nil {
		_, pfStep = d.Macros.Commands[`\step`]
	}
	marks := EndMarks(d.Macros)
	para := map[int]bool{}
	all := starts(d.Text, d.Body, d.End)
	if d.Tree != nil {
		// A head on the line after a section head starts a paragraph, as
		// "\section{Introduction}" and "{\bf Theorem 1.}" on the next line.
		// Seen in the gold pilot (set X).
		for _, n := range d.Tree.Nodes {
			if n.Kind == "section" && n.Inner > d.Body && n.Inner < d.End {
				all = append(all, skipLabels(d.Text, n.Inner, d.End))
			}
		}
	}
	// paras is the title of each \paragraph, at its offset.
	paras := map[int]string{}
	if d.Tree != nil {
		for _, n := range d.Tree.Nodes {
			if n.Kind == "section" && n.Level >= 4 {
				paras[n.From] = strings.TrimRight(strings.TrimSpace(stripFonts(n.Title)), ".: ")
			}
		}
	}
	user := layoutMacros(d.Macros)
	for _, p := range all {
		para[skipUserLayout(d.Text, p, d.End, user)] = true
	}
	// A proof head on the line of the end of an environment, as
	// "\end{lem} \proof", starts the proof. Seen in the gold pilot (set X).
	inline := map[int]bool{}
	for _, m := range envEndInline.FindAllIndex(d.Text[d.Body:d.End], -1) {
		inline[skipUserLayout(d.Text, d.Body+m[1]-1, d.End, user)] = true
	}
	more := lineStarts(d.Text, d.Body, d.End)
	for p := range inline {
		more = append(more, p)
	}
	sort.Ints(more)
	for _, p := range append(all, more...) {
		i := skipUserLayout(d.Text, p, d.End, user)
		if i >= d.End || seen[i] {
			continue
		}
		e, in := enclosing(envs, i)
		if in && atStart(d.Text, e, i) && !e.Proof {
			continue
		}
		h, inner, ok := headAt(d.Text, i, t)
		direct := ok
		if !ok {
			h, inner, ok = macroHeadAt(d.Text, i, macros, t)
		}
		if !ok {
			h, inner, ok = commandHeadAt(d.Text, i, cmds)
		}
		if !ok && in && e.Proof {
			h, inner, ok = numberHeadAt(d.Text, i)
		}
		if !ok && in && e.Proof {
			h, inner, ok = stepHeadAt(d.Text, i, steps)
		}
		if !ok && in && e.Proof && pfStep {
			h, inner, ok = pfStepAt(d.Text, i)
		}
		if !ok {
			continue
		}
		if t, ok2 := paras[i]; ok2 && in && e.Proof && !atStart(d.Text, e, i) && (h.Proof && !proofOfTitle.MatchString(t) || strings.Contains(h.Number, `\ref`)) {
			// \paragraph{Proof for $N=2$.} or \paragraph{Property
			// \ref{p1}:} in a proof is a case of the proof named by its
			// title, not a proof or an item of its own. Seen in the gold
			// pilot (set X).
			h = Head{Item: "other/case", Name: t, Form: h.Form}
		}
		if in && atStart(d.Text, e, i) && h.Item != "other/case" {
			// A case or a stage at the start of a proof, as
			// "\begin{proof} \textbf{Stage A:} ...", is an item of the
			// proof; any other head there is the head of the proof written
			// again. Seen in the gold pilot (set X).
			continue
		}
		if !para[i] && !(inline[i] && h.Proof) && h.Form != "paragraph" && (h.Item != "other/case" || h.Form == "plain" || h.Form == "command") && !(breakBefore(d.Text, p, d.Body) && (h.Form == "bold" || h.Form == "italic")) {
			// Inside a paragraph, only a case or a step in a font starts an
			// item at the start of a line, as "\\ {\bf Case 1:}". Seen in the
			// gold pilot (set X). A head in a font on the line after a line
			// break, as "\\" and then "{\bf{Lemma 2.1.}}", starts an item: the
			// paper breaks lines in place of paragraphs. Seen in the gold
			// pilot (set X). A \paragraph head starts a paragraph itself,
			// with no empty line before it. Seen in the gold pilot (set X).
			continue
		}
		if direct && !para[i] && h.Item == "other/case" && h.Form != "paragraph" && !breakBefore(d.Text, p, d.Body) && !headPunct(d.Text, i, inner) {
			// "go to the\n{\it Final step} with": inside a paragraph, a case
			// head in the text with no colon or period after it is a word of
			// the sentence. A macro head has its period in the macro. Seen in
			// the gold pilot (set X).
			continue
		}
		seen[i] = true
		if h.Proof && proofEnvNext.Match(d.Text[inner:min(d.End, inner+300)]) {
			// \paragraph{Proof of Lemma 1} and a proof environment right
			// after it: the head belongs to the environment, which
			// attaches by it (AX3.6). Seen in the gold pilot (set E).
			continue
		}
		h.Off, h.Inner = i, inner
		out = append(out, h)
	}
	out = append(out, itemClaims(d, out)...)
	out = append(out, sectionItems(d, t, seen)...)
	for _, n := range d.Tree.Nodes {
		_, in := enclosing(envs, n.From)
		if n.Kind != "section" || seen[n.From] || in && (n.Level >= 4) {
			// A \paragraph head is found once at the start of its paragraph.
			continue
		}
		h, ok := parseHead(stripFonts(texorpdf.ReplaceAllString(n.Title, "$1")), t)
		if ok && h.Item == "other/case" {
			// \subsection*{Case: $n=1$} in a proof is a case of the proof.
			// Out of a proof, a section "Case $p\equiv 1$" is a case too, of
			// the argument that the section before it starts. Seen in the
			// gold pilot (set X).
			h.Off, h.Inner, h.Form = n.From, n.Inner, "section"
			out = append(out, h)
			continue
		}
		if in || !ok || h.Proof || h.Number == "" {
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
		if out[k].Form == "item" {
			continue
		}
		if e, ok := bodyArg(d, out[k]); ok {
			out[k].End, out[k].EndMark = e, true
			continue
		}
		out[k].End, out[k].EndMark = itemEnd(d, out, k, marks)
		if e, ok := enclosing(envs, out[k].Off); ok && out[k].End > e.Close {
			out[k].End, out[k].EndMark = e.Close, false
		}
	}
	return out
}

// boldNumber is a number in parentheses in bold, as \textbf{(1.1)}.
var boldNumber = regexp.MustCompile(`^(?:\\textbf\s*\{\s*|\{\s*\\bf\s+)(\(\d+(?:\.\d+)*\))\s*\}`)

// numberHeadAt finds a case of a proof headed by its number alone, in bold
// and in parentheses, as "\textbf{(1.1)}, If the element is ...". Seen in
// the gold pilot (set X).
func numberHeadAt(text []byte, i int) (Head, int, bool) {
	m := boldNumber.FindSubmatch(text[i:min(len(text), i+80)])
	if m == nil {
		return Head{}, 0, false
	}
	return Head{Item: "other/case", Number: string(m[1]), Form: "bold"}, i + len(m[0]), true
}

// bodyArg is the end of a proof written as a command whose one argument is
// the body, as \renewcommand{\proof}[1]{\textsc{Proof.} #1$\qed$} used as
// \proof{...}: the proof ends at the close of the argument. Seen in the gold
// pilot (set E).
func bodyArg(d *tex.Document, h Head) (int, bool) {
	if !h.Proof || d.Macros == nil {
		return 0, false
	}
	m := headCall.FindSubmatchIndex(d.Text[h.Off:min(len(d.Text), h.Off+80)])
	if m == nil {
		return 0, false
	}
	c, ok := d.Macros.Commands[`\`+string(d.Text[h.Off+m[2]:h.Off+m[3]])]
	if !ok || c.Args != 1 || c.HasDefault || !strings.Contains(c.Body, "#1") {
		return 0, false
	}
	j := skipSpaces(d.Text, h.Off+m[1])
	if j >= d.End || d.Text[j] != '{' {
		return 0, false
	}
	_, e, ok := tex.Group(d.Text, j)
	return e, ok && e <= d.End
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
		// A proof without an end mark ends at two empty lines in a row,
		// which a paper puts between a proof and the remark after it. A
		// comment line is spaces after cleaning, so it is not empty. Seen in
		// the gold pilot (set E).
		if i := bytes.Index(d.Text[h.Inner:end], []byte("\n\n\n")); i >= 0 {
			return h.Inner + i + 1, false
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

// itemLabel is the optional argument of an \item that labels a claim, as
// \item[{\bf A1.}\,].
var (
	itemLabel  = regexp.MustCompile(`\\item\s*\[((?:[^\[\]{}]|\{[^{}]*\})*)\]`)
	claimLabel = regexp.MustCompile(`^[A-Z]{1,2}\d{1,2}[a-z]?$`)
	itemStop   = regexp.MustCompile(`\\item\b|\\end\s*\{(?:enumerate|itemize|description)\*?\}`)
	thinSpace  = regexp.MustCompile(`\\[,;:! ]|~`)
)

// ClaimLabel is the label that a list item or a proof head names, as A1 of
// "{\bf A1.}\," or of "\,A1", or "".
func ClaimLabel(s string) string {
	s = strings.Trim(strings.TrimSpace(thinSpace.ReplaceAllString(stripFonts(s), "")), ".:")
	if claimLabel.MatchString(s) {
		return s
	}
	return ""
}

// itemClaims is a claim for each item of a list with a bold label, as
// \item[{\bf A1.}\,], that a proof head after it names, as "Proof of A1": a
// proof states its claims in a list and proves them after it. The claim ends
// at the next item or at the end of the list. Seen in the gold pilot (set X).
func itemClaims(d *tex.Document, heads []Head) []Head {
	proved := map[string][]int{}
	for _, h := range heads {
		if l := ClaimLabel(h.Target); h.Proof && l != "" {
			proved[l] = append(proved[l], h.Off)
		}
	}
	if len(proved) == 0 {
		return nil
	}
	var out []Head
	body := d.Text[d.Body:d.End]
	for _, m := range itemLabel.FindAllSubmatchIndex(body, -1) {
		arg := string(body[m[2]:m[3]])
		l := ClaimLabel(arg)
		if l == "" || !strings.Contains(arg, `\bf`) && !strings.Contains(arg, `\textbf`) {
			continue
		}
		if !slices.ContainsFunc(proved[l], func(off int) bool { return off > d.Body+m[1] }) {
			continue
		}
		end := d.End
		if x := itemStop.FindIndex(body[m[1]:]); x != nil {
			end = d.Body + m[1] + x[0]
		}
		for end > d.Body+m[1] && (d.Text[end-1] == ' ' || d.Text[end-1] == '\t' || d.Text[end-1] == '\n' || d.Text[end-1] == '\r') {
			end--
		}
		out = append(out, Head{Off: d.Body + m[0], Inner: d.Body + m[1], End: end, Item: "statement/claim", Number: l, Form: "item"})
	}
	return out
}

// listHead is the bold head of an item of a list, as \item[\textbf{BB1}] or
// \item \textbf{Hessian regularity:}.
var listHead = regexp.MustCompile(`\\item\s*(?:\[\s*\\textbf\s*\{([^{}]{1,40})\}\s*\]|\\textbf\s*\{([^{}]{1,80}?)[.:]?\s*\}\s*:?)`)

// sectionItems is each item of a list in a section that a plural title
// names, as the items \item[\textbf{BB1}] of a section "Axioms BB1 -- BB3"
// or \item \textbf{Hessian regularity:} of a section "Assumptions": each
// item with a bold head is an item of the kind of the title. The head of an
// item in brackets is its number, and a head after \item is its name. Seen
// in the gold pilot (set X).
func sectionItems(d *tex.Document, t *Titles, seen map[int]bool) []Head {
	if d.Tree == nil {
		return nil
	}
	var out []Head
	for _, n := range d.Tree.Nodes {
		if n.Kind != "section" || n.Inner <= d.Body || n.Inner >= d.End {
			continue
		}
		w := strings.Fields(stripFonts(n.Title))
		if len(w) == 0 {
			continue
		}
		first := strings.ToLower(strings.TrimRight(w[0], ".:"))
		kind := t.Kind(first)
		if !strings.HasSuffix(first, "s") || kind != "context/hypothesis" && kind != "statement/axiom" {
			continue
		}
		to := d.End
		if s := nextSection(d, n.Inner); s > 0 && s < to {
			to = s
		}
		body := d.Text[n.Inner:to]
		for _, m := range listHead.FindAllSubmatchIndex(body, -1) {
			off := n.Inner + m[0]
			if seen[off] {
				continue
			}
			h := Head{Off: off, Inner: n.Inner + m[1], Item: kind, Form: "item"}
			if m[2] >= 0 {
				h.Number = strings.TrimSpace(string(body[m[2]:m[3]]))
			} else {
				h.Name = strings.TrimSpace(string(body[m[4]:m[5]]))
			}
			h.End = to
			if x := itemStop.FindIndex(body[m[1]:]); x != nil {
				h.End = n.Inner + m[1] + x[0]
			}
			for h.End > h.Inner && (d.Text[h.End-1] == ' ' || d.Text[h.End-1] == '\t' || d.Text[h.End-1] == '\n' || d.Text[h.End-1] == '\r') {
				h.End--
			}
			out = append(out, h)
		}
	}
	return out
}

// headPunct reports whether the head text[i:inner] ends with a colon or a
// period, inside its group or right after it.
func headPunct(text []byte, i, inner int) bool {
	s := strings.TrimRight(string(text[i:inner]), " \t}$")
	if strings.HasSuffix(s, ".") || strings.HasSuffix(s, ":") {
		return true
	}
	r := strings.TrimLeft(string(text[inner:min(len(text), inner+4)]), " \t}$")
	return strings.HasPrefix(r, ".") || strings.HasPrefix(r, ":")
}

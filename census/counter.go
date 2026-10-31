package census

import (
	"bytes"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/tamnd/arxiv-reader/tex"
)

// The counter model of AX4.4.
//
// It steps the counters of LaTeX in the order of the source, so that it knows
// the number of each occurrence without a compiler. It is not a TeX engine. A
// \the form that it cannot read gives no number, and the check of contiguity
// skips a counter that the paper sets by hand.

type model struct {
	value  map[string]int
	parent map[string]string
	// resets is each counter that another resets with no change to its
	// format, as \@addtoreset{theorem}{chapter} and \newcounter{x}[y] do.
	resets map[string]string
	format map[string]string
	// touched is each counter that the paper sets with \setcounter or
	// \addtocounter, or whose format the model cannot read.
	touched  map[string]bool
	appendix bool
	chapters bool
	// consts is each command that a local file defines as plain text with
	// no argument, as \def\siamprelabel{} of siamart.cls.
	consts map[string]string
}

var (
	maketitleCmd = regexp.MustCompile(`\\maketitle\b`)
	ecSwitchCmd  = regexp.MustCompile(`\\ECSwitch\b`)
	counterCmd   = regexp.MustCompile(`\\(setcounter|addtocounter|stepcounter|refstepcounter)\s*\{([^{}]+)\}(?:\s*\{((?:[^{}]|\{[^{}]*\})*)\})?`)
	theCmd       = regexp.MustCompile(`\\(?:re)?newcommand\*?\s*\{?\\the([A-Za-z@]+)\}?\s*\{`)
	theDef       = regexp.MustCompile(`\\def\\the([A-Za-z@]+)\s*\{`)
	theRef       = regexp.MustCompile(`\\the([A-Za-z@]+)`)
	valueCmd     = regexp.MustCompile(`\\value\s*\{([A-Za-z@0-9*]+)\}`)
	numexprCmd   = regexp.MustCompile(`\\the\s*\\numexpr\s*([-+()\d ]+)`)
	styleCmd     = regexp.MustCompile(`\\(arabic|Alph|alph|roman|Roman|fnsymbol)\s*\{([A-Za-z@0-9*]+)\}`)
	atStyle      = regexp.MustCompile(`\\@(arabic|Alph|alph|roman|Roman)\s*\\c@([A-Za-z@]+)`)
	wrapCmd      = regexp.MustCompile(`\\(?:textup|textrm|textnormal|textbf|mbox|upshape|rm|relax|normalfont)\b\s*`)
	equationEn   = regexp.MustCompile(`\\(begin|end)\s*\{(equation|align|gather|multline|eqnarray|flalign|alignat)(\*?)\}`)
	noNumber     = regexp.MustCompile(`\\(nonumber|notag)\b`)
	equationLet  = regexp.MustCompile(`\\let\s*\\c@equation\s*=?\s*\\c@([A-Za-z]+)`)
	onlyRefs     = regexp.MustCompile(`showonlyrefs`)
	appendixDef  = regexp.MustCompile(`\\(?:(?:re)?newcommand\*?\s*(?:\{\s*\\appendix\s*\}|\\appendix)|[gex]?def\s*\\appendix)\s*\{`)
	eqLabel      = regexp.MustCompile(`\\label\s*\{([^{}]+)\}`)
	eqRef        = regexp.MustCompile(`\\(?:eqref|refeq)\s*\{([^{}]+)\}`)
	envName      = regexp.MustCompile(`^[A-Za-z]+$`)
)

func newModel(chapters bool) *model {
	m := &model{value: map[string]int{}, parent: map[string]string{}, resets: map[string]string{}, format: map[string]string{}, touched: map[string]bool{}, chapters: chapters, consts: map[string]string{}}
	if chapters {
		m.parent["section"] = "chapter"
	}
	m.parent["subsection"] = "section"
	m.parent["subsubsection"] = "subsection"
	m.parent["paragraph"] = "subsubsection"
	m.parent["subparagraph"] = "paragraph"
	if chapters {
		m.parent["equation"] = "chapter"
	}
	return m
}

// step steps a counter and resets the counters within it.
func (m *model) step(c string) {
	m.value[c]++
	m.reset(c, map[string]bool{})
}

func (m *model) reset(c string, seen map[string]bool) {
	if seen[c] {
		return
	}
	seen[c] = true
	for _, within := range []map[string]string{m.parent, m.resets} {
		for child, p := range within {
			if p == c {
				m.value[child] = 0
				m.reset(child, seen)
			}
		}
	}
}

// the is the printed form of a counter.
func (m *model) the(c string, depth int) (string, bool) {
	if depth > 8 {
		return "", false
	}
	f, ok := m.format[c]
	if !ok {
		f = `\arabic{` + c + `}`
		if p := m.parent[c]; p != "" {
			f = `\the` + p + `.\arabic{` + c + `}`
		}
		if c == "section" && !m.chapters && m.appendix {
			f = `\Alph{section}`
		}
		if c == "chapter" && m.appendix {
			f = `\Alph{chapter}`
		}
	}
	return m.eval(f, depth)
}

// intArg is the value of the number argument of \setcounter or
// \addtocounter: an integer, or \value{equation}. Seen in the gold pilot
// (set X).
func (m *model) intArg(arg string) (int, bool) {
	if n, err := strconv.Atoi(arg); err == nil {
		return n, true
	}
	if x := valueCmd.FindStringSubmatch(arg); x != nil && strings.TrimSpace(arg) == x[0] {
		return m.value[x[1]], true
	}
	return 0, false
}

func (m *model) eval(f string, depth int) (string, bool) {
	ok := true
	s := wrapCmd.ReplaceAllString(f, "")
	s = atStyle.ReplaceAllString(s, `\$1{$2}`)
	s = styleCmd.ReplaceAllStringFunc(s, func(x string) string {
		sub := styleCmd.FindStringSubmatch(x)
		return styled(sub[1], m.value[sub[2]])
	})
	s = valueCmd.ReplaceAllStringFunc(s, func(x string) string {
		return strconv.Itoa(m.value[valueCmd.FindStringSubmatch(x)[1]])
	})
	s = numexprCmd.ReplaceAllStringFunc(s, func(x string) string {
		e := numexprCmd.FindStringSubmatch(x)[1]
		v, n := intExpr(e)
		if n == 0 {
			ok = false
			return x
		}
		return strconv.Itoa(v) + e[n:]
	})
	s = theRef.ReplaceAllStringFunc(s, func(x string) string {
		name := x[len(`\the`):]
		v, good := m.the(name, depth+1)
		if !good {
			ok = false
		}
		return v
	})
	s = controlWord.ReplaceAllStringFunc(s, func(x string) string {
		if v, ok := m.consts[x[1:]]; ok {
			return v
		}
		return x
	})
	s = strings.NewReplacer("{", "", "}", "", "~", "", " ", "").Replace(s)
	if strings.Contains(s, `\`) {
		return "", false
	}
	return s, ok
}

// expandNumexpr expands each command of the paper in a format whose body is
// a \numexpr, as \def\zarabic#1{\the\numexpr(\arabic{#1})-1\relax}, which
// numbers from zero. Seen in the gold pilot (set X).
func (c *Census) expandNumexpr(f string) string {
	ms := c.In.Doc.Macros
	if ms == nil || !strings.Contains(f, `\`) {
		return f
	}
	sub := tex.NewMacros()
	for name, x := range ms.Commands {
		if strings.Contains(x.Body, `\numexpr`) && !strings.HasPrefix(name, `\the`) {
			sub.Commands[name] = x
		}
	}
	if len(sub.Commands) == 0 {
		return f
	}
	return sub.Expand(f)
}

// intExpr reads the longest integer expression of + and - and parentheses at
// the start of s. It gives the value and the bytes it read, or 0 bytes when s
// starts with no expression.
func intExpr(s string) (int, int) {
	i := 0
	space := func() {
		for i < len(s) && s[i] == ' ' {
			i++
		}
	}
	var term func() (int, bool)
	var expr func() (int, bool)
	term = func() (int, bool) {
		space()
		if i < len(s) && s[i] == '(' {
			at := i
			i++
			v, ok := expr()
			space()
			if !ok || i >= len(s) || s[i] != ')' {
				i = at
				return 0, false
			}
			i++
			return v, true
		}
		sign, at := 1, i
		if i < len(s) && s[i] == '-' {
			sign, i = -1, i+1
		}
		j := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == j {
			i = at
			return 0, false
		}
		v, _ := strconv.Atoi(s[j:i])
		return sign * v, true
	}
	expr = func() (int, bool) {
		v, ok := term()
		if !ok {
			return 0, false
		}
		for {
			at := i
			space()
			if i >= len(s) || (s[i] != '+' && s[i] != '-') {
				i = at
				return v, true
			}
			op := s[i]
			i++
			w, ok := term()
			if !ok {
				i = at
				return v, true
			}
			if op == '+' {
				v += w
			} else {
				v -= w
			}
		}
	}
	v, ok := expr()
	if !ok {
		return 0, 0
	}
	return v, i
}

func styled(style string, v int) string {
	switch style {
	case "Alph":
		if v >= 1 && v <= 26 {
			return string(rune('A' + v - 1))
		}
	case "alph":
		if v >= 1 && v <= 26 {
			return string(rune('a' + v - 1))
		}
	case "roman":
		return strings.ToLower(roman(v))
	case "Roman":
		return roman(v)
	}
	return strconv.Itoa(v)
}

func roman(v int) string {
	if v <= 0 || v > 3999 {
		return strconv.Itoa(v)
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	var b strings.Builder
	for i, n := range vals {
		for v >= n {
			b.WriteString(syms[i])
			v -= n
		}
	}
	return b.String()
}

type event struct {
	off  int
	kind string
	name string
	arg  string
	occ  *Occurrence
	star bool
	n    int
}

var ieeeThm = []byte(`\@IEEEthmcounterin`)

var (
	withinCmd   = regexp.MustCompile(`\\(?:numberwithin|counterwithin)\s*(?:\[[^\]]*\]\s*)?\{([^{}]+)\}`)
	controlWord = regexp.MustCompile(`\\[A-Za-z@]+`)
	constDef    = regexp.MustCompile(`\\def\s*\\([A-Za-z@]+)\s*\{([^{}\\#]*)\}`)
)

var resetDecl = regexp.MustCompile(`\\newcounter\s*\{([^{}]+)\}\s*\[([^\]]+)\]|\\@addtoreset\s*\{([^{}]+)\}\s*\{([^{}]+)\}`)

func sortedNames(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// number runs the counter model over the body and gives each env occurrence
// its number. Then it takes the number of the best witness.
func (c *Census) number() {
	d := c.In.Doc
	chapters := false
	if d.Tree != nil {
		for _, n := range d.Tree.Nodes {
			if n.Kind == "section" && n.Name == "chapter" {
				chapters = true
			}
		}
	}
	// svmult sets a contribution of a book with \maketitle, which steps the
	// chapter, and \thesection is \thechapter.\arabic{section} unless the
	// option numart is on. Seen in the gold pilot (set X).
	titleChapter := d.Class == "svmult" && !slices.Contains(d.Options, "numart")
	if titleChapter {
		chapters = true
	}
	m := newModel(chapters)
	for _, decl := range c.In.Set.List() {
		if decl.Within != "" && decl.Counter == decl.Env {
			m.parent[decl.Env] = decl.Within
		}
		if decl.EmptyWithin && decl.Counter == decl.Env && (!strings.HasPrefix(d.Class, "ams") || slices.Contains(d.Packages, "amsthm")) {
			// \newtheorem{theorem}{Theorem}[] of the LaTeX kernel and of the
			// amsthm package prints "01": \the of the empty name has no
			// value. The \newtheorem of an AMS class without the amsthm
			// package prints "1". Seen in the gold pilot (set X).
			m.format[decl.Env] = `0\arabic{` + decl.Env + `}`
		}
	}
	for counter, within := range c.In.Set.NumberWithin {
		m.parent[counter] = within
	}
	// revtex4, IEEEtran and ieeeconf print \thesection as \Roman{section},
	// but revtex4 with the AIP society or secnumarabic prints it in Arabic.
	// Seen in the gold pilot (set X).
	revtex := strings.HasPrefix(d.Class, "revtex4") && !slices.Contains(d.Options, "aip") && !slices.Contains(d.Options, "secnumarabic")
	if revtex || d.Class == "IEEEtran" || d.Class == "ieeeconf" {
		m.format["section"] = `\Roman{section}`
	}
	// IEEEtran, ieeeconf and the classes made from them define \newtheorem{thm}{..}[section]
	// again to print \arabic{section}, not \thesection, which is Roman. Seen
	// in the gold pilot (set X).
	if d.Class == "IEEEtran" || d.Class == "ieeeconf" || slices.ContainsFunc(tex.LocalFiles(d), func(f []byte) bool { return bytes.Contains(f, ieeeThm) }) {
		for _, decl := range c.In.Set.List() {
			if decl.Within != "" && decl.Counter == decl.Env {
				m.format[decl.Env] = `\arabic{` + decl.Within + `}.\arabic{` + decl.Env + `}`
			}
		}
	}
	// A format can name a command of a local file that is plain text, as
	// \renewcommand{\thetheorem}{\siamprelabel\arabic{theorem}} of siamart.cls
	// with \def\siamprelabel{} unless the option supplement is on. Seen in
	// the gold pilot (set X).
	for _, text := range tex.LocalFiles(d) {
		for _, x := range constDef.FindAllSubmatchIndex(text, -1) {
			if name := string(text[x[2]:x[3]]); !strings.HasPrefix(name, "the") && !inDefinition(text, x[0]) {
				m.consts[name] = string(text[x[4]:x[5]])
			}
		}
	}
	// \newcounter{thm}[section] and \@addtoreset{thm}{section} reset a counter
	// too, and leave its format as it is. Seen in the gold pilot (set X).
	for _, text := range append([][]byte{d.Text}, tex.LocalFiles(d)...) {
		for _, r := range resetDecl.FindAllSubmatch(text, -1) {
			if len(r[2]) > 0 {
				m.resets[strings.TrimSpace(string(r[1]))] = strings.TrimSpace(string(r[2]))
			} else if len(r[4]) > 0 {
				m.resets[strings.TrimSpace(string(r[3]))] = strings.TrimSpace(string(r[4]))
			}
		}
	}
	// \let\c@equation\c@subsection makes the equations step the counter of
	// the subsections, which tac.cls numbers the theorems with. Seen in the
	// gold pilot (set X).
	eqCounter := "equation"
	if x := equationLet.FindSubmatch(d.Preamble()); x != nil {
		eqCounter = string(x[1])
	}
	usesEquation := false
	for _, decl := range c.In.Set.List() {
		if decl.Counter == eqCounter {
			usesEquation = true
		}
		// \setcounter{item}{\value{equation}} in the code of the
		// environment numbers it with the equations too.
		if e := d.Macros.Envs[decl.Env]; e != nil && strings.Contains(e.Begin+e.End, `\value{equation}`) {
			usesEquation = true
		}
	}
	// The formats that the preamble and the local files set.
	c.formats(m, d.Preamble(), -1)
	for _, f := range d.Local {
		c.formats(m, f, -1)
	}

	var evs []event
	if d.Tree != nil {
		for _, n := range d.Tree.Nodes {
			if n.Kind == "section" {
				evs = append(evs, event{off: n.From, kind: "section", name: n.Name, star: n.Star})
			}
		}
		if d.Tree.Appendix > 0 {
			evs = append(evs, event{off: d.Tree.Appendix, kind: "appendix"})
		}
	}
	for _, o := range c.Occurrences {
		if o.Type == Env && o.Counter != "" {
			evs = append(evs, event{off: o.Off, kind: "occ", occ: o})
		}
	}
	// An unnumbered statement right after an empty \paragraph{} prints the
	// number of the paragraph, when \setcounter{secnumdepth}{4} numbers the
	// paragraphs: "2.0.1 Proposition." Seen in the gold pilot (set X).
	if d.Tree != nil {
		for _, n := range d.Tree.Nodes {
			if n.Kind != "section" || n.Star || n.Title != "" || (n.Name != "paragraph" && n.Name != "subparagraph") {
				continue
			}
			for _, o := range c.Occurrences {
				if o.Type == Env && o.Counter == "" && o.Off >= n.Inner && len(bytes.TrimSpace(d.Text[n.Inner:o.Off])) == 0 {
					evs = append(evs, event{off: o.Off, kind: "paragraph", name: n.Name, occ: o})
				}
			}
		}
	}
	// A case written as a section, as \subsection{The inductive step}, prints
	// the number of the section. Seen in the gold pilot (set X).
	if d.Tree != nil {
		at := map[int]tex.Node{}
		for _, n := range d.Tree.Nodes {
			if n.Kind == "section" && !n.Star {
				at[n.From] = n
			}
		}
		for _, o := range c.Occurrences {
			if n, ok := at[o.Off]; ok && o.Type == Head && o.Number == "" && slices.Contains(o.Flags, "section") {
				evs = append(evs, event{off: o.Off, kind: "sectionhead", name: n.Name, n: n.Level, occ: o})
			}
		}
	}
	// \ECSwitch of the INFORMS classes starts the e-companion: it numbers
	// the sections and the theorems again from one, after EC. Seen in the
	// gold pilot (set X).
	if strings.HasPrefix(d.Class, "informs") {
		for _, x := range ecSwitchCmd.FindAllIndex(d.Text[d.Body:d.End], -1) {
			evs = append(evs, event{off: d.Body + x[0], kind: "ecswitch"})
		}
	}
	if titleChapter {
		for _, x := range maketitleCmd.FindAllIndex(d.Text[d.Body:d.End], -1) {
			evs = append(evs, event{off: d.Body + x[0], kind: "section", name: "chapter"})
		}
	}
	// \setcounter{section}{-1} in the preamble sets the counter before the
	// body. Seen in the gold pilot (set X).
	pre := d.Preamble()
	for _, x := range counterCmd.FindAllSubmatchIndex(pre, -1) {
		if k := string(pre[x[2]:x[3]]); (k != "setcounter" && k != "addtocounter") || x[6] < 0 || inDefinition(pre, x[0]) {
			continue
		}
		evs = append(evs, event{off: x[0], kind: string(pre[x[2]:x[3]]), name: strings.TrimSpace(string(pre[x[4]:x[5]])), arg: strings.TrimSpace(string(pre[x[6]:x[7]]))})
	}
	body := d.Text[d.Body:d.End]
	for _, x := range counterCmd.FindAllSubmatchIndex(body, -1) {
		e := event{off: d.Body + x[0], kind: string(body[x[2]:x[3]]), name: strings.TrimSpace(string(body[x[4]:x[5]]))}
		if x[6] >= 0 {
			e.arg = strings.TrimSpace(string(body[x[6]:x[7]]))
		}
		evs = append(evs, e)
	}
	// The code of an environment sets counters at each use: the begin code,
	// as {\setcounter{item}{\value{equation}}\refstepcounter{item}..}, before
	// the step of the use, and the end code, as {\end{thm}\addtocounter{thm}
	// {-1}} of a restate environment, at its \end. Seen in the gold pilot
	// (set X). An environment that is not an item, as \begin{nonsec} with
	// \addtocounter{equation}{1}, sets them too.
	for _, x := range beginEnv.FindAllSubmatchIndex(body, -1) {
		name := strings.TrimSpace(string(body[x[2]:x[3]]))
		e := d.Macros.Envs[name]
		if e == nil || !strings.Contains(e.Begin+e.End, "counter") {
			continue
		}
		off := d.Body + x[0]
		end, _ := closeOf(d, name, d.Body+x[1])
		for _, code := range []struct {
			text string
			off  int
		}{{e.Begin, off - 1}, {e.End, end}} {
			text := []byte(code.text)
			for _, x := range counterCmd.FindAllSubmatchIndex(text, -1) {
				if k := string(text[x[2]:x[3]]); (k == "setcounter" || k == "addtocounter") && x[6] >= 0 {
					evs = append(evs, event{off: code.off, kind: k, name: strings.TrimSpace(string(text[x[4]:x[5]])), arg: strings.TrimSpace(string(text[x[6]:x[7]]))})
				}
			}
		}
	}
	// \lemma of \newcommand{\lemma}{\setcounter{lemma}{\value{Mycounter}}
	// \refstepcounter{lemma}\stepcounter{Mycounter}{\bf \thelemma:\ }} sets
	// the counters at each head, and the head prints \thelemma. Seen in the
	// gold pilot (set X).
	for _, o := range c.Occurrences {
		if o.Type != Head || o.code == "" {
			continue
		}
		text := []byte(o.code)
		for _, x := range counterCmd.FindAllSubmatchIndex(text, -1) {
			e := event{off: o.Off, kind: string(text[x[2]:x[3]]), name: strings.TrimSpace(string(text[x[4]:x[5]]))}
			if x[6] >= 0 {
				e.arg = strings.TrimSpace(string(text[x[6]:x[7]]))
			}
			evs = append(evs, e)
		}
		evs = append(evs, event{off: o.Off, kind: "head", occ: o})
	}
	// \newtheorem{nota}[thm]{Notation} defines \nota, which opens the
	// environment without \begin and steps its counter. Seen in the gold
	// pilot (set X).
	for _, decl := range c.In.Set.List() {
		if !decl.Numbered || decl.Counter == "" || decl.From == "used" || decl.From == "wrapper" || !envName.MatchString(decl.Env) {
			continue
		}
		bare := regexp.MustCompile(`\\` + decl.Env + `(?:[^A-Za-z@*]|$)`)
		for _, x := range bare.FindAllIndex(body, -1) {
			// Richter\\theorem in a tikz node is a line break and the word.
			// Seen in the gold pilot (set X).
			if !inDefinition(body, x[0]) && !escaped(body, x[0]) {
				evs = append(evs, event{off: d.Body + x[0], kind: "stepcounter", name: decl.Counter})
			}
		}
	}
	for _, x := range theCmd.FindAllSubmatchIndex(body, -1) {
		evs = append(evs, event{off: d.Body + x[0], kind: "format", name: string(body[x[2]:x[3]]), n: d.Body + x[1] - 1})
	}
	if usesEquation {
		evs = append(evs, c.equations()...)
	}
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].off < evs[j].off })

	for _, e := range evs {
		switch e.kind {
		case "section":
			if !e.star {
				m.step(e.name)
			}
		case "appendix":
			// \appendix sets the format of the chapter or the section to
			// \Alph, over a format that a style file set before, as
			// \def\thesection{\arabic{section}} of icml2024.sty. Seen in the
			// gold pilot (set X).
			m.appendix = true
			c.appendixCode(m, d)
			if chapters {
				m.value["chapter"] = 0
				delete(m.format, "chapter")
			} else {
				m.value["section"] = 0
				delete(m.format, "section")
			}
		case "occ":
			m.step(e.occ.Counter)
			if v, ok := m.the(e.occ.Counter, 0); ok {
				// \renewcommand{\thedefinition}{S.\arabic{definition}.}
				// prints a dot after the number, which is not part of it.
				// Seen in the gold pilot (set X).
				e.occ.Model = strings.TrimSuffix(v, ".")
			}
		case "paragraph":
			if depth := map[string]int{"paragraph": 4, "subparagraph": 5}[e.name]; m.touched["secnumdepth"] && m.value["secnumdepth"] >= depth {
				if v, ok := m.the(e.name, 0); ok {
					e.occ.Model = strings.TrimSuffix(v, ".")
				}
			}
		case "sectionhead":
			depth := 3
			if m.touched["secnumdepth"] {
				depth = m.value["secnumdepth"]
			}
			if e.n <= depth {
				if v, ok := m.the(e.name, 0); ok {
					e.occ.Model = strings.TrimSuffix(v, ".")
				}
			}
		case "head":
			if v, ok := m.the(e.occ.Counter, 0); ok {
				// \thelemma is "Lemma \thesection.\arabic{lemma}": the head
				// word is not part of the number.
				v = strings.TrimSpace(strings.TrimPrefix(v, e.occ.Title))
				e.occ.Model = strings.TrimSuffix(strings.Trim(v, "~ "), ".")
			}
		case "setcounter":
			if n, ok := m.intArg(e.arg); ok {
				m.value[e.name] = n
			}
			m.touched[e.name] = true
		case "addtocounter":
			if n, ok := m.intArg(e.arg); ok {
				m.value[e.name] += n
			}
			m.touched[e.name] = true
		case "stepcounter", "refstepcounter":
			m.step(e.name)
			m.touched[e.name] = true
		case "format":
			c.formats(m, d.Text[e.off:min(e.n+400, d.End)], 0)
		case "equation":
			m.value[eqCounter] += e.n
			if eqCounter != "equation" && e.n > 0 {
				m.touched[eqCounter] = true
			}
		case "ecswitch":
			m.value["section"] = 0
			m.format["section"] = `EC.\arabic{section}`
			for _, decl := range c.In.Set.List() {
				n := decl.Counter
				if n == "" || sectionCounter[n] {
					continue
				}
				m.value[n] = 0
				if m.parent[n] == "" && m.resets[n] == "" && !strings.HasPrefix(m.format[n], "EC.") {
					f, ok := m.format[n]
					if !ok {
						f = `\arabic{` + n + `}`
					}
					m.format[n] = "EC." + f
				}
			}
		}
	}
	c.touched = m.touched
	c.witnesses()
}

// formats reads each \renewcommand{\thex}{...} of a text into the model.
func (c *Census) formats(m *model, text []byte, _ int) {
	at := map[string]int{}
	for _, re := range []*regexp.Regexp{theCmd, theDef} {
		for _, x := range re.FindAllSubmatchIndex(text, -1) {
			name := string(text[x[2]:x[3]])
			if name == "" || inDefinition(text, x[0]) {
				// \renewcommand\appendix{... \renewcommand\thesection{\Alph{section}}}
				// sets the format when the command runs, not here. Seen in
				// the gold pilot (set X).
				continue
			}
			open := x[1] - 1
			in, end, ok := groupAt(text, open)
			if !ok {
				continue
			}
			_ = end
			if bytes.Contains(text[in:end-1], []byte("#")) {
				// \def\thethm{#1} in the body of a wrapper sets the format
				// of one use and not of the paper. Seen in the gold pilot
				// (set E).
				continue
			}
			m.format[name] = c.expandNumexpr(string(text[in : end-1]))
			at[name] = max(at[name], x[0])
			if _, good := m.eval(m.format[name], 0); !good || strings.Contains(m.format[name], `\numexpr`) {
				// A format with \numexpr prints a number that is not the
				// value of the counter, so the counter is not checked for
				// contiguity.
				m.touched[name] = true
			}
		}
	}
	// \numberwithin{theorem}{section} after the format sets the format again
	// to \thesection.\arabic{theorem}, as in siamart.cls. Seen in the gold
	// pilot (set X).
	for _, x := range withinCmd.FindAllSubmatchIndex(text, -1) {
		name := strings.TrimSpace(string(text[x[2]:x[3]]))
		if o, ok := at[name]; ok && x[0] > o && !inDefinition(text, x[0]) {
			delete(m.format, name)
			delete(m.touched, name)
		}
	}
}

// appendixCode runs the code of an \appendix that a class or the preamble
// defines on the counters of the theorems: mdpi.cls sets \thelemma to
// A\arabic{lemma} and the lemma counter to 0. Seen in the gold pilot (set X).
func (c *Census) appendixCode(m *model, d *tex.Document) {
	var code []byte
	for _, text := range append(tex.LocalFiles(d), d.Preamble()) {
		for _, x := range appendixDef.FindAllIndex(text, -1) {
			if in, end, ok := groupAt(text, x[1]-1); ok {
				code = text[in : end-1]
			}
		}
	}
	if code == nil {
		return
	}
	counters := map[string]bool{}
	for _, decl := range c.In.Set.List() {
		if decl.Counter != "" && !sectionCounter[decl.Counter] && decl.Counter != "equation" {
			counters[decl.Counter] = true
		}
	}
	for _, x := range theCmd.FindAllSubmatchIndex(code, -1) {
		name := string(code[x[2]:x[3]])
		if in, end, ok := groupAt(code, x[1]-1); ok && counters[name] && !bytes.Contains(code[in:end-1], []byte("#")) {
			m.format[name] = string(code[in : end-1])
		}
	}
	for _, x := range counterCmd.FindAllSubmatchIndex(code, -1) {
		name := strings.TrimSpace(string(code[x[4]:x[5]]))
		if string(code[x[2]:x[3]]) != "setcounter" || x[6] < 0 || !counters[name] {
			continue
		}
		if n, ok := m.intArg(strings.TrimSpace(string(code[x[6]:x[7]]))); ok {
			m.value[name] = n
		}
	}
}

// escaped tells whether the backslash at offset i of text follows an odd
// number of backslashes, as the second one of \\.
func escaped(text []byte, i int) bool {
	n := 0
	for j := i - 1; j >= 0 && text[j] == '\\'; j-- {
		n++
	}
	return n%2 == 1
}

// defHead is the end of the head of a definition, before the group of its
// body: \def\x, \newcommand{\x}[1], \renewcommand\x, \newenvironment{x}.
var defHead = regexp.MustCompile(`(?:\\[gex]?def\s*\\[A-Za-z@]+[^{}\\]*|\\(?:re)?newcommand\*?\s*(?:\{\s*\\[A-Za-z@]+\s*\}|\\[A-Za-z@]+)\s*(?:\[[^\]]*\]\s*)*|\\providecommand\*?\s*(?:\{\s*\\[A-Za-z@]+\s*\}|\\[A-Za-z@]+)\s*(?:\[[^\]]*\]\s*)*|\\(?:re)?newenvironment\*?\s*\{[^{}]*\}\s*(?:\[[^\]]*\]\s*)*)$`)

// inDefinition tells whether offset off of text is inside the body of a
// command definition.
func inDefinition(text []byte, off int) bool {
	depth := 0
	for i := off - 1; i >= 0 && i >= off-20000; i-- {
		if i > 0 && text[i-1] == '\\' {
			continue
		}
		switch text[i] {
		case '}':
			depth++
		case '{':
			if depth > 0 {
				depth--
				continue
			}
			if defHead.Match(text[max(0, i-80):i]) {
				return true
			}
		}
	}
	return false
}

func groupAt(text []byte, i int) (int, int, bool) {
	if i < 0 || i >= len(text) || text[i] != '{' {
		return 0, 0, false
	}
	depth := 0
	for j := i; j < len(text); j++ {
		switch text[j] {
		case '\\':
			j++
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1, j + 1, true
			}
		}
	}
	return 0, 0, false
}

// equations is the steps of the equation counter, for a paper whose theorems
// share it.
func (c *Census) equations() []event {
	d := c.In.Doc
	body := d.Text[d.Body:d.End]
	// The showonlyrefs option of mathtools numbers only the equations that
	// an \eqref cites. Seen in the gold pilot (set X).
	var cited map[string]bool
	if onlyRefs.Match(d.Preamble()) {
		cited = map[string]bool{}
		for _, x := range eqRef.FindAllSubmatch(body, -1) {
			cited[strings.TrimSpace(string(x[1]))] = true
		}
	}
	var out []event
	var open int = -1
	var star bool
	var name string
	for _, x := range equationEn.FindAllSubmatchIndex(body, -1) {
		which := string(body[x[2]:x[3]])
		if which == "begin" && open < 0 {
			open, name, star = x[1], string(body[x[4]:x[5]]), x[6] < x[7]
			continue
		}
		if which == "end" && open >= 0 && string(body[x[4]:x[5]]) == name {
			n := 0
			if !star {
				inner := body[open:x[0]]
				n = 1
				if name != "equation" && name != "multline" {
					n = rows(inner)
				}
				n -= len(noNumber.FindAllIndex(inner, -1))
				if n < 0 {
					n = 0
				}
				if cited != nil {
					n = 0
					for _, l := range eqLabel.FindAllSubmatch(inner, -1) {
						if cited[strings.TrimSpace(string(l[1]))] {
							n++
						}
					}
				}
			}
			out = append(out, event{off: d.Body + open, kind: "equation", n: n})
			open = -1
		}
	}
	return out
}

// rows is the number of rows of the body of an align: the \\ at its top
// level and one. A \\ in a group, or in an environment in it as split or
// cases, does not end a row. Seen in the gold pilot (set X).
func rows(inner []byte) int {
	n, depth, envs := 1, 0, 0
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '{':
			depth++
		case '}':
			depth--
		case '\\':
			rest := inner[i+1:]
			switch {
			case len(rest) > 0 && rest[0] == '\\':
				if depth == 0 && envs == 0 {
					n++
				}
				i++
			case bytes.HasPrefix(rest, []byte("begin{")):
				envs++
			case bytes.HasPrefix(rest, []byte("end{")):
				envs--
			case len(rest) > 0:
				i++
			}
		}
	}
	return n
}

// witnesses gives each occurrence the number of its best witness: the .aux
// file, then the render, then the counter model, then the head (AX4.4).
func (c *Census) witnesses() {
	for _, o := range c.Occurrences {
		if o.Type != Env && o.Type != Head {
			continue
		}
		for _, l := range o.Labels {
			// \label{x} after an equation in a definition takes the number
			// of the equation, with the anchor equation.2.3. Seen in the gold
			// pilot (set X).
			if a := c.auxAnchor[l]; otherAnchor[a] && a != o.Counter {
				continue
			}
			if n, ok := c.AuxLabels[l]; ok {
				o.Aux = n
				break
			}
		}
		switch {
		case o.Aux != "":
			o.Number, o.NumberFrom = o.Aux, "aux"
		case o.Arg != "":
			o.Number, o.NumberFrom = o.Arg, "argument"
		case o.Model != "":
			o.Number, o.NumberFrom = o.Model, "counter"
		case o.Type == Env && o.Counter == "" && o.Number == "":
			// \newtheorem*{thmA}{Theorem A}: the title prints the number.
			// Seen in the gold pilot (set X).
			if m := titleNumber.FindStringSubmatch(o.Title); m != nil {
				o.Number, o.NumberFrom = m[1], "title"
			}
		}
	}
	// A head that prints the number of a label, as the restated
	// \begin{restate}{\ref{thm:main}}, takes the number of the item of the
	// label. Seen in the gold pilot (set X).
	for _, o := range c.Occurrences {
		if o.Ref == "" || o.Aux != "" {
			continue
		}
		if i, err := strconv.Atoi(c.Labels[o.Ref]); err == nil && c.Occurrences[i] != o && c.Occurrences[i].Number != "" {
			o.Number, o.NumberFrom = c.Occurrences[i].Number, "ref"
		}
	}
	if c.In.Render != nil {
		c.alignRender()
	}
}

// titleNumber is the number at the end of the title of an unnumbered
// environment, as "Theorem A" or "Main Theorem 2".
var titleNumber = regexp.MustCompile(`^[^\\{}]*[A-Za-z]\s+([A-Z]'?|[A-Z]\d'?|\d+'?)$`)

// aux

var newLabel = regexp.MustCompile(`\\newlabel\{([^{}]+)\}\{\{([^{}]*(?:\{[^{}]*\}[^{}]*)*)\}`)

// labelAnchor is the counter of the hyperref anchor of a \newlabel, as
// equation of {{3}{3}{Preliminaries}{equation.2.3}{}}.
var labelAnchor = regexp.MustCompile(`\\newlabel\{([^{}]+)\}\{\{(?:[^{}]|\{[^{}]*\})*\}\{[^{}]*\}\{(?:[^{}]|\{(?:[^{}]|\{[^{}]*\})*\})*\}\{([A-Za-z]+)\.`)

// otherAnchor is a counter of an anchor that is not of an item.
var otherAnchor = map[string]bool{"equation": true, "AMS": true, "figure": true, "table": true, "enumi": true, "enumii": true, "enumiii": true, "Item": true, "footnote": true}

// auxAnchors reads the counter of the anchor of each label of an .aux file.
func auxAnchors(b []byte) map[string]string {
	out := map[string]string{}
	for _, m := range labelAnchor.FindAllSubmatch(b, -1) {
		out[string(m[1])] = string(m[2])
	}
	return out
}

// ParseAux reads the \newlabel lines of an .aux file, and gives the number of
// each label.
func ParseAux(b []byte) map[string]string {
	out := map[string]string{}
	for _, m := range newLabel.FindAllSubmatch(b, -1) {
		key := string(m[1])
		if strings.HasSuffix(key, "@cref") {
			continue
		}
		v := strings.TrimSpace(string(m[2]))
		v = strings.NewReplacer("{", "", "}", "", `\relax`, "", " ", "").Replace(v)
		out[key] = v
	}
	return out
}

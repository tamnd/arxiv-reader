package census

import (
	"bytes"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/tamnd/arxiv-reader/tex"
	"github.com/tamnd/arxiv-reader/theorems"
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
	format map[string]string
	// touched is each counter that the paper sets with \setcounter or
	// \addtocounter, or whose format the model cannot read.
	touched  map[string]bool
	appendix bool
	chapters bool
	// romanSections is true for a class that prints the sections as II.
	romanSections bool
	// styles is the body of each macro of one argument that prints a
	// counter, as \zarabic in \def\zarabic#1{\the\numexpr\value{#1}-1\relax}.
	styles map[string]string
	// words is the body of each command with no arguments whose code is a
	// short word, as \siamprelabel in \def\siamprelabel{SM}.
	words map[string]string
	// resetOnly is each counter that a section resets but does not number
	// within, as \@addtoreset{theorem}{chapter} does.
	resetOnly map[string]bool
	// arabicWithin is true for a class of the IEEE, which numbers a theorem
	// within section III as 3.1.
	arabicWithin bool
}

var (
	counterCmd = regexp.MustCompile(`\\(setcounter|addtocounter|stepcounter|refstepcounter)\s*\{([^{}]+)\}(?:\s*\{((?:[^{}]|\{[^{}]*\})*)\})?`)
	theCmd     = regexp.MustCompile(`\\(?:re)?newcommand\*?\s*\{?\\the([A-Za-z@]+)\}?\s*\{`)
	theDef     = regexp.MustCompile(`\\def\\the([A-Za-z@]+)\s*\{`)
	theRef     = regexp.MustCompile(`\\the([A-Za-z@]+)`)
	styleCmd   = regexp.MustCompile(`\\(arabic|Alph|alph|roman|Roman|fnsymbol)\s*\{([A-Za-z@0-9]+\*?)\}`)
	atStyle    = regexp.MustCompile(`\\@(arabic|Alph|alph|roman|Roman)\s*\\c@([A-Za-z@]+)`)
	valueCmd   = regexp.MustCompile(`\\value\s*\{([A-Za-z@0-9]+)\}`)
	numExpr    = regexp.MustCompile(`\\the\s*\\numexpr([-+0-9()\s]+)\\relax`)
	wordCmd    = regexp.MustCompile(`\\[A-Za-z]+\s*`)
	plainWord  = regexp.MustCompile(`^[A-Za-z0-9.\-]{0,8}$`)
	styleArg   = regexp.MustCompile(`\\([A-Za-z]+)\s*\{([A-Za-z@]+)\}`)
	wrapCmd    = regexp.MustCompile(`\\(?:textup|textrm|textnormal|textbf|mbox|upshape|rm|relax|normalfont)\b\s*`)
	equationEn = regexp.MustCompile(`\\(begin|end)\s*\{(equation|align|gather|multline|eqnarray|flalign|alignat)(\*?)\}`)
	noNumber   = regexp.MustCompile(`\\(nonumber|notag)\b`)
)

func newModel(chapters bool) *model {
	m := &model{value: map[string]int{}, parent: map[string]string{}, format: map[string]string{}, touched: map[string]bool{}, styles: map[string]string{}, resetOnly: map[string]bool{}, words: map[string]string{}, chapters: chapters}
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
	for child, p := range m.parent {
		if p == c {
			m.value[child] = 0
			m.reset(child, seen)
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
		if p := m.parent[c]; p != "" && !m.resetOnly[c] {
			f = `\the` + p + `.\arabic{` + c + `}`
			if m.arabicWithin && sectionCmd[p] {
				f = arabicChain(m, p) + `.\arabic{` + c + `}`
			}
		}
		if c == "section" && m.romanSections {
			f = `\Roman{section}`
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

func (m *model) eval(f string, depth int) (string, bool) {
	ok := true
	s := styleArg.ReplaceAllStringFunc(f, func(x string) string {
		sub := styleArg.FindStringSubmatch(x)
		if b, ok := m.styles[sub[1]]; ok {
			return strings.ReplaceAll(b, "#1", sub[2])
		}
		return x
	})
	s = wordCmd.ReplaceAllStringFunc(s, func(x string) string {
		if b, ok := m.words[strings.TrimSpace(x)[1:]]; ok {
			return b
		}
		return x
	})
	s = valueCmd.ReplaceAllString(s, `\arabic{$1}`)
	s = atStyle.ReplaceAllString(s, `\$1{$2}`)
	s = styleCmd.ReplaceAllStringFunc(s, func(x string) string {
		sub := styleCmd.FindStringSubmatch(x)
		return styled(sub[1], m.value[sub[2]])
	})
	s = numExpr.ReplaceAllStringFunc(s, func(x string) string {
		if v, good := intExpr(numExpr.FindStringSubmatch(x)[1]); good {
			return strconv.Itoa(v)
		}
		ok = false
		return x
	})
	s = wrapCmd.ReplaceAllString(s, "")
	s = theRef.ReplaceAllStringFunc(s, func(x string) string {
		name := x[len(`\the`):]
		v, good := m.the(name, depth+1)
		if !good {
			ok = false
		}
		return v
	})
	s = strings.NewReplacer("{", "", "}", "", "~", "", " ", "").Replace(s)
	if strings.Contains(s, `\`) {
		return "", false
	}
	return s, ok
}

// intExpr is the value of an expression of \numexpr with + and - only, as
// (3)-1.
func intExpr(e string) (int, bool) {
	e = strings.NewReplacer("(", "", ")", "", " ", "", "\n", "", "\t", "").Replace(e)
	if e == "" {
		return 0, false
	}
	sum, sign, n, digits := 0, 1, 0, false
	for i := 0; i <= len(e); i++ {
		if i < len(e) && e[i] >= '0' && e[i] <= '9' {
			n, digits = n*10+int(e[i]-'0'), true
			continue
		}
		if i < len(e) && !digits && (e[i] == '-' || e[i] == '+') {
			if e[i] == '-' {
				sign = -sign
			}
			continue
		}
		if !digits {
			return 0, false
		}
		sum, n, digits = sum+sign*n, 0, false
		sign = 1
		if i < len(e) {
			switch e[i] {
			case '-':
				sign = -1
			case '+':
			default:
				return 0, false
			}
		}
	}
	return sum, true
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

var resetDecl = regexp.MustCompile(`\\newcounter\s*\{([^{}]+)\}\s*\[([^\]]+)\]|\\@addtoreset\s*\{([^{}]+)\}\s*\{([^{}]+)\}`)

func localFiles(d *tex.Document) [][]byte {
	var out [][]byte
	for _, name := range sortedNames(d.Local) {
		out = append(out, d.Local[name])
	}
	return out
}

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
	// The \maketitle of a class such as svmult starts a chapter, and each
	// paper of the volume is one.
	titleChapter := false
	if d.Macros != nil {
		if t := d.Macros.Commands[`\maketitle`]; t != nil && chapterStep.MatchString(t.Body) {
			titleChapter, chapters = true, true
		}
	}
	m := newModel(chapters)
	m.romanSections = romanClass[d.Class]
	m.arabicWithin = strings.HasPrefix(strings.ToLower(d.Class), "ieee")
	if d.Macros != nil {
		for name, mac := range d.Macros.Commands {
			if mac.Args == 1 && !mac.HasDefault && strings.Contains(mac.Body, "#1") && !strings.Contains(mac.Body, "#2") &&
				(valueCmd.MatchString(strings.ReplaceAll(mac.Body, "#1", "c")) || styleCmd.MatchString(strings.ReplaceAll(mac.Body, "#1", "c"))) {
				m.styles[strings.TrimPrefix(name, `\`)] = mac.Body
			}
			if mac.Args == 0 && plainWord.MatchString(strings.TrimSpace(mac.Body)) && !styleNames[strings.TrimPrefix(name, `\`)] && !strings.HasPrefix(name, `\the`) {
				m.words[strings.TrimPrefix(name, `\`)] = strings.TrimSpace(mac.Body)
			}
		}
	}
	for _, decl := range c.In.Set.List() {
		if decl.Within != "" && decl.Counter == decl.Env {
			m.parent[decl.Env] = decl.Within
		}
	}
	for counter, within := range c.In.Set.NumberWithin {
		m.parent[counter] = within
	}
	// \newcounter{thm}[section] and \@addtoreset{thm}{section} reset a counter
	// too. They do not put the section in its number, unless the code right
	// after them writes \thethm with \thesection, as a class does in
	// \def\thetheorem{\thesection\@thmcountersep\thetheorem}.
	for _, text := range append([][]byte{d.Text}, localFiles(d)...) {
		for _, x := range resetDecl.FindAllSubmatchIndex(text, -1) {
			c, p := x[2], x[4]
			if p < 0 {
				c, p = x[6], x[8]
			}
			if p < 0 {
				continue
			}
			counter := strings.TrimSpace(string(text[c : bytes.IndexAny(text[c:], "}")+c]))
			within := strings.TrimSpace(string(text[p : bytes.IndexAny(text[p:], "]}")+p]))
			if old := m.parent[counter]; old == "" || m.resetOnly[counter] {
				after := string(text[x[1]:min(len(text), x[1]+300)])
				m.resetOnly[counter] = !(strings.Contains(after, `\the`+counter) && strings.Contains(after, `\the`+within))
			}
			m.parent[counter] = within
		}
	}
	usesEquation := false
	for _, decl := range c.In.Set.List() {
		if decl.Counter == "equation" || strings.Contains(decl.Code, "{equation}") {
			usesEquation = true
		}
	}
	// The formats that the preamble and the local files set.
	c.formats(m, d.Preamble(), -1)
	for _, f := range d.Local {
		c.formats(m, f, -1)
	}

	// \setcounter{chapter}{5} in the preamble, out of any definition, sets
	// the counter before the body starts.
	pre := d.Preamble()
	for _, x := range counterCmd.FindAllSubmatchIndex(pre, -1) {
		if x[6] < 0 || depthAt(pre, x[0]) != 0 {
			continue
		}
		if k := string(pre[x[2]:x[3]]); k == "setcounter" || k == "addtocounter" {
			m.apply(event{kind: k, name: strings.TrimSpace(string(pre[x[4]:x[5]])), arg: strings.TrimSpace(string(pre[x[6]:x[7]]))})
		}
	}

	var evs []event
	if titleChapter {
		body := d.Text[d.Body:d.End]
		for _, x := range maketitle.FindAllIndex(body, -1) {
			evs = append(evs, event{off: d.Body + x[0], kind: "stepcounter", name: "chapter"})
		}
	}
	if d.Tree != nil {
		for _, n := range d.Tree.Nodes {
			if n.Kind == "section" {
				evs = append(evs, event{off: n.From, kind: "section", name: n.Name, star: n.Star})
			}
			// \begin{section}{Title} is \section{Title}, as old papers write
			// it.
			if name := strings.TrimSuffix(n.Name, "*"); n.Kind == "env" && sectionEnvs[name] {
				evs = append(evs, event{off: n.From, kind: "section", name: name, star: name != n.Name})
			}
		}
		// \renewcommand{\sec}[2]{\section{#2}\label{s:#1}} makes each \sec
		// of the body a section, as in math/9902147v1.
		for _, w := range sectionCommands(d.Macros) {
			re := regexp.MustCompile(regexp.QuoteMeta(w.call) + `(?:[^A-Za-z@]|$)`)
			for _, x := range re.FindAllIndex(d.Text[d.Body:d.End], -1) {
				evs = append(evs, event{off: d.Body + x[0], kind: "section", name: w.name, star: w.star})
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
		if o.Type == Env && o.Counter == "" && o.Item != "" && o.Item != "proof" {
			evs = append(evs, event{off: o.Off, kind: "bare", occ: o})
		}
		if o.Type == Head && o.code != "" {
			evs = append(evs, event{off: o.Off, kind: "code", occ: o})
		}
	}
	body := d.Text[d.Body:d.End]
	for _, x := range counterCmd.FindAllSubmatchIndex(body, -1) {
		e := event{off: d.Body + x[0], kind: string(body[x[2]:x[3]]), name: strings.TrimSpace(string(body[x[4]:x[5]]))}
		if x[6] >= 0 {
			e.arg = strings.TrimSpace(string(body[x[6]:x[7]]))
		}
		evs = append(evs, e)
	}
	// An environment of the paper that is not theorem-like, as a numbered
	// heading {\bf\setcounter{item}{\value{equation}}\refstepcounter{item}
	// \theitem.}, changes the counters at each \begin and \end.
	evs = append(evs, c.envCounters()...)
	// \newcommand{\cc}{\setcounter{equation}{0}} changes a counter at each
	// use of \cc.
	for _, w := range counterCommands(d.Macros) {
		re := regexp.MustCompile(regexp.QuoteMeta(w.name) + `(?:[^A-Za-z@]|$)`)
		for _, x := range re.FindAllIndex(body, -1) {
			if inDefinition(d.Text, d.Body+x[0]) {
				continue
			}
			for _, e := range w.evs {
				e.off = d.Body + x[0]
				evs = append(evs, e)
			}
		}
	}
	for _, x := range theCmd.FindAllSubmatchIndex(body, -1) {
		evs = append(evs, event{off: d.Body + x[0], kind: "format", name: string(body[x[2]:x[3]]), n: d.Body + x[1] - 1})
	}
	for _, e := range evs {
		if e.name == "equation" && e.kind != "format" {
			// An item counter that copies the equations, as
			// \setcounter{item}{\value{equation}}, needs them counted.
			usesEquation = true
		}
	}
	if usesEquation {
		evs = append(evs, c.equations()...)
	}
	through, ec := ecSwitch(d)
	if ec {
		for _, x := range ecCall.FindAllIndex(body, -1) {
			if !inDefinition(d.Text, d.Body+x[0]) {
				evs = append(evs, event{off: d.Body + x[0], kind: "ec"})
			}
		}
	}
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].off < evs[j].off })
	state := stateCounter(d)

	for _, e := range evs {
		switch e.kind {
		case "section":
			if !e.star {
				m.step(e.name)
			}
		case "appendix":
			m.appendix = true
			// \appendix sets \thesection to \Alph again, over the format
			// that the class or the preamble gave it. A format of the body
			// after it comes later.
			if chapters {
				m.value["chapter"] = 0
				delete(m.format, "chapter")
			} else {
				m.value["section"] = 0
				delete(m.format, "section")
			}
		case "occ":
			if e.occ.argNumber != "" {
				e.occ.Model = e.occ.argNumber
				continue
			}
			if e.occ.code != "" {
				// A wrapper that numbers itself, as {\setcounter{item}{\value{equation}}
				// \addtocounter{equation}{1}\refstepcounter{item} ...}.
				m.code(e.occ)
				continue
			}
			counter := e.occ.Counter
			if state != "" {
				counter = state
			}
			m.step(counter)
			if v, ok := m.the(counter, 0); ok {
				// {S.\arabic{definition}.} prints the dot after the number,
				// and the number is S.9.
				e.occ.Model = strings.TrimSuffix(v, ".")
			}
		case "setcounter", "addtocounter", "stepcounter", "refstepcounter":
			m.apply(e)
		case "code":
			m.code(e.occ)
		case "format":
			c.formats(m, d.Text[e.off:min(e.n+400, d.End)], 0)
		case "equation":
			m.value["equation"] += e.n
		case "bare":
			// \paragraph{}\begin{Proposition}, where the environment has no
			// number, gives the environment the number of the heading.
			if name := emptyHeadBefore(d.Text, e.off, d.Macros); name != "" {
				if v, ok := m.the(name, 0); ok {
					e.occ.Model = v
				}
			} else if t := letterTitle.FindStringSubmatch(e.occ.Title); t != nil {
				// \newtheorem*{theoA}{Theorem A} names the theorem A.
				e.occ.Model = t[1] + t[2]
			}
		case "ec":
			// The e-companion starts its sections and theorems again, as
			// EC.1, and the theorems numbered through take EC. too.
			m.value["section"] = 0
			m.format["section"] = `EC.\arabic{section}`
			for _, decl := range c.In.Set.List() {
				if decl.Counter == "" {
					continue
				}
				m.value[decl.Counter] = 0
				if through && m.parent[decl.Counter] == "" {
					f, ok := m.format[decl.Counter]
					if !ok {
						f = `\arabic{` + decl.Counter + `}`
					}
					if !strings.HasPrefix(f, "EC.") {
						m.format[decl.Counter] = "EC." + f
					}
				}
			}
		}
	}
	c.noteNumbers()
	c.touched = m.touched
	c.witnesses()
}

var (
	styleDecl = regexp.MustCompile(`\\newtheoremstyle\s*\{([^{}]+)\}`)
	refNote   = regexp.MustCompile(`^\\(?:ref|cref|autoref)\s*\{([^{}]+)\}$`)
	shortNote = regexp.MustCompile(`^[A-Z0-9][A-Za-z0-9.'-]{0,7}$`)
)

// noteStyles is each style of \newtheoremstyle whose head writes the note and
// not the number, as {\thmname{#1}\thmnote{ #3}}.
func noteStyles(d *tex.Document) map[string]bool {
	out := map[string]bool{}
	for _, text := range append([][]byte{d.Preamble()}, localFiles(d)...) {
		for _, x := range styleDecl.FindAllSubmatchIndex(text, -1) {
			a, _, ok := tex.ReadArgs(text, x[1], "mmmmmmmm")
			if !ok {
				continue
			}
			head := a.Values[7]
			out[strings.TrimSpace(string(text[x[2]:x[3]]))] = strings.Contains(head, "#3") && !strings.Contains(head, "#2")
		}
	}
	return out
}

// noteNumbers gives each theorem whose style writes the note in place of the
// number the number of its note, as \begin{repthm}[\ref{thm:main}] restates
// Theorem 1.7 as 1.7.
func (c *Census) noteNumbers() {
	styles := noteStyles(c.In.Doc)
	if len(styles) == 0 {
		return
	}
	byLabel := map[string]string{}
	for _, o := range c.Occurrences {
		for _, l := range o.Labels {
			byLabel[l] = o.Model
		}
	}
	for _, o := range c.Occurrences {
		decl := c.In.Set.Get(o.Env)
		if o.Type != Env || decl == nil || !styles[decl.Style] || !o.HasOpt {
			continue
		}
		note := strings.TrimSpace(o.Opt)
		if m := refNote.FindStringSubmatch(note); m != nil {
			if v := byLabel[strings.TrimSpace(m[1])]; v != "" {
				o.Model = v
			} else if v := c.AuxLabels[strings.TrimSpace(m[1])]; v != "" {
				o.Model = v
			}
		} else if shortNote.MatchString(note) {
			o.Model = note
		}
	}
}

// envCounters gives the counter commands of the begin and the end code of
// each use of an environment of the paper that is not in the set.
func (c *Census) envCounters() []event {
	d := c.In.Doc
	if d.Macros == nil {
		return nil
	}
	var out []event
	for name, e := range d.Macros.Envs {
		decl := c.In.Set.Get(name)
		if decl != nil && !decl.LeadOnly {
			continue
		}
		begin, end := counterCmd.FindAllStringSubmatch(e.Begin, -1), counterCmd.FindAllStringSubmatch(e.End, -1)
		if len(begin) == 0 && len(end) == 0 {
			continue
		}
		re := regexp.MustCompile(`\\(begin|end)\s*\{` + regexp.QuoteMeta(name) + `\}`)
		for _, x := range re.FindAllSubmatchIndex(d.Text[d.Body:d.End], -1) {
			code := begin
			if string(d.Text[d.Body+x[2]:d.Body+x[3]]) == "end" {
				code = end
			} else if decl != nil {
				// A use of a lead-only environment that is an item runs
				// its code as an item. A heading runs it here.
				if _, item, _ := decl.TitleOf(d.Text, d.Body+x[1], c.In.Titles); item != theorems.Unknown {
					continue
				}
			}
			for _, m := range code {
				out = append(out, event{off: d.Body + x[0], kind: m[1], name: strings.TrimSpace(m[2]), arg: strings.TrimSpace(m[3])})
			}
		}
	}
	return out
}

// counterCommand is a command of the paper with no arguments whose body only
// changes counters, with what it does.
type counterCommand struct {
	name string
	evs  []event
}

// counterCommands gives the commands of the paper that only change counters,
// in the order of their names.
func counterCommands(m *tex.Macros) []counterCommand {
	if m == nil {
		return nil
	}
	var out []counterCommand
	for name, c := range m.Commands {
		if c.Args != 0 || len(name) < 2 {
			continue
		}
		xs := counterCmd.FindAllStringSubmatchIndex(c.Body, -1)
		if len(xs) == 0 || strings.TrimSpace(counterCmd.ReplaceAllString(c.Body, "")) != "" {
			continue
		}
		w := counterCommand{name: name}
		for _, x := range xs {
			e := event{kind: c.Body[x[2]:x[3]], name: strings.TrimSpace(c.Body[x[4]:x[5]])}
			if x[6] >= 0 {
				e.arg = strings.TrimSpace(c.Body[x[6]:x[7]])
			}
			if strings.HasSuffix(e.kind, "stepcounter") && x[6] >= 0 {
				// \stepcounter{c}{\bf \thec.} prints a head after it.
				w.evs = nil
				break
			}
			w.evs = append(w.evs, e)
		}
		if len(w.evs) == 0 {
			continue
		}
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

var (
	stateNewThm = regexp.MustCompile(`\\def\\@@newthm\b[^\n]*\n?[^\n]*\\state\b`)
	stateLevel  = regexp.MustCompile(`\\def\\state#1\{\\(section|subsection|subsubsection)\b`)
	levelSwitch = regexp.MustCompile(`\\(first|third)leveltheorems\b`)
	amsthmLoad  = regexp.MustCompile(`\\usepackage\s*(?:\[[^\]]*\])?\s*\{[^}]*\b(?:amsthm|ntheorem)\b`)
)

// stateCounter is the sectioning counter that each theorem steps, when the
// class makes each theorem a section heading, as the class of TAC does:
// \def\@@newthm#1[#2]#3[#4]{...\state{#3.}...} with \def\state#1{\subsection{\sc#1}...}.
// \thirdleveltheorems and \firstleveltheorems of the paper move the theorems
// to the subsubsections and the sections. A paper that loads amsthm has its
// \newtheorem back.
func stateCounter(d *tex.Document) string {
	level := ""
	for _, f := range localFiles(d) {
		if !stateNewThm.Match(f) {
			continue
		}
		if m := stateLevel.FindSubmatch(f); m != nil {
			level = string(m[1])
		}
	}
	if level == "" || amsthmLoad.Match(d.Preamble()) {
		return ""
	}
	for _, m := range levelSwitch.FindAllSubmatchIndex(d.Text, -1) {
		if inDefinition(d.Text, m[0]) || m[0] >= d.Body {
			continue
		}
		level = map[string]string{"first": "section", "third": "subsubsection"}[string(d.Text[m[2]:m[3]])]
	}
	return level
}

var (
	ecCall   = regexp.MustCompile(`\\ECSwitch\b`)
	ecDef    = regexp.MustCompile(`\\def\\ECSwitch\b`)
	ecScheme = regexp.MustCompile(`\\TheoremsNumbered(Through|BySection|ByChapter)\b`)
)

// ecSwitch is whether the class of the paper has the \ECSwitch of INFORMS,
// which starts the e-companion, and whether the paper numbers its theorems
// through, as \TheoremsNumberedThrough sets.
func ecSwitch(d *tex.Document) (through, ok bool) {
	for _, f := range localFiles(d) {
		if ecDef.Match(f) {
			ok = true
		}
	}
	if !ok {
		return false, false
	}
	pre := d.Preamble()
	for _, m := range ecScheme.FindAllSubmatchIndex(pre, -1) {
		if !inDefinition(pre, m[0]) {
			through = string(pre[m[2]:m[3]]) == "Through"
		}
	}
	return through, true
}

// arabicChain is the format of a sectioning counter and the ones it is
// within in arabic numbers, as \arabic{section}.\arabic{subsection}.
func arabicChain(m *model, c string) string {
	f := `\arabic{` + c + `}`
	for p := m.parent[c]; p != "" && sectionCmd[p]; p = m.parent[p] {
		f = `\arabic{` + p + `}.` + f
	}
	return f
}

// romanClass is the classes that print the sections in capital Roman
// numerals, as II, and their appendices in capital letters.
var romanClass = map[string]bool{"revtex4": true, "revtex4-1": true, "revtex4-2": true}

// sectionEnvs is the sectioning commands that a paper can use as
// environments.
var sectionEnvs = map[string]bool{"chapter": true, "section": true, "subsection": true, "subsubsection": true}

var (
	chapterStep = regexp.MustCompile(`\\(?:ref)?stepcounter\s*\{chapter\}`)
	maketitle   = regexp.MustCompile(`\\maketitle\b`)
)

// depthAt is the depth of the braces of text at i.
func depthAt(text []byte, i int) int {
	depth := 0
	for j := 0; j < i && j < len(text); j++ {
		switch text[j] {
		case '\\':
			j++
		case '{':
			depth++
		case '}':
			depth--
		}
	}
	return depth
}

var (
	emptyHead  = regexp.MustCompile(`\\([A-Za-z]+)\s*\{\s*\}\s*(?:\\label\s*\{[^{}]*\}\s*)*$`)
	sectionCmd = map[string]bool{"section": true, "subsection": true, "subsubsection": true, "paragraph": true, "subparagraph": true}
)

// emptyHeadBefore is the sectioning counter of a heading with an empty title
// right before off, as \paragraph{} or a command of the paper that writes one,
// or "".
func emptyHeadBefore(text []byte, off int, macros *tex.Macros) string {
	m := emptyHead.FindSubmatch(text[max(0, off-200):off])
	if m == nil {
		return ""
	}
	name := string(m[1])
	if sectionCmd[name] {
		return name
	}
	for _, w := range sectionCommands(macros) {
		if w.call == `\`+name && !w.star {
			return w.name
		}
	}
	return ""
}

// sectionCommand is a command of the paper that writes a sectioning command.
type sectionCommand struct {
	call, name string
	star       bool
}

var sectionIn = regexp.MustCompile(`\\(chapter|section|subsection|subsubsection|paragraph|subparagraph)\b(\*)?`)

// sectionCommands is each command of the paper whose code writes a sectioning
// command, with the name of that command. A command named as a sectioning
// command is the sectioning command itself, and the tree has its calls.
func sectionCommands(m *tex.Macros) []sectionCommand {
	if m == nil {
		return nil
	}
	var out []sectionCommand
	for name, c := range m.Commands {
		if c.File != "" || sectionEnvs[strings.TrimPrefix(name, `\`)] || strings.ContainsAny(name, "@*") {
			continue
		}
		ms := sectionIn.FindAllStringSubmatch(c.Body, -1)
		if len(ms) != 1 || (c.How == "let" && c.Body != ms[0][0]) {
			continue
		}
		out = append(out, sectionCommand{call: name, name: ms[0][1], star: ms[0][2] != ""})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].call < out[j].call })
	return out
}

// defOpen is the text before the brace that opens the code of a definition.
var defOpen = regexp.MustCompile(`(?:\\(?:re)?newcommand\*?\s*(?:\{\s*\\[A-Za-z@]+\s*\}|\\[A-Za-z@]+)\s*(?:\[\d\])?\s*(?:\[[^\]]*\])?|\\[gex]?def\s*\\[A-Za-z@]+[^{}\\]*)\s*$`)

// inDefinition tells if i of text is in the code of a command that the text
// defines.
func inDefinition(text []byte, i int) bool {
	var open []int
	for j := 0; j < i && j < len(text); j++ {
		switch text[j] {
		case '\\':
			j++
		case '%':
			for j < i && text[j] != '\n' {
				j++
			}
		case '{':
			open = append(open, j)
		case '}':
			if len(open) > 0 {
				open = open[:len(open)-1]
			}
		}
	}
	for _, o := range open {
		if defOpen.Match(text[max(0, o-120):o]) || envOpen.Match(text[max(0, o-120):o]) {
			return true
		}
		// The end code of \newenvironment comes after the group of its
		// begin code.
		j := o - 1
		for j >= 0 && (text[j] == ' ' || text[j] == '\t' || text[j] == '\n') {
			j--
		}
		if j >= 0 && text[j] == '}' {
			if b := groupStart(text, j); b >= 0 && envOpen.Match(text[max(0, b-120):b]) {
				return true
			}
		}
	}
	return false
}

// envOpen is the text before the brace that opens the begin code of an
// environment that the text defines.
var envOpen = regexp.MustCompile(`\\(?:re)?newenvironment\*?\s*\{[^{}]*\}\s*(?:\[\d\])?\s*(?:\[[^\]]*\])?\s*$`)

// groupStart gives the offset of the brace that opens the group that closes
// at end, or -1.
func groupStart(text []byte, end int) int {
	depth := 0
	for j := end; j >= 0; j-- {
		if j > 0 && text[j-1] == '\\' {
			continue
		}
		switch text[j] {
		case '}':
			depth++
		case '{':
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

var valueOf = regexp.MustCompile(`^\\value\s*\{([^{}]+)\}$`)

// apply runs one counter command. The value can be a number or \value{c}.
func (m *model) apply(e event) {
	n, err := strconv.Atoi(e.arg)
	if v := valueOf.FindStringSubmatch(e.arg); v != nil {
		n, err = m.value[strings.TrimSpace(v[1])], nil
	}
	switch e.kind {
	case "setcounter":
		if err == nil {
			m.value[e.name] = n
		}
	case "addtocounter":
		if err == nil {
			m.value[e.name] += n
		}
	case "stepcounter", "refstepcounter":
		m.step(e.name)
	}
	m.touched[e.name] = true
}

// code runs the counter commands of the command that writes a head, and gives
// the head the number of the counter that the command steps last with
// \refstepcounter, without the title that the format of the counter writes,
// as Theorem in {Theorem \thesection.\arabic{theorem}}.
func (m *model) code(o *Occurrence) {
	last := ""
	for _, x := range counterCmd.FindAllStringSubmatch(o.code, -1) {
		e := event{kind: x[1], name: strings.TrimSpace(x[2]), arg: strings.TrimSpace(x[3])}
		m.apply(e)
		if e.kind == "refstepcounter" {
			last = e.name
		}
	}
	if last == "" {
		return
	}
	if v, ok := m.the(last, 0); ok {
		if v = strings.TrimPrefix(v, o.Title); v != "" {
			o.Model = v
		}
	}
}

// formats reads each \renewcommand{\thex}{...} of a text into the model.
func (c *Census) formats(m *model, text []byte, pos int) {
	at := map[string]int{}
	for _, re := range []*regexp.Regexp{theCmd, theDef} {
		for _, x := range re.FindAllSubmatchIndex(text, -1) {
			name := string(text[x[2]:x[3]])
			if name == "" {
				continue
			}
			open := x[1] - 1
			in, end, ok := groupAt(text, open)
			if !ok {
				continue
			}
			_ = end
			if pos < 0 && inDefinition(text, x[0]) {
				// \newcommand\bppendix{... \renewcommand\thechapter{...}}
				// sets the format when the paper calls \bppendix, as in
				// svmult.cls.
				continue
			}
			if strings.Contains(string(text[in:end-1]), "#") {
				// \def\thethm{#1} in the code of a macro sets the format of
				// each use of the macro, not of the paper.
				continue
			}
			m.format[name] = string(text[in : end-1])
			at[name] = x[0]
			if _, good := m.eval(m.format[name], 0); !good {
				m.touched[name] = true
			}
		}
	}
	// \numberwithin{theorem}{section} after a format of \thetheorem in the
	// same text writes the format again, as \thesection.\arabic{theorem}.
	for _, x := range withinCmd.FindAllSubmatchIndex(text, -1) {
		name := strings.TrimSpace(string(text[x[2]:x[3]]))
		if a, ok := at[name]; ok && a < x[0] && !inDefinition(text, x[0]) {
			delete(m.format, name)
			delete(m.touched, name)
		}
	}
}

// styleNames is the commands that the model reads itself.
var styleNames = map[string]bool{"arabic": true, "Alph": true, "alph": true, "roman": true, "Roman": true, "fnsymbol": true, "value": true, "the": true, "relax": true, "numexpr": true}

// letterTitle is a title that ends in a number of its own, as Theorem A or
// Conjecture 2'.
var letterTitle = regexp.MustCompile(`^\pL[\pL ]*\pL\s+([A-Z]|[0-9]+)('*)$`)

var withinCmd = regexp.MustCompile(`\\numberwithin\s*\{([^{}]+)\}\s*\{([^{}]+)\}`)

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
					n = strings.Count(string(oneRow(inner)), `\\`) + 1
				}
				n -= len(noNumber.FindAllIndex(inner, -1))
				if n < 0 {
					n = 0
				}
			}
			out = append(out, event{off: d.Body + open, kind: "equation", n: n})
			open = -1
		}
	}
	return out
}

// rowEnv is an environment inside a display whose rows make one row of the
// display, as split in align or a matrix.
var rowEnv = regexp.MustCompile(`\\(begin|end)\s*\{(split|aligned|alignedat|gathered|multlined|cases|dcases|rcases|array|matrix|pmatrix|bmatrix|vmatrix|Vmatrix|Bmatrix|smallmatrix|subarray|CD|tabular)\*?\}`)

// oneRow removes each environment of rowEnv from the body of a display, and
// each \substack, so that the rows of the display are its own rows only.
func oneRow(b []byte) []byte {
	var out []byte
	depth, last := 0, 0
	for _, x := range rowEnv.FindAllSubmatchIndex(b, -1) {
		if string(b[x[2]:x[3]]) == "begin" {
			if depth == 0 {
				out = append(out, b[last:x[0]]...)
			}
			depth++
			continue
		}
		if depth > 0 {
			depth--
			if depth == 0 {
				last = x[1]
			}
		}
	}
	if depth == 0 {
		out = append(out, b[last:]...)
	}
	return substack.ReplaceAll(out, nil)
}

// substack is a \substack with rows, which is one row of the display.
var substack = regexp.MustCompile(`\\substack\s*\{[^{}]*\}`)

// witnesses gives each occurrence the number of its best witness: the .aux
// file, then the render, then the counter model, then the head (AX4.4).
func (c *Census) witnesses() {
	for _, o := range c.Occurrences {
		if o.Type != Env && o.Type != Head {
			continue
		}
		for _, l := range o.Labels {
			if n, ok := c.AuxLabels[l]; ok {
				o.Aux = n
				break
			}
		}
		switch {
		case o.Aux != "":
			o.Number, o.NumberFrom = o.Aux, "aux"
		case o.Model != "":
			o.Number, o.NumberFrom = o.Model, "counter"
		}
	}
	c.headRefs()
	if c.In.Render != nil {
		c.alignRender()
	}
}

var headRef = regexp.MustCompile(`\\ref\s*\{([^{}]*)\}`)

// headRefs puts the number of the label in a head number written with a
// reference, as {\bf Theorem {\ref{main}}A} for Theorem 1.1A, and in the
// number that a restatement gives as an argument.
func (c *Census) headRefs() {
	numbers := map[string]string{}
	for _, o := range c.Occurrences {
		for _, l := range o.Labels {
			if _, ok := numbers[l]; !ok && o.Number != "" && !strings.Contains(o.Number, `\ref`) {
				numbers[l] = o.Number
			}
		}
	}
	for l, n := range c.AuxLabels {
		numbers[l] = n
	}
	for _, o := range c.Occurrences {
		if (o.Type != Head && o.Type != Env) || !strings.Contains(o.Number, `\ref`) {
			continue
		}
		o.Number = headRef.ReplaceAllStringFunc(o.Number, func(r string) string {
			if n, ok := numbers[strings.TrimSpace(headRef.FindStringSubmatch(r)[1])]; ok {
				return n
			}
			return r
		})
	}
}

// aux

var newLabel = regexp.MustCompile(`\\newlabel\{([^{}]+)\}\{\{([^{}]*(?:\{[^{}]*\}[^{}]*)*)\}`)

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

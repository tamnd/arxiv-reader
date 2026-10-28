package census

import (
	"regexp"
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
	format map[string]string
	// touched is each counter that the paper sets with \setcounter or
	// \addtocounter, or whose format the model cannot read.
	touched  map[string]bool
	appendix bool
	chapters bool
}

var (
	counterCmd = regexp.MustCompile(`\\(setcounter|addtocounter|stepcounter|refstepcounter)\s*\{([^{}]+)\}(?:\s*\{((?:[^{}]|\{[^{}]*\})*)\})?`)
	theCmd     = regexp.MustCompile(`\\(?:re)?newcommand\*?\s*\{?\\the([A-Za-z@]+)\}?\s*\{`)
	theDef     = regexp.MustCompile(`\\def\\the([A-Za-z@]+)\s*\{`)
	theRef     = regexp.MustCompile(`\\the([A-Za-z@]+)`)
	styleCmd   = regexp.MustCompile(`\\(arabic|Alph|alph|roman|Roman|fnsymbol)\s*\{([A-Za-z@]+)\}`)
	atStyle    = regexp.MustCompile(`\\@(arabic|Alph|alph|roman|Roman)\s*\\c@([A-Za-z@]+)`)
	wrapCmd    = regexp.MustCompile(`\\(?:textup|textrm|textnormal|textbf|mbox|upshape|rm|relax|normalfont)\b\s*`)
	equationEn = regexp.MustCompile(`\\(begin|end)\s*\{(equation|align|gather|multline|eqnarray|flalign|alignat)(\*?)\}`)
	noNumber   = regexp.MustCompile(`\\(nonumber|notag)\b`)
)

func newModel(chapters bool) *model {
	m := &model{value: map[string]int{}, parent: map[string]string{}, format: map[string]string{}, touched: map[string]bool{}, chapters: chapters}
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

func (m *model) eval(f string, depth int) (string, bool) {
	ok := true
	s := wrapCmd.ReplaceAllString(f, "")
	s = atStyle.ReplaceAllString(s, `\$1{$2}`)
	s = styleCmd.ReplaceAllStringFunc(s, func(x string) string {
		sub := styleCmd.FindStringSubmatch(x)
		return styled(sub[1], m.value[sub[2]])
	})
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
	for _, decl := range c.In.Set.List() {
		if decl.Within != "" && decl.Counter == decl.Env {
			m.parent[decl.Env] = decl.Within
		}
	}
	for counter, within := range c.In.Set.NumberWithin {
		m.parent[counter] = within
	}
	// \newcounter{thm}[section] and \@addtoreset{thm}{section} reset a counter
	// too.
	for _, text := range append([][]byte{d.Text}, localFiles(d)...) {
		for _, r := range resetDecl.FindAllSubmatch(text, -1) {
			if len(r[2]) > 0 {
				m.parent[strings.TrimSpace(string(r[1]))] = strings.TrimSpace(string(r[2]))
			} else if len(r[4]) > 0 {
				m.parent[strings.TrimSpace(string(r[3]))] = strings.TrimSpace(string(r[4]))
			}
		}
	}
	usesEquation := false
	for _, decl := range c.In.Set.List() {
		if decl.Counter == "equation" {
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
			m.appendix = true
			if chapters {
				m.value["chapter"] = 0
			} else {
				m.value["section"] = 0
			}
		case "occ":
			m.step(e.occ.Counter)
			if v, ok := m.the(e.occ.Counter, 0); ok {
				e.occ.Model = v
			}
		case "setcounter", "addtocounter", "stepcounter", "refstepcounter":
			m.apply(e)
		case "code":
			m.code(e.occ)
		case "format":
			c.formats(m, d.Text[e.off:min(e.n+400, d.End)], 0)
		case "equation":
			m.value["equation"] += e.n
		}
	}
	c.touched = m.touched
	c.witnesses()
}

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

// sectionCommand is a command of the paper that writes a sectioning command.
type sectionCommand struct {
	call, name string
	star       bool
}

var sectionIn = regexp.MustCompile(`\\(chapter|section|subsection|subsubsection)\b(\*)?`)

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
		if defOpen.Match(text[max(0, o-120):o]) {
			return true
		}
	}
	return false
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
			if _, good := m.eval(m.format[name], 0); !good {
				m.touched[name] = true
			}
		}
	}
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
					n = strings.Count(string(inner), `\\`) + 1
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
	if c.In.Render != nil {
		c.alignRender()
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

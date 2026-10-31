package tex

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Macro is one command that a paper defines.
type Macro struct {
	Name string `json:"name"`
	// Args is the number of arguments, and Default is the value of the first
	// argument when it is optional.
	Args       int    `json:"args,omitempty"`
	Default    string `json:"default,omitempty"`
	HasDefault bool   `json:"-"`
	// Default2 is the value of the second argument when the command has two
	// optional arguments, as \newcommandtwoopt of the xargs package defines.
	Default2 string `json:"default2,omitempty"`
	Opts     int    `json:"-"`
	Body     string `json:"body"`
	// How is the command that defined it: newcommand, def, let or operator.
	How string `json:"how"`
	// File is the local style file of the definition, or "" for the paper.
	File string `json:"file,omitempty"`
}

// Env is one environment that a paper defines with \newenvironment.
type Env struct {
	Name string `json:"name"`
	Args int    `json:"args,omitempty"`
	// Default is the value of the first argument when the use does not give
	// it. The first argument is then optional, as in [1][Proof].
	Default    string `json:"default,omitempty"`
	HasDefault bool   `json:"-"`
	Begin      string `json:"begin"`
	End        string `json:"end"`
	// Off is the offset of the declaration in the text it was read from.
	Off int `json:"-"`
	// File is the local style file of the declaration, or "" for the paper.
	File string `json:"file,omitempty"`
}

// Macros is the macro table of a paper.
//
// A later definition replaces an earlier one, as in TeX, with one exception:
// \providecommand does not replace.
type Macros struct {
	Commands map[string]*Macro
	Envs     map[string]*Env
	// EnvOrder is the names of Envs in the order of their declarations.
	EnvOrder []string
}

// NewMacros is an empty macro table.
func NewMacros() *Macros {
	return &Macros{Commands: map[string]*Macro{}, Envs: map[string]*Env{}}
}

// Add copies the definitions of o into m.
func (m *Macros) Add(o *Macros) {
	for k, v := range o.Commands {
		m.Commands[k] = v
	}
	for _, k := range o.EnvOrder {
		if _, ok := m.Envs[k]; !ok {
			m.EnvOrder = append(m.EnvOrder, k)
		}
		m.Envs[k] = o.Envs[k]
	}
}

var defineCmd = regexp.MustCompile(`\\(newcommandtwoopt|renewcommandtwoopt|newcommand|renewcommand|providecommand|DeclareRobustCommand|def|gdef|edef|xdef|let|DeclareMathOperator|newenvironment|renewenvironment|NewDocumentCommand|RenewDocumentCommand|DeclareDocumentCommand)\b`)

var defParams = regexp.MustCompile(`^(?:#[1-9])*`)

// ReadMacros reads the macro table from the text and from the local style
// files.
func ReadMacros(text []byte, local map[string][]byte) *Macros {
	m := NewMacros()
	for _, name := range sortedKeys(local) {
		m.read(local[name], name)
	}
	m.read(text, "")
	return m
}

// refCmds are the commands of the cross references. A class that defines
// \ref again for its own typesetting, as birkmult.cls does with
// \def\ref#1{{{\rm\@newref{#1}}}}, would hide the reference from each reader
// that expands the macros first, so a definition of one of them that does not
// give a reference again is not read. Seen in the gold pilot (set X).
var refCmds = map[string]bool{`\ref`: true, `\eqref`: true, `\pageref`: true, `\label`: true, `\cref`: true, `\Cref`: true, `\autoref`: true}

var refBody = regexp.MustCompile(`\\(?:ref|eqref|pageref|label|cref|Cref|autoref)\b`)

// definerAlias is a short name for a command that defines commands, as
// \newcommand{\nc}{\newcommand} or \let\nc\newcommand. Seen in the gold pilot
// (set X).
var definerAlias = regexp.MustCompile(`\\(?:(?:re)?newcommand\*?\s*\{\s*\\([a-zA-Z]+)\s*\}\s*\{\s*|def\s*\\([a-zA-Z]+)\s*\{\s*|let\s*\\([a-zA-Z]+)\s*=?\s*)\\(newcommand|renewcommand|providecommand|newenvironment|renewenvironment|def)\b(\s*\})?`)

// definers is defineCmd with the short names of the text for the commands
// that define commands.
func definers(text []byte) (*regexp.Regexp, map[string]string) {
	how := map[string]string{}
	for _, m := range definerAlias.FindAllSubmatch(text, -1) {
		name := string(m[1]) + string(m[2]) + string(m[3])
		if (len(m[3]) > 0) == (len(m[5]) > 0) || defineCmd.MatchString(`\`+name) {
			// A \newcommand or \def body closes after the command, and a
			// \let has no brace.
			continue
		}
		how[name] = string(m[4])
	}
	if len(how) == 0 {
		return defineCmd, how
	}
	names := make([]string, 0, len(how))
	for n := range how {
		names = append(names, n)
	}
	slices.Sort(names)
	return regexp.MustCompile(`\\(` + strings.Join(names, "|") + `|` + strings.TrimSuffix(strings.TrimPrefix(defineCmd.String(), `\\(`), `)\b`) + `)\b`), how
}

func (m *Macros) read(text []byte, file string) {
	re, alias := definers(text)
	for _, loc := range re.FindAllSubmatchIndex(text, -1) {
		if escaped(text, loc[0]) {
			continue
		}
		name, prev := "", (*Macro)(nil)
		if a, _, ok := ReadArgs(text, loc[1], "*c"); ok {
			name = strings.TrimSpace(a.Values[0])
			prev = m.Commands[name]
		}
		how := string(text[loc[2]:loc[3]])
		if a, ok := alias[how]; ok {
			how = a
		}
		m.readOne(text, file, how, loc)
		if c := m.Commands[name]; refCmds[name] && c != nil && !refBody.MatchString(c.Body) {
			if prev != nil {
				m.Commands[name] = prev
			} else {
				delete(m.Commands, name)
			}
		}
	}
}

// readOne reads the definition at loc. A definition of a reference command
// that gives no reference again keeps the definition before it.
func (m *Macros) readOne(text []byte, file, how string, loc []int) {
	at := loc[1]
	switch how {
	case "newcommand", "renewcommand", "providecommand", "DeclareRobustCommand":
		a, _, ok := ReadArgs(text, at, "*coom")
		if !ok {
			return
		}
		name := strings.TrimSpace(a.Values[0])
		if how == "providecommand" {
			if _, have := m.Commands[name]; have {
				return
			}
		}
		n, _ := strconv.Atoi(strings.TrimSpace(a.Values[1]))
		m.Commands[name] = &Macro{Name: name, Args: n, Default: a.Values[2], HasDefault: a.Has[2], Body: a.Values[3], How: "newcommand", File: file}
	case "newcommandtwoopt", "renewcommandtwoopt":
		// \newcommandtwoopt{\theo}[3][][]{...}: the first two arguments
		// are optional. Seen in the gold pilot (set E).
		a, _, ok := ReadArgs(text, at, "*cooom")
		if !ok {
			return
		}
		name := strings.TrimSpace(a.Values[0])
		n, _ := strconv.Atoi(strings.TrimSpace(a.Values[1]))
		if n < 2 {
			return
		}
		m.Commands[name] = &Macro{Name: name, Args: n, Default: a.Values[2], HasDefault: true, Default2: a.Values[3], Opts: 2, Body: a.Values[4], How: "newcommand", File: file}
	case "def", "gdef", "edef", "xdef":
		a, end, ok := ReadArgs(text, at, "c")
		if !ok || !strings.HasPrefix(a.Values[0], `\`) {
			return
		}
		params := defParams.Find(text[end:])
		body := skipSpace(text, end+len(params))
		if body >= len(text) || text[body] != '{' {
			return
		}
		in, e, gok := Group(text, body)
		if !gok {
			return
		}
		m.Commands[a.Values[0]] = &Macro{Name: a.Values[0], Args: len(params) / 2, Body: string(text[in : e-1]), How: "def", File: file}
	case "let":
		a, end, ok := ReadArgs(text, at, "c")
		if !ok {
			return
		}
		j := skipSpace(text, end)
		if j < len(text) && text[j] == '=' {
			j++
		}
		b, _, ok := ReadArgs(text, j, "c")
		if !ok || !strings.HasPrefix(b.Values[0], `\`) {
			return
		}
		m.Commands[a.Values[0]] = &Macro{Name: a.Values[0], Body: b.Values[0], How: "let", File: file}
	case "DeclareMathOperator":
		a, _, ok := ReadArgs(text, at, "*cm")
		if !ok {
			return
		}
		op := `\operatorname`
		if a.Star {
			op += "*"
		}
		m.Commands[a.Values[0]] = &Macro{Name: a.Values[0], Body: op + "{" + a.Values[1] + "}", How: "operator", File: file}
	case "newenvironment", "renewenvironment":
		a, _, ok := ReadArgs(text, at, "*moomm")
		if !ok {
			return
		}
		name := strings.TrimSpace(a.Values[0])
		n, _ := strconv.Atoi(strings.TrimSpace(a.Values[1]))
		if _, have := m.Envs[name]; !have {
			m.EnvOrder = append(m.EnvOrder, name)
		}
		m.Envs[name] = &Env{Name: name, Args: n, Default: a.Values[2], HasDefault: a.Has[2], Begin: a.Values[3], End: a.Values[4], Off: loc[0], File: file}
	case "NewDocumentCommand", "RenewDocumentCommand", "DeclareDocumentCommand":
		a, _, ok := ReadArgs(text, at, "cmm")
		if !ok {
			return
		}
		n := strings.Count(a.Values[1], "m") + strings.Count(a.Values[1], "o")
		m.Commands[a.Values[0]] = &Macro{Name: a.Values[0], Args: n, Body: a.Values[2], How: "newcommand", File: file}
	}
}

// Spec is the argument spec of a call of the command, as ReadArgs reads it.
func (c *Macro) Spec() string {
	o := c.Opts
	if o == 0 && c.HasDefault {
		o = 1
	}
	o = max(0, min(o, c.Args))
	return strings.Repeat("o", o) + strings.Repeat("m", c.Args-o)
}

// Fill is the body of the command with the arguments of a call in it.
func (c *Macro) Fill(a Args) string {
	body := c.Body
	for i := len(a.Values) - 1; i >= 0; i-- {
		v := a.Values[i]
		if i < len(a.Has) && !a.Has[i] {
			v = c.Default
			if i == 1 {
				v = c.Default2
			}
		}
		body = strings.ReplaceAll(body, "#"+strconv.Itoa(i+1), v)
	}
	return body
}

// maxExpansion stops the expansion of a text that grows without end.
const maxExpansion = 1 << 20

// Expand replaces the paper's own commands in s by their bodies.
//
// It is used to normalize mathematics, so that \R and \mathbb{R} give the same
// text. It expands to a depth of eight and stops when the text grows past
// maxExpansion, so a recursive definition cannot hang it.
func (m *Macros) Expand(s string) string {
	if m == nil || len(m.Commands) == 0 {
		return s
	}
	for depth := 0; depth < 8; depth++ {
		next, changed := m.expandOnce(s)
		if !changed || len(next) > maxExpansion {
			return s
		}
		s = next
	}
	return s
}

func (m *Macros) expandOnce(s string) (string, bool) {
	text := []byte(s)
	var out strings.Builder
	changed := false
	at := 0
	for _, loc := range cmdName.FindAllIndex(text, -1) {
		if loc[0] < at || escaped(text, loc[0]) {
			continue
		}
		mac, ok := m.Commands[string(text[loc[0]:loc[1]])]
		if !ok || mac.How == "def" && strings.Contains(mac.Body, "\\def") {
			continue
		}
		a, end, ok := ReadArgs(text, loc[1], mac.Spec())
		if !ok {
			continue
		}
		body := mac.Fill(a)
		out.Write(text[at:loc[0]])
		out.WriteString(body)
		// A command name ends at the first byte that is not a letter. Keep a
		// space after a body that ends in a command name, or the next letter
		// joins it.
		if len(body) > 0 && isLetter(body[len(body)-1]) && end < len(text) && isLetter(text[end]) {
			out.WriteByte(' ')
		}
		at = end
		changed = true
	}
	out.Write(text[at:])
	return out.String(), changed
}

package tex

import (
	"regexp"
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
	Body       string `json:"body"`
	// How is the command that defined it: newcommand, def, let or operator.
	How string `json:"how"`
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

var defineCmd = regexp.MustCompile(`\\(newcommand|renewcommand|providecommand|DeclareRobustCommand|def|gdef|edef|xdef|let|DeclareMathOperator|newenvironment|renewenvironment|NewDocumentCommand|RenewDocumentCommand|DeclareDocumentCommand)\b`)

var defParams = regexp.MustCompile(`^(?:#[1-9])*`)

// ReadMacros reads the macro table from the text and from the local style
// files.
func ReadMacros(text []byte, local map[string][]byte) *Macros {
	m := NewMacros()
	for _, name := range sortedKeys(local) {
		m.read(local[name])
	}
	m.read(text)
	return m
}

func (m *Macros) read(text []byte) {
	for _, loc := range defineCmd.FindAllSubmatchIndex(text, -1) {
		if escaped(text, loc[0]) {
			continue
		}
		how := string(text[loc[2]:loc[3]])
		at := loc[1]
		switch how {
		case "newcommand", "renewcommand", "providecommand", "DeclareRobustCommand":
			a, _, ok := ReadArgs(text, at, "*coom")
			if !ok {
				continue
			}
			name := strings.TrimSpace(a.Values[0])
			if how == "providecommand" {
				if _, have := m.Commands[name]; have {
					continue
				}
			}
			n, _ := strconv.Atoi(strings.TrimSpace(a.Values[1]))
			m.Commands[name] = &Macro{Name: name, Args: n, Default: a.Values[2], HasDefault: a.Has[2], Body: a.Values[3], How: "newcommand"}
		case "def", "gdef", "edef", "xdef":
			a, end, ok := ReadArgs(text, at, "c")
			if !ok || !strings.HasPrefix(a.Values[0], `\`) {
				continue
			}
			params := defParams.Find(text[end:])
			body := skipSpace(text, end+len(params))
			if body >= len(text) || text[body] != '{' {
				continue
			}
			in, e, gok := Group(text, body)
			if !gok {
				continue
			}
			m.Commands[a.Values[0]] = &Macro{Name: a.Values[0], Args: len(params) / 2, Body: string(text[in : e-1]), How: "def"}
		case "let":
			a, end, ok := ReadArgs(text, at, "c")
			if !ok {
				continue
			}
			j := skipSpace(text, end)
			if j < len(text) && text[j] == '=' {
				j++
			}
			b, _, ok := ReadArgs(text, j, "c")
			if !ok || !strings.HasPrefix(b.Values[0], `\`) {
				continue
			}
			m.Commands[a.Values[0]] = &Macro{Name: a.Values[0], Body: b.Values[0], How: "let"}
		case "DeclareMathOperator":
			a, _, ok := ReadArgs(text, at, "*cm")
			if !ok {
				continue
			}
			op := `\operatorname`
			if a.Star {
				op += "*"
			}
			m.Commands[a.Values[0]] = &Macro{Name: a.Values[0], Body: op + "{" + a.Values[1] + "}", How: "operator"}
		case "newenvironment", "renewenvironment":
			a, _, ok := ReadArgs(text, at, "*moomm")
			if !ok {
				continue
			}
			name := strings.TrimSpace(a.Values[0])
			n, _ := strconv.Atoi(strings.TrimSpace(a.Values[1]))
			if _, have := m.Envs[name]; !have {
				m.EnvOrder = append(m.EnvOrder, name)
			}
			m.Envs[name] = &Env{Name: name, Args: n, Default: a.Values[2], HasDefault: a.Has[2], Begin: a.Values[3], End: a.Values[4], Off: loc[0]}
		case "NewDocumentCommand", "RenewDocumentCommand", "DeclareDocumentCommand":
			a, _, ok := ReadArgs(text, at, "cmm")
			if !ok {
				continue
			}
			n := strings.Count(a.Values[1], "m") + strings.Count(a.Values[1], "o")
			m.Commands[a.Values[0]] = &Macro{Name: a.Values[0], Args: n, Body: a.Values[2], How: "newcommand"}
		}
	}
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
		spec := strings.Repeat("m", mac.Args)
		if mac.HasDefault && mac.Args > 0 {
			spec = "o" + strings.Repeat("m", mac.Args-1)
		}
		a, end, ok := ReadArgs(text, loc[1], spec)
		if !ok {
			continue
		}
		body := mac.Body
		for i := len(a.Values) - 1; i >= 0; i-- {
			v := a.Values[i]
			if !a.Has[i] {
				v = mac.Default
			}
			body = strings.ReplaceAll(body, "#"+strconv.Itoa(i+1), v)
		}
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

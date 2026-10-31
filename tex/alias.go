package tex

import (
	"bytes"
	"regexp"
	"strings"
)

// theoremDecl is the declaration of a theorem environment, with its name.
var theoremDecl = regexp.MustCompile(`\\newtheorem\*?\s*\{([A-Za-z]+)\}`)

// commandEnvs returns the commands that open and close a theorem
// environment, as \definition and \enddefinition: LaTeX lets a paper write
// an environment so, with no \begin and \end. A name is one only when the
// body has its \end command. Seen in the gold pilot (set X).
func (d *Document) commandEnvs() map[string]string {
	out := map[string]string{}
	body := d.Text[d.Body:d.End]
	for _, m := range theoremDecl.FindAllSubmatch(d.Text[:d.Body], -1) {
		name := string(m[1])
		if _, ok := d.Macros.Commands[`\`+name]; ok {
			continue
		}
		if regexp.MustCompile(`\\end` + name + `\b`).Match(body) {
			out[`\`+name] = `\begin{` + name + `}`
			out[`\end`+name] = `\end{` + name + `}`
		}
	}
	return out
}

// fullAlias is the body of a command that stands for one \begin or \end of a
// named environment, as \newcommand{\bthm}{\begin{theorem}}, or for a short
// text with one of them in it, as \def\eprf{\qed \end{proof}}.
var fullAlias = regexp.MustCompile(`^[^#]{0,80}\\(begin|end)\s*\{[^{}\\]{1,64}\}[^#]{0,80}$`)

// beginEnd finds each \begin and \end in the body of an alias.
var beginEnd = regexp.MustCompile(`\\(begin|end)\b`)

// envName is the argument of a command that stands for \begin or \end alone.
var envName = regexp.MustCompile(`^\{\s*[A-Za-z@*]{1,64}\s*\}`)

// argAlias is the body of a command that opens a named environment and gives
// its own arguments to the environment in order, as
// \newcommand{\lem}[1]{\begin{lemma}{#1}}.
// sectionAlias is the body of a command that writes a section head with its
// argument and then more that takes no argument, as
// \newcommand{\mysection}[1]{\section{#1}\setcounter{equation}{0}}. Seen in
// the gold pilot (set X).
var sectionAlias = regexp.MustCompile(`^(\\(?:part|chapter|(?:sub)*section)\*?)\s*\{#1\}[^#]*$`)

// sectionCall is the body of a command of several arguments that writes a
// section head with one argument and labels it with another, as
// \renewcommand{\sec}[2]{\section{#2}\label{s:#1}}. Seen in the gold pilot
// (set X).
var sectionCall = regexp.MustCompile(`^\\(?:part|chapter|(?:sub)*section)\*?\s*\{#[1-9]\}\s*(?:\\label\s*\{[^{}#\\]{0,40}#[1-9][^{}#\\]{0,40}\})?$`)

// sectionCalls returns the commands that sectionCall matches, each argument
// used once, with their bodies.
func (m *Macros) sectionCalls() map[string]Macro {
	out := map[string]Macro{}
	for name, c := range m.Commands {
		if c.Args < 2 || c.HasDefault || len(name) < 2 {
			continue
		}
		body := strings.TrimSpace(c.Body)
		if !sectionCall.MatchString(body) {
			continue
		}
		seen := map[string]bool{}
		for _, r := range argRef.FindAllStringSubmatch(body, -1) {
			seen[r[1]] = true
		}
		if len(seen) == c.Args && len(argRef.FindAllString(body, -1)) == c.Args {
			out[name] = Macro{Args: c.Args, Body: body}
		}
	}
	return out
}

// sectionName is the body of a command with no arguments that stands for a
// sectioning command, as \newcommand{\newpar}{\paragraph}. Seen in the gold
// pilot (set X).
var sectionName = regexp.MustCompile(`^\\(?:part|chapter|(?:sub)*section|(?:sub)?paragraph)\*?$`)

var argAlias = regexp.MustCompile(`^(\\begin\s*\{[^{}\\#]{1,64}\})((?:\s*\{#[1-9]\})+)$`)

var argRef = regexp.MustCompile(`#([1-9])`)

// mathDelim is the delimiters of math, which no paper means as an alias.
var mathDelim = map[string]bool{`\[`: true, `\]`: true, `\(`: true, `\)`: true}

// labelAlias is the body of a command that writes a label with a prefix, as
// \newcommand{\prlabel}[1]{\label{pr:#1}}.
var labelAlias = regexp.MustCompile(`^\\label\s*\{([^{}#\\]{0,40})#1([^{}#\\]{0,40})\}$`)

// beginLabel is the body of a command that opens a named environment and
// labels it with its argument, as \def\lemm#1{\begin{lem}\lbl{#1}}.
var beginLabel = regexp.MustCompile(`^(\\begin\s*\{[^{}\\#]{1,64}\})\s*(\\[A-Za-z]+)\s*\{#1\}$`)

// labelAliases returns the commands of one argument that write a label, with
// the text before and after the argument in the label, and the \begin that
// the command writes before the label, if any. The front end writes
// \prlabel{a} as \label{pr:a}, so that each reader of the labels sees the
// label that \ref{pr:a} names, and \lemm{a} as \begin{lem}\label{a}. Seen in
// the gold pilot (set X).
func (m *Macros) labelAliases() map[string][3]string {
	out := map[string][3]string{}
	for name, c := range m.Commands {
		if c.Args != 1 || c.HasDefault || name == `\label` {
			continue
		}
		if a := labelAlias.FindStringSubmatch(strings.TrimSpace(c.Body)); a != nil {
			out[name] = [3]string{a[1], a[2], ""}
		}
	}
	for name, c := range m.Commands {
		if c.Args != 1 || c.HasDefault {
			continue
		}
		a := beginLabel.FindStringSubmatch(strings.TrimSpace(c.Body))
		if a == nil {
			continue
		}
		if a[2] == `\label` {
			out[name] = [3]string{"", "", a[1]}
		} else if l, ok := out[a[2]]; ok && l[2] == "" {
			out[name] = [3]string{l[0], l[1], a[1]}
		}
	}
	return out
}

// structureAliases returns the commands with no arguments that stand for
// \begin or \end, with what each one stands for. A paper can write
// \def\>{\end} and close each environment with \>{lemma}, or write
// \def\bthm{\begin{theorem}}. The front end does not expand commands, so
// without this the scanners of the environments do not see them.
//
// A command with arguments is an alias when its body is the \begin of an
// environment followed by the arguments {#1} to {#n} in order. The call
// \lem{a} then stands for \begin{lemma}{a}, so the alias is the \begin and the
// arguments of the call stay where they are.
func (m *Macros) structureAliases() map[string]string {
	out := map[string]string{}
	for name, c := range m.Commands {
		if len(name) < 2 {
			continue
		}
		if c.Args > 0 && !c.HasDefault {
			if a := argAlias.FindStringSubmatch(strings.TrimSpace(c.Body)); a != nil && inOrder(a[2], c.Args) {
				out[name] = a[1]
			}
			if a := sectionAlias.FindStringSubmatch(strings.TrimSpace(c.Body)); a != nil && c.Args == 1 && name != a[1] {
				out[name] = a[1]
			}
			continue
		}
		if c.Args != 0 {
			continue
		}
		body := strings.TrimSpace(c.Body)
		if mathDelim[name] || strings.ContainsAny(body, "\n%") || strings.Contains(body, `\if`) {
			// A class that redefines \] to close its own trivlist, over
			// several lines and under \ifmmode, is not an alias: written in
			// for each \], it adds lines and an \end that the paper does not
			// have. Seen in the gold pilot (set E).
			continue
		}
		if sectionName.MatchString(body) && body != name {
			out[name] = body
			continue
		}
		if body == `\begin` || body == `\end` || (fullAlias.MatchString(body) && len(beginEnd.FindAllStringIndex(body, -1)) == 1) || emptyEnv(body) {
			out[name] = body
		}
	}
	return out
}

// emptyItem is the body of a command that writes an environment with nothing
// in it, as \def\st{\begin{sta} \ \end{sta}}: the paper numbers its
// paragraphs with an untitled theorem environment. Seen in the gold pilot
// (set E).
var emptyItem = regexp.MustCompile(`^\\begin\s*\{([A-Za-z@*]{1,64})\}(?:\s|\\ |~)*\\end\s*\{([A-Za-z@*]{1,64})\}$`)

func emptyEnv(body string) bool {
	m := emptyItem.FindStringSubmatch(body)
	return m != nil && m[1] == m[2]
}

// inOrder tells if the arguments are #1 to #n, each once and in order.
func inOrder(args string, n int) bool {
	refs := argRef.FindAllStringSubmatch(args, -1)
	if len(refs) != n {
		return false
	}
	for i, r := range refs {
		if r[1] != string(rune('1'+i)) {
			return false
		}
	}
	return true
}

// wrapBody is the body of a command that puts its last argument in a named
// environment, as \newcommand{\clai}[2][]{\begin{claim}[#1]#2\end{claim}}.
var wrapBody = regexp.MustCompile(`(?s)^\\begin\s*\{([A-Za-z@*]{1,64})\}(.{0,300})\\end\s*\{([A-Za-z@*]{1,64})\}$`)

// wrapMath is the environments of mathematics, which a wrapper of a formula
// opens. The front end reads them as they are, so they are not unwrapped.
var wrapMath = regexp.MustCompile(`^(?:align|alignat|flalign|equation|eqnarray|gather|multline|displaymath|math|split|cases|array|[a-zA-Z]*matrix)\*?$`)

// emptyArgs is an empty optional argument at the start of a head, or an
// empty \label.
var emptyArgs = regexp.MustCompile(`^\s*\[\s*\]|\\label\s*\{\s*\}`)

// wrapper is a command that puts its last argument in an environment: the
// call is the \begin of the environment with Head after it, the last argument
// stays where it is, and its closing brace is Tail and the \end.
type wrapper struct {
	mac             *Macro
	env, head, tail string
}

// wrappers returns the commands that put their last argument in a named
// environment. A paper can define \lemm[title][label]{text} as a lemma with
// that title and label. Seen in the gold pilot (set E).
func (m *Macros) wrappers() map[string]wrapper {
	out := map[string]wrapper{}
	for name, c := range m.Commands {
		if len(name) < 2 || c.Args == 0 {
			continue
		}
		w := wrapBody.FindStringSubmatch(strings.TrimSpace(c.Body))
		if w == nil || w[1] != w[3] || wrapMath.MatchString(w[1]) {
			continue
		}
		last := "#" + string(rune('0'+c.Args))
		k := strings.Index(w[2], last)
		if k < 0 || len(w[2])-k > 80 {
			continue
		}
		head, tail := w[2][:k], strings.ReplaceAll(w[2][k+2:], "\n", " ")
		if beginEnd.MatchString(head+tail) || strings.Contains(tail, last) || strings.Contains(head+tail, "%") || !balanced(head) || !balanced(tail) {
			continue
		}
		out[name] = wrapper{mac: c, env: w[1], head: head, tail: tail}
	}
	return out
}

// balanced tells if the braces of s are balanced.
func balanced(s string) bool {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\':
			i++
		case s[i] == '{':
			depth++
		case s[i] == '}':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}

// unwrap writes a call of a wrapper as the \begin of its environment. It
// returns the text for the call up to the opening brace of the last argument,
// the offset after that brace, and the offset of the closing brace. The text
// has the newlines of the call, so the lines stay right.
func (w wrapper) unwrap(text []byte, at, limit int) (string, int, int, bool) {
	spec := w.mac.Spec()
	a, end, ok := ReadArgs(text, at, spec[:len(spec)-1])
	if !ok {
		return "", 0, 0, false
	}
	open := skipSpace(text, end)
	if open >= limit || text[open] != '{' {
		return "", 0, 0, false
	}
	_, close, ok := Group(text, open)
	if !ok || close > limit || bytes.IndexByte(text[at:open], '%') >= 0 {
		return "", 0, 0, false
	}
	a.Values = append(a.Values, "#"+string(rune('0'+w.mac.Args)))
	a.Has = append(a.Has, true)
	head := (&Macro{Body: w.head, Default: w.mac.Default, Default2: w.mac.Default2}).Fill(a)
	// A call without the optional arguments leaves [] and \label{}, which
	// LaTeX prints as nothing.
	head = emptyArgs.ReplaceAllString(strings.ReplaceAll(head, "\n", " "), "")
	head = `\begin{` + w.env + `}` + head + strings.Repeat("\n", bytes.Count(text[at:open], []byte("\n")))
	return head, open + 1, close - 1, true
}

// defining is the commands that define the command after them.
var defining = regexp.MustCompile(`\\(def|gdef|edef|xdef|let|global|newcommand\*?\s*\{|renewcommand\*?\s*\{|providecommand\*?\s*\{|DeclareRobustCommand\*?\s*\{)\s*$`)

// unalias writes each use of a structure alias in the body as the \begin or
// \end that it stands for, and moves the offsets after it. The text gets
// longer, but no line is added, so the lines of the text stay right.
func (d *Document) unalias() int {
	aliases := d.Macros.structureAliases()
	if d.Body > 0 && d.End > d.Body {
		for name, body := range d.commandEnvs() {
			if _, ok := aliases[name]; !ok {
				aliases[name] = body
			}
		}
	}
	wraps := d.Macros.wrappers()
	labels := d.Macros.labelAliases()
	calls := d.Macros.sectionCalls()
	if len(aliases)+len(wraps)+len(labels)+len(calls) == 0 || d.Body == 0 || d.End <= d.Body {
		return 0
	}
	type edit struct{ at, delta int }
	var edits []edit
	var out bytes.Buffer
	text := d.Text
	out.Write(text[:d.Body])
	last := d.Body
	// closes is the closing braces of the wrappers that are open, with the
	// text that replaces each one.
	closes := map[int]string{}
	for i := d.Body; i < d.End; i++ {
		if c, ok := closes[i]; ok {
			out.Write(text[last:i])
			out.WriteString(c)
			edits = append(edits, edit{at: i, delta: len(c) - 1})
			last = i + 1
			continue
		}
		if text[i] != '\\' || escaped(text, i) {
			continue
		}
		j := i + 1
		for j < d.End && isLetter(text[j]) {
			j++
		}
		if j == i+1 && j < d.End {
			j++ // a control symbol, as \>
		}
		if w, ok := wraps[string(text[i:j])]; ok && !defining.Match(text[max(d.Body, i-40):i]) {
			if head, in, close, ok := w.unwrap(text, j, d.End); ok {
				out.Write(text[last:i])
				out.WriteString(head)
				edits = append(edits, edit{at: i, delta: len(head) - (in - i)})
				closes[close] = w.tail + `\end{` + w.env + `}`
				last = in
				i = in - 1
				continue
			}
		}
		if l, ok := labels[string(text[i:j])]; ok && !defining.Match(text[max(d.Body, i-40):i]) {
			if k := skipSpace(text, j); k < d.End && text[k] == '{' {
				if in, e, ok := Group(text, k); ok && e <= d.End {
					with := l[2] + `\label{` + l[0] + string(text[in:e-1]) + l[1] + `}`
					out.Write(text[last:i])
					out.WriteString(with)
					edits = append(edits, edit{at: i, delta: len(with) - (e - i)})
					last = e
					i = e - 1
					continue
				}
			}
		}
		if c, ok := calls[string(text[i:j])]; ok && !defining.Match(text[max(d.Body, i-40):i]) {
			if with, e, ok := expandCall(text, j, d.End, c); ok {
				out.Write(text[last:i])
				out.WriteString(with)
				edits = append(edits, edit{at: i, delta: len(with) - (e - i)})
				last = e
				i = e - 1
				continue
			}
		}
		with, ok := aliases[string(text[i:j])]
		if !ok || defining.Match(text[max(d.Body, i-40):i]) {
			i = j - 1
			continue
		}
		if (with == `\begin` || with == `\end`) && !envName.Match(text[skipSpace(text, j):min(d.End, j+70)]) {
			i = j - 1
			continue
		}
		out.Write(text[last:i])
		out.WriteString(with)
		if (fullAlias.MatchString(with) || sectionName.MatchString(with)) && j < d.End && isLetter(text[j]) {
			out.WriteByte(' ')
			with += " "
		}
		edits = append(edits, edit{at: i, delta: len(with) - (j - i)})
		last = j
		i = j - 1
	}
	if len(edits) == 0 {
		return 0
	}
	out.Write(text[last:])
	shift := func(off int) int {
		s := off
		for _, e := range edits {
			if e.at < off {
				s += e.delta
			}
		}
		return s
	}
	for k := range d.segs {
		d.segs[k].off = shift(d.segs[k].off)
	}
	for k := range d.Conditional {
		d.Conditional[k].From, d.Conditional[k].To = shift(d.Conditional[k].From), shift(d.Conditional[k].To)
	}
	d.End = shift(d.End)
	d.Text = out.Bytes()
	return len(edits)
}

// expandCall reads the arguments of a section call at j and writes the body
// with them, and returns where the call ends.
func expandCall(text []byte, j, limit int, c Macro) (string, int, bool) {
	var args []string
	e := j
	for range c.Args {
		k := skipSpace(text, e)
		if k >= limit || text[k] != '{' {
			return "", 0, false
		}
		in, end, ok := Group(text, k)
		if !ok || end > limit {
			return "", 0, false
		}
		args = append(args, string(text[in:end-1]))
		e = end
	}
	with := argRef.ReplaceAllStringFunc(c.Body, func(r string) string { return args[r[1]-'1'] })
	return with, e, true
}

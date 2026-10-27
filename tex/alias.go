package tex

import (
	"bytes"
	"regexp"
	"strings"
)

// fullAlias is the body of a command that stands for one \begin or \end of a
// named environment, as \newcommand{\bthm}{\begin{theorem}}, or for a short
// text with one of them in it, as \def\eprf{\qed \end{proof}}.
var fullAlias = regexp.MustCompile(`^[^#]{0,80}\\(begin|end)\s*\{[^{}\\]{1,64}\}[^#]{0,80}$`)

// beginEnd finds each \begin and \end in the body of an alias.
var beginEnd = regexp.MustCompile(`\\(begin|end)\b`)

// envName is the argument of a command that stands for \begin or \end alone.
var envName = regexp.MustCompile(`^\{\s*[A-Za-z@*]{1,64}\s*\}`)

// structureAliases returns the commands with no arguments that stand for
// \begin or \end, with what each one stands for. A paper can write
// \def\>{\end} and close each environment with \>{lemma}, or write
// \def\bthm{\begin{theorem}}. The front end does not expand commands, so
// without this the scanners of the environments do not see them.
func (m *Macros) structureAliases() map[string]string {
	out := map[string]string{}
	for name, c := range m.Commands {
		if c.Args != 0 || len(name) < 2 {
			continue
		}
		body := strings.TrimSpace(c.Body)
		if body == `\begin` || body == `\end` || (fullAlias.MatchString(body) && len(beginEnd.FindAllStringIndex(body, -1)) == 1) {
			out[name] = body
		}
	}
	return out
}

// defining is the commands that define the command after them.
var defining = regexp.MustCompile(`\\(def|gdef|edef|xdef|let|global|newcommand\*?\s*\{|renewcommand\*?\s*\{|providecommand\*?\s*\{|DeclareRobustCommand\*?\s*\{)\s*$`)

// unalias writes each use of a structure alias in the body as the \begin or
// \end that it stands for, and moves the offsets after it. The text gets
// longer, but no line is added, so the lines of the text stay right.
func (d *Document) unalias() int {
	aliases := d.Macros.structureAliases()
	if len(aliases) == 0 || d.Body == 0 || d.End <= d.Body {
		return 0
	}
	type edit struct{ at, delta int }
	var edits []edit
	var out bytes.Buffer
	text := d.Text
	out.Write(text[:d.Body])
	last := d.Body
	for i := d.Body; i < d.End; i++ {
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
		if fullAlias.MatchString(with) && j < d.End && isLetter(text[j]) {
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

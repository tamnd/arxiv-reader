package tex

import (
	"regexp"
	"strings"
)

// dropping returns the commands of the paper that take arguments and print
// nothing, as \newcommand{\off}[1]{}. A paper uses such a command to hide a
// part of the text, as a comment does. A command with an optional argument,
// a command of a style file and a command that the text defines more than
// once are not in the result, because then the call can print its argument.
func (d *Document) dropping() map[string]int {
	out := map[string]int{}
	for name, c := range d.Macros.Commands {
		if len(name) < 2 || c.Args == 0 || c.HasDefault || c.File != "" || strings.TrimSpace(c.Body) != "" {
			continue
		}
		if c.How != "newcommand" && c.How != "def" {
			continue
		}
		defs := regexp.MustCompile(`\\(?:re)?newcommand\*?\s*\{?\s*` + regexp.QuoteMeta(name) + `\b|\\[gex]?def\s*` + regexp.QuoteMeta(name) + `\b|\\let\s*` + regexp.QuoteMeta(name) + `\b`)
		if len(defs.FindAllIndex(d.Text, 2)) != 1 {
			continue
		}
		out[name] = c.Args
	}
	return out
}

// drop blanks each use of a command that prints nothing in the body, with its
// arguments. The text keeps its length and its lines.
func (d *Document) drop() int {
	cmds := d.dropping()
	if len(cmds) == 0 || d.Body == 0 || d.End <= d.Body {
		return 0
	}
	text, n := d.Text, 0
	for i := d.Body; i < d.End; i++ {
		if text[i] != '\\' || escaped(text, i) {
			continue
		}
		j := i + 1
		for j < d.End && isLetter(text[j]) {
			j++
		}
		args, ok := cmds[string(text[i:j])]
		if !ok || defining.Match(text[max(d.Body, i-40):i]) {
			i = max(i, j-1)
			continue
		}
		end, k := j, 0
		for ; k < args; k++ {
			at := skipSpace(text, end)
			if at >= d.End || text[at] != '{' {
				break
			}
			_, e, ok := Group(text, at)
			if !ok || e > d.End {
				break
			}
			end = e
		}
		if k < args {
			i = j - 1
			continue
		}
		blank(text, i, end)
		n++
		i = end - 1
	}
	return n
}

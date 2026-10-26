package tex

import (
	"regexp"
)

// primitives are the conditionals that TeX and the common engines define.
//
// The commands of etoolbox (\ifdef, \ifbool, \ifstrequal) take arguments and
// have no \fi, so they are not in this list. Neither is \iff, which is the
// symbol "if and only if".
var primitives = map[string]bool{
	"if": true, "ifcat": true, "ifnum": true, "ifdim": true, "ifodd": true,
	"ifvmode": true, "ifhmode": true, "ifmmode": true, "ifinner": true,
	"ifvoid": true, "ifhbox": true, "ifvbox": true, "ifx": true, "ifeof": true,
	"iftrue": true, "iffalse": true, "ifcase": true, "ifdefined": true,
	"ifcsname": true, "iffontchar": true, "ifincsname": true,
	"ifpdf": true, "ifxetex": true, "ifluatex": true, "ifpdftex": true,
	"ifPDFTeX": true, "ifetex": true, "ifvtex": true, "ifpdfprimitive": true,
}

var (
	cmdName = regexp.MustCompile(`\\([a-zA-Z@]+)`)
	newIf   = regexp.MustCompile(`\\newif\s*\\if([a-zA-Z@]+)`)
)

type condTok struct {
	name     string
	off, end int
}

type cond struct {
	start   condTok
	els, fi condTok
	hasElse bool
}

// scanConds pairs each conditional with its \else and its \fi.
func scanConds(text []byte, declared map[string]bool) []cond {
	var stack []cond
	var out []cond
	for _, m := range cmdName.FindAllSubmatchIndex(text, -1) {
		if escaped(text, m[0]) {
			continue
		}
		name := string(text[m[2]:m[3]])
		t := condTok{name: name, off: m[0], end: m[1]}
		switch {
		case name == "fi":
			if n := len(stack); n > 0 {
				c := stack[n-1]
				stack = stack[:n-1]
				c.fi = t
				out = append(out, c)
			}
		case name == "else":
			if n := len(stack); n > 0 && !stack[n-1].hasElse {
				stack[n-1].els, stack[n-1].hasElse = t, true
			}
		case primitives[name] || (len(name) > 2 && name[:2] == "if" && declared[name[2:]]):
			if before(text, m[0], `\newif`) {
				continue
			}
			stack = append(stack, cond{start: t})
		}
	}
	return out
}

// before tells if the text just before off, after spaces, is the command cmd.
func before(text []byte, off int, cmd string) bool {
	i := off
	for i > 0 && (text[i-1] == ' ' || text[i-1] == '\t') {
		i--
	}
	return i >= len(cmd) && string(text[i-len(cmd):i]) == cmd
}

// blankFalse removes the branches that never print: \iffalse and \if0.
//
// It runs before the macro table and the body exist, because a hidden part
// can hold a \begin{document} or a declaration that must not count.
func blankFalse(text []byte) {
	for _, c := range scanConds(text, nil) {
		zero := c.start.name == "if" && c.start.end < len(text) && text[c.start.end] == '0'
		if c.start.name != "iffalse" && !zero {
			continue
		}
		takeBranch(text, c, false)
	}
}

// takeBranch keeps one branch of a conditional and blanks the other.
func takeBranch(text []byte, c cond, value bool) {
	switch {
	case value && c.hasElse:
		blank(text, c.start.off, c.start.end)
		blank(text, c.els.off, c.fi.end)
	case value:
		blank(text, c.start.off, c.start.end)
		blank(text, c.fi.off, c.fi.end)
	case c.hasElse:
		blank(text, c.start.off, c.els.end)
		blank(text, c.fi.off, c.fi.end)
	default:
		blank(text, c.start.off, c.fi.end)
	}
}

// conditionals decides the conditionals that \newif declares.
//
// The value of \ifdraft at a place is the last \drafttrue or \draftfalse
// before it, and false when there is none, as in TeX. A conditional that the
// front end cannot decide keeps both branches. When it is in the body, its
// span goes into Conditional.
func (d *Document) conditionals() int {
	declared := map[string]bool{}
	for _, m := range newIf.FindAllSubmatch(d.Text, -1) {
		declared[string(m[1])] = true
	}
	type set struct {
		off   int
		value bool
	}
	sets := map[string][]set{}
	for name := range declared {
		re := regexp.MustCompile(`\\` + regexp.QuoteMeta(name) + `(true|false)\b`)
		for _, m := range re.FindAllSubmatchIndex(d.Text, -1) {
			sets[name] = append(sets[name], set{off: m[0], value: string(d.Text[m[2]:m[3]]) == "true"})
		}
	}
	decided := 0
	for _, c := range scanConds(d.Text, declared) {
		name := c.start.name
		switch {
		case name == "iftrue":
			takeBranch(d.Text, c, true)
			decided++
		case len(name) > 2 && declared[name[2:]]:
			v := false
			for _, s := range sets[name[2:]] {
				if s.off < c.start.off {
					v = s.value
				}
			}
			takeBranch(d.Text, c, v)
			decided++
		default:
			if d.Body > 0 && c.start.off > d.Body {
				d.Conditional = append(d.Conditional, Region{From: c.start.off, To: c.fi.end})
			}
		}
	}
	return decided
}

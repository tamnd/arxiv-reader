package tex

import (
	"path"
	"regexp"
	"strings"
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

var (
	packageOpts   = regexp.MustCompile(`\\(?:usepackage|RequirePackage|documentclass|LoadClass)\s*\[([^\]]*)\]\s*\{([^{}]*)\}`)
	declareOption = regexp.MustCompile(`\\DeclareOption\s*\{([^{}]*)\}\s*\{`)
)

// decideLocal decides the conditionals of a local style file that the file
// declares with \newif, as the paper loads it. A \footrue in the body of a
// \DeclareOption{foo} counts only when the paper passes the option foo, as in
// \usepackage[foo]{style}; another one counts from where it is, and a
// conditional is false when nothing sets it, as in TeX.
func decideLocal(text []byte, name string, sty []byte) []byte {
	declared := map[string]bool{}
	for _, m := range newIf.FindAllSubmatch(sty, -1) {
		declared[string(m[1])] = true
	}
	flags := map[string]bool{}
	for _, m := range letFlag.FindAllSubmatch(sty, -1) {
		flags[string(m[1])] = true
	}
	if len(declared) == 0 && len(flags) == 0 {
		return sty
	}
	base := strings.TrimSuffix(path.Base(name), path.Ext(name))
	opts := map[string]bool{}
	for _, m := range packageOpts.FindAllSubmatch(text, -1) {
		for _, p := range strings.Split(string(m[2]), ",") {
			if strings.TrimSpace(p) != base {
				continue
			}
			for _, o := range strings.Split(string(m[1]), ",") {
				opts[strings.TrimSpace(o)] = true
			}
		}
	}
	type span struct {
		from, to int
		opt      string
	}
	var options []span
	for _, m := range declareOption.FindAllSubmatchIndex(sty, -1) {
		if _, end, ok := Group(sty, m[1]-1); ok {
			options = append(options, span{m[1], end, string(sty[m[2]:m[3]])})
		}
	}
	counts := func(off int) bool {
		for _, o := range options {
			if o.from <= off && off < o.to {
				return opts[o.opt]
			}
		}
		return true
	}
	out := append([]byte(nil), sty...)
	for _, c := range scanConds(out, declared) {
		n := c.start.name
		if len(n) <= 2 || !declared[n[2:]] {
			continue
		}
		re := regexp.MustCompile(`\\` + regexp.QuoteMeta(n[2:]) + `(true|false)\b`)
		v := false
		for _, m := range re.FindAllSubmatchIndex(sty, -1) {
			if m[0] < c.start.off && counts(m[0]) {
				v = string(sty[m[2]:m[3]]) == "true"
			}
		}
		takeBranch(out, c, v)
	}
	// A flag that an option sets with \let, as \DeclareOption{numberwithinsect}
	// {\let\numberwithinsect\relax} of the LIPIcs class, and that the file tests
	// with \ifx\numberwithinsect\relax: the test is true when the last \let of
	// the flag that counts before it makes it \relax, and false when nothing
	// sets it, as an undefined command is not \relax.
	if len(flags) > 0 {
		for _, c := range scanConds(out, declared) {
			if c.start.name != "ifx" {
				continue
			}
			m := ifxRelax.FindSubmatch(out[c.start.off:min(len(out), c.start.off+80)])
			if m == nil || !flags[string(m[1])] {
				continue
			}
			re := regexp.MustCompile(`\\let\s*\\` + regexp.QuoteMeta(string(m[1])) + `\s*=?\s*\\([a-zA-Z@]+)`)
			v := false
			for _, l := range re.FindAllSubmatchIndex(sty, -1) {
				if l[0] < c.start.off && counts(l[0]) {
					v = string(sty[l[2]:l[3]]) == "relax"
				}
			}
			takeBranch(out, c, v)
		}
	}
	return out
}

var (
	// letFlag is a flag that an option sets: \DeclareOption{x}{\let\x\relax}.
	letFlag = regexp.MustCompile(`\\DeclareOption\s*\{[^{}]*\}\s*\{\s*\\let\s*\\([a-zA-Z@]+)\s*=?\s*\\relax\s*\}`)
	// ifxRelax is the test of a flag: \ifx\x\relax.
	ifxRelax = regexp.MustCompile(`^\\ifx\s*\\([a-zA-Z@]+)\s*\\relax\b`)
)

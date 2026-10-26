package tex

import (
	"regexp"
)

// The flags of etoolbox and ifthen. Their tests take the two branches as
// arguments and have no \fi, so scanConds does not see them.
var (
	flagNew  = regexp.MustCompile(`\\(newtoggle|providetoggle|newbool|providebool|newboolean|provideboolean)\s*\{([^{}]+)\}`)
	flagSet  = regexp.MustCompile(`\\(toggletrue|togglefalse|booltrue|boolfalse)\s*\{([^{}]+)\}|\\(settoggle|setbool|setboolean)\s*\{([^{}]+)\}\s*\{\s*(true|false)\s*\}`)
	flagTest = regexp.MustCompile(`\\(iftoggle|nottoggle|ifbool|notbool)\s*\{([^{}]+)\}|\\ifthenelse\s*\{\s*\\boolean\s*\{([^{}]+)\}\s*\}`)
)

type flagSetting struct {
	off   int
	value bool
}

// toggles decides the tests of etoolbox and ifthen on flags that the paper
// declares, as \iftoggle{arxiv}{...}{...}.
//
// The value at a place is the last setting before it, and false when there is
// none. The tests are decided from the last to the first, so that an inner
// test is gone before its outer test reads its branches.
func (d *Document) toggles() int {
	declared := map[string]bool{}
	for _, m := range flagNew.FindAllSubmatch(d.Text, -1) {
		declared[trimSpace(string(m[2]))] = true
	}
	if len(declared) == 0 {
		return 0
	}
	sets := map[string][]flagSetting{}
	for _, m := range flagSet.FindAllSubmatchIndex(d.Text, -1) {
		if escaped(d.Text, m[0]) {
			continue
		}
		if m[2] >= 0 {
			cmd := string(d.Text[m[2]:m[3]])
			name := trimSpace(string(d.Text[m[4]:m[5]]))
			sets[name] = append(sets[name], flagSetting{off: m[0], value: cmd == "toggletrue" || cmd == "booltrue"})
			continue
		}
		name := trimSpace(string(d.Text[m[8]:m[9]]))
		sets[name] = append(sets[name], flagSetting{off: m[0], value: string(d.Text[m[10]:m[11]]) == "true"})
	}
	tests := flagTest.FindAllSubmatchIndex(d.Text, -1)
	decided := 0
	for k := len(tests) - 1; k >= 0; k-- {
		m := tests[k]
		if escaped(d.Text, m[0]) {
			continue
		}
		var name string
		negate := false
		if m[2] >= 0 {
			cmd := string(d.Text[m[2]:m[3]])
			name = string(d.Text[m[4]:m[5]])
			negate = cmd == "nottoggle" || cmd == "notbool"
		} else {
			name = string(d.Text[m[6]:m[7]])
		}
		name = trimSpace(name)
		if !declared[name] {
			continue
		}
		yes, ok := branch(d.Text, m[1])
		if !ok {
			continue
		}
		no, ok := branch(d.Text, yes[2])
		if !ok {
			continue
		}
		v := false
		for _, s := range sets[name] {
			if s.off < m[0] {
				v = s.value
			}
		}
		if negate {
			v = !v
		}
		keep, drop := yes, no
		if !v {
			keep, drop = no, yes
		}
		// The test and the braces of the kept branch go, and so does all of
		// the other branch. The branch that stays is the text that prints.
		blank(d.Text, m[0], yes[0])
		blank(d.Text, drop[0], drop[2])
		if keep[1] > keep[0] {
			blank(d.Text, keep[0], keep[0]+1)
			blank(d.Text, keep[2]-1, keep[2])
		}
		blank(d.Text, yes[2], no[0])
		decided++
	}
	return decided
}

// branch reads one argument after off: a {group}, or one token as TeX reads
// an argument with no braces. It gives the start, the first byte inside a
// group (the start when there is no group) and the end.
//
// A blank line before the argument is the token \par, and the argument is
// that \par. It gives an empty span, so the text after the blank line stays.
func branch(text []byte, off int) ([3]int, bool) {
	i := skipSpace(text, off)
	if parBetween(text[off:i]) {
		return [3]int{off, off, off}, true
	}
	if i >= len(text) {
		return [3]int{}, false
	}
	if text[i] == '{' {
		inner, end, ok := Group(text, i)
		if !ok {
			return [3]int{}, false
		}
		return [3]int{i, inner, end}, true
	}
	if text[i] == '}' {
		return [3]int{}, false
	}
	j := i + 1
	if text[i] == '\\' && j < len(text) {
		if isLetter(text[j]) {
			for j < len(text) && isLetter(text[j]) {
				j++
			}
		} else {
			j++
		}
	}
	return [3]int{i, i, j}, true
}

// parBetween tells if a run of spaces holds a blank line.
func parBetween(space []byte) bool {
	lines := 0
	for _, c := range space {
		if c == '\n' {
			lines++
			if lines == 2 {
				return true
			}
		}
	}
	return false
}

func trimSpace(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\n') {
		j--
	}
	return s[i:j]
}

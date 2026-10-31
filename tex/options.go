package tex

import (
	"path"
	"regexp"
	"slices"
	"strings"
)

// LocalFiles is the text of each local file of the document, in the order of
// the names, with the blocks of the options that the paper does not give
// blanked out.
func LocalFiles(d *Document) [][]byte {
	var out [][]byte
	for _, name := range sortedKeys(d.Local) {
		out = append(out, LocalText(d, name))
	}
	return out
}

var (
	// optionFlag is \DeclareOption{o}{\let\flag\relax} or \DeclareOption{o}{\flagtrue}.
	optionFlag     = regexp.MustCompile(`\\DeclareOption\s*\{([^{}]+)\}\s*\{\s*(?:\\let\s*\\([A-Za-z@]+)\s*=?\s*\\relax|\\([A-Za-z@]+)true)\s*\}`)
	executeOptions = regexp.MustCompile(`\\ExecuteOptions\s*\{([^{}]*)\}`)
	controlSeq     = regexp.MustCompile(`\\([A-Za-z@]+)`)
)

// OptionsOff blanks each block of a class or a style file that runs only with
// an option that the document does not give, as
// \ifx\numberwithinsect\relax \@addtoreset{theorem}{section} \fi of
// lipics-v2021.cls. A flag that the file sets outside \DeclareOption is left
// as it is. Seen in the gold pilot (set X).
func OptionsOff(text []byte, opts []string) []byte {
	given := map[string]bool{}
	for _, o := range opts {
		given[o] = true
	}
	for _, m := range executeOptions.FindAllSubmatch(text, -1) {
		for o := range strings.SplitSeq(string(m[1]), ",") {
			given[strings.TrimSpace(o)] = true
		}
	}
	// The flags that only an option sets, and whether the option is given.
	set, count := map[string]bool{}, map[string]int{}
	for _, m := range optionFlag.FindAllSubmatch(text, -1) {
		o := strings.TrimSpace(string(m[1]))
		if len(m[2]) > 0 {
			f := string(m[2])
			count["let "+f]++
			set["let "+f] = set["let "+f] || given[o]
		} else {
			f := string(m[3])
			count["true "+f]++
			set["true "+f] = set["true "+f] || given[o]
		}
	}
	if len(count) == 0 {
		return text
	}
	// A flag is off when each place that sets it is an option that is not
	// given, and on when an option that sets it is given. The \else block of
	// a flag that is on does not run, as \if@theoremSection
	// \newtheorem{theorem}{Theorem}[section]\else \newtheorem{theorem}{Theorem}\fi
	// of a style file with the option theorem-section. Seen in the gold pilot
	// (set X).
	off, on := map[string]bool{}, map[string]bool{}
	for k, n := range count {
		on[k] = set[k]
		kind, f, _ := strings.Cut(k, " ")
		pat := `\\let\s*\\` + regexp.QuoteMeta(f) + `\s*=?\s*\\relax`
		if kind == "true" {
			pat = `\\` + regexp.QuoteMeta(f) + `true\b`
		}
		if !set[k] && len(regexp.MustCompile(pat).FindAllIndex(text, -1)) == n {
			off[k] = true
		}
	}
	type frame struct {
		blank, on bool
		from, els int
	}
	var stack []frame
	var cuts [][2]int
	cs := controlSeq.FindAllSubmatchIndex(text, -1)
	for i := 0; i < len(cs); i++ {
		name := string(text[cs[i][2]:cs[i][3]])
		switch {
		case name == "let":
			i += 2
		case name == "newif" || name == "def" || name == "gdef" || name == "edef" || name == "xdef":
			i++
		case name == "ifx":
			relax := i+2 < len(cs) && string(text[cs[i+2][2]:cs[i+2][3]]) == "relax"
			flag := ""
			if i+1 < len(cs) {
				flag = "let " + string(text[cs[i+1][2]:cs[i+1][3]])
			}
			stack = append(stack, frame{blank: relax && off[flag], on: relax && on[flag], from: cs[i][0], els: -1})
		case strings.HasPrefix(name, "if"):
			flag := "true " + strings.TrimPrefix(name, "if")
			stack = append(stack, frame{blank: off[flag], on: on[flag], from: cs[i][0], els: -1})
		case name == "else" && len(stack) > 0:
			t := &stack[len(stack)-1]
			if t.blank {
				cuts = append(cuts, [2]int{t.from, cs[i][0]})
				t.blank = false
			}
			if t.on {
				t.els = cs[i][0]
			}
		case name == "fi" && len(stack) > 0:
			if t := stack[len(stack)-1]; t.blank {
				cuts = append(cuts, [2]int{t.from, cs[i][1]})
			} else if t.els >= 0 {
				cuts = append(cuts, [2]int{t.els, cs[i][1]})
			}
			stack = stack[:len(stack)-1]
		}
	}
	if len(cuts) == 0 {
		return text
	}
	out := slices.Clone(text)
	for _, c := range cuts {
		for j := c[0]; j < c[1]; j++ {
			if out[j] != '\n' {
				out[j] = ' '
			}
		}
	}
	return out
}

// LocalText is the text of the local file name, with the blocks of the
// options that the paper does not give blanked out.
func LocalText(d *Document, name string) []byte {
	if strings.EqualFold(path.Ext(name), ".cfg") {
		// A configuration file tests the flags of the options of the file
		// that loads it, as \if@revealBool of fabsmfart.cls in
		// ntheoremSMF.cfg, so the blocks are read with that file before it.
		// Seen in the gold pilot (set X).
		for _, by := range sortedKeys(d.Local) {
			if by != name && loadedBy(path.Base(name), map[string][]byte{by: d.Local[by]}) {
				text := append(append(slices.Clone(d.Local[by]), '\n'), d.Local[name]...)
				return OptionsOff(text, localOptions(d, by))[len(d.Local[by])+1:]
			}
		}
	}
	return OptionsOff(d.Local[name], localOptions(d, name))
}

// localOptions is the options that the local file name runs with.
func localOptions(d *Document, name string) []string {
	opts := d.Options
	if base := path.Base(name); strings.HasSuffix(base, ".sty") {
		opts = append(slices.Clone(opts), d.PackageOptions[strings.TrimSuffix(base, ".sty")]...)
	}
	return opts
}

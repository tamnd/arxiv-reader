package tex

// Args is the arguments of one command, read by ReadArgs.
type Args struct {
	// Star is true when the command has a *.
	Star bool
	// Values holds one entry for each letter of the spec except *. An
	// optional argument that is not there is the empty string, and Has
	// says which ones were there.
	Values []string
	Has    []bool
	// Offs is the offset of each value in the text, or -1.
	Offs []int
}

// Group reads the balanced group that starts at off, which must be { or [.
//
// It returns the offset of the first byte inside the group and the offset just
// after the closing brace or bracket. A [ group ends at the first ] that is not
// inside braces. Escaped braces do not count.
func Group(text []byte, off int) (inner, end int, ok bool) {
	if off >= len(text) {
		return 0, 0, false
	}
	open := text[off]
	var close byte
	switch open {
	case '{':
		close = '}'
	case '[':
		close = ']'
	default:
		return 0, 0, false
	}
	depth := 0
	for i := off + 1; i < len(text); i++ {
		c := text[i]
		if c == '\\' {
			i++
			continue
		}
		switch {
		case c == '{':
			depth++
		case c == '}' && depth > 0:
			depth--
		case c == close && depth == 0:
			return off + 1, i + 1, true
		case c == '}' && depth == 0:
			// A } with no {: the group is broken.
			return 0, 0, false
		}
	}
	return 0, 0, false
}

// ReadArgs reads the arguments after a command name.
//
// The spec has one letter for each argument, as in xparse: * for an optional
// star, o for an optional [argument], m for a mandatory {argument} or one
// token, and c for a command name, written \name or {\name}. Spaces between
// the arguments are skipped. ok is false when a mandatory argument is not
// there.
func ReadArgs(text []byte, off int, spec string) (a Args, end int, ok bool) {
	i := off
	for _, s := range spec {
		j := skipSpace(text, i)
		switch s {
		case '*':
			if j < len(text) && text[j] == '*' {
				a.Star = true
				i = j + 1
			}
		case 'o':
			if j < len(text) && text[j] == '[' {
				if in, e, gok := Group(text, j); gok {
					a.Values = append(a.Values, string(text[in:e-1]))
					a.Has = append(a.Has, true)
					a.Offs = append(a.Offs, in)
					i = e
					continue
				}
			}
			a.Values = append(a.Values, "")
			a.Has = append(a.Has, false)
			a.Offs = append(a.Offs, -1)
		case 'm', 'c':
			if j >= len(text) {
				return a, i, false
			}
			switch {
			case text[j] == '{':
				in, e, gok := Group(text, j)
				if !gok {
					return a, i, false
				}
				a.Values = append(a.Values, string(text[in:e-1]))
				a.Offs = append(a.Offs, in)
				i = e
			case text[j] == '\\':
				e := j + 1
				for e < len(text) && isLetter(text[e]) {
					e++
				}
				if e == j+1 && e < len(text) {
					e++
				}
				a.Values = append(a.Values, string(text[j:e]))
				a.Offs = append(a.Offs, j)
				i = e
			case s == 'm' && text[j] != '}' && text[j] != '[':
				a.Values = append(a.Values, string(text[j:j+1]))
				a.Offs = append(a.Offs, j)
				i = j + 1
			default:
				return a, i, false
			}
			a.Has = append(a.Has, true)
		}
	}
	return a, i, true
}

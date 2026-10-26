package tex

import (
	"regexp"
	"strings"
)

// Node is one section or one environment of the body.
type Node struct {
	// Kind is section or env.
	Kind string
	// Name is the environment name, or the sectioning command for a section.
	Name string
	Star bool
	// Level is the depth of a section: part is -1, chapter 0, section 1,
	// subsection 2, subsubsection 3, paragraph 4, subparagraph 5.
	Level int
	// Title is the title of a section.
	Title string
	// From is the offset of the command, Inner is the offset after its
	// arguments, Close is the offset of \end, and To is the offset after the
	// end. For a section, Close and To are where the next section of the same
	// level or higher starts.
	From, Inner, Close, To int
	// Opt is the optional argument of \begin{env}[opt], and OptAt its offset.
	Opt    string
	OptAt  int
	HasOpt bool
	// Appendix is true when the node is after \appendix.
	Appendix bool
	// Parent is the index of the enclosing node, or -1.
	Parent int
	// Closed is false for an environment with no \end.
	Closed bool
}

// Tree is the tree of the body, in the order of the text.
type Tree struct {
	Nodes []Node
	// Appendix is the offset of \appendix, or 0.
	Appendix int
}

var sectionLevels = map[string]int{
	"part": -1, "chapter": 0, "section": 1, "subsection": 2,
	"subsubsection": 3, "paragraph": 4, "subparagraph": 5,
}

var structure = regexp.MustCompile(`\\(begin|end)\s*\{([^{}]{1,64})\}|\\(part|chapter|section|subsection|subsubsection|paragraph|subparagraph)\b|\\appendix\b`)

// BuildTree reads the sections and the environments of text[from:to].
//
// An \end with no \begin is ignored. An \end that closes an environment below
// the top of the stack closes the ones above it too, because a paper that
// compiles has its environments balanced, and an unbalanced one here means a
// macro that the front end did not expand.
func BuildTree(text []byte, from, to int) *Tree {
	t := &Tree{}
	if to <= from {
		return t
	}
	var envs []int
	var secs []int
	appendix := false
	parent := func() int {
		p := -1
		if len(secs) > 0 {
			p = secs[len(secs)-1]
		}
		if len(envs) > 0 && envs[len(envs)-1] > p {
			p = envs[len(envs)-1]
		}
		return p
	}
	body := text[from:to]
	for _, m := range structure.FindAllSubmatchIndex(body, -1) {
		off := from + m[0]
		if escaped(text, off) {
			continue
		}
		switch {
		case m[2] >= 0 && string(body[m[2]:m[3]]) == "begin":
			name := strings.TrimSpace(string(body[m[4]:m[5]]))
			n := Node{Kind: "env", Name: name, From: off, Inner: from + m[1], Parent: parent(), Appendix: appendix}
			if name == "appendix" || name == "appendices" {
				appendix = true
				n.Appendix = true
				if t.Appendix == 0 {
					t.Appendix = off
				}
			}
			if j := skipSpace(text, n.Inner); j < to && text[j] == '[' {
				if in, e, ok := Group(text, j); ok && e-j < 2000 {
					n.Opt, n.OptAt, n.HasOpt, n.Inner = string(text[in:e-1]), in, true, e
				}
			}
			t.Nodes = append(t.Nodes, n)
			envs = append(envs, len(t.Nodes)-1)
		case m[2] >= 0:
			name := strings.TrimSpace(string(body[m[4]:m[5]]))
			for k := len(envs) - 1; k >= 0; k-- {
				if t.Nodes[envs[k]].Name != name {
					continue
				}
				for _, open := range envs[k:] {
					t.Nodes[open].Close, t.Nodes[open].To = off, from+m[1]
				}
				t.Nodes[envs[k]].Closed = true
				envs = envs[:k]
				break
			}
		case m[6] >= 0:
			cmd := string(body[m[6]:m[7]])
			a, end, ok := ReadArgs(text, from+m[1], "*om")
			if !ok {
				continue
			}
			level := sectionLevels[cmd]
			for len(secs) > 0 && t.Nodes[secs[len(secs)-1]].Level >= level {
				s := secs[len(secs)-1]
				t.Nodes[s].Close, t.Nodes[s].To = off, off
				secs = secs[:len(secs)-1]
			}
			n := Node{Kind: "section", Name: cmd, Star: a.Star, Level: level, Title: strings.TrimSpace(a.Values[1]), From: off, Inner: end, Parent: parent(), Appendix: appendix, Closed: true}
			t.Nodes = append(t.Nodes, n)
			secs = append(secs, len(t.Nodes)-1)
		default:
			appendix = true
			if t.Appendix == 0 {
				t.Appendix = off
			}
		}
	}
	for _, s := range secs {
		t.Nodes[s].Close, t.Nodes[s].To = to, to
	}
	for _, e := range envs {
		if !t.Nodes[e].Closed {
			t.Nodes[e].Close, t.Nodes[e].To = to, to
		}
	}
	return t
}

// Section is the innermost section that holds the offset, or -1.
func (t *Tree) Section(off int) int {
	best := -1
	for i, n := range t.Nodes {
		if n.Kind == "section" && n.From <= off && off < n.To {
			best = i
		}
	}
	return best
}

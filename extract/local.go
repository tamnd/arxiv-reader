package extract

import (
	"fmt"
	"strings"
)

// A local identifier is the name an object has inside its own paper.
//
// It is "thm-1" and not "0A3F". The tag is permanent and survives renumbering
// between versions, and it is deliberately meaningless; the local identifier is
// what a URL shows and what a person reads, so it is the author's own number
// with a prefix saying what kind of thing carries it. One paper, one namespace,
// and the mapping between the two lives in the tag register.
//
// Both are needed and neither does the other's job. A tag in a URL is
// unreadable, and a local identifier in a cross corpus reference breaks the day
// the author inserts a lemma.

// envClasses maps an environment name onto the object class it belongs to.
//
// Five of the sixteen object kinds are one thing in LaTeX. They are all
// theorem-shaped environments and LaTeXML marks them all ltx_theorem, and the
// split into statement, definition, remark, problem and exercise is a judgement
// this project makes rather than something the markup says. It is worth making:
// a reader browsing open problems does not want lemmas, and a dependency edge
// from a statement to a definition is not the same edge as a remark mentioning
// one.
//
// The author's own name for the environment is kept whatever this decides, in
// the env attribute, because a paper with a \newtheorem{observation} has an
// observation in it and calling it a remark on the page would be this project
// overwriting the author.
var envClasses = map[string]string{
	"theorem":     "statement",
	"thm":         "statement",
	"lemma":       "statement",
	"lem":         "statement",
	"proposition": "statement",
	"prop":        "statement",
	"corollary":   "statement",
	"cor":         "statement",
	"claim":       "statement",
	"fact":        "statement",
	"assumption":  "statement",
	"definition":  "definition",
	"defn":        "definition",
	"def":         "definition",
	"remark":      "remark",
	"rmk":         "remark",
	"example":     "remark",
	"observation": "remark",
	"note":        "remark",
	"notation":    "remark",
	"conjecture":  "problem",
	"problem":     "problem",
	"question":    "problem",
	"openproblem": "problem",
	"exercise":    "exercise",
}

// prefixes are the local identifier prefixes, one per class.
var prefixes = map[string]string{
	"statement":  "thm",
	"definition": "def",
	"remark":     "rem",
	"problem":    "prob",
	"exercise":   "exr",
	"proof":      "proof",
	"equation":   "eq",
	"figure":     "fig",
	"table":      "tab",
	"code":       "lst",
	"section":    "s",
}

// Kinds are the sixteen object kinds of 2166-06, each under the class it is
// written as.
//
// The list is here, next to the mapping that produces it, rather than in the
// audit that reads it. A list kept on the reading side drifts from the writing
// side the first time a kind is added, and the whole point of a fixed set of
// kinds is that every later group dispatches on it.
var Kinds = map[string]bool{
	"section":    true,
	"statement":  true,
	"definition": true,
	"remark":     true,
	"problem":    true,
	"exercise":   true,
	"proof":      true,
	"equation":   true,
	"figure":     true,
	"table":      true,
	"code":       true,
	"result":     true,
	"artefact":   true,
	"reference":  true,
	"note":       true,
	"front":      true,
}

// FileKinds are what the kind field of a content file may say.
//
// Four and not sixteen, because a file is the front matter or it is a section.
// Appendix and references are sections too and they are named separately
// because the splitter and the audit both have to know where the appendices
// start: a references section belongs last, and an appendix is the one thing
// allowed after it.
var FileKinds = map[string]bool{
	"front":      true,
	"section":    true,
	"appendix":   true,
	"references": true,
}

// classOf is the object class a block belongs to, and it is the class written
// into the attribute block.
func classOf(b Block) string {
	switch b.Kind {
	case KindTheorem:
		if c, ok := envClasses[strings.ToLower(b.Env)]; ok {
			return c
		}
		// An environment nobody has seen before is a statement, because that is
		// what most of them are and because the author's own name for it is
		// kept beside this anyway.
		return "statement"
	case KindProof:
		return "proof"
	case KindEquation:
		return "equation"
	case KindFigure:
		return "figure"
	case KindTable:
		return "table"
	case KindListing, KindAlgorithm:
		return "code"
	}
	return ""
}

// names hands out local identifiers, in document order, for one paper.
//
// A counter and not a pure function, because two figures the author did not
// number have to end up with different identifiers and nothing in either of
// them says which is which. Walking the paper in reading order is what makes
// the result the same on every run, which is what makes a re-extraction of an
// unchanged paper write nothing.
type names struct {
	seen map[string]int
	// anchors maps LaTeXML's own id onto the local identifier that replaced it,
	// so "S3.F2" becomes "fig-2". Every cross reference in the rendering points
	// at the left hand side and every anchor written out is the right hand
	// side, so the links have to be rewritten through this before the files are
	// finished.
	anchors map[string]string
}

func newNames() *names {
	return &names{seen: map[string]int{}, anchors: map[string]string{}}
}

// anchor records that a rendering id is now a local identifier.
func (n *names) anchor(from, to string) {
	if from == "" || to == "" {
		return
	}
	if _, ok := n.anchors[from]; ok {
		return
	}
	n.anchors[from] = to
}

// block is the local identifier for a block, or the empty string for one that
// cannot be referenced.
//
// An equation the author did not number gets nothing. That is the rule from the
// spec and it is the right one: the corpus does not invent a locator with no
// counterpart in the PDF a reader might have open beside it. Everything else
// gets an identifier even when it is unnumbered, because a figure still has a
// file and a theorem still has a tag, and an object with no name is an object
// nothing can point at.
func (n *names) block(b Block) string {
	class := classOf(b)
	if class == "" {
		return ""
	}
	prefix := prefixes[class]
	if b.Tag != "" {
		id := n.unique(prefix + "-" + slugNumber(b.Tag))
		n.anchor(b.ID, id)
		return id
	}
	if b.Kind == KindEquation {
		return ""
	}
	// The u marks a number this project made up rather than one the author
	// printed, so a reader who finds fig-u3 in a URL knows not to look for
	// Figure 3 in the paper.
	n.seen[prefix+"-u"]++
	id := n.unique(fmt.Sprintf("%s-u%d", prefix, n.seen[prefix+"-u"]))
	n.anchor(b.ID, id)
	return id
}

// panel is the identifier of a sub float, which hangs off its parent's.
//
// A subfigure is cited as Figure 2(a) and not as a figure of its own, so its
// identifier is its parent's with the panel letter on the end. A panel the
// renderer gave no letter gets one by position, which is what the author's own
// \subfigure would have printed.
//
// A panel of a different kind from the float around it is not a panel at all.
// Authors put two numbered algorithms inside one figure environment to get them
// side by side, and those are Algorithm 1 and Algorithm 2 in every reference to
// them in the paper, so they keep their own numbers rather than becoming
// fig-u1a and fig-u1b.
func (n *names) panel(parent within, b Block, i int) string {
	if parent.id == "" || classOf(b) != parent.class {
		return n.block(b)
	}
	if letter := panelLetter(b.Tag); letter != "" {
		id := n.unique(parent.id + letter)
		n.anchor(b.ID, id)
		return id
	}
	id := n.unique(parent.id + string(rune('a'+i%26)))
	n.anchor(b.ID, id)
	return id
}

// within is the float a block is inside, or the zero value at the top level.
type within struct {
	id    string
	class string
}

// panelLetter is the (a) a subfigure prints, and nothing for anything else.
//
// A panel whose printed tag is a number is a numbered float that happens to sit
// inside another one, and gluing that number onto the parent's identifier is
// how figure 2 and algorithm 1 collide into fig-21.
func panelLetter(tag string) string {
	s := slugNumber(tag)
	if len(s) == 1 && s[0] >= 'a' && s[0] <= 'z' {
		return s
	}
	return ""
}

// section is the identifier of a heading.
//
// The author's printed number with the dots turned into hyphens, so 4.1 is
// s4-1 and appendix B.2 is sb-2. An unnumbered heading falls back to its
// position in the tree, which is the same shape and is marked with the u.
func (n *names) section(s Section, path []int) string {
	id := n.sectionID(s.Tag, path)
	n.anchor(s.ID, id)
	return id
}

func (n *names) sectionID(tag string, path []int) string {
	if tag != "" {
		return n.unique("s" + slugNumber(tag))
	}
	parts := make([]string, 0, len(path))
	for _, i := range path {
		parts = append(parts, fmt.Sprint(i))
	}
	return n.unique("su" + strings.Join(parts, "-"))
}

// unique keeps two objects from claiming one identifier.
//
// It happens. An author who numbers a theorem and a lemma both as 1 in the same
// paper has given this package two blocks that want thm-1, and silently letting
// the second overwrite the first is how a cross reference ends up pointing at
// the wrong statement.
func (n *names) unique(id string) string {
	n.seen[id]++
	if n.seen[id] == 1 {
		return id
	}
	return fmt.Sprintf("%s-%d", id, n.seen[id])
}

// slugNumber makes a printed number safe to put in a URL fragment.
func slugNumber(tag string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(tag) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-', r == ' ', r == '_':
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

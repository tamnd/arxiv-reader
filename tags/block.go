package tags

import (
	"regexp"
	"strings"
)

// An object carries its attribute block on the line it starts, which is the
// notation Bourbaki and papers both use and the one the extractor already
// writes.
//
//	**Theorem 1** {#thm-1 .statement env=theorem}
//
// This package's whole job in the content files is to put a tag= into that
// block and to leave every other character of the file alone. It is a rewrite of
// committed prose, some of which has been corrected by hand, so it is done by
// pattern on the block itself rather than by parsing the Markdown and printing
// it again. A round trip through a Markdown printer would reflow a paragraph
// somebody fixed and bury the tag in a diff nobody can read.

// block matches one attribute block at the end of a line.
//
// The four groups are the character in front of it, the anchor, the classes and
// the key value pairs. The character in front is captured so that a \{ in
// mathematics is not mistaken for the start of a block, which is the one false
// positive this shape can have and the one that would corrupt an equation.
var block = regexp.MustCompile(`(?m)(^|[^\\\n])\{#([A-Za-z0-9][A-Za-z0-9._-]*)((?:\s+\.[A-Za-z][A-Za-z0-9-]*)*)((?:\s+[A-Za-z_][A-Za-z0-9_-]*=(?:"(?:[^"\\]|\\.)*"|[^\s"}]*))*)\}[ \t]*$`)

// tagPair matches the tag= this package writes, so a second run replaces it
// rather than writing a second one.
var tagPair = regexp.MustCompile(`\s+tag=("(?:[^"\\]|\\.)*"|[^\s"}]*)`)

// Object is one taggable thing found in the content plane.
//
// File and Local together are enough to find it again, and Class is what says
// whether it is taggable at all. Label and Text are what the matching passes
// need when a paper has been extracted again from a new version and Local no
// longer says the same thing: the author's own name for the object, which
// survives a renumbering, and the object's own prose, which survives a move.
type Object struct {
	File  string
	Local string
	Class string
	Label string
	Text  string
}

// Objects reads every taggable attribute block out of one file's body, in
// reading order.
//
// Reading order is the order of the file, which is the order of the paper. It
// matters here only because it is the order the register is written in, and a
// register whose order is stable is a register a re-run does not rewrite.
func Objects(file, body string) []Object {
	var out []Object
	for _, o := range Blocks(file, body) {
		if !Taggable(o.Class) {
			continue
		}
		out = append(out, o)
	}
	return out
}

// Blocks reads every attribute block, taggable or not.
//
// Objects is what the assignment walks, because a footnote does not get a tag.
// This is what a reader of the object model walks, because a footnote is still
// an object and its kind is still one of the sixteen.
func Blocks(file, body string) []Object {
	ss := Spans(body)
	out := make([]Object, 0, len(ss))
	for i, s := range ss {
		// An object's text is everything from the end of its own attribute block
		// to the line the next one starts on. That is not a parse of the
		// Markdown and it does not need to be: what pass three and pass four
		// want is a stretch of the file that belongs to this object and to no
		// other, in the same order every time, and the span between two anchors
		// is exactly that.
		end := len(body)
		if i+1 < len(ss) {
			end = LineStart(body, ss[i+1].Start)
		}
		if end < s.End {
			end = s.End // Two blocks on one line, so this one has no text.
		}
		out = append(out, Object{
			File:  file,
			Local: s.Local,
			Class: s.Class,
			Label: s.Attrs["label"],
			Text:  strings.TrimSpace(body[s.End:end]),
		})
	}
	return out
}

// Span is one attribute block and where it sits in the body.
//
// Blocks is the reading most of this package wants and it throws away two things
// the object model needs: where the block is, and every attribute except the
// label. An object record has to say what the paper printed in front of the
// block, which is the line the block is on, and it has to carry the env and the
// tag. Both readings run off this one so that there is one idea of what an
// attribute block is in the corpus rather than two regular expressions drifting
// apart in two packages.
type Span struct {
	Local string
	Class string
	// Attrs is every key value pair in the block, so label, env and tag.
	Attrs map[string]string
	// Start and End bracket the block itself, not the object. The text in front
	// of Start on the same line is what the paper printed as the object's
	// heading, and what follows End belongs to the object until the next block.
	Start int
	End   int
}

// Spans reads every attribute block in a body, in reading order.
func Spans(body string) []Span {
	ms := block.FindAllStringSubmatchIndex(body, -1)
	out := make([]Span, 0, len(ms))
	for _, m := range ms {
		out = append(out, Span{
			Local: body[m[4]:m[5]],
			Class: firstClass(body[m[6]:m[7]]),
			Attrs: values(body[m[8]:m[9]]),
			// m[0] is the character in front of the block, which the pattern
			// captures so that a brace escaped in mathematics is not read as the
			// start of one. The block itself starts after it.
			Start: m[3],
			End:   m[1],
		})
	}
	return out
}

// Lead is the text before the first attribute block.
//
// A top level section is a whole file and its heading is in the front matter, so
// it has no attribute block of its own and none of the file's spans belong to
// it. This is what does: the prose between the front matter and the first thing
// inside the section.
func Lead(body string) string {
	if m := block.FindStringIndex(body); m != nil {
		return strings.TrimSpace(body[:LineStart(body, m[0])])
	}
	return strings.TrimSpace(body)
}

// LineStart is the index of the beginning of the line index i is on.
func LineStart(s string, i int) int {
	if j := strings.LastIndexByte(s[:i], '\n'); j >= 0 {
		return j + 1
	}
	return 0
}

// pairs matches the key value pairs of an attribute block.
var pairs = regexp.MustCompile(`\s+([A-Za-z_][A-Za-z0-9_-]*)=("(?:[^"\\]|\\.)*"|[^\s"}]*)`)

// values is every pair in a block.
//
// A repeated key keeps the first one, and a block with a key twice in it is
// malformed either way.
func values(block string) map[string]string {
	out := map[string]string{}
	for _, p := range pairs.FindAllStringSubmatch(block, -1) {
		if _, held := out[p[1]]; !held {
			out[p[1]] = strings.Trim(p[2], `"`)
		}
	}
	return out
}

// Taggable says whether an object of this class gets a tag.
//
// Fifteen of the sixteen object kinds do. The footnote is the one that does not:
// footnotes are not cited from outside the paper, they move constantly, and
// giving them tags would roughly double the register for no reader who benefits.
//
// A block with no class at all is taggable. It is an object the extractor gave
// an anchor to and did not classify, which is a gap in the mapping rather than a
// reason to leave it unnameable, and an anchor nothing can be written against is
// worse than a tag on something that turns out to be a footnote.
func Taggable(class string) bool { return class != "note" }

// firstClass is the object's class, which is the first one in the block.
//
// A block can carry more than one class and the first is the object kind, since
// that is the one the extractor writes and the rest are decoration.
func firstClass(classes string) string {
	for _, f := range strings.Fields(classes) {
		if strings.HasPrefix(f, ".") {
			return f[1:]
		}
	}
	return ""
}

// Retag writes each object's tag into its attribute block and returns the new
// body and how many blocks it touched.
//
// The tag goes immediately after the class and before every other pair, which is
// where 03-tags.md section 4 puts it. That position is not arbitrary: the anchor
// and the class say what the object is to this corpus and the tag says what it
// is to everybody else, so the three that make up the object's identity sit
// together and the pairs that describe it follow.
func Retag(body string, assigned map[string]Tag) (string, int) {
	n := 0
	out := block.ReplaceAllStringFunc(body, func(s string) string {
		m := block.FindStringSubmatch(s)
		tag, ok := assigned[m[2]]
		if !ok {
			return s
		}
		n++
		pairs := tagPair.ReplaceAllString(m[4], "")
		return m[1] + "{#" + m[2] + m[3] + " tag=" + string(tag) + pairs + "}"
	})
	return out, n
}

// Tagged reads back the tag on each object, which is what a verify pass compares
// against the register.
func Tagged(body string) map[string]Tag {
	out := map[string]Tag{}
	for _, m := range block.FindAllStringSubmatch(body, -1) {
		if p := tagPair.FindStringSubmatch(m[4]); p != nil {
			out[m[2]] = Tag(strings.Trim(p[1], `"`))
		}
	}
	return out
}

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
// whether it is taggable at all.
type Object struct {
	File  string
	Local string
	Class string
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
	var out []Object
	for _, m := range block.FindAllStringSubmatchIndex(body, -1) {
		out = append(out, Object{File: file, Local: body[m[4]:m[5]], Class: firstClass(body[m[6]:m[7]])})
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

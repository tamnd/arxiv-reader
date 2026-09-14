// Package tikz compiles a picture from the author's own description of it.
//
// 05-extract.md section 7 says a TikZ figure compiled locally is the best output
// this pipeline produces, and it is worth saying why. It is vector, it scales, it
// has real text in it that a screen reader can read and a translator could in
// principle translate, and it came from the author's description of the drawing
// rather than from a photograph of it. Every other path to a figure starts from
// something arXiv already rasterised.
//
// This package finds those pictures in a submission and builds a document that
// draws one of them on its own. Compiling it needs a TeX installation, which is
// the one dependency this project cannot vendor, so a machine without one says
// how to get one and the picture falls back to the rendering's version.
package tikz

import (
	"regexp"
	"strings"
)

// Picture is one tikzpicture in a submission.
type Picture struct {
	// Body is the environment, \begin and \end and all, exactly as the author
	// wrote it. Nothing in it is rewritten, because a drawing is a program and
	// the whole point of this path is to run the author's own.
	Body string
	// Caption and Label come from the figure the picture is in, and are empty
	// for a picture drawn in the middle of the prose.
	Caption string
	Label   string
	// Line is where the picture starts, one based, so a message about it names
	// somewhere in the author's file.
	Line int
}

var (
	opens  = regexp.MustCompile(`\\begin\s*\{tikzpicture\}`)
	closes = regexp.MustCompile(`\\end\s*\{tikzpicture\}`)
	// loads is any of the packages that mean a submission draws its own
	// pictures. pgfplots and pgfpicture are the two that are TikZ without
	// saying so.
	loads = regexp.MustCompile(`\\usepackage(\[[^\]]*\])?\s*\{[^}]*\b(tikz|pgfplots|pgf)\b[^}]*\}`)
)

// Uses says whether a submission draws its own pictures at all.
//
// Asked before anything is unpacked or compiled, because the answer is no for
// most of arXiv and a run that tried every paper would spend a TeX process per
// paper finding that out.
func Uses(tex string) bool {
	return loads.MatchString(tex) || opens.MatchString(tex)
}

// Pictures is every tikzpicture in a file, in the order they are drawn.
//
// A picture inside a comment is not a picture. Authors comment out the version
// of a drawing they did not use and leave it in the file, and compiling one
// would put a figure in the corpus that is not in the paper.
func Pictures(tex string) []Picture {
	var out []Picture
	for at := 0; at < len(tex); {
		m := opens.FindStringIndex(tex[at:])
		if m == nil {
			break
		}
		start, after := at+m[0], at+m[1]
		if commented(tex, start) {
			at = after
			continue
		}
		e := closes.FindStringIndex(tex[after:])
		if e == nil {
			// An environment that is opened and never closed is a file this
			// cannot read, and taking the rest of it as one picture would
			// compile the whole paper.
			break
		}
		end := after + e[1]
		p := Picture{Body: tex[start:end], Line: 1 + strings.Count(tex[:start], "\n")}
		p.Caption, p.Label = around(tex, start, end)
		out = append(out, p)
		at = end
	}
	return out
}

// commented says the position is on a line that has already been commented out.
//
// A per cent sign starts a comment unless it is escaped, and \% is how an author
// writes one in prose, so the backslashes in front of it are counted: an odd
// number means the per cent is a character and an even number means it is a
// comment.
func commented(tex string, at int) bool {
	line := strings.LastIndexByte(tex[:at], '\n') + 1
	for i := line; i < at; i++ {
		if tex[i] != '%' {
			continue
		}
		n := 0
		for j := i - 1; j >= line && tex[j] == '\\'; j-- {
			n++
		}
		if n%2 == 0 {
			return true
		}
	}
	return false
}

var (
	figureOpen  = regexp.MustCompile(`\\begin\s*\{figure\*?\}`)
	figureClose = regexp.MustCompile(`\\end\s*\{figure\*?\}`)
)

// around is the caption and the label of the figure a picture is in.
//
// The nearest \begin{figure} before it with no \end{figure} in between, which is
// the enclosing one. A paper that draws a picture outside a figure has given it
// no caption and no number, and that is a fact about the paper rather than
// something to go looking for further up the file.
func around(tex string, start, end int) (caption, label string) {
	open := figureOpen.FindAllStringIndex(tex[:start], -1)
	if len(open) == 0 {
		return "", ""
	}
	from := open[len(open)-1][1]
	if figureClose.MatchString(tex[from:start]) {
		return "", ""
	}
	to := len(tex)
	if m := figureClose.FindStringIndex(tex[end:]); m != nil {
		to = end + m[0]
	}
	in := tex[from:to]
	return argument(in, `\caption`), argument(in, `\label`)
}

// argument is the braced argument of the first occurrence of a command, with the
// braces inside it counted rather than the first close bracket taken.
//
// A caption with \emph{something} in it has a brace in the middle, and reading
// to the first one would cut the caption in half.
func argument(tex, cmd string) string {
	at := strings.Index(tex, cmd)
	if at < 0 {
		return ""
	}
	rest := tex[at+len(cmd):]
	// \caption[short]{long} is the two argument form, and what the paper prints
	// in the figure is the long one.
	if i := strings.IndexByte(rest, '['); i >= 0 && strings.TrimSpace(rest[:i]) == "" {
		j := strings.IndexByte(rest, ']')
		if j < 0 {
			return ""
		}
		rest = rest[j+1:]
	}
	open := strings.IndexByte(rest, '{')
	if open < 0 || strings.TrimSpace(rest[:open]) != "" {
		return ""
	}
	depth := 0
	for i := open; i < len(rest); i++ {
		switch rest[i] {
		case '{':
			if i == 0 || rest[i-1] != '\\' {
				depth++
			}
		case '}':
			if i > 0 && rest[i-1] == '\\' {
				continue
			}
			depth--
			if depth == 0 {
				return strings.TrimSpace(rest[open+1 : i])
			}
		}
	}
	return ""
}

var (
	class = regexp.MustCompile(`\\documentclass\s*(\[[^\]]*\])?\s*\{[^}]*\}`)
	begin = regexp.MustCompile(`\\begin\s*\{document\}`)
)

// Preamble is everything the author loaded and defined before the document
// started, with the document class taken off the front.
//
// All of it and not the tikz lines out of it. A drawing uses the author's own
// macros, their colours, their lengths and their libraries, and a preamble cut
// down to what looks relevant is a preamble that compiles nine pictures out of
// ten and leaves the tenth undefined for a reason nobody can see. The class goes
// because the document this builds has its own, and it is the one that crops the
// page to the drawing.
func Preamble(tex string) string {
	if m := class.FindStringIndex(tex); m != nil {
		tex = tex[m[1]:]
	}
	if m := begin.FindStringIndex(tex); m != nil {
		tex = tex[:m[0]]
	}
	return strings.TrimSpace(tex)
}

// Standalone is a document that draws one picture and nothing else.
//
// The standalone class crops the page to what is on it, which is what makes the
// output a picture rather than a picture in the corner of an A4 page. The tikz
// option loads TikZ before the preamble runs, so an author's \usetikzlibrary
// line has something to add a library to.
func Standalone(preamble, body string) string {
	var b strings.Builder
	b.WriteString("\\documentclass[tikz,border=2pt]{standalone}\n")
	if p := strings.TrimSpace(preamble); p != "" {
		b.WriteString(p)
		b.WriteString("\n")
	}
	b.WriteString("\\begin{document}\n")
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n\\end{document}\n")
	return b.String()
}

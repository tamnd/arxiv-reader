package extract

import (
	"strings"

	"golang.org/x/net/html"
)

// inline renders everything under a node as one line of Markdown.
//
// One line, always. Every run of whitespace in the rendering collapses to a
// single space and the result is trimmed, so a paragraph is a paragraph and not
// however LaTeXML happened to wrap it. That is also the house rule for every
// piece of prose this project writes: a sentence does not have a newline in the
// middle of it, because a diff of a reflowed paragraph is unreadable and a
// translator handed half a sentence produces half a translation.
func inline(n *html.Node) string {
	var b strings.Builder
	writeInline(&b, n)
	return tidy(b.String())
}

func writeInline(b *strings.Builder, n *html.Node) {
	if n == nil {
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch {
		case c.Type == html.TextNode:
			b.WriteString(escape(c.Data))
		case c.Type != html.ElementNode:
			continue
		case c.Data == "math":
			b.WriteString(maths(c))
		case c.Data == "br":
			b.WriteByte(' ')
		// The tag is the printed number of the thing the caller is already
		// recording as a number, so writing it here would give every list item
		// two bullets and every heading its number twice.
		case hasClass(c, "ltx_tag"):
			continue
		// A footnote's content is not part of the sentence it hangs off. The
		// mark stays so the sentence still reads, and the content is collected
		// by the caller.
		case hasClass(c, "ltx_note"):
			b.WriteString(escape(text(firstClass(c, "ltx_note_mark"))))
		case c.Data == "a":
			writeLink(b, c)
		case hasClass(c, "ltx_font_bold"):
			wrap(b, c, "**")
		case hasClass(c, "ltx_font_italic"), c.Data == "em", c.Data == "i":
			wrap(b, c, "*")
		case hasClass(c, "ltx_font_typewriter"), hasClass(c, "ltx_font_monospace"), c.Data == "code":
			wrap(b, c, "`")
		default:
			writeInline(b, c)
		}
	}
}

func writeLink(b *strings.Builder, n *html.Node) {
	href := attr(n, "href")
	inner := inline(n)
	if inner == "" {
		inner = escape(text(n))
	}
	if href == "" || inner == "" {
		b.WriteString(inner)
		return
	}
	b.WriteByte('[')
	b.WriteString(inner)
	b.WriteString("](")
	b.WriteString(href)
	b.WriteByte(')')
}

func wrap(b *strings.Builder, n *html.Node, with string) {
	inner := inline(n)
	if inner == "" {
		return
	}
	b.WriteString(with)
	b.WriteString(inner)
	b.WriteString(with)
}

// maths renders one piece of mathematics as the author wrote it.
//
// The alttext attribute carries the LaTeX that went in and the MathML beside it
// is what LaTeXML made of it. The corpus keeps the LaTeX and throws the MathML
// away, because the LaTeX is what the author typed, it is what every emitter
// can re-render from, and it is what the translator protects character for
// character. The MathML is regenerated at build time by KaTeX.
func maths(n *html.Node) string {
	tex := strings.TrimSpace(attr(n, "alttext"))
	if tex == "" {
		// LaTeXML puts the same string in an annotation element when it could
		// not put it in the attribute, and a piece of mathematics with neither
		// is one this project cannot keep.
		tex = strings.TrimSpace(text(firstTag(n, "annotation")))
	}
	if tex == "" {
		return ""
	}
	tex = strings.Join(strings.Fields(tex), " ")
	if attr(n, "display") == "block" {
		return " \\[" + tex + "\\] "
	}
	return "$" + tex + "$"
}

// escaped is what has to be written with a backslash in front of it so that
// Markdown reads it as the character the author typed.
//
// The backslash goes first because escaping it after the others would escape
// the backslashes the others just added. Mathematics is not escaped, because it
// is emitted inside dollar signs and a reader that treats it as Markdown has
// already gone wrong.
var escaped = strings.NewReplacer(
	`\`, `\\`,
	`*`, `\*`,
	`_`, `\_`,
	"`", "\\`",
	`[`, `\[`,
	`]`, `\]`,
	`<`, `\<`,
	`>`, `\>`,
)

func escape(s string) string { return escaped.Replace(s) }

// tidy collapses whitespace and the invisible characters LaTeXML leaves behind.
func tidy(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		// A non-breaking space is a space. Keeping it would make two words that
		// look identical compare unequal, which is a bug that takes a day to
		// find.
		case '\u00a0', '\u2007', '\u202f', '\u2009':
			return ' '
		// Zero width characters carry no meaning outside MathML and they break
		// every string comparison they touch.
		case '\u200b', '\u200c', '\u200d', '\u2060', '\ufeff':
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

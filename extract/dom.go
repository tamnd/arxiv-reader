package extract

import (
	"strings"

	"golang.org/x/net/html"
)

// The helpers in this file are the whole of this package's dependency on how
// HTML is shaped.
//
// They are here rather than taken from a query library because everything this
// package asks of a document is either a class or a tag, the walks are all in
// document order, and a selector engine would add a dependency and a string
// language in exchange for nothing. There are eight of them and none is longer
// than a screen.

func attr(n *html.Node, name string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

func classes(n *html.Node) []string {
	if n == nil {
		return nil
	}
	return strings.Fields(attr(n, "class"))
}

func hasClass(n *html.Node, want string) bool {
	for _, c := range classes(n) {
		if c == want {
			return true
		}
	}
	return false
}

// find is the first element under n the predicate accepts, in document order.
//
// It stops at the first match. That is the whole point of it, and it is written
// as its own recursion rather than on top of walk because walk's visitor says
// whether to descend and has no way to say stop.
func find(n *html.Node, ok func(*html.Node) bool) *html.Node {
	if n == nil {
		return nil
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		if ok(c) {
			return c
		}
		if got := find(c, ok); got != nil {
			return got
		}
	}
	return nil
}

// firstClass is the first element under n carrying a class, in document order.
//
// First, and this matters more than it looks. A section holds the titles of
// every heading under it, so a reader that took the last match would give
// section 3 the title of its last paragraph heading.
func firstClass(n *html.Node, want string) *html.Node {
	return find(n, func(c *html.Node) bool { return hasClass(c, want) })
}

// firstID is the first element under n with an id.
func firstID(n *html.Node, want string) *html.Node {
	return find(n, func(c *html.Node) bool { return attr(c, "id") == want })
}

func firstTag(n *html.Node, tag string) *html.Node {
	return find(n, func(c *html.Node) bool { return c.Data == tag })
}

// allClass is every element under n carrying a class, in document order, with
// anything inside a match left alone.
//
// Not descending into a match is what makes this usable for nesting. A table
// inside a table cell is the cell's business, and a caller asking for the rows
// of one table does not want the rows of the one inside it as well.
func allClass(n *html.Node, want string) []*html.Node {
	var out []*html.Node
	walk(n, func(c *html.Node) bool {
		if hasClass(c, want) {
			out = append(out, c)
			return false
		}
		return true
	})
	return out
}

// findUntil is the first element under n carrying a class, without looking
// inside anything stop accepts.
//
// The bound is what keeps a float reading its own caption. A figure holding two
// algorithm panels holds three captions, and the outer one owns none of them.
func findUntil(n *html.Node, want string, stop func(*html.Node) bool) *html.Node {
	if n == nil {
		return nil
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		if hasClass(c, want) {
			return c
		}
		if stop(c) {
			continue
		}
		if got := findUntil(c, want, stop); got != nil {
			return got
		}
	}
	return nil
}

// allUntil is every element under n carrying a class, in document order,
// without looking inside a match or inside anything stop accepts.
func allUntil(n *html.Node, want string, stop func(*html.Node) bool) []*html.Node {
	if n == nil {
		return nil
	}
	var out []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		if hasClass(c, want) {
			out = append(out, c)
			continue
		}
		if stop(c) {
			continue
		}
		out = append(out, allUntil(c, want, stop)...)
	}
	return out
}

func allTag(n *html.Node, tag string) []*html.Node {
	var out []*html.Node
	walk(n, func(c *html.Node) bool {
		if c.Data == tag {
			out = append(out, c)
			return false
		}
		return true
	})
	return out
}

func countClass(n *html.Node, want string) int {
	return len(allClass(n, want))
}

// walk visits every element under n in document order, and stops descending
// where the visitor says to.
func walk(n *html.Node, visit func(*html.Node) bool) {
	if n == nil {
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		if visit(c) {
			walk(c, visit)
		}
	}
}

// text is everything under a node with the markup taken off.
//
// For reading a number or a label out of an element, and never for reading
// prose. Prose goes through inline, which keeps the emphasis and turns the
// mathematics back into LaTeX.
func text(n *html.Node) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	var rec func(*html.Node)
	rec = func(c *html.Node) {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
		for k := c.FirstChild; k != nil; k = k.NextSibling {
			rec(k)
		}
	}
	rec(n)
	return b.String()
}

package extract

import (
	"regexp"
	"strings"
)

// A cross reference in the rendering points at LaTeXML's anchor.
//
// The rendering says [Figure 2](#S3.F2), because S3.F2 is the id LaTeXML gave
// the third section's second float. Nothing outside that one HTML file knows
// what S3.F2 is, and the corpus has just given the same object the name fig-2,
// so leaving the link alone would ship a corpus whose every internal link
// points at an identifier the corpus does not contain.
//
// The rewrite happens after the whole paper has been walked, because a forward
// reference in section 1 points at a figure in section 4 and the name for that
// figure does not exist until the walk reaches it. That is why Files builds
// every file in memory before any of them is finished.
//
// A reference this cannot place is left exactly as it was. Most of those are
// bibliography entries, which ax refs resolves into the metadata plane in its
// own pass, and the rest are anchors LaTeXML emitted for things this package
// does not model. Both are better left readable than rewritten into a guess.

// mdLink matches the target of a Markdown link to a fragment on this page.
var mdLink = regexp.MustCompile(`\]\(#([^)\s]+)\)`)

// link rewrites the rendering's anchors into local identifiers.
func link(body string, anchors map[string]string) string {
	if len(anchors) == 0 || !strings.Contains(body, "](#") {
		return body
	}
	return mdLink.ReplaceAllStringFunc(body, func(m string) string {
		from := m[3 : len(m)-1]
		to, ok := anchors[from]
		if !ok {
			// LaTeXML hangs a sub-anchor off the object's own id for the pieces
			// of a split reference, so S2.E3.1 is subequation 3a of equation 3.
			// Falling back to the longest known prefix lands the reader on the
			// equation rather than nowhere.
			if to, ok = prefixAnchor(from, anchors); !ok {
				return m
			}
		}
		return "](#" + to + ")"
	})
}

// prefixAnchor finds the object a sub-anchor hangs off.
//
// Only a numeric tail is dropped. LaTeXML names a subequation S2.E3.1 and the
// 1 is part of equation 3, so dropping it lands the reader on the equation.
// The E in S2.E2 is not part of anything: dropping it would turn a reference to
// equation 2 into a reference to section 2, which is a link that works, goes
// somewhere real and is wrong, and that is worse than a link this leaves alone.
func prefixAnchor(from string, anchors map[string]string) (string, bool) {
	for i := strings.LastIndex(from, "."); i > 0; i = strings.LastIndex(from[:i], ".") {
		if !numeric(from[i+1:]) {
			return "", false
		}
		if to, ok := anchors[from[:i]]; ok {
			return to, true
		}
	}
	return "", false
}

func numeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Anchors is the mapping from the rendering's ids to the corpus ids, which is
// what the reference resolver in ax refs starts from.
func (n *names) Anchors() map[string]string { return n.anchors }

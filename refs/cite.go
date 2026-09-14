package refs

import (
	"regexp"
	"strings"
)

// Cite is one place in a paper that cites one bibliography entry.
//
// One per occurrence and not one per work cited, which is what makes this a
// different file from the bibliography. A paper that leans on one result cites
// it in nine proofs, and that is one line in the refs manifest and nine lines
// here. The nine are the interesting ones: they say where in the paper the work
// was needed, and once the tags are in they will say which theorem needed it.
type Cite struct {
	// Entry is the bibliography anchor this citation points at, so the key into
	// the refs manifest of the same paper.
	Entry string `yaml:"entry"`
	// Section is the content file the citation sits in, which is the page the
	// reading app serves it on.
	Section string `yaml:"section"`
	// Kind and Number are the locator, when the citation carried one.
	Kind   string `yaml:"kind,omitempty"`
	Number string `yaml:"number,omitempty"`
	// Via is the pattern that read the locator.
	Via string `yaml:"via,omitempty"`
	// Text is the prose around the citation, and it is only kept when there is
	// a locator to check against it. A paper has a thousand citations and nine
	// of them carry a locator, so keeping the context for all thousand would be
	// a megabyte of prose that nobody reads to answer a question nobody asks.
	Text string `yaml:"text,omitempty"`
}

// Locator is the pair the graph stores.
func (c Cite) Locator() Locator {
	return Locator{Kind: c.Kind, Number: c.Number, Via: c.Via}
}

// citeLink matches a link into the paper's own anchors.
//
// A citation comes out of the renderer as a link to the bibliography anchor, so
// [Vaswani et al., 2017](#bib.bibx106) in an author-year style and [106](#bib.bib106)
// in a numeric one. Both are the same shape and the anchor is what matters. The
// anchor is not required to be a bibliography one here, because which anchors
// are is Bibliographic's answer and not a pattern's.
var citeLink = regexp.MustCompile(`\[([^\]]*)\]\(#([^)\s]*)\)`)

// Bibliographic says whether a link target is a bibliography anchor.
//
// LaTeXML names them bib.bibx106 and bib.bib106 depending on the style, so the
// prefix is what they have in common. It is a function rather than a prefix
// written in two places because audit rule T12 has to tell a link into the
// bibliography from a link into nothing, and a rule that disagreed with this
// about which is which would report every citation in the corpus.
func Bibliographic(anchor string) bool { return strings.HasPrefix(anchor, "bib") }

// window is how much prose on either side of a citation the patterns see.
//
// Wide enough for "as was proved in Theorem 3.4 of" and narrow enough that a
// before pattern cannot reach back into the previous sentence. Every pattern
// that reads the text before a citation is anchored at the end of it, so the
// only thing a wider window buys is a slower match.
const window = 120

// Scan reads every citation out of one section of a paper.
//
// The body is the section's Markdown with its front matter already taken off,
// because a front matter field is not prose and nothing in it cites anything.
func Scan(section, body string, l *Locators) []Cite {
	var out []Cite
	for _, m := range citeLink.FindAllStringSubmatchIndex(body, -1) {
		anchor := body[m[4]:m[5]]
		if !Bibliographic(anchor) {
			continue
		}
		before := tail(body[:m[0]], window)
		after := head(body[m[1]:], window)
		c := Cite{Entry: anchor, Section: section}
		if loc := l.Find(before, after); !loc.Empty() {
			c.Kind, c.Number, c.Via = loc.Kind, loc.Number, loc.Via
			c.Text = around(before, body[m[0]:m[1]], after)
		}
		out = append(out, c)
	}
	return out
}

// around is the prose a person checks a locator against.
//
// Centred on the citation and not clipped from the front of the window, because
// the locator is on one side of the citation or the other and a context that
// starts seventy words earlier and stops before reaching it shows neither. Both
// halves are short: the question this answers is whether the pattern read the
// right words, and that is settled by the few words either side.
func around(before, link, after string) string {
	const half = 60
	s := tail(before, half) + link + head(after, half)
	if len([]rune(before)) > half {
		s = "..." + s
	}
	if len([]rune(after)) > half {
		s += "..."
	}
	return strings.Join(strings.Fields(s), " ")
}

// tail and head are rune counted, because the window is a window on prose and
// half the prose in this corpus is not ASCII. Cutting a window mid character
// would put a replacement rune in front of a pattern.
func tail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

func head(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// Body is the section with its front matter taken off.
func Body(s string) string {
	if !strings.HasPrefix(s, "---\n") {
		return s
	}
	if i := strings.Index(s[4:], "\n---\n"); i >= 0 {
		return s[4+i+5:]
	}
	return s
}

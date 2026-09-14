package figures

import (
	"fmt"
	"regexp"
	"strings"
)

// Suspicion is what this knows about who owns a figure.
type Suspicion string

const (
	// Owned is a figure with nothing against it, which is published under the
	// article's own licence.
	Owned Suspicion = "owned"
	// Suspected is a figure whose caption says somebody else drew it.
	Suspected Suspicion = "suspected"
	// Confirmed is a figure whose bytes are already in the corpus under a
	// different licence.
	Confirmed Suspicion = "confirmed"
)

// Near is how close to a citation the word has to be, in characters.
//
// Sixty, which is about a line. The proximity is the whole rule and not a
// refinement of it: adapted, reproduced and reprinted are ordinary English and
// they turn up in captions constantly, in "adapted to the task" and "our
// adapted architecture" and "reproduced across five seeds". Next to a citation
// the same word is an author saying where the picture came from.
const Near = 60

var (
	// The four words the licence spec names, and no more of them. Every word
	// added here is a figure withheld from every paper that happens to use it,
	// so the list grows by evidence and not by imagination.
	credited = regexp.MustCompile(`(?i)\b(reproduced|adapted|reprinted|courtesy)\b`)
	// A citation in the content plane is a link into the bibliography. It is
	// bib while the rendering's own anchors are still in place and ref once
	// ax refs has resolved them, and both are matched so this does not change
	// its mind halfway through the pipeline.
	//
	// The third form is the author's own \cite, and it is here because of the
	// TikZ path. A drawing compiled from a submission carries the caption as the
	// author typed it, so nothing has turned the citations into links yet, and a
	// rule that only knew the link would have gone quiet on exactly the pictures
	// this project makes itself. \citep, \citet and the rest of the natbib family
	// are the same word with a letter on the end.
	citation = regexp.MustCompile(`\]\(#(?:bib|ref)[^)]*\)|\\(?:no)?cite[a-zA-Z]*\s*(?:\[[^\]]*\]\s*)*\{[^}]*\}`)
	// The copyright symbol, the word, and the (c) that a caption written in a
	// terminal uses instead of the symbol.
	claimed = regexp.MustCompile(`(?i)(\x{00a9}|\bcopyright\b|\(c\)\s*[12][0-9]{3})`)
)

// InspectCaption reads a caption for a claim that somebody else owns the figure.
//
// Conservative on purpose. A figure the corpus only suspects is treated exactly
// like one it has confirmed, because the cost of being wrong one way is a
// missing picture and the cost of being wrong the other way is republishing
// somebody's copyrighted work under a licence they never granted.
func InspectCaption(caption string) (Suspicion, string) {
	if m := claimed.FindString(caption); m != "" {
		return Suspected, fmt.Sprintf("the caption claims a copyright, at %q", strings.TrimSpace(m))
	}
	cites := citation.FindAllStringIndex(caption, -1)
	for _, w := range credited.FindAllStringIndex(caption, -1) {
		for _, c := range cites {
			if gap(w, c) <= Near {
				return Suspected, fmt.Sprintf("the caption says %q within %d characters of a citation", caption[w[0]:w[1]], Near)
			}
		}
	}
	return Owned, ""
}

// gap is the distance between two spans, and zero when they overlap.
func gap(a, b []int) int {
	switch {
	case a[1] <= b[0]:
		return b[0] - a[1]
	case b[1] <= a[0]:
		return a[0] - b[1]
	}
	return 0
}

// Owner is the paper a figure's bytes were first seen in.
type Owner struct {
	Paper   string
	Licence string
}

// Elsewhere reports a figure whose bytes are already in the corpus under a
// different licence.
//
// This is the only one of the signals that finds a figure lifted without a
// caption admitting it, and it is cheap because every committed figure is
// hashed anyway. Rule F05 asks the same question inside one paper and gets a
// different answer: across three million papers the same institutional logo and
// the same standard schematic appear in thousands of documents, so a byte match
// is only evidence of anything when the two papers disagree about the licence.
func Elsewhere(sum, paper, licence string, seen map[string]Owner) (Suspicion, string) {
	o, ok := seen[sum]
	if !ok || o.Paper == paper || o.Licence == licence {
		return Owned, ""
	}
	return Confirmed, fmt.Sprintf("the same bytes are in %s under %s and this paper is %s", o.Paper, o.Licence, licence)
}

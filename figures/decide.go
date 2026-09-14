package figures

import (
	"fmt"

	"github.com/tamnd/arxiv-reader/internal/prose"
)

// SizeCap is the largest a committed figure may be, from rule F03.
//
// Five hundred kilobytes. Carried over from papers unchanged, because the
// number was never about disk: a figure over it is a photograph or a raster of
// something that should have been vector, and both read worse than the smaller
// file that replaces them.
const SizeCap = 500 << 10

// MinSide is the smallest a committed figure may be on either side, from rule
// F02.
//
// A hundred pixels. Anything under it is a spacer, a rule, a bullet or a piece
// of a formula that LaTeXML could not set, and none of those is a figure.
const MinSide = 100

// Decision is whether a figure may be committed, and why.
//
// The rule is in here as well as the sentence, because the sentence is for a
// person reading a report and the rule is what the audit fails on, and a report
// that says one and an audit that fails the other is how a corpus ends up with
// two answers to the same question.
type Decision struct {
	// Commit is whether the bytes may go into the corpus. A figure that may not
	// is not a hole: the caption is published, the figure keeps its number and
	// its tag, the prose that references it still resolves, and the page says
	// the image is withheld and links to it on arXiv.
	Commit bool
	// Rule is the numbered rule that refused it, empty when nothing did.
	Rule string
	// Why is the sentence a report prints.
	Why string
	// Suspicion is what is known about who owns the figure.
	Suspicion Suspicion
	// PageFraction is what rule F06 measured, and zero when it had nothing to
	// measure.
	PageFraction float64
}

// Decide runs the rules that can be answered from one file and its caption.
//
// F05 and the confirmed half of F09 are not here because neither can be
// answered from one figure: both are comparisons against every other figure the
// corpus holds. Elsewhere answers the second of them once the caller has the
// index.
func Decide(caption string, im Image) Decision {
	d := Decision{Commit: true, Suspicion: Owned, PageFraction: im.PageFraction()}
	// The licence rule runs first. The others are about a picture being a bad
	// picture and this one is about the corpus not being entitled to it, and a
	// figure that is refused should be refused for the reason that matters.
	if s, why := InspectCaption(caption); s != Owned {
		return d.Withhold("F09", s, why)
	}
	if d.PageFraction > PageCap {
		return d.Withhold("F06", Owned, fmt.Sprintf("it covers %.0f per cent of a page at the %.2f by %.2f inches the file states, and the cap is %.0f per cent", d.PageFraction*100, im.WidthIn, im.HeightIn, PageCap*100))
	}
	if im.Width < MinSide || im.Height < MinSide {
		return d.Withhold("F02", Owned, fmt.Sprintf("it is %d by %d pixels, which is under the %d a figure has to be on both sides", im.Width, im.Height, MinSide))
	}
	if im.Bytes > SizeCap {
		return d.Withhold("F03", Owned, fmt.Sprintf("it is %s, which is over the %s cap", prose.Bytes(im.Bytes), prose.Bytes(SizeCap)))
	}
	return d
}

// Withhold turns a decision into a refusal.
//
// Exported because the two rules that cannot be answered from one file, F05 and
// the confirmed half of F09, are answered by the caller and have to record a
// refusal the same way the rules in here do. There are four fields that have to
// agree, and a caller that sets three of them leaves a manifest entry saying a
// figure was committed and also saying why it was not.
func (d Decision) Withhold(rule string, s Suspicion, why string) Decision {
	d.Commit, d.Rule, d.Suspicion, d.Why = false, rule, s, why
	return d
}

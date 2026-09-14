package figures

// PageWidthIn and PageHeightIn are US Letter, which is what arXiv's own
// renderings and most of its submissions are set on.
//
// A4 is 8.27 by 11.69 inches, which is three per cent more area, and the
// fraction is clamped at one, so an A4 page measures as a whole page here too
// and the choice between the two sizes changes nothing this rule decides.
const (
	PageWidthIn  = 8.5
	PageHeightIn = 11.0
)

// PageCap is the fraction of a page a committed figure may cover, from rule F06.
//
// Three quarters. The rule is what separates a corpus from a mirror of
// copyrighted PDFs: a picture that covers a whole page is a page, and a page of
// somebody else's paper is not a figure whatever the caption under it says.
const PageCap = 0.75

// PageFraction is how much of a printed page this figure covers.
//
// Zero when the file states no physical size, and that is the honest answer
// rather than a defect. A resolution cannot be guessed from a pixel count: at
// 150 dots per inch every high resolution figure measures as a page, and at 300
// every page scan measures as a figure, so the two wrong answers are wrong in
// opposite directions and neither is better than admitting the file said
// nothing. A figure this cannot measure is still held by rules F02, F03, F05
// and F09, and the manifest records that F06 had nothing to go on.
func (im Image) PageFraction() float64 {
	if im.WidthIn <= 0 || im.HeightIn <= 0 {
		return 0
	}
	f := (im.WidthIn * im.HeightIn) / (PageWidthIn * PageHeightIn)
	if f > 1 {
		return 1
	}
	return f
}

// Measured says rule F06 had something to measure.
func (im Image) Measured() bool { return im.Stated != "" && im.WidthIn > 0 && im.HeightIn > 0 }

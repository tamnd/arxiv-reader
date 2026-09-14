// Package extract turns a paper into tagged Markdown.
//
// There are four paths to that and this package walks the cheapest one. The
// render path reads arXiv's own LaTeXML rendering, which costs one HTTP GET and
// no model call at all, and which carries the author's original LaTeX in the
// alttext attribute of every piece of mathematics. That attribute is the single
// fact that makes this project affordable: the MathML beside it is what a
// browser draws, and the alttext is what the author typed.
//
// What this package produces is a model and not a file. Reading and writing are
// separate because the reject rule sits between them: a rendering LaTeXML could
// not finish is thrown away and the paper falls through to the source path, and
// a reader that had already written half a paper out would have to undo it.
package extract

import (
	"fmt"
	"strings"
)

// Kind is what a block of a paper is.
//
// These are blocks and not yet objects. The sixteen object kinds in the spec
// are a reading of a paper's argument, and they arrive with the object model.
// This is a reading of its typography, which is all a rendering can give.
type Kind string

const (
	// KindParagraph is running prose, which is most of a paper.
	KindParagraph Kind = "paragraph"
	// KindEquation is a numbered display equation, or a group of them where
	// the author wrote an align.
	KindEquation Kind = "equation"
	// KindFigure is a float with a caption and at least one image.
	KindFigure Kind = "figure"
	// KindTable is a float with a caption and a tabular.
	KindTable Kind = "table"
	// KindTheorem is anything LaTeXML marked as a theorem environment, which
	// is a theorem, a lemma, a definition, a remark or a proposition. Which of
	// those it is, is in Env.
	KindTheorem Kind = "theorem"
	// KindProof is a proof, which follows the statement it proves.
	KindProof Kind = "proof"
	// KindListing is a code listing.
	KindListing Kind = "listing"
	// KindList is an itemize or an enumerate.
	KindList Kind = "list"
	// KindAlgorithm is an algorithm float, which LaTeXML marks as a float
	// rather than as a figure because it is neither a picture nor a table.
	KindAlgorithm Kind = "algorithm"
	// KindQuote is a quotation or a display block of prose.
	KindQuote Kind = "quote"
)

// Image is one graphic a figure names.
type Image struct {
	// Src is the path the rendering gave, relative to the rendering's own URL.
	Src string
	// Alt is the alt text, which for a LaTeXML rendering is usually the file
	// name and is occasionally the author's description.
	Alt string
}

// Cell is one table cell.
type Cell struct {
	Text string
	// Header is true for a cell LaTeXML marked as a row or column heading.
	Header bool
	// Align is left, center or right, empty when the rendering said nothing.
	Align string
	// Span is the number of columns the cell covers, and it is 1 for almost
	// all of them.
	Span int
	// Down is the number of rows the cell covers, and it is 1 for almost all of
	// them. Markdown cannot express it at all, which is the whole reason a
	// table is kept a second time as markup.
	Down int
}

// Row is one table row.
//
// Above and Below are the horizontal rules. They are read because a booktabs
// table says what its header is by drawing a line under it and nothing else,
// so a representation that drops the rules loses the structure rather than the
// decoration.
type Row struct {
	Cells []Cell
	Above bool
	Below bool
}

// Block is one thing in a section, in reading order.
//
// One struct for every kind rather than an interface per kind. The blocks are
// written to disk, read back and audited, and a tagged union that has to be
// type switched at every one of those boundaries buys nothing over a Kind field
// that can be compared, counted and printed.
type Block struct {
	Kind Kind
	// ID is LaTeXML's own anchor, "S3.E4" or "S2.F1", and it is empty when the
	// element carried none. It is what a cross reference points at.
	ID string
	// Tag is the printed number, so "4" for equation 4 and "1" for figure 1.
	// Empty for an unnumbered block.
	Tag string
	// Label is the name the author gave this in \label, so "thm:main", and it
	// is empty when the author named nothing or when the paper came off the
	// render path. It is the one name in a paper that a revision does not
	// change, which is why ax tags diff asks for it first.
	Label string
	// Env is the environment name for a theorem, so theorem, lemma,
	// definition, remark, proposition or corollary.
	Env string
	// Title is the run-in title of a theorem, which is what the author put in
	// the square brackets.
	Title string
	// Text is the block's content as Markdown, with mathematics in $ and \[.
	Text string
	// Caption is a float's caption, as Markdown.
	Caption string
	// Images are the graphics a figure names.
	Images []Image
	// Rows are a table's cells.
	Rows []Row
	// Items are a list's items, as Markdown.
	Items []string
	// Ordered is true for an enumerate and false for an itemize.
	Ordered bool
	// Verbatim is true for a listing that holds no mathematics, which is code
	// rather than pseudocode. The two are emitted differently: code goes in a
	// fence where a backslash is a backslash, and pseudocode keeps its dollar
	// signs live or an algorithm prints as the LaTeX somebody typed.
	Verbatim bool
	// Blocks are the blocks nested inside this one, which is what a figure
	// panel, a theorem body and a row of an align are.
	Blocks []Block
}

// Section is one heading and everything under it.
type Section struct {
	// ID is LaTeXML's anchor, "S3.SS1", which is also the fragment a reader
	// links to.
	ID string
	// Kind is section, subsection, subsubsection, paragraph or appendix.
	Kind string
	// Level is 1 for a section and 4 for a paragraph. An appendix is a
	// section, so it is level 1.
	Level int
	// Tag is the printed number, "3.1", empty for an unnumbered heading.
	Tag string
	// Label is the name the author gave this heading in \label, so "sec:intro",
	// empty when there was none or when the paper came off the render path.
	Label string
	// Title is the heading with its number taken off, because the number is
	// already in Tag and repeating it is how a corpus ends up with headings
	// like "3.1 3.1 Selection".
	Title string
	// Blocks is the content directly under this heading, with anything under a
	// nested heading left to that heading.
	Blocks []Block
	// Sections are the subsections in reading order.
	Sections []Section
}

// Author is a name and, where the rendering says one, an institution.
type Author struct {
	Name        string
	Affiliation string
}

// Fault is one thing LaTeXML could not read.
//
// Both the count and the place matter, which is why this is a type rather than
// an integer. An error inside a section body means a sentence of the paper is
// missing. An error in the bibliography means one entry printed badly, and the
// bibliography is parsed from its own markup anyway.
type Fault struct {
	// Where is the id of the section it was found under, or "bibliography",
	// or "frontmatter".
	Where string
	// Text is what the rendering put in place of what it could not read.
	Text string
}

// Paper is a rendering read into the shape the corpus stores.
type Paper struct {
	// ID and Version are the paper this was read for, as the caller named it,
	// and not as the rendering claims. Stamp is what the rendering claims, and
	// the two disagreeing is worth knowing about.
	ID      string
	Version int
	// Stamp is arXiv's watermark line, "arXiv:2312.00752v2 [cs.LG] 31 May
	// 2024", which names the version arXiv considers this rendering to be of.
	Stamp string
	// Licence is the label the rendering's own info box states, "CC BY 4.0".
	//
	// A third statement of the licence, after the bulk surfaces and the abs
	// page, and this one is attached to the rendering of one version. It is
	// read so that the gate's decision can be checked against the page the
	// extraction actually came from.
	Licence  string
	Title    string
	Authors  []Author
	Abstract []Block
	Sections []Section
	// Faults is every .ltx_ERROR in the rendering, with where it was found.
	Faults []Fault
	// Unparsed is how many pieces of mathematics LaTeXML rendered but could
	// not parse into MathML. The alttext is still there and still correct, so
	// this is a note and not a fault.
	Unparsed int
	// Bibliography is the reference list, in the order the paper prints it.
	Bibliography []Bibitem
}

// Bibitem is one entry of the reference list as the rendering carries it.
//
// Read into the model rather than parsed here, because what a bibliography
// block means depends on the style the author used and that is ax refs build's
// problem. This is the typography: the anchor everything in the paper points
// at, the label the paper prints, and the blocks in the order they are printed.
type Bibitem struct {
	// ID is LaTeXML's anchor, "bib.bibx106", which is what every citation in
	// the paper links to and is how an entry is found again.
	ID string
	// Label is the refnum the paper prints, so "Vaswani et al. (2017)" in an
	// author-year style and "[106]" in a numeric one.
	Label string
	// Blocks are the printed lines of the entry, as Markdown, in order. A
	// BibTeX style puts the authors in the first, the title in the second and
	// the venue in the rest, and an author who wrote the entry by hand puts
	// the whole thing in one.
	Blocks []string
}

// Text is the whole entry as one line, which is how it is published.
func (b Bibitem) Text() string { return strings.Join(b.Blocks, " ") }

// FaultLimit is how many conversion errors a rendering may carry outside a
// section body before it is rejected.
//
// Five. About a quarter of arXiv's conversions carry at least one error and
// most of those are a single unreadable bibliography entry, so rejecting on the
// first one would throw away most of the corpus to fix a typo in a reference.
const FaultLimit = 5

// Rejection is why a rendering was thrown away.
type Rejection struct {
	Why    string
	Faults []Fault
}

func (r Rejection) Error() string { return "extract: " + r.Why }

// Reject decides whether this rendering can be used.
//
// Two rules and they are deliberately blunt. Any error inside a section body is
// a rejection, because a section body is the paper and an error there means a
// sentence of it is gone. More than FaultLimit errors anywhere is a rejection,
// because a conversion that went wrong five times went wrong in a way nobody
// has looked at.
//
// A rejected paper is not a failed paper. It falls through to the source path,
// where LaTeXML runs here with this project's flags rather than arXiv's, and a
// good share of the errors do not happen twice.
func (p Paper) Reject() (Rejection, bool) {
	var inBody []Fault
	for _, f := range p.Faults {
		if f.Where != "bibliography" {
			inBody = append(inBody, f)
		}
	}
	if len(inBody) > 0 {
		return Rejection{
			Why:    fmt.Sprintf("the rendering has %s inside the body of the paper, so a piece of the paper is missing", plural(len(inBody), "conversion error")),
			Faults: inBody,
		}, true
	}
	if len(p.Faults) > FaultLimit {
		return Rejection{
			Why:    fmt.Sprintf("the rendering has %s, which is more than the %d a bibliography is forgiven", plural(len(p.Faults), "conversion error"), FaultLimit),
			Faults: p.Faults,
		}, true
	}
	return Rejection{}, false
}

// Counts is how many of each kind of block a paper holds, over every section
// at every depth.
func (p Paper) Counts() map[Kind]int {
	out := map[Kind]int{}
	var walk func([]Block)
	walk = func(bs []Block) {
		for _, b := range bs {
			out[b.Kind]++
			walk(b.Blocks)
		}
	}
	walk(p.Abstract)
	var sections func([]Section)
	sections = func(ss []Section) {
		for _, s := range ss {
			walk(s.Blocks)
			sections(s.Sections)
		}
	}
	sections(p.Sections)
	return out
}

// Headings is how many headings a paper has, at every depth.
func (p Paper) Headings() int {
	n := 0
	var walk func([]Section)
	walk = func(ss []Section) {
		for _, s := range ss {
			n++
			walk(s.Sections)
		}
	}
	walk(p.Sections)
	return n
}

// StampVersion is the version the rendering says it is of, or zero when the
// stamp says nothing this understands.
//
// Worth checking. arXiv serves /html/<id>v<n> and a request for a version it
// has no rendering of can land on a different one, and extracting v4 into a
// corpus that decided v1 was the cc-by version would be republishing something
// nobody was given the right to republish.
func (p Paper) StampVersion() int {
	i := strings.Index(p.Stamp, "v")
	for i >= 0 && i+1 < len(p.Stamp) {
		n, rest := 0, p.Stamp[i+1:]
		j := 0
		for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
			n = n*10 + int(rest[j]-'0')
			j++
		}
		// A version number is followed by a space or a bracket, never by a
		// letter, which is what keeps this off the v in a category name.
		if j > 0 && (j == len(rest) || rest[j] == ' ' || rest[j] == '[') {
			return n
		}
		next := strings.Index(p.Stamp[i+1:], "v")
		if next < 0 {
			return 0
		}
		i = i + 1 + next
	}
	return 0
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

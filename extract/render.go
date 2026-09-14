package extract

import (
	"bytes"
	"errors"
	"strings"

	"golang.org/x/net/html"
)

// Parse reads arXiv's rendering of one version into the model.
//
// The id and the version come from the caller and not from the page, because
// the caller is the one that decided which version this corpus is allowed to
// publish. What the page claims is kept separately in Stamp, and the two
// disagreeing is a finding rather than something to reconcile quietly.
func Parse(body []byte, id string, version int) (*Paper, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	article := firstClass(doc, "ltx_document")
	if article == nil {
		return nil, errors.New("extract: the page has no ltx_document, so either it is not a LaTeXML rendering or arXiv has changed the markup")
	}

	p := &Paper{
		ID:      id,
		Version: version,
		Stamp:   tidy(text(firstID(doc, "watermark-tr"))),
		Licence: licenceLabel(doc),
		Title:   inline(firstClass(article, "ltx_title_document")),
	}
	p.Authors = authors(article)
	if a := firstClass(article, "ltx_abstract"); a != nil {
		p.Abstract = blocks(a)
	}
	p.Sections = sections(article)
	p.Bibliography = bibliography(article)
	p.Faults = faults(article)
	p.Unparsed = countClass(article, "ltx_math_unparsed")
	return p, nil
}

// licenceLabel is what the rendering's own info box says the licence is.
//
// Worth reading even though the gate has already decided. This is arXiv stating
// a licence on the page for one version, which is the same kind of statement
// the abs page makes, so a rendering whose label disagrees with the licence the
// corpus resolved is a rendering of a different version or a resolution that
// has gone stale. Either way somebody should look.
func licenceLabel(doc *html.Node) string {
	n := firstID(doc, "license-tr")
	if n == nil {
		return ""
	}
	s := tidy(text(n))
	return strings.TrimSpace(strings.TrimPrefix(s, "License:"))
}

// authors reads the byline.
//
// Read, but not believed. The metadata plane has the author list arXiv holds
// for this version and that is the one the corpus publishes; this is the
// rendering of whatever the author typed in the preamble, which is a byline
// laid out for a page and not a list of people. A creator with no name at all
// is a block of affiliations the author attached to nobody, and it is dropped
// rather than turned into an author called nothing.
func authors(article *html.Node) []Author {
	var out []Author
	for _, c := range allClass(article, "ltx_creator") {
		name := tidy(text(firstClass(c, "ltx_personname")))
		// The thanks note is inside the name span, so the name comes back with
		// the footnote's whole text stuck on the end of it unless the note is
		// taken out first.
		if mark := firstClass(c, "ltx_note"); mark != nil {
			name = strings.TrimSpace(strings.TrimSuffix(name, tidy(text(mark))))
		}
		if name == "" {
			continue
		}
		a := Author{Name: name}
		if aff := firstClass(c, "ltx_role_affiliation"); aff != nil {
			a.Affiliation = affiliation(aff)
		}
		out = append(out, a)
	}
	return out
}

// affiliation is a contact line with the label LaTeXML prints in front of it
// taken off.
func affiliation(n *html.Node) string {
	s := tidy(text(n))
	if label := firstClass(n, "ltx_contact_name"); label != nil {
		s = strings.TrimSpace(strings.TrimPrefix(s, tidy(text(label))))
	}
	return s
}

// headings are the classes LaTeXML puts on a sectioning element, with the level
// the corpus gives each one.
//
// An appendix is a section. It is numbered differently and it sits after the
// bibliography, and neither of those is a reason to give it a depth of its own.
var headings = []struct {
	class string
	level int
}{
	{"ltx_section", 1},
	{"ltx_appendix", 1},
	{"ltx_subsection", 2},
	{"ltx_subsubsection", 3},
	{"ltx_paragraph", 4},
}

func headingOf(n *html.Node) (kind string, level int, ok bool) {
	for _, h := range headings {
		if hasClass(n, h.class) {
			return strings.TrimPrefix(h.class, "ltx_"), h.level, true
		}
	}
	return "", 0, false
}

func sections(n *html.Node) []Section {
	var out []Section
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		kind, level, ok := headingOf(c)
		if !ok {
			continue
		}
		title := firstClass(c, "ltx_title")
		s := Section{
			ID:       attr(c, "id"),
			Kind:     kind,
			Level:    level,
			Tag:      tagOf(firstClass(title, "ltx_tag")),
			Title:    inline(title),
			Blocks:   blocks(c),
			Sections: sections(c),
		}
		out = append(out, s)
	}
	return out
}

// blocks reads everything directly under a node, in reading order.
//
// The default case recurses rather than dropping what it does not know, because
// LaTeXML wraps content in layout divs that carry no meaning and change between
// versions of the converter. A reader that only descended into containers it
// recognised would silently lose a paragraph the first time arXiv upgraded
// LaTeXML, and losing a paragraph silently is the worst thing this package can
// do.
func blocks(n *html.Node) []Block {
	if n == nil {
		return nil
	}
	var out []Block
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		if _, _, isHeading := headingOf(c); isHeading {
			// Collected by sections, which walks the same children.
			continue
		}
		switch {
		case hasClass(c, "ltx_title"), hasClass(c, "ltx_tag"), hasClass(c, "ltx_TOC"),
			hasClass(c, "ltx_pagination"), hasClass(c, "ltx_bibliography"), hasClass(c, "ltx_authors"):
			continue
		case hasClass(c, "ltx_theorem"):
			out = append(out, theorem(c))
		case hasClass(c, "ltx_proof"):
			out = append(out, proof(c))
		case hasClass(c, "ltx_equationgroup"), hasClass(c, "ltx_equation"):
			out = append(out, equation(c))
		case hasClass(c, "ltx_table"):
			out = append(out, float(c, KindTable))
		case hasClass(c, "ltx_figure"):
			out = append(out, float(c, KindFigure))
		case hasClass(c, "ltx_float"):
			out = append(out, float(c, floatKind(c)))
		case hasClass(c, "ltx_listing"):
			out = append(out, listing(c))
		case hasClass(c, "ltx_itemize"), hasClass(c, "ltx_enumerate"), hasClass(c, "ltx_description"):
			out = append(out, list(c))
		case hasClass(c, "ltx_quote"):
			out = append(out, Block{Kind: KindQuote, ID: attr(c, "id"), Text: inline(c)})
		case hasClass(c, "ltx_tabular"):
			// A tabular with no float around it, which is what a table inside a
			// figure panel is.
			out = append(out, tabular(c, Block{Kind: KindTable, ID: attr(c, "id")}))
		case c.Data == "p" && hasClass(c, "ltx_p"):
			if t := inline(c); t != "" {
				out = append(out, Block{Kind: KindParagraph, ID: attr(c, "id"), Text: t})
			}
		default:
			out = append(out, blocks(c)...)
		}
	}
	return out
}

// theorem reads a theorem environment, whatever the author called it.
//
// LaTeXML suffixes the author's own environment name onto the class, so this
// takes whatever the suffix says rather than matching against a list. A paper
// with a \newtheorem{claim} gets a claim, because mapping it onto the nearest
// of six known names would be this package inventing a fact about the paper.
func theorem(n *html.Node) Block {
	b := Block{Kind: KindTheorem, ID: attr(n, "id")}
	for _, c := range classes(n) {
		if rest, ok := strings.CutPrefix(c, "ltx_theorem_"); ok && rest != "" {
			b.Env = rest
		}
	}
	if title := firstClass(n, "ltx_title"); title != nil {
		b.Tag = tagOf(firstClass(title, "ltx_tag"))
		b.Title = runin(title)
	}
	b.Blocks = blocks(n)
	return b
}

func proof(n *html.Node) Block {
	b := Block{Kind: KindProof, ID: attr(n, "id")}
	if title := firstClass(n, "ltx_title"); title != nil {
		b.Title = inline(title)
	}
	b.Blocks = blocks(n)
	return b
}

// runin is the author's own title for a theorem, which is what they put in the
// square brackets and nothing else.
//
// LaTeXML writes the heading as the number in one span and the full stop after
// it in another, both bold, so reading the whole heading gives back "Theorem 1"
// and a stray piece of punctuation that has already been accounted for. The tag
// span is skipped because the number is read separately, and what is left is
// trimmed, because a title of "." is not a title.
func runin(title *html.Node) string {
	var b strings.Builder
	for c := title.FirstChild; c != nil; c = c.NextSibling {
		if hasClass(c, "ltx_tag") {
			continue
		}
		writeInline(&b, c)
	}
	s := strings.TrimSpace(tidy(b.String()))
	if strings.Trim(s, "*.,:;()[] ") == "" {
		return ""
	}
	return s
}

// equation reads one display equation, or a group of them where the author
// wrote an align.
//
// A group keeps its rows as nested blocks rather than being flattened into one
// string, because each row of an align can carry its own number and a cross
// reference points at the row and not at the group.
func equation(n *html.Node) Block {
	b := Block{Kind: KindEquation, ID: attr(n, "id")}
	rows := allClass(n, "ltx_eqn_row")
	if len(rows) <= 1 {
		b.Tag = eqnTag(n)
		b.Text = eqnText(n)
		if len(rows) == 1 {
			if id := eqnID(rows[0]); id != "" {
				b.ID = id
			}
		}
		return b
	}
	// A group of aligned equations carries no number of its own. Each row has
	// one, and a cross reference to (1b) points at the row, so giving the group
	// the first row's number would make two blocks both claim to be (1a).
	for _, r := range rows {
		text := eqnText(r)
		if text == "" {
			continue
		}
		b.Blocks = append(b.Blocks, Block{Kind: KindEquation, ID: eqnID(r), Tag: eqnTag(r), Text: text})
	}
	return b
}

// eqnID is the identifier every cross reference to one equation points at.
//
// LaTeXML numbers a display by putting it in a tbody of its own and hanging the
// identifier there, so a single numbered equation comes out as a group whose
// table is S4.EGx18 and whose tbody is S4.E8. The paper links to S4.E8 and
// nothing at all links to S4.EGx18, so the identifier on the table is the one
// this must not take.
//
// Audit rule T12 found this, on KAN, where two links into equations of section
// four pointed at anchors the rewrite had never been told about. It is the
// shape most of that paper's numbered equations are in.
func eqnID(row *html.Node) string {
	if id := attr(row, "id"); id != "" {
		return id
	}
	for p := row.Parent; p != nil; p = p.Parent {
		if p.Data == "tbody" {
			return attr(p, "id")
		}
		if p.Data == "table" {
			return ""
		}
	}
	return ""
}

// eqnText is every piece of mathematics in an equation cell, joined.
//
// Joined with a space and not with an alignment marker. LaTeXML splits an align
// at the ampersand into separate cells, and putting the ampersand back would
// mean guessing at a column count this project has no use for. The corpus keeps
// the mathematics and lets the emitter set it.
func eqnText(n *html.Node) string {
	var parts []string
	for _, cell := range allClass(n, "ltx_eqn_cell") {
		if hasClass(cell, "ltx_eqn_eqno") {
			continue
		}
		for _, m := range allTag(cell, "math") {
			tex := strings.Join(strings.Fields(attr(m, "alttext")), " ")
			// LaTeXML puts a \displaystyle in front of every cell of an align,
			// because each cell is its own piece of inline mathematics on the
			// page and has to be told to typeset large. The author did not
			// write it, the cells are joined back into one display here, and
			// leaving it in gives "\displaystyle x \displaystyle= y" in the
			// middle of a $$ block that is already display mathematics.
			tex = strings.TrimSpace(strings.TrimPrefix(tex, `\displaystyle`))
			if tex != "" {
				parts = append(parts, tex)
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// eqnTag is the printed number of an equation or of one row of a group.
func eqnTag(n *html.Node) string {
	for _, cell := range allClass(n, "ltx_eqn_eqno") {
		if s := tagOf(cell); s != "" {
			return s
		}
	}
	return ""
}

func floatKind(n *html.Node) Kind {
	if hasClass(n, "ltx_float_algorithm") {
		return KindAlgorithm
	}
	return KindFigure
}

// isFloat says whether an element is a float, which is where a float stops
// reading.
func isFloat(n *html.Node) bool {
	return hasClass(n, "ltx_figure") || hasClass(n, "ltx_table") || hasClass(n, "ltx_float")
}

// float reads a figure, a table or an algorithm, which LaTeXML marks up the
// same way: a caption, and something under it.
//
// Panels are kept nested. A figure of six subfigures is one figure with six
// blocks under it, because the author numbered it once and a reader following a
// cross reference to it expects one thing. Everything the float reads for
// itself stops at the panels, so the outer figure of two algorithms does not
// come back carrying both their captions and both their images.
func float(n *html.Node, kind Kind) Block {
	b := Block{Kind: kind, ID: attr(n, "id")}
	if c := findUntil(n, "ltx_caption", isFloat); c != nil {
		b.Tag = tagOf(firstClass(c, "ltx_tag"))
		b.Caption = caption(c)
	}
	for _, img := range allUntil(n, "ltx_graphics", isFloat) {
		b.Images = append(b.Images, image(img))
	}
	// Any float can hold a tabular, and plenty of them do. Authors put results
	// tables inside a figure environment all the time, and the block keeps its
	// kind because the author numbered it as a figure and every cross reference
	// in the paper calls it one.
	if t := findUntil(n, "ltx_tabular", isFloat); t != nil {
		b = tabular(t, b)
	}
	b.Blocks = panels(n)
	return b
}

// image reads one graphic.
//
// LaTeXML writes an img for a raster and an object for an SVG, and the two put
// the file in a different attribute. Both are graphics as far as this corpus is
// concerned, and a reader that only knew about img would lose every vector
// figure in the paper.
func image(n *html.Node) Image {
	src := attr(n, "src")
	if src == "" {
		src = attr(n, "data")
	}
	return Image{Src: src, Alt: attr(n, "alt")}
}

// caption is the words of a caption, without the label in front of them.
//
// The label goes to Tag and the separator after it goes nowhere. In the
// renderings this was built against LaTeXML keeps the separator inside the tag
// element, where skipping the tag already takes care of it, and the trim is
// here because a caption that begins with a colon is wrong whichever side of
// the tag the colon was written on.
func caption(n *html.Node) string {
	return strings.TrimSpace(strings.TrimPrefix(inline(n), ":"))
}

// tagOf reads the printed number off a LaTeXML tag element.
//
// LaTeXML prints the whole label: "Figure 1:", "Theorem 3.1.", "(a)" for a
// panel, "(4)" for an equation. The corpus keeps the number alone. The word in
// front of it is already the block's kind, and the punctuation around it is the
// rendering's own house style, so keeping either would hand every emitter
// something to strip before it could print the label its own way.
func tagOf(n *html.Node) string {
	s := strings.TrimRight(tidy(text(n)), ":. ")
	if i := strings.LastIndex(s, " "); i >= 0 {
		s = s[i+1:]
	}
	return strings.Trim(s, "()")
}

// panels are the sub floats inside a float, and nothing else.
//
// Deliberately narrow. Running the general block reader over a figure would
// pick up the caption as a paragraph and every cell of its table twice.
func panels(n *html.Node) []Block {
	var out []Block
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		switch {
		case hasClass(c, "ltx_caption"), hasClass(c, "ltx_tag"):
			continue
		case hasClass(c, "ltx_table"):
			out = append(out, float(c, KindTable))
		case hasClass(c, "ltx_figure"), hasClass(c, "ltx_float"):
			out = append(out, float(c, floatKind(c)))
		case hasClass(c, "ltx_listing"):
			out = append(out, listing(c))
		default:
			out = append(out, panels(c)...)
		}
	}
	return out
}

// tabular reads the cells of a table.
//
// Both representations of a table are the corpus's, and this is the first of
// them. The second is the LaTeX, which is not in the rendering at all and comes
// from the source path.
func tabular(t *html.Node, b Block) Block {
	for _, tr := range allClass(t, "ltx_tr") {
		var row Row
		for c := tr.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode || !hasClass(c, "ltx_td") {
				continue
			}
			cell := Cell{Text: inline(c), Header: hasClass(c, "ltx_th"), Span: 1, Down: 1}
			switch {
			case hasClass(c, "ltx_align_right"):
				cell.Align = "right"
			case hasClass(c, "ltx_align_center"):
				cell.Align = "center"
			case hasClass(c, "ltx_align_left"):
				cell.Align = "left"
			}
			if n := number(attr(c, "colspan")); n > 0 {
				cell.Span = n
			}
			if n := number(attr(c, "rowspan")); n > 0 {
				cell.Down = n
			}
			// The rules are per cell in the rendering and per row in a tabular,
			// and a rule drawn under some of a row's columns is drawn under the
			// row. LaTeXML writes ltx_border_tt for a double rule, and one line
			// is as much as either representation is going to carry.
			row.Above = row.Above || hasClass(c, "ltx_border_t") || hasClass(c, "ltx_border_tt")
			row.Below = row.Below || hasClass(c, "ltx_border_b") || hasClass(c, "ltx_border_bb")
			row.Cells = append(row.Cells, cell)
		}
		if len(row.Cells) > 0 {
			b.Rows = append(b.Rows, row)
		}
	}
	return b
}

// number reads a small positive integer out of an attribute.
//
// Anything that is not one is zero, which the callers read as the attribute
// saying nothing. A colspan of "2 " or of "two" is a rendering this does not
// understand, and guessing at it would put a cell in the wrong column.
func number(s string) int {
	if s == "" {
		return 0
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func listing(n *html.Node) Block {
	b := Block{Kind: KindListing, ID: attr(n, "id")}
	if c := findUntil(n, "ltx_caption", isFloat); c != nil {
		b.Tag = tagOf(firstClass(c, "ltx_tag"))
		b.Caption = caption(c)
	}
	// A listing with no mathematics in it is code, and code is written
	// verbatim. One with mathematics is pseudocode, which is an algorithm
	// written with the algorithmic package and is mathematics with keywords
	// around it. The two cannot be emitted the same way: code goes in a fence
	// where a backslash is a backslash, and pseudocode has to keep its dollar
	// signs live or the algorithm prints as the LaTeX somebody typed.
	b.Verbatim = len(allTag(n, "math")) == 0
	lines := allClass(n, "ltx_listingline")
	if len(lines) == 0 {
		b.Text = strings.TrimRight(verbatim(n), "\n")
		return b
	}
	// A listing keeps its line breaks, which is the one place in this package
	// where a newline in the middle of something is correct. Code is not prose.
	var out []string
	for _, l := range lines {
		if b.Verbatim {
			out = append(out, strings.TrimRight(verbatim(l), "\n"))
			continue
		}
		out = append(out, inline(l))
	}
	b.Text = strings.TrimRight(strings.Join(out, "\n"), "\n")
	return b
}

// verbatim is the text of a listing line with its indentation intact.
//
// Not tidy, which collapses every run of whitespace, because the indentation of
// a line of code is part of the code. LaTeXML indents with non-breaking spaces,
// so those become ordinary spaces and the zero width characters go, and
// everything else is left exactly as it was written.
func verbatim(n *html.Node) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\u00a0', '\u2007', '\u202f', '\u2009':
			return ' '
		case '\u200b', '\u200c', '\u200d', '\u2060', '\ufeff':
			return -1
		}
		return r
	}, text(n))
}

func list(n *html.Node) Block {
	b := Block{Kind: KindList, ID: attr(n, "id"), Ordered: hasClass(n, "ltx_enumerate")}
	for _, item := range allClass(n, "ltx_item") {
		if s := inline(item); s != "" {
			b.Items = append(b.Items, s)
		}
	}
	return b
}

// bibliography reads the reference list.
//
// The fields are not read here and the entry is not resolved here. What a block
// holds depends on the bibliography style the author chose, resolving an entry
// needs the metadata plane, and this package reads one HTML file and knows
// nothing about either. So the typography comes out and ax refs build does the
// rest, the same division tables and figures are on.
//
// A block's text is run through inline rather than taken flat, because a
// reference carries the title in quotation marks, the venue in italics and an
// arXiv id inside a link, and the markup around those is what tells them apart.
func bibliography(article *html.Node) []Bibitem {
	list := firstClass(article, "ltx_biblist")
	if list == nil {
		return nil
	}
	var out []Bibitem
	for _, item := range allClass(list, "ltx_bibitem") {
		b := Bibitem{ID: attr(item, "id")}
		if tag := firstClass(item, "ltx_tag_bibitem"); tag != nil {
			b.Label = tidy(text(tag))
		}
		for _, block := range allClass(item, "ltx_bibblock") {
			if s := inline(block); s != "" {
				b.Blocks = append(b.Blocks, s)
			}
		}
		if b.ID == "" || len(b.Blocks) == 0 {
			continue
		}
		out = append(out, b)
	}
	return out
}

// faults finds every conversion error and says where it was.
//
// Where matters more than how many. LaTeXML leaves ltx_ERROR in place of what
// it could not read, and one in a bibliography entry is a reference that prints
// badly while one in a section body is a sentence of the paper that is gone.
func faults(article *html.Node) []Fault {
	var out []Fault
	var walk func(n *html.Node, where string)
	walk = func(n *html.Node, where string) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			at := where
			switch {
			case hasClass(c, "ltx_bibliography"):
				at = "bibliography"
			case attr(c, "id") != "":
				if _, _, isHeading := headingOf(c); isHeading {
					at = attr(c, "id")
				}
			}
			if hasClass(c, "ltx_ERROR") {
				out = append(out, Fault{Where: at, Text: tidy(text(c))})
				continue
			}
			walk(c, at)
		}
	}
	walk(article, "frontmatter")
	return out
}

package extract

import (
	"fmt"
	"strings"
)

// The content plane is Markdown and it is the source of truth.
//
// Not a database, not JSON, not the model in paper.go. Every prior project here
// learned the same thing the same way: a corpus whose truth is a structured
// file is a corpus nobody can fix with a text editor, and the fixing is most of
// the work. So the object record under work/ is a build artefact regenerated
// from these files, and these files are what a person reads, corrects and
// reviews in a diff.
//
// Markdown on its own cannot say that a paragraph is a theorem, so every object
// carries an attribute block on the line it starts, which is the same notation
// Bourbaki and papers both use.
//
//	**Theorem 1** {#thm-1 .statement env=theorem}
//
// The anchor is the local identifier and the class is one of the sixteen object
// kinds. The permanent tag is added to the same block by ax tags assign, which
// is why the block is there from the first write rather than being introduced
// later.

// attrs is one attribute block.
type attrs struct {
	id    string
	class string
	pairs []string
}

func (a attrs) with(key, value string) attrs {
	if value == "" {
		return a
	}
	a.pairs = append(a.pairs, key, value)
	return a
}

func (a attrs) String() string {
	if a.id == "" && a.class == "" && len(a.pairs) == 0 {
		return ""
	}
	var parts []string
	if a.id != "" {
		parts = append(parts, "#"+a.id)
	}
	if a.class != "" {
		parts = append(parts, "."+a.class)
	}
	for i := 0; i+1 < len(a.pairs); i += 2 {
		parts = append(parts, a.pairs[i]+"="+quote(a.pairs[i+1]))
	}
	return "{" + strings.Join(parts, " ") + "}"
}

// quote puts a value in double quotes when it would otherwise end the block
// early or split into two attributes.
func quote(v string) string {
	if !strings.ContainsAny(v, " \t\"{}=") {
		return v
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
}

// body is the Markdown of one file, and the counts of what went into it.
//
// The counts are collected here rather than by a second walk over the model,
// because the number in the front matter has to be the number of objects in the
// file beside it. Two walks are two chances to disagree, and an audit rule
// checking one against the other would then be checking this package against
// itself.
type body struct {
	out        strings.Builder
	names      *names
	objects    int
	equations  int
	code       int
	figures    []string
	tables     []string
	statements []string
}

func (w *body) String() string {
	return strings.TrimRight(w.out.String(), "\n") + "\n"
}

// para writes one block of Markdown and the blank line after it.
func (w *body) para(s string) {
	if strings.TrimSpace(s) == "" {
		return
	}
	w.out.WriteString(strings.TrimRight(s, "\n"))
	w.out.WriteString("\n\n")
}

// label writes the run-in heading an object starts with.
func (w *body) label(text string, a attrs) {
	w.objects++
	if block := a.String(); block != "" {
		w.para("**" + text + "** " + block)
		return
	}
	w.para("**" + text + "**")
}

// section writes a heading and everything under it.
//
// The depth is the Markdown heading level, so a subsection of the section this
// file is comes out as ##. The file's own heading is not written at all: it is
// in the front matter, because a section that is a whole file has its title
// where the file's metadata is and repeating it in the body would give every
// page two titles.
func (w *body) section(s Section, depth int, path []int) {
	id := w.names.section(s, path)
	w.objects++
	heading := strings.Repeat("#", depth) + " "
	if s.Tag != "" {
		heading += s.Tag + " "
	}
	a := attrs{id: id, class: "section"}
	if s.Kind == "appendix" {
		a = a.with("kind", "appendix")
	}
	w.para(heading + s.Title + " " + a.String())
	w.blocks(s.Blocks, within{})
	for i, sub := range s.Sections {
		w.section(sub, min(depth+1, 6), append(path, i+1))
	}
}

func (w *body) blocks(bs []Block, parent within) {
	for i, b := range bs {
		w.block(b, parent, i)
	}
}

// block writes one block and returns the local identifier it was given, which
// is empty for a block nothing can point at.
func (w *body) block(b Block, parent within, i int) string {
	// Checked before anything is named, so that a container does not take a
	// fig-u number away from a figure that has a picture in it.
	if container(b) {
		return w.contained(b)
	}
	id := w.names.panel(parent, b, i)
	switch b.Kind {
	case KindParagraph:
		w.para(b.Text)
	case KindQuote:
		w.para("> " + b.Text)
	case KindList:
		w.list(b)
	case KindEquation:
		w.equation(b, id)
	case KindFigure, KindTable, KindAlgorithm:
		w.float(b, id)
	case KindListing:
		w.listing(b, id)
	case KindTheorem:
		w.theorem(b, id)
	case KindProof:
		w.label(proofTitle(b), attrs{id: id, class: "proof"})
		w.blocks(b.Blocks, within{})
	default:
		w.para(b.Text)
	}
	return id
}

func (w *body) list(b Block) {
	var lines []string
	for i, item := range b.Items {
		if b.Ordered {
			lines = append(lines, fmt.Sprintf("%d. %s", i+1, item))
			continue
		}
		lines = append(lines, "- "+item)
	}
	w.para(strings.Join(lines, "\n"))
}

// equation writes display mathematics.
//
// A group of aligned equations is written as its rows, one display each, and
// not as one block with ampersands in it. The rows are what the author numbered
// and what a cross reference points at, and the alignment is a typesetting
// decision the emitter makes rather than a fact about the paper.
func (w *body) equation(b Block, id string) {
	if len(b.Blocks) > 0 {
		first := ""
		for i, row := range b.Blocks {
			if rid := w.block(row, within{}, i); first == "" {
				first = rid
			}
		}
		// The paper refers to an align both ways, as (2) for the group and as
		// (2a) for the row. The group has no line of its own to carry an
		// anchor, so a reference to it is sent to its first row, which is where
		// somebody looking for equation 2 wants to land.
		w.names.anchor(b.ID, first)
		return
	}
	if strings.TrimSpace(b.Text) == "" {
		return
	}
	w.equations++
	w.objects++
	s := "$$\n" + b.Text + "\n$$"
	if a := (attrs{id: id, class: "equation"}); id != "" {
		s += "\n" + a.String()
	}
	w.para(s)
}

// float writes a figure, a table or an algorithm.
//
// One shape for all three, because they are one shape in the paper: a label
// with a number, a body, and a caption under it. What differs is the body, and
// a float can have more than one kind of body at once. Authors put results
// tables inside figure environments constantly, and a figure of six subfigures
// is one numbered thing with six panels under it.
func (w *body) float(b Block, id string) {
	class := classOf(b)
	w.label(floatTitle(b), attrs{id: id, class: class})
	switch class {
	case "figure":
		w.figures = append(w.figures, id)
	case "table":
		w.tables = append(w.tables, id)
	case "code":
		w.code++
	}
	for _, img := range b.Images {
		// The source is the name arXiv's rendering gave, relative to the
		// rendering's own URL. ax figures downloads them, converts them and
		// rewrites these lines to the committed file, which is the point at
		// which the third party check in the licence spec runs. Until then the
		// line says truthfully where the picture is, which is on arXiv.
		w.para(fmt.Sprintf("![%s](%s)", img.Alt, img.Src))
	}
	if len(b.Rows) > 0 {
		w.para(table(b.Rows))
	}
	if b.Text != "" {
		w.listingBody(b)
	}
	w.para(b.Caption)
	for i, panel := range b.Blocks {
		// The body of an algorithm float is a listing, and the float carries
		// the number the paper prints, so labelling the listing as well would
		// give one algorithm two headings.
		if panel.Kind == KindListing && class == "code" && panel.Tag == "" {
			w.listingBody(panel)
			w.para(panel.Caption)
			continue
		}
		w.block(panel, within{id: id, class: class}, i)
	}
}

// container says a float has nothing of its own but the things inside it.
//
// LaTeXML makes one when the author put two numbered algorithms in a single
// figure environment to get them side by side on the page. It has no number, no
// picture and no caption, and labelling it would put a bare **Figure** above
// two algorithms that are already labelled.
func container(b Block) bool {
	switch b.Kind {
	case KindFigure, KindTable, KindAlgorithm:
	default:
		return false
	}
	return b.Tag == "" && b.Caption == "" && b.Text == "" &&
		len(b.Images) == 0 && len(b.Rows) == 0 && len(b.Blocks) > 0
}

// contained writes the panels as though the container were not there.
//
// A reference to the container itself is sent to the first of them, because the
// container is where the author's \label went and the first panel is what they
// meant by it.
func (w *body) contained(b Block) string {
	first := ""
	for i, panel := range b.Blocks {
		if id := w.block(panel, within{}, i); first == "" {
			first = id
		}
	}
	w.names.anchor(b.ID, first)
	return first
}

func (w *body) listing(b Block, id string) {
	w.label(floatTitle(b), attrs{id: id, class: "code"})
	w.code++
	w.listingBody(b)
	w.para(b.Caption)
}

// listingBody writes the lines of a listing, keeping them as lines.
//
// Code goes in a fence. Pseudocode does not, because pseudocode written with
// the algorithmic package is mathematics with keywords around it and a fence
// would print the LaTeX instead of the algorithm. It gets the trailing
// backslash that means a hard line break, which is the only way to keep a line
// a line without stopping the mathematics from rendering.
func (w *body) listingBody(b Block) {
	if b.Verbatim {
		w.para("```\n" + b.Text + "\n```")
		return
	}
	lines := strings.Split(b.Text, "\n")
	for i := range lines[:max(len(lines)-1, 0)] {
		lines[i] += " \\"
	}
	w.para(strings.Join(lines, "\n"))
}

func (w *body) theorem(b Block, id string) {
	class := classOf(b)
	a := attrs{id: id, class: class}.with("env", strings.ToLower(b.Env))
	w.label(theoremTitle(b), a)
	if class == "statement" {
		w.statements = append(w.statements, id)
	}
	w.blocks(b.Blocks, within{})
}

// theoremTitle is what the paper prints in front of a theorem.
//
// The author's own word for the environment, capitalised, and then the number.
// A paper with a \newtheorem{observation} gets Observation 4, because that is
// what its own PDF says and a reader holding the PDF open beside this should
// see the same words.
func theoremTitle(b Block) string {
	name := title(b.Env)
	if name == "" {
		name = "Theorem"
	}
	if b.Tag != "" {
		name += " " + b.Tag
	}
	if b.Title != "" {
		name += " " + b.Title
	}
	return name
}

// proofTitle is what the paper prints in front of a proof, which is usually
// Proof and is sometimes "Proof of Theorem 1".
//
// The full stop LaTeXML runs into the heading comes off, because the emitter
// puts the heading in bold and adds its own, and a proof headed "Proof." would
// come out as "**Proof.**" beside a theorem headed "**Theorem 1**".
func proofTitle(b Block) string {
	if s := strings.TrimRight(strings.TrimSpace(b.Title), "."); s != "" {
		return s
	}
	return "Proof"
}

func floatTitle(b Block) string {
	name := map[Kind]string{
		KindFigure:    "Figure",
		KindTable:     "Table",
		KindAlgorithm: "Algorithm",
		KindListing:   "Listing",
	}[b.Kind]
	if name == "" {
		name = "Figure"
	}
	if b.Tag != "" {
		name += " " + b.Tag
	}
	return name
}

func title(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(strings.ToLower(s))
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// table writes the cells as a Markdown table.
//
// Lossy, on purpose, and the loss is recorded rather than hidden. Markdown has
// no multi row span, no multi column header and no booktabs rule, and academic
// tables use all three constantly, so the original markup is kept beside this
// by ax tables and this is the representation a reader and a translator work
// on. A cell spanning three columns is written once and followed by two empty
// cells, which keeps the columns lined up and loses the fact that they were one
// cell.
func table(rows []Row) string {
	width := 0
	for _, r := range rows {
		n := 0
		for _, c := range r.Cells {
			n += max(c.Span, 1)
		}
		width = max(width, n)
	}
	if width == 0 {
		return ""
	}
	// Markdown has to have a header row, so a table whose first row is not one
	// gets an empty header and keeps all its rows.
	head := rows[0]
	rest := rows[1:]
	if !isHeader(head) {
		head = Row{}
		rest = rows
	}
	var out []string
	out = append(out, line(head, width))
	out = append(out, rule(rows[0], width))
	for _, r := range rest {
		out = append(out, line(r, width))
	}
	return strings.Join(out, "\n")
}

func isHeader(r Row) bool {
	for _, c := range r.Cells {
		if !c.Header {
			return false
		}
	}
	return len(r.Cells) > 0
}

func line(r Row, width int) string {
	cells := make([]string, 0, width)
	for _, c := range r.Cells {
		cells = append(cells, strings.ReplaceAll(c.Text, "|", `\|`))
		for i := 1; i < c.Span; i++ {
			cells = append(cells, "")
		}
	}
	for len(cells) < width {
		cells = append(cells, "")
	}
	return "| " + strings.Join(cells[:width], " | ") + " |"
}

// rule is the row of dashes, which is where Markdown puts the alignment.
func rule(first Row, width int) string {
	aligns := make([]string, 0, width)
	for _, c := range first.Cells {
		for i := 0; i < max(c.Span, 1); i++ {
			switch c.Align {
			case "left":
				aligns = append(aligns, ":---")
			case "center":
				aligns = append(aligns, ":---:")
			case "right":
				aligns = append(aligns, "---:")
			default:
				aligns = append(aligns, "---")
			}
		}
	}
	for len(aligns) < width {
		aligns = append(aligns, "---")
	}
	return "| " + strings.Join(aligns[:width], " | ") + " |"
}

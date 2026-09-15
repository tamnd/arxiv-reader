package extract

import (
	"regexp"
	"strings"
)

// Vision reads a paper out of what a model made of pictures of its pages.
//
// The fourth path and the only one that costs money. The three above it read
// something a machine wrote: markup from arXiv, markup from a local conversion,
// or the characters a PDF was printed with. This one reads a reading. A page of
// the paper was drawn as a picture, something was asked to write down what is on
// it, and what comes back is one page of Markdown with no guarantee attached
// beyond what the acceptance rules in the vision package could check.
//
// The pages come in as the model returned them, one string per page of the file,
// and a page nobody could read comes in empty. That is the shape this function
// wants: a paper with a hole in it is a fact the caller has already decided to
// accept, and a page that is missing is a page with no blocks rather than an
// error.
//
// What this reads is Markdown and not prose, which is the one thing that makes it
// different from the native path. A heading is a heading because it has a hash in
// front of it rather than because of where it was printed, mathematics is between
// dollars because the model was asked for LaTeX, and a table came back as a table.
// So the structure here is read rather than recovered, and what is unreliable
// about this path is not the structure but whether the words are the words on the
// page.
func Vision(pages []string, id string, version int) *Paper {
	p := &Paper{ID: id, Version: version, Pages: span{first: 1, last: len(pages)}.String()}
	if len(pages) == 0 {
		p.Pages = ""
	}
	var seen []looked
	for i, page := range pages {
		seen = append(seen, onePage(page, i+1)...)
	}
	assemble(p, paired(seen))
	return p
}

// looked is one thing a model wrote, and which page of the paper it wrote it on.
//
// A heading and a block rather than one of the two, because the walk that places
// them has to treat the two differently and a Kind for headings would make every
// switch in this file carry a case that cannot happen.
type looked struct {
	head  *heading
	block Block
	page  int
}

// onePage reads one page of Markdown into the things it holds.
//
// A small reader and deliberately not a Markdown library. What is being read is
// not arbitrary Markdown: it is what a model was asked for by the prompt in the
// vision package, which is headings, paragraphs, dollars, fences, pipe tables and
// numbered reference lines. A full parser would buy inline emphasis, nested lists
// and reference style links, and every one of those is a thing the prompt asks
// for in prose and this path publishes as prose.
func onePage(text string, page int) []looked {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var out []looked
	var para []string
	// flush ends the paragraph being gathered. A paragraph is joined with spaces
	// rather than kept as lines, because where a model broke its lines is where the
	// page broke its lines and that is not a fact about the paper.
	flush := func() {
		joined := strings.TrimSpace(strings.Join(para, " "))
		para = nil
		if joined == "" {
			return
		}
		out = append(out, looked{block: blockOfMarkdown(joined), page: page})
	}
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "#"):
			flush()
			h := marked(line)
			out = append(out, looked{head: &h, page: page})
		case strings.HasPrefix(line, "```"):
			flush()
			body, next := fenced(lines, i)
			// Verbatim says a fence is code and not pseudocode, and the difference
			// is whether a dollar sign in it is money, a shell variable or
			// mathematics. An algorithm a model wrote out of a page keeps its
			// dollars live and a listing does not.
			out = append(out, looked{block: Block{Kind: KindListing, Text: body, Verbatim: !strings.Contains(body, "$")}, page: page})
			i = next
		case line == "$$":
			flush()
			body, next := displayed(lines, i)
			b := Block{Kind: KindEquation, Text: body}
			if m := tagged.FindStringSubmatch(body); m != nil {
				// The number the page printed, which the prompt asks for as \tag
				// because that is where LaTeX itself puts it. It comes out of the
				// mathematics and into the block, so the corpus numbers the
				// equation the way it numbers one off the other three paths.
				b.Tag = strings.TrimSpace(m[1])
				b.Text = strings.TrimSpace(tagged.ReplaceAllString(body, ""))
			}
			out = append(out, looked{block: b, page: page})
			i = next
		case strings.HasPrefix(line, "|"):
			flush()
			rows, next := tabled(lines, i)
			out = append(out, looked{block: Block{Kind: KindTable, Rows: rows}, page: page})
			i = next
		case listOpening.MatchString(line):
			flush()
			items, ordered, next := listed(lines, i)
			out = append(out, looked{block: Block{Kind: KindList, Items: items, Ordered: ordered}, page: page})
			i = next
		case strings.HasPrefix(line, ">"):
			flush()
			body, next := quoted(lines, i)
			out = append(out, looked{block: Block{Kind: KindQuote, Text: body}, page: page})
			i = next
		default:
			para = append(para, line)
		}
	}
	flush()
	return out
}

// tagged is the equation number the prompt asks for, written where LaTeX writes
// one.
var tagged = regexp.MustCompile(`\\tag\{([^}]*)\}`)

// fenced is the body of a code fence and the line the fence closed on.
//
// A fence nothing closes runs to the end of the page rather than being dropped.
// The page ended and the listing did not, which happens on every page that has a
// listing running over the bottom of it, and what is on the page is worth keeping
// even though the block is half of one.
func fenced(lines []string, at int) (string, int) {
	var body []string
	for i := at + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
			return strings.Join(body, "\n"), i
		}
		body = append(body, lines[i])
	}
	return strings.Join(body, "\n"), len(lines) - 1
}

// displayed is the LaTeX of a display block and the line its dollars closed on.
func displayed(lines []string, at int) (string, int) {
	var body []string
	for i := at + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "$$" {
			return strings.TrimSpace(strings.Join(body, "\n")), i
		}
		body = append(body, lines[i])
	}
	return strings.TrimSpace(strings.Join(body, "\n")), len(lines) - 1
}

// tabled reads a pipe table and returns its rows and the line it ended on.
//
// The rule row, which is the one made of dashes and colons, is what says the row
// above it is the header. It is not kept as a row of its own: it is typography and
// the corpus records the fact it stated, which is Cell.Header.
func tabled(lines []string, at int) ([]Row, int) {
	var rows []Row
	last := at
	for i := at; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "|") {
			break
		}
		last = i
		cells := cellsOf(line)
		if ruleRow(cells) {
			if n := len(rows); n > 0 {
				rows[n-1].Below = true
				for j := range rows[n-1].Cells {
					rows[n-1].Cells[j].Header = true
				}
			}
			continue
		}
		row := Row{}
		for _, c := range cells {
			row.Cells = append(row.Cells, Cell{Text: c, Span: 1, Down: 1})
		}
		rows = append(rows, row)
	}
	return rows, last
}

// cellsOf cuts a pipe row into its cells.
func cellsOf(line string) []string {
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	var out []string
	for _, c := range strings.Split(line, "|") {
		out = append(out, strings.TrimSpace(c))
	}
	return out
}

// ruleRow says a row is the line under a header rather than a row of the table.
func ruleRow(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		if c == "" || strings.Trim(c, "-: ") != "" {
			return false
		}
	}
	return true
}

// listed reads a list and returns its items, whether it was numbered, and the
// line it ended on.
//
// A wrapped item is joined onto the item above it, because a list item that ran
// over its line is one item and a reader who gets two has been told the paper says
// something it does not.
func listed(lines []string, at int) ([]string, bool, int) {
	ordered := !strings.HasPrefix(listOpening.FindString(strings.TrimSpace(lines[at])), "-") &&
		!strings.HasPrefix(listOpening.FindString(strings.TrimSpace(lines[at])), "*") &&
		!strings.HasPrefix(listOpening.FindString(strings.TrimSpace(lines[at])), "+")
	var items []string
	last := at
	for i := at; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			break
		}
		if m := listOpening.FindString(line); m != "" {
			items = append(items, strings.TrimSpace(line[len(m):]))
			last = i
			continue
		}
		if n := len(items); n > 0 {
			items[n-1] += " " + line
			last = i
			continue
		}
		break
	}
	return items, ordered, last
}

// quoted reads a block quotation and returns its text and the line it ended on.
func quoted(lines []string, at int) (string, int) {
	var body []string
	last := at
	for i := at; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, ">") {
			break
		}
		body = append(body, strings.TrimSpace(strings.TrimPrefix(line, ">")))
		last = i
	}
	return strings.TrimSpace(strings.Join(body, " ")), last
}

// marked reads a Markdown heading the way recovered reads a printed line.
//
// The depth is the number of hashes and is not guessed at, which is the whole
// difference between this path's headings and the native path's. What still has to
// be worked out is the same as there: the number the paper printed, whether this
// is an appendix, and whether the heading is one of the two that are not sections
// of the paper at all.
func marked(line string) heading {
	hashes := 0
	for _, r := range line {
		if r != '#' {
			break
		}
		hashes++
	}
	text := strings.TrimSpace(strings.Trim(strings.TrimSpace(line[hashes:]), "#"))
	h := heading{title: text, level: hashes}
	if h.level < 1 {
		h.level = 1
	}
	if m := appendix.FindStringSubmatch(text); m != nil && len(strings.Fields(text)) > 0 && strings.EqualFold(strings.Fields(text)[0], "appendix") {
		return heading{tag: m[1], title: strings.TrimSpace(m[2]), level: 1, kind: "appendix"}
	}
	if m := numbered.FindStringSubmatch(text); m != nil {
		h.tag, h.title = m[1], m[2]
	} else if m := roman.FindStringSubmatch(text); m != nil {
		h.tag, h.title = m[1], m[2]
	}
	lower := strings.ToLower(strings.TrimRight(h.title, ".:"))
	if _, ok := named[lower]; ok {
		h.references = lower == "references" || lower == "bibliography"
		h.abstract = lower == "abstract"
	}
	return h
}

// blockOfMarkdown reads one paragraph into the block it is.
//
// The same three questions blockOf asks of a printed paragraph, and one it does
// not ask. Nothing here escapes what Markdown would read as a marker, because on
// this path the answer came back as Markdown on purpose: the emphasis is emphasis,
// the dollars are mathematics the model was asked for, and a backslash in front of
// them would publish the marks instead of the meaning.
func blockOfMarkdown(text string) Block {
	if m := captionOpening.FindStringSubmatch(text); m != nil {
		kind := KindFigure
		switch strings.ToLower(m[1]) {
		case "tab", "table":
			kind = KindTable
		case "alg", "algorithm":
			kind = KindAlgorithm
		case "listing":
			kind = KindListing
		}
		return Block{Kind: kind, Tag: m[2], Caption: m[3]}
	}
	if m := proofOpening.FindStringSubmatch(text); m != nil {
		return Block{Kind: KindProof, Title: m[1], Blocks: []Block{{Kind: KindParagraph, Text: m[2]}}}
	}
	if m := statement.FindStringSubmatch(text); m != nil {
		return Block{Kind: KindTheorem, Env: strings.ToLower(m[1]), Tag: m[2], Text: m[3]}
	}
	return Block{Kind: KindParagraph, Text: text}
}

// paired puts a table's caption and a table's rows into one block.
//
// A page prints them as two things and a model writes them as two things: a pipe
// table, and a paragraph above or below it that starts with the word Table and a
// number. A corpus that published those separately would have a table with no
// number and a caption with nothing under it, and the emitters, the audit and the
// object model all read a table as one block with rows and a caption.
func paired(seen []looked) []looked {
	var out []looked
	for i := 0; i < len(seen); i++ {
		l := seen[i]
		if rows, ok := rowsOnly(l); ok {
			// The caption under the table, which is where most styles print one.
			if i+1 < len(seen) {
				if cap, ok := captionOnly(seen[i+1], KindTable); ok {
					cap.block.Rows = rows
					out = append(out, cap)
					i++
					continue
				}
			}
			// And the caption above it, which is where the rest print one. The
			// caption keeps its own place in the order either way, because that is
			// where the page had it.
			if n := len(out); n > 0 {
				if cap, ok := captionOnly(out[n-1], KindTable); ok && cap.block.Rows == nil {
					cap.block.Rows = rows
					out[n-1] = cap
					continue
				}
			}
		}
		out = append(out, l)
	}
	return out
}

// rowsOnly says a looked is a table with cells in it and nothing else.
func rowsOnly(l looked) ([]Row, bool) {
	if l.head != nil || l.block.Kind != KindTable || len(l.block.Rows) == 0 || l.block.Caption != "" {
		return nil, false
	}
	return l.block.Rows, true
}

// captionOnly says a looked is a caption of this kind with nothing under it.
func captionOnly(l looked, kind Kind) (looked, bool) {
	if l.head != nil || l.block.Kind != kind || l.block.Caption == "" {
		return looked{}, false
	}
	return l, true
}

// assemble places what was read into the paper.
//
// The same walk build does over a printed page, with the parts that had to guess
// taken out. A heading is a heading here, so there is no rule about the shape of a
// line and no rule about the gaps around it, and the two headings that are not
// sections are found by their names the same way.
func assemble(p *Paper, seen []looked) {
	topAt, subAt := -1, -1
	var topSpan, subSpan span
	abstract, bib := false, false
	var opening []Block
	var openSpan span
	// Whether the paper says the word Abstract anywhere, which decides whether the
	// abstract is read off a heading or guessed at. The same question build asks,
	// and for the same reason: the guess is the first long paragraph of the paper,
	// and on a page with thirty authors on it the first long paragraph is the
	// byline.
	says := false
	for _, l := range seen {
		if l.head != nil && l.head.abstract {
			says = true
			break
		}
	}
	closeSub := func() {
		if subAt >= 0 {
			p.Sections[topAt].Sections[subAt].Pages = subSpan.String()
		}
		subAt, subSpan = -1, span{}
	}
	closeTop := func() {
		closeSub()
		if topAt >= 0 {
			p.Sections[topAt].Pages = topSpan.String()
		}
		topAt, topSpan = -1, span{}
	}
	// openings writes the held prose out as a section of its own, which is the one
	// heading this path invents, and it invents it for the same reason the native
	// path does: a paper that opens with two unheaded paragraphs has text that
	// belongs to nothing, and filing it under the heading that follows it puts the
	// introduction inside whatever comes after the introduction.
	openings := func(title string) {
		if len(opening) == 0 {
			return
		}
		p.Sections = append(p.Sections, Section{Level: 1, Title: title, Blocks: opening, Pages: openSpan.String()})
		opening, openSpan = nil, span{}
	}
	for _, l := range seen {
		at := span{first: l.page, last: l.page}
		if h := l.head; h != nil {
			switch {
			case h.references:
				closeTop()
				openings("Introduction")
				abstract, bib = false, true
				continue
			case h.abstract:
				closeTop()
				abstract, bib = true, false
				continue
			}
			// The title of the paper, which a model writes as the first heading of
			// the first page because that is what it is on the page. It is recorded
			// and it is not a section, and what gets published is the metadata
			// plane's title rather than this one.
			if p.Title == "" && topAt < 0 && len(p.Abstract) == 0 && l.page == 1 && h.level == 1 && h.tag == "" && !bib {
				p.Title = h.title
				continue
			}
			openings("Introduction")
			abstract, bib = false, false
			if h.level > 1 && topAt >= 0 {
				closeSub()
				p.Sections[topAt].Sections = append(p.Sections[topAt].Sections, h.section())
				subAt = len(p.Sections[topAt].Sections) - 1
				subSpan, topSpan = subSpan.widen(at), topSpan.widen(at)
				continue
			}
			closeTop()
			p.Sections = append(p.Sections, h.section())
			topAt = len(p.Sections) - 1
			topSpan = topSpan.widen(at)
			continue
		}
		b := l.block
		if bib {
			// A reference entry is a paragraph here rather than a line, because the
			// prompt asks for one entry to a line and a paragraph is what a line
			// becomes. An entry that does not open with a printed number is the
			// wrapped tail of the entry above it, which is what a model writes when
			// the page wrapped the entry and it kept the break.
			text := strings.TrimSpace(b.Text)
			if text == "" {
				continue
			}
			if n := len(p.Bibliography); n > 0 && !bibLabel.MatchString(text) {
				p.Bibliography[n-1].Blocks = append(p.Bibliography[n-1].Blocks, text)
				continue
			}
			p.Bibliography = append(p.Bibliography, bibitem("vision", len(p.Bibliography)+1, text))
			continue
		}
		if topAt < 0 {
			// Everything above the first heading is the title block: the title, the
			// byline, the affiliations, a date, and then the abstract. Only the
			// abstract is wanted, because the metadata plane holds the rest and
			// holds it better.
			if abstract || (!says && len(p.Abstract) == 0 && b.Kind == KindParagraph && len(b.Text) >= abstractFloor && ends(b.Text)) {
				p.Abstract = append(p.Abstract, b)
				p.FrontPages = span{first: l.page, last: l.page}.String()
				continue
			}
			if len(p.Abstract) > 0 {
				opening = append(opening, b)
				openSpan = openSpan.widen(at)
			}
			continue
		}
		topSpan = topSpan.widen(at)
		if subAt >= 0 {
			subSpan = subSpan.widen(at)
			p.Sections[topAt].Sections[subAt].Blocks = append(p.Sections[topAt].Sections[subAt].Blocks, b)
			continue
		}
		p.Sections[topAt].Blocks = append(p.Sections[topAt].Blocks, b)
	}
	closeTop()
	// A paper whose pages came back with no heading in them at all, which is a
	// short note and is also what a page nobody could read leaves behind. Its body
	// is whatever was held, and it is called Body because that is the name the
	// splitter gives the one file of a paper with no sections.
	openings("Body")
}

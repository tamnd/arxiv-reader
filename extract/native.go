package extract

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// The native path, which is the third of the four and the first one that has to
// guess.
//
// The render path and the source path both read a document that already has a
// structure in it. LaTeXML says this is a section and that is a theorem, and the
// extractor's job is to carry that over without losing it. A PDF says nothing of
// the kind. Its text layer is the characters that were typeset, in the order and
// roughly the position they were printed in, and every piece of structure above
// a sentence was thrown away by the typesetter years before anybody here looked
// at it. So everything above a sentence is recovered here from the shape of the
// printed page, and anything recovered that way is sometimes wrong.
//
// The path is worth having anyway, and the reason is the sentences. Nothing
// guessed them: they were printed, pdftotext read them back, and they are the
// author's words. A native paper is a real paper with an unreliable table of
// contents, which is a much better thing to publish than nothing, and the front
// matter says path: native so that every reader of it knows which kind it has.
//
// Four things this path does not do, and 2166-05 says the same four. It does not
// reconstruct mathematics, because display mathematics comes back as the
// characters that were printed where it was, in the order they were printed,
// which is not the order they were written. It does not extract figures, only
// their captions. It does not extract tables, only the fact that there was one.
// And it does not mark a recovered heading as low confidence one heading at a
// time, because the confidence is a property of the path and not of the heading:
// path: native in the front matter says every heading in the file was recovered
// this way, and a number per heading would be a number this made up.

const (
	// slack is how far from the paper's cut a row's own gap may be, for nearest.
	//
	// Four columns either way. Measured on a nineteen page paper, the right hand
	// column starts within two columns of the paper's cut on every page that has
	// one, and the pages that need more than that are the ones a figure pushed the
	// column across on, which is a whole page and not a row. Wider than this and
	// the nearest word space of a full width line starts to qualify.
	slack = 4
	// gutterWidth is how many near blank columns down the middle of a page mean
	// the page was printed in two columns.
	//
	// Four. A two column paper's gutter comes back six to ten characters wide
	// under -layout, and the widest run of spaces inside a single column of
	// justified prose is the three or four that a short last line leaves, which
	// is why the run has to be found in the middle of the page and not anywhere
	// on it.
	gutterWidth = 4
	// gutterCross is the share of a page's two column lines that may put a
	// character in the gutter and have it still count as a gutter.
	//
	// Three in ten, which sounds far too generous and is not. The share is taken
	// against the lines that carry text on both sides of the gap and not against
	// the whole page, and on a real gutter that share is nought on most pages, so
	// what this tolerance buys is the pages where it is not: a bibliography whose
	// entries the typesetter let run wide, a footnote rule, a table that spilled.
	// Measured over a nineteen page paper, a fifth of the pages have a gutter that
	// a stricter rule loses, and a page whose gutter was missed is a page whose
	// every line reads as the end of one paragraph and the start of another.
	gutterCross = 0.30
	// narrowest is the printed width below which a page is one column whatever
	// its spaces look like.
	narrowest = 60
	// runOn is how many lines apart two entries of one reference list can be.
	//
	// Forty, which is most of a column. Consecutive entries are a line or two apart
	// and what this has to cover is the page break, the running head and the figure
	// in the middle of the list. Anything further than this is the next [1] of
	// something else.
	runOn = 40
	// fewestLines is how many lines a page needs before a gap down its middle is
	// believed to be a gutter.
	//
	// A title page, a plate and a page holding one wide table all have gaps in
	// the middle that are not gutters, and all three are short.
	fewestLines = 8
	// edge is how many lines at each end of a page can be furniture.
	edge = 3
	// repeats is how many pages a line has to be printed at the edge of before
	// it is the journal's running head rather than a sentence of the paper.
	repeats = 3
	// abstractFloor is how long the first paragraph of prose on page one has to
	// be before it is taken for the abstract.
	//
	// The title block above it is a run of short lines: the title, the byline,
	// the affiliations, a date. Two hundred characters is shorter than any real
	// abstract and longer than any of those.
	abstractFloor = 200
	// mathLetters is the share of a line's characters that have to be letters
	// before the line is prose rather than what is left of an equation.
	mathLetters = 0.55
	// mathLines is how many lines a run of flattened mathematics can be.
	//
	// Four. A display equation prints as one line, and the broken accents,
	// superscripts and denominators around it print as one line each, so a
	// single equation comes back as up to four. A longer run of lines that all
	// look like mathematics is a table, and a table is not mathematics.
	mathLines = 4
)

// stamp is the line arXiv prints down the left margin of every PDF it serves.
//
// It comes back as an ordinary line of text sitting in the middle of page one's
// line sequence, because pdftotext reads the rotated text and then files it by
// where it started. It is not part of the paper and it is the only line in a PDF
// that says which version the file is of, so it is taken off the page and kept.
var stamp = regexp.MustCompile(`^arXiv:\S+(\s+\[[^\]]*\])?\s+\d{1,2}\s+\p{L}+\s+\d{4}$`)

// digits is what a page number is replaced with before furniture is counted.
var digits = regexp.MustCompile(`\d+`)

// numbered is a section heading with a printed number, which is what most
// papers use.
var numbered = regexp.MustCompile(`^(\d+(?:\.\d+)*)\.?\s+(\p{Lu}\S*(?:\s+\S+)*)$`)

// roman is the numbering the physics journals use, so "II. DATA".
var roman = regexp.MustCompile(`^([IVXLC]+)\.\s+(\p{Lu}\S*(?:\s+\S+)*)$`)

// appendix is an appendix heading in either of the two shapes papers print it.
var appendix = regexp.MustCompile(`^(?i:appendix)\s*([A-Z]?)[.:]?\s*(.*)$`)

// statement is the opening of a theorem, a definition or anything else a paper
// prints a name and a number in front of.
var statement = regexp.MustCompile(`^(Theorem|Lemma|Proposition|Corollary|Definition|Remark|Example|Conjecture|Claim|Observation|Assumption|Problem|Exercise|Fact|Notation|Axiom|Question)\s*(\d+(?:\.\d+)*)?\s*[.:]\s*(.*)$`)

// proofOpening is the word a proof starts with, with or without the statement it
// names.
var proofOpening = regexp.MustCompile(`^(Proof(?:\s+of\s+[^.:]{1,60})?)\s*[.:]\s*(.*)$`)

// captionOpening is the opening of a figure, table, algorithm or listing caption, in
// the several shapes journals print it.
var captionOpening = regexp.MustCompile(`^(?i:(fig|figure|tab|table|alg|algorithm|listing))\.?\s*(\d+[a-z]?|[IVXLC]+)\s*[.:]\s*(\S.*)$`)

// bibLabel is the number a numeric bibliography prints in front of an entry.
var bibLabel = regexp.MustCompile(`^\[(\d+)\]\s*(\S.*)$`)

// named are the headings papers print with no number in front of them.
//
// A list and not a heuristic, because these are the headings the short line rule
// gets wrong most often: "Acknowledgements" sits on its own line above a
// paragraph and so does the last line of the paragraph before it.
var named = map[string]int{
	"abstract":         1,
	"introduction":     1,
	"background":       1,
	"related work":     1,
	"methods":          1,
	"method":           1,
	"results":          1,
	"discussion":       1,
	"conclusion":       1,
	"conclusions":      1,
	"summary":          1,
	"acknowledgement":  1,
	"acknowledgements": 1,
	"acknowledgment":   1,
	"acknowledgments":  1,
	"references":       1,
	"bibliography":     1,
	"appendix":         1,
	"appendices":       1,
}

// Native reads a paper out of the characters its PDF was printed with.
//
// The pages come in as pdftotext read them, one string per page of the file, so
// that this package does not have to know how pdftotext is run or that it is
// pdftotext at all. The identifier and the version come from the caller rather
// than from the file for the same reason they do on the render path: what the
// file says it is, is a thing to check, and what it was asked for is a thing to
// record.
//
// Nothing here fails. A PDF whose text layer is empty produces a paper with no
// sections, which is a fact about the PDF that the caller decides what to do
// about, and a PDF that cannot be read at all never reaches this function.
func Native(pages []string, id string, version int) *Paper {
	p := &Paper{ID: id, Version: version, Pages: span{first: 1, last: len(pages)}.String()}
	printedPages := pageLines(pages)
	p.Stamp = stampOf(printedPages)
	running := furniture(printedPages)
	var lines []printed
	for _, page := range printedPages {
		for _, l := range page {
			if l.text != "" && (stamp.MatchString(l.text) || running[normal(l.text)]) {
				continue
			}
			lines = append(lines, l)
		}
	}
	chunks := joined(paragraphs(lines))
	p.Encoding = encodingOf(chunks)
	build(p, chunks)
	return p
}

// joined puts back together a paragraph that was broken across a column or a
// page.
//
// A paragraph that ran off the bottom of one column and on at the top of the next
// comes out of paragraphs as two chunks, because there is a gap between them and
// the second one starts at the margin. The join is the sentence: the first half
// stopped in the middle of one and the second half starts with a lower case
// letter, which no paragraph and no heading a paper prints ever does.
//
// This is worth more than the paragraph it repairs. Without it the tail end of the
// broken column is a single line with a gap above it and a gap below it, which is
// exactly the shape of an unnumbered heading, and every second page of a two
// column paper invents a section out of the last line of a paragraph.
func joined(chunks []chunk) []chunk {
	var out []chunk
	for _, c := range chunks {
		// Nothing is ever joined onto a heading. A heading does not end in a full
		// stop and the line under it is the first line of a section, so a section
		// that opens with a lower case word would otherwise have its first paragraph
		// glued to the back of its own heading and the heading would be lost.
		if n := len(out); n > 0 && startsLower(c.text()) && !ends(out[n-1].text()) && !opens(c.lines[0].text) && !strong(out[n-1]) {
			out[n-1].lines = append(out[n-1].lines, c.lines...)
			out[n-1].last = c.last
			out[n-1].after = c.after
			continue
		}
		out = append(out, c)
	}
	return out
}

// printed is one line as it came off the page.
type printed struct {
	// text is the line with its leading and trailing space taken off, and it is
	// empty for a blank line, which is kept because it is one of the two signals
	// that a paragraph ended.
	text string
	// indent is how many characters in the line started, which is the signal
	// that a paragraph began.
	indent int
	// page is the page of the PDF it was printed on, counted from one.
	page int
	// column is which column of the page it was printed in, counted from one, and
	// it is 1 on a page that was printed in one column.
	//
	// Kept for the running heads. A page of a two column paper is read as its left
	// column and then its right, so the top of the right column is the middle of
	// the page's line sequence, and a rule that looks for a running head at the top
	// of a page would never look there.
	column int
}

// span is the pages one piece of a paper was read off.
type span struct {
	first int
	last  int
}

// String is the pages as the front matter records them, so "7" for one page and
// "3-7" for a run.
func (s span) String() string {
	switch {
	case s.first == 0 || s.last == 0:
		return ""
	case s.first == s.last:
		return fmt.Sprintf("%d", s.first)
	default:
		return fmt.Sprintf("%d-%d", s.first, s.last)
	}
}

// add widens a span to hold a chunk.
func (s span) add(c chunk) span {
	return s.widen(span{first: c.first, last: c.last})
}

// widen is the smallest span holding both of these.
//
// A zero span is nothing read yet rather than page zero, so widening by one is
// the other one. That is what lets a caller start with a zero value and feed it
// pages in whatever order they arrive.
func (s span) widen(o span) span {
	if o.first == 0 && o.last == 0 {
		return s
	}
	if s.first == 0 || o.first < s.first {
		s.first = o.first
	}
	if o.last > s.last {
		s.last = o.last
	}
	return s
}

// pageLines reads every page into lines, splitting each page into its columns
// first.
//
// The gutter is found for the page when the page shows one and for the paper when
// it does not, which is the one thing here that a page at a time cannot do. A two
// column paper is two column on every page of its body, so a page whose own gutter
// is buried under a wide table or a bibliography the typesetter let run long still
// gets cut in the right place. Pages that disagree with the paper are read as one
// column, which is what the title page is.
func pageLines(pages []string) [][]printed {
	sheets := make([][][]rune, 0, len(pages))
	cuts := make([]int, 0, len(pages))
	for _, page := range pages {
		rows := rowsOf(page)
		sheets = append(sheets, rows)
		cuts = append(cuts, gutter(rows))
	}
	fallback := middling(cuts)
	out := make([][]printed, 0, len(pages))
	for i, rows := range sheets {
		at := cuts[i]
		if at == 0 {
			at = fallback
		}
		var got []printed
		for n, col := range sliced(rows, at) {
			for _, raw := range strings.Split(col, "\n") {
				trimmed := strings.TrimLeft(raw, " \t")
				got = append(got, printed{
					text:   strings.TrimRight(trimmed, " \t"),
					indent: len([]rune(raw)) - len([]rune(trimmed)),
					page:   i + 1,
					column: n + 1,
				})
			}
		}
		out = append(out, got)
	}
	return out
}

// Columns splits one page into the columns it was printed in.
//
// The one piece of position information this path has and the one that matters
// most. pdftotext -layout keeps a two column page's columns apart on the page
// and not apart in the file: every line of the file holds a line of the left
// column, a run of spaces where the gutter was, and a line of the right column.
// Read in file order that is the two columns interleaved a line at a time, which
// is unreadable as prose, and every heading in the right column comes out glued
// to the end of a sentence in the left one.
//
// A line with a character at the cut was printed full width and is kept whole in
// the first column, because a table caption in the wrong place is a much smaller
// loss than a table caption cut down the middle.
//
// Exported because it is the one piece of this file that is worth looking at on
// its own when a paper comes out wrong, and because a page nobody can split is
// the first thing to check. It reads the one page it is given, so it is what the
// paper wide pass is built out of and not the other way round.
func Columns(page string) []string {
	rows := rowsOf(page)
	return sliced(rows, gutter(rows))
}

// rowsOf is a page as its lines, with the trailing space taken off each so that
// the length of a line is where its text ends.
func rowsOf(page string) [][]rune {
	var rows [][]rune
	for _, l := range strings.Split(page, "\n") {
		rows = append(rows, []rune(strings.TrimRight(l, " \t")))
	}
	return rows
}

// sliced cuts a page at one column, or hands it back whole when there is nowhere
// to cut it or when this page does not look like the rest of the paper.
//
// One column and not a band, which is the correction that makes this work on a real
// paper. A band has to be wide enough to hold the gutter on every page and the
// gutter is a character or two further over on some of them, so a band wide enough
// for the paper reaches into the right hand column on half its pages, and every
// line it reaches into is then read as full width and left interleaved. A single
// column in the middle of the quiet run is inside the gutter on every page that has
// one, and the leading space it leaves on the right hand column is the indent that
// column is measured by anyway.
func sliced(rows [][]rune, at int) []string {
	if at == 0 || twoColumn(rows, at) < fewestLines {
		return []string{joinRows(rows)}
	}
	var left, right strings.Builder
	for _, r := range rows {
		c := nearest(r, at)
		if c < 0 {
			left.WriteString(strings.TrimRight(string(r), " "))
			left.WriteString("\n")
			right.WriteString("\n")
			continue
		}
		left.WriteString(strings.TrimRight(cut(r, 0, c), " "))
		left.WriteString("\n")
		right.WriteString(strings.TrimRight(cut(r, c, len(r)), " "))
		right.WriteString("\n")
	}
	return []string{left.String(), right.String()}
}

// nearest moves the paper's cut onto the gap this row has, and says -1 when the row
// has none there.
//
// The column a paper is cut at is one column for the whole paper and the gutter of
// a page is not that tidy. A right hand column starts a character or two further
// over on the rows whose first word is set in italic, on the rows pdftotext rounded
// the other way, and on every row of a page whose figure pushed the column across,
// so the paper's cut lands on the first letter of the right hand column on some
// rows of most pages. Taken literally that row is full width, and a row of a
// reference list read as full width is two entries run into one line.
//
// What is asked of the gap is that it be a gap and not a word space: a blank run of
// at least the width a gutter has to be, within a few columns of where the paper
// said to cut. A line set the whole way across the page has word spaces near the
// cut and no run of that width, which is how one is told apart from the other.
func nearest(r []rune, at int) int {
	if !crosses(r, at) {
		return at
	}
	for d := 1; d <= slack; d++ {
		for _, c := range [2]int{at - d, at + d} {
			if c < 0 || c >= len(r) || crosses(r, c) {
				continue
			}
			lo, hi := c, c
			for lo > 0 && !crosses(r, lo-1) {
				lo--
			}
			for hi < len(r) && !crosses(r, hi) {
				hi++
			}
			if hi-lo >= gutterWidth {
				return c
			}
		}
	}
	return -1
}

func joinRows(rows [][]rune) string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = string(r)
	}
	return strings.Join(out, "\n")
}

// twoColumn counts the lines of a page that were printed as two, which is a line
// carrying text on both sides of the cut and nothing at it.
//
// This is what keeps the title page out. A page cut where the paper is cut has
// dozens of these and a page that merely has a wide left margin has none, and the
// count says which without asking anything about what the page holds.
func twoColumn(rows [][]rune, at int) int {
	n := 0
	for _, r := range rows {
		if inked(r, 0, at) && inked(r, at+1, len(r)) && !crosses(r, at) {
			n++
		}
	}
	return n
}

// middling is the middle of the cuts the pages agreed on, and nothing when none of
// them found one.
//
// The middle and not the average, because the pages that disagree disagree wildly:
// a page of full width plates has no gutter at all and a page with one wide table
// has one four characters off. A median over the pages that found one lands inside
// the gutter the typesetter actually used.
func middling(cuts []int) int {
	var found []int
	for _, c := range cuts {
		if c > 0 {
			found = append(found, c)
		}
	}
	if len(found) == 0 {
		return 0
	}
	sort.Ints(found)
	return found[len(found)/2]
}

// gutter is the column a page printed in two columns should be cut at, and nought
// when the page was printed in one.
//
// Searched for in the middle half of the page only. The blank margins down both
// edges are wider than any gutter and a run found there would split a one column
// page into a page and nothing.
//
// What is counted at each column is not how much ink is in it but how much ink is
// in it compared with how many lines have text on both sides of it. That
// distinction is the whole of this function. A column of ink measured on its own
// says a bibliography page with six wide entries has no gutter, because six lines
// of ink is more than a fixed threshold allows; measured against the forty lines
// that are plainly in two columns, six is the exception the gutter survives.
func gutter(rows [][]rune) int {
	width, filled := 0, 0
	for _, r := range rows {
		if len(r) > width {
			width = len(r)
		}
		if len(r) > 0 {
			filled++
		}
	}
	if width < narrowest || filled < fewestLines {
		return 0
	}
	lo, hi := width/4, width*3/4
	quiet := make([]bool, width)
	for c := lo; c < hi; c++ {
		both, hit := 0, 0
		for _, r := range rows {
			if !inked(r, 0, c) || !inked(r, c+1, len(r)) {
				continue
			}
			both++
			if crosses(r, c) {
				hit++
			}
		}
		quiet[c] = both >= fewestLines && float64(hit) <= float64(both)*gutterCross
	}
	best, start, end := 0, 0, 0
	for c := lo; c < hi; {
		if !quiet[c] {
			c++
			continue
		}
		j := c
		for j < hi && quiet[j] {
			j++
		}
		if j-c > best {
			best, start, end = j-c, c, j
		}
		c = j
	}
	if best < gutterWidth {
		return 0
	}
	return (start + end) / 2
}

// inked says a stretch of a line has a character in it.
func inked(r []rune, from, to int) bool {
	for i := max(from, 0); i < to && i < len(r); i++ {
		if r[i] != ' ' && r[i] != '\t' {
			return true
		}
	}
	return false
}

// crosses says a line has a character at the cut, so it was printed full width.
func crosses(r []rune, at int) bool {
	return at < len(r) && r[at] != ' ' && r[at] != '\t'
}

// cut is one column's share of a line, with the leading space kept so that the
// indent inside the column can still be measured.
func cut(r []rune, from, to int) string {
	if from >= len(r) {
		return ""
	}
	return string(r[from:min(to, len(r))])
}

// stampOf is arXiv's margin line, which is the only statement inside a PDF about
// which version of a paper it is.
func stampOf(pages [][]printed) string {
	for _, page := range pages {
		for _, l := range page {
			if stamp.MatchString(l.text) {
				return l.text
			}
		}
	}
	return ""
}

// furniture is the lines that were printed page after page and are not part of
// the paper: the journal's running head, the paper's own title repeated at the
// top of every page, and the page number.
//
// Found by repetition and not by position, because there is no reliable position
// to find them at. A running head is at the top of a page, a page number is at
// either end depending on the style, and a page holding a wide figure has both
// pushed somewhere else. Only the first and last few lines of each page are
// considered, so a sentence that happens to be printed twice in the body is
// never taken for a header.
//
// Numbers are taken out before counting, which is what makes a page number
// count as the same furniture on every page it differs on.
func furniture(pages [][]printed) map[string]bool {
	if len(pages) < repeats {
		return nil
	}
	seen := map[string]int{}
	for _, page := range pages {
		// Each column has two ends of its own. The running head of a two column
		// journal is printed across the whole width of the page on some pages and
		// inside the right hand column on others, and on the pages where it is
		// inside the column it is nowhere near either end of the page.
		columns := map[int][]printed{}
		var order []int
		for _, l := range page {
			if l.text == "" {
				continue
			}
			if _, ok := columns[l.column]; !ok {
				order = append(order, l.column)
			}
			columns[l.column] = append(columns[l.column], l)
		}
		on := map[string]bool{}
		for _, n := range order {
			live := columns[n]
			for i, l := range live {
				if i >= edge && i < len(live)-edge {
					continue
				}
				// A long line at the edge of a page is the last sentence of a
				// paragraph that happened to end there, and no running head is that
				// long.
				if len(l.text) > 80 {
					continue
				}
				on[normal(l.text)] = true
			}
		}
		for k := range on {
			seen[k]++
		}
	}
	out := map[string]bool{}
	for k, n := range seen {
		// The empty line is not furniture and everything else that repeats at the
		// edge of three pages is, down to and including the bare "#" a page number
		// normalises to, which is the commonest piece of furniture there is.
		if n >= repeats && k != "" {
			out[k] = true
		}
	}
	return out
}

// normal is a line with its numbers taken out and its spaces collapsed, which is
// the form furniture is counted in.
func normal(s string) string {
	return digits.ReplaceAllString(strings.Join(strings.Fields(s), " "), "#")
}

// chunk is a run of lines that were printed as one paragraph.
type chunk struct {
	lines []printed
	first int
	last  int
	// before and after say the typesetter left a gap above and below this
	// paragraph, counting the top and the bottom of the page as a gap. They are
	// what tells an unnumbered heading apart from a line of prose that happened to
	// be cut short, because a heading is set apart on both sides and a line of a
	// paragraph never is.
	before bool
	after  bool
}

// text is the paragraph as one line, with the typesetter's hyphens taken out.
//
// A word broken across a line end is joined back up when the second half starts
// with a lower case letter. That is right most of the time and it is wrong for a
// real hyphen at a line end, so "well-known" broken across two lines comes back
// as "wellknown" now and then. The alternative is leaving every broken word
// broken, which is wrong far more often, and a dictionary to tell the two apart
// is not something this path is going to carry.
func (c chunk) text() string {
	var parts []string
	for _, l := range c.lines {
		s := l.text
		if s == "" {
			continue
		}
		if n := len(parts); n > 0 {
			last := parts[n-1]
			if strings.HasSuffix(last, "-") && !strings.HasSuffix(last, "--") && startsLower(s) {
				parts[n-1] = strings.TrimSuffix(last, "-") + s
				continue
			}
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

// raw is the paragraph with its lines kept apart and its indents kept, which is
// how flattened mathematics is published.
//
// The shape is all there is. The characters of a broken equation are in the
// order they were printed and not in the order they were written, so the one
// thing that helps a reader work out what it said is seeing which of them were
// printed above which.
func (c chunk) raw() string {
	least := -1
	for _, l := range c.lines {
		if l.text == "" {
			continue
		}
		if least < 0 || l.indent < least {
			least = l.indent
		}
	}
	var out []string
	for _, l := range c.lines {
		if l.text == "" {
			continue
		}
		out = append(out, strings.Repeat(" ", l.indent-least)+l.text)
	}
	return strings.Join(out, "\n")
}

func startsLower(s string) bool {
	for _, r := range s {
		return unicode.IsLower(r)
	}
	return false
}

// paragraphs groups the lines of a paper into the paragraphs they were printed
// as.
//
// The signal is the first line indent and not the blank line, and that is the one
// decision in this file worth defending before anybody changes it. Two column
// prose read off a PDF has blank lines all through the middle of its paragraphs: a
// superscript or a denominator in the other column is printed on a line of its
// own, and cutting the page at the gutter leaves a gap opposite it. LaTeX indents
// the first line of every paragraph but the first in a section and that indent
// survives -layout exactly, which makes it the one reliable signal on the page.
//
// The indent is measured against the line below and not against the paper, which
// is the correction that makes it work. A paper has several body indents at once:
// the abstract is set as an indented block, a quotation is indented further, a
// footnote is set narrower. Measured against one number for the whole paper, every
// line of the abstract is a new paragraph. Measured against the line under it, a
// first line indent is what it looks like on the page, which is one line sticking
// in further than the one below it.
//
// A blank line still ends a paragraph when the line before it ended a sentence, or
// when the paragraph so far is a single line that reads as a heading. Those two
// are what carry the papers set with no paragraph indent at all, which is most of
// the ones a journal typeset rather than the author.
func paragraphs(lines []printed) []chunk {
	var out []chunk
	var cur chunk
	// The first line of the paper has the top of the page above it, which is a gap.
	blank := true
	flush := func() {
		if len(cur.lines) > 0 {
			cur.after = blank
			out = append(out, cur)
		}
		cur = chunk{}
	}
	for i, l := range lines {
		if l.text == "" {
			blank = true
			continue
		}
		if len(cur.lines) == 0 || indented(lines, i) || opens(l.text) || starts(l) || strong(cur) || (blank && (ends(cur.text()) || headingLike(cur))) {
			flush()
		}
		if len(cur.lines) == 0 {
			cur.first = l.page
			cur.before = blank
		}
		cur.lines = append(cur.lines, l)
		cur.last = l.page
		blank = false
	}
	// The last line of the paper has the bottom of the page under it.
	blank = true
	flush()
	return out
}

// indented says a line has the first line indent of a new paragraph, which is
// that it starts further in than the line under it.
//
// One character of tolerance, because a justified line can be nudged by the
// rounding pdftotext does when it turns a position on the page into a column in a
// text file. The last line of a paragraph is never an opener under this test,
// because the line under it is the next paragraph's opener and is indented
// further.
func indented(lines []printed, i int) bool {
	if i+1 >= len(lines) {
		return false
	}
	next := lines[i+1]
	// A gap under a line says nothing about the line's indent, and the blank line
	// rule is what reads the gap. A line at the foot of a page says nothing about it
	// either, because the line under it was printed at the top of the next one.
	if next.text == "" || next.page != lines[i].page {
		return false
	}
	return lines[i].indent > next.indent+1
}

// opens says a printed line begins something that is a block of its own whether
// or not the typesetter left a gap above it.
//
// A caption, a theorem and a proof are all set flush left and directly under the
// paragraph above them in plenty of styles. Without this they come out as the
// tail of that paragraph, and a figure whose caption is the last two sentences of
// somebody's argument is worse than no figure at all.
func opens(s string) bool {
	return captionOpening.MatchString(s) || statement.MatchString(s) || proofOpening.MatchString(s)
}

// starts says this line is a heading by what it says, so the paragraph above it
// ends here whether or not the typesetter left a gap between them.
//
// The other half of strong, and the half a two column paper needs. A gap in a
// column is a row where both columns are blank, and a heading in the right hand
// column has the left hand column's prose running alongside it, so the row above
// the heading carries text and the cut leaves the heading directly under the line
// before it with no gap anywhere. Without this the heading is glued to the end of
// the paragraph above and its section is lost.
func starts(l printed) bool {
	return strong(chunk{lines: []printed{l}})
}

// strong says the paragraph so far is a single line that is a heading by what it
// says rather than by where it was printed, which ends it whether or not the
// typesetter left a gap under it.
//
// The gap under a heading is not always there. Plenty of styles set a section
// heading with space above it and none below, so the first line of the section runs
// directly under it, and the gap rule glues the heading to the front of that
// paragraph. A printed number or one of the names papers use is enough on its own
// to cut there, and neither of them is a thing a line of prose ever starts with.
func strong(c chunk) bool {
	if len(c.lines) != 1 {
		return false
	}
	c.before, c.after = true, true
	h, ok := recovered(c, c.text())
	return ok && !h.weak
}

// headingLike says the paragraph so far is a single line that reads as a heading,
// which a blank line under it then ends.
//
// This is what recovers the unnumbered headings and there is no way round it. An
// unnumbered heading in a two column paper is printed flush left with a gap above
// and below and nothing else to mark it, so the indent rule does not see it and
// the sentence rule does not either, because a heading does not end in a full
// stop. Without this every such heading is glued to the front of the paragraph
// under it and its section is lost.
func headingLike(c chunk) bool {
	if len(c.lines) != 1 {
		return false
	}
	// The blank line this is being asked about is the gap under the paragraph, and
	// it has not been recorded on the chunk yet because the chunk is still open.
	c.after = true
	_, ok := recovered(c, c.text())
	return ok
}

// ends says a line finished a sentence.
func ends(s string) bool {
	s = strings.TrimRight(s, `"'”’)]}`)
	if s == "" {
		return false
	}
	switch s[len(s)-1] {
	case '.', '?', '!':
		return true
	}
	return false
}

// build reads the paragraphs into the paper's sections.
//
// One pass, with the current top level section and the current subsection held
// by index rather than by pointer, because appending to a section list moves it
// and a pointer into it would then be writing into the old one.
func build(p *Paper, chunks []chunk) {
	topAt, subAt := -1, -1
	var topSpan, subSpan span
	abstract, bib := false, false
	// The indent the abstract was set at, so the body under it can be told apart
	// from the rest of it. An abstract is set as an indented block in nearly every
	// style there is, and the first line of the body under it goes back out to the
	// margin, which is the only mark on the page that says the abstract has ended.
	// Without this the abstract runs to the first heading the paper prints, and a
	// paper whose first section heading is on page 6 gets six pages of abstract.
	abstractIndent := -1
	// The prose between the end of the abstract and the first heading, held rather
	// than dropped. Papers open with an unheaded paragraph or two often enough that
	// throwing it away loses real text, and it cannot go in the abstract because it
	// is not the abstract.
	var opening []Block
	var openSpan span
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
	// heading this path invents. It invents it because the alternatives are worse:
	// filing the paper's opening paragraphs under the first heading that follows
	// them puts the introduction inside whatever comes after it, and dropping them
	// loses text the author wrote. A reader who sees Introduction on a file whose
	// front matter says path: native knows what that means.
	openings := func(title string) {
		if len(opening) == 0 {
			return
		}
		p.Sections = append(p.Sections, Section{Level: 1, Title: title, Blocks: opening, Pages: openSpan.String()})
		opening, openSpan = nil, span{}
	}
	// Whether the paper prints the word Abstract anywhere, which decides whether
	// the abstract is read off a heading or guessed at. The scan is up front because
	// the guess has to be made before the heading would be reached, and a paper that
	// says where its abstract is should not have one guessed at: the guess is the
	// first long paragraph of the paper, and on a title page with thirty authors on
	// it the first long paragraph is the byline.
	says := false
	for _, c := range chunks {
		if h, ok := recovered(c, c.text()); ok && h.abstract {
			says = true
			break
		}
	}
	chunks, bibAt, bibEnd := referenced(chunks)
	for i, c := range chunks {
		text := c.text()
		// The reference list a paper printed no word above, which is the house style
		// of a whole publisher and not an oddity, and the end of the one it did.
		if i == bibAt && !bib {
			closeTop()
			openings("Introduction")
			abstract, bib = false, true
			abstractIndent = -1
		}
		if i == bibEnd {
			bib = false
		}
		// A heading found inside a reference list is not one. A list of a hundred and
		// sixty entries holds a line that reads like a heading somewhere in it, and
		// letting that line end the list loses every entry under it. This is only
		// safe because the list has an end of its own: the paper it was found in goes
		// on with its funding and its acknowledgments after it, and those headings
		// are real.
		run := bibAt >= 0 && i >= bibAt && i < bibEnd
		if h, ok := recovered(c, text); ok && (!run || h.kind == "appendix") {
			// The title of the paper, which is printed as a short line with a gap on
			// both sides above everything else, and which is not a section. Dropped
			// rather than kept, because the metadata plane holds the title.
			if h.weak && len(p.Abstract) == 0 && !abstract {
				continue
			}
			switch {
			case h.references:
				closeTop()
				openings("Introduction")
				abstract, bib = false, true
				abstractIndent = -1
				continue
			case h.abstract:
				closeTop()
				abstract, bib = true, false
				abstractIndent = -1
				continue
			}
			// Nothing is held once a section is open, so this only ever fires on the
			// first heading of the paper, and it fires before that heading's own
			// section is appended so the opening prose keeps its place in the order.
			openings("Introduction")
			abstract, bib = false, false
			abstractIndent = -1
			if h.level > 1 && topAt >= 0 {
				closeSub()
				p.Sections[topAt].Sections = append(p.Sections[topAt].Sections, h.section())
				subAt = len(p.Sections[topAt].Sections) - 1
				subSpan = subSpan.add(c)
				topSpan = topSpan.add(c)
				continue
			}
			closeTop()
			p.Sections = append(p.Sections, h.section())
			topAt = len(p.Sections) - 1
			topSpan = topSpan.add(c)
			continue
		}
		if bib {
			// The one place a chunk is read line by line rather than as a paragraph.
			// A reference list is set with a hanging indent, which is the opposite
			// of a first line indent, so the paragraph rule cuts an entry after its
			// first line and then runs the rest of it into the entry below. The
			// printed number is what says where an entry starts and nothing else
			// does, and asking that of the lines rather than of the paragraph keeps
			// the question inside the bibliography, where a line opening with "[12]"
			// is a reference and not a citation that happened to wrap.
			for _, l := range c.lines {
				if l.text == "" {
					continue
				}
				n := len(p.Bibliography)
				if n > 0 && !bibLabel.MatchString(l.text) {
					p.Bibliography[n-1].Blocks = append(p.Bibliography[n-1].Blocks, l.text)
					continue
				}
				p.Bibliography = append(p.Bibliography, bibitem("native", n+1, l.text))
			}
			continue
		}
		if abstract && abstractIndent >= 0 && indentOf(c) < abstractIndent {
			abstract = false
		}
		b := blockOf(c, text)
		if topAt < 0 {
			// Everything printed before the first heading is the title block: the
			// title, the byline, the affiliations, a date, and then the abstract.
			// Only the abstract is wanted here, because the metadata plane holds
			// the rest and holds it better, so the first paragraph of real prose
			// is taken and everything above it is dropped.
			if abstract || (!says && len(p.Abstract) == 0 && b.Kind == KindParagraph && len(text) >= abstractFloor && ends(text)) {
				p.Abstract = append(p.Abstract, b)
				p.FrontPages = span{first: c.first, last: c.last}.add(c).String()
				if abstractIndent < 0 {
					abstractIndent = indentOf(c)
				}
				continue
			}
			// Past the abstract and still above the first heading, so this is the
			// paper opening without a heading of its own. Held, not dropped, and
			// only once there is an abstract to be past: above one, this is still
			// the byline and the affiliations.
			if len(p.Abstract) > 0 {
				opening = append(opening, b)
				openSpan = openSpan.add(c)
			}
			continue
		}
		topSpan = topSpan.add(c)
		if subAt >= 0 {
			subSpan = subSpan.add(c)
			p.Sections[topAt].Sections[subAt].Blocks = append(p.Sections[topAt].Sections[subAt].Blocks, b)
			continue
		}
		p.Sections[topAt].Blocks = append(p.Sections[topAt].Blocks, b)
	}
	closeTop()
	// A paper that printed no heading this path could recover, which happens with a
	// short note and with a scan clean enough to have a text layer. Its body is
	// whatever was held, and it is called Body because that is the name the splitter
	// gives the one file of a paper with no sections.
	openings("Body")
}

// indentOf is the indent of a paragraph, which is the indent of its second line
// and not of its first.
//
// The first line of a paragraph is the indented one, and comparing that against
// the block above would say every paragraph in the paper is set further in than
// the one before it. A paragraph of one line has nothing but its first line and
// that is what gets used.
// referenced finds the reference list of a paper that prints no word above it,
// and hands back the chunks with that list starting and ending at a chunk of its
// own.
//
// Two whole publishers set a reference list with no heading over it. A rule runs
// across the column, the entries start under it, and the only thing on the page
// that says so is the printed numbers, so a paper read this way loses its whole
// bibliography into the body unless the numbers are read.
//
// What is asked for to start one is three entries numbered one, two and three in
// that order in the back half of the paper. A single [1] at the start of a line is
// a citation that wrapped and there is one of those on most pages, a run of three
// counted up from one is a reference list and is nothing else, and the back half is
// where a paper keeps one.
//
// Where it ends matters as much as where it starts, and for a reason that only
// shows up on a real paper. A list of a hundred and sixty entries holds a line that
// reads like a heading somewhere in it, so a heading cannot be what ends the list.
// But a paper in the style Nature prints prints its references in the middle and
// then goes on with its funding, its author contributions and its competing
// interests, so the list cannot run to the end of the paper either. The run of
// ascending numbers is what says how far it goes: it ends at the last entry whose
// number is higher than the one before it and which is close enough behind it to be
// the same list.
//
// Either end can fall in the middle of a chunk, because the chunk above the list is
// the last paragraph of the acknowledgments and nothing in the text of either says
// where one ends and the other begins. Those chunks are cut so the caller can treat
// the halves as what they are.
func referenced(chunks []chunk) ([]chunk, int, int) {
	total := 0
	for _, c := range chunks {
		total += len(c.lines)
	}
	type mark struct{ at, n int }
	var marks []mark
	seen := 0
	for _, c := range chunks {
		for j, l := range c.lines {
			if n, ok := bibNumber(l.text); ok {
				marks = append(marks, mark{at: seen + j, n: n})
			}
		}
		seen += len(c.lines)
	}
	opens, from := -1, -1
	for k, m := range marks {
		if m.n != 1 || m.at < total/2 || k+2 >= len(marks) {
			continue
		}
		if marks[k+1].n == 2 && marks[k+2].n == 3 {
			opens, from = k, m.at
			break
		}
	}
	if from < 0 {
		return chunks, -1, -1
	}
	// The last entry of the list, found by counting up. A number lower than the one
	// before it is an inline citation that a row of two columns ran into the middle
	// of an entry, so it is stepped over rather than being the end of anything.
	last, high := from, 1
	for _, m := range marks[opens+1:] {
		if m.at-last > runOn {
			break
		}
		if m.n <= high {
			continue
		}
		high, last = m.n, m.at
	}
	out, at := divided(chunks, from, last+1)
	return out, at[0], at[1]
}

// divided cuts chunks at the given line offsets and says which chunk each offset
// became the start of, or that it is past the end.
func divided(chunks []chunk, offsets ...int) ([]chunk, []int) {
	want := make(map[int]bool, len(offsets))
	for _, o := range offsets {
		want[o] = true
	}
	out := make([]chunk, 0, len(chunks)+len(want))
	index := make(map[int]int, len(want))
	seen := 0
	for _, c := range chunks {
		start, off := seen, 0
		seen += len(c.lines)
		for j := 1; j < len(c.lines); j++ {
			if !want[start+j] {
				continue
			}
			index[start+off] = len(out)
			out = append(out, piece(c, off, j))
			off = j
		}
		index[start+off] = len(out)
		out = append(out, piece(c, off, len(c.lines)))
	}
	said := make([]int, len(offsets))
	for i, o := range offsets {
		if k, ok := index[o]; ok {
			said[i] = k
			continue
		}
		// An offset nobody landed on is the end of the last line of the paper, which
		// is a list that runs to the last page.
		said[i] = len(out)
	}
	return out, said
}

// piece is the part of a chunk between two of its lines.
//
// The gap above and the gap below belong to the whole chunk, so only the outer ends
// of the pieces keep them: a cut in the middle of a paragraph has no gap at it,
// which is what made it a paragraph.
func piece(c chunk, from, to int) chunk {
	out := c
	out.lines = c.lines[from:to]
	out.first, out.last = c.lines[from].page, c.lines[to-1].page
	if from > 0 {
		out.before = false
	}
	if to < len(c.lines) {
		out.after = false
	}
	return out
}

// bibNumber reads the printed number a reference list marks its entries with.
func bibNumber(s string) (int, bool) {
	m := bibLabel.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

func indentOf(c chunk) int {
	if len(c.lines) > 1 {
		return c.lines[1].indent
	}
	if len(c.lines) == 1 {
		return c.lines[0].indent
	}
	return 0
}

// heading is a recovered section heading.
type heading struct {
	tag   string
	title string
	level int
	kind  string
	// references and abstract are the two headings that are not sections of the
	// paper. What follows the first is the bibliography, which is published as
	// the paper's reference list and not as a section of its body, and what
	// follows the second is the abstract, which belongs in the front matter.
	references bool
	abstract   bool
	// weak says this heading was recovered from the shape of the line alone, with
	// no printed number and no name off the list, which is the rule that is
	// sometimes wrong. The one place that matters is the title page, where the title
	// itself is a short line with a gap on both sides and is a heading by every test
	// here and is not a section of the paper.
	weak bool
}

func (h heading) section() Section {
	return Section{
		Kind:  h.kind,
		Level: h.level,
		Tag:   h.tag,
		Title: h.title,
	}
}

// recovered decides whether a paragraph is a heading, and what kind.
//
// Three rules in order of how much they can be trusted. A printed number in
// front of a short line is a heading and the number says what depth it is at. A
// line that is one of the names papers use with no number is a heading at the
// top level. And a short line that ends in nothing, starts with a capital and
// holds few enough words is a heading because that is what headings look like,
// which is the rule that is sometimes wrong and is the reason this path records
// path: native.
func recovered(c chunk, text string) (heading, bool) {
	if len(c.lines) != 1 {
		return heading{}, false
	}
	text = strings.TrimSpace(text)
	if text == "" || len([]rune(text)) > 60 {
		return heading{}, false
	}
	if m := appendix.FindStringSubmatch(text); m != nil && strings.EqualFold(strings.Fields(text)[0], "appendix") {
		return heading{tag: m[1], title: strings.TrimSpace(m[2]), level: 1, kind: "appendix"}, true
	}
	if m := numbered.FindStringSubmatch(text); m != nil && plainHeading(m[2]) {
		return heading{tag: m[1], title: m[2], level: strings.Count(m[1], ".") + 1}, true
	}
	if m := roman.FindStringSubmatch(text); m != nil && plainHeading(m[2]) {
		return heading{tag: m[1], title: m[2], level: 1}, true
	}
	lower := strings.ToLower(strings.TrimRight(text, ".:"))
	if _, ok := named[lower]; ok {
		return heading{
			title:      text,
			level:      1,
			references: lower == "references" || lower == "bibliography",
			abstract:   lower == "abstract",
		}, true
	}
	// Last and weakest, so it asks for the gap on both sides as well. A numbered
	// heading and a heading with one of the names above are both saying what they
	// are in the text of the line, and a line that says nothing of the kind is a
	// heading only because of where it was printed, which means where it was
	// printed has to be checked.
	if c.before && c.after && plainHeading(text) {
		return heading{title: text, level: 1, weak: true}, true
	}
	return heading{}, false
}

// plainHeading says a line looks like a heading with no number on it.
//
// Short, few words, opens with a capital, and ends in none of the punctuation a
// sentence ends in. The word count is what keeps the last line of a paragraph
// out: a line that finished a sentence has been excluded by the punctuation, and
// a line that did not is usually longer than a heading.
//
// A comma anywhere in the line disqualifies it, which is the rule that keeps the
// title page out. An affiliation is a list and it is printed as one, so
// "Enthought, Inc., Austin, TX, USA" is five words, opens with a capital and ends
// in a letter, and it passes every other test here. Headings with a comma in them
// exist and this loses them, and that is the cheaper of the two mistakes: a lost
// heading leaves its prose under the heading above, and an affiliation taken for
// a heading puts a section break in the middle of a byline.
func plainHeading(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || len([]rune(s)) > 60 {
		return false
	}
	words := strings.Fields(s)
	if len(words) == 0 || len(words) > 8 {
		return false
	}
	if strings.ContainsAny(s, ",;") {
		return false
	}
	switch s[len(s)-1] {
	case '.', ',', ';', ':', '!', '?', '-':
		return false
	}
	first := []rune(s)[0]
	if !unicode.IsUpper(first) {
		return false
	}
	// A heading is words. A line that is mostly symbols is what is left of a
	// piece of mathematics that happened to be printed short.
	return !mathematical(s)
}

// blockOf reads one paragraph into the block it is.
func blockOf(c chunk, text string) Block {
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
		// The caption and nothing else. A figure's picture is in the PDF as a
		// drawing and not as a file, so there is no image to name, and a table's
		// cells come back as text in columns that nothing here can tell apart
		// from a paragraph with a lot of spaces in it. Recording that the float
		// was there, with the number the paper printed and the words the author
		// wrote under it, is the whole of what this path can honestly say.
		return Block{Kind: kind, Tag: m[2], Caption: m[3]}
	}
	if m := proofOpening.FindStringSubmatch(text); m != nil {
		return Block{Kind: KindProof, Title: m[1], Blocks: []Block{{Kind: KindParagraph, Text: m[2]}}}
	}
	if m := statement.FindStringSubmatch(text); m != nil {
		return Block{Kind: KindTheorem, Env: strings.ToLower(m[1]), Tag: m[2], Text: m[3]}
	}
	if flattened(c) {
		return Block{Kind: KindNote, Title: "Displayed mathematics", Text: c.raw()}
	}
	return Block{Kind: KindParagraph, Text: unmarked(text)}
}

// unmarked puts a backslash in front of a first character Markdown would read as
// the start of a block.
//
// The paths above this one hand the writer Markdown, because they read a
// document that already had emphasis and links in it. This path hands it prose,
// so a character that means something to Markdown means nothing here and has to
// be stopped from meaning something in the file. It is not a hypothetical: a
// Python comment inside a listing that pdftotext flattened into a paragraph
// begins with a hash, and a paragraph that begins with a hash is a heading, so
// the NumPy paper published a code comment as a section title of the corpus.
//
// Only the first character, and only in a paragraph. A hash in the middle of a
// line is a hash, the rest of Markdown's markers only mean anything at the start
// of a line, and the lines inside a paragraph are joined into one.
func unmarked(text string) string {
	switch {
	case text == "":
		return text
	case strings.HasPrefix(text, "#"), strings.HasPrefix(text, ">"), strings.HasPrefix(text, "|"), strings.HasPrefix(text, "```"):
		return `\` + text
	}
	// A dash, a star or a digit and a dot is a list to Markdown only when a space
	// follows it, which is what keeps a minus sign and a numbered sentence alone.
	if m := listOpening.FindString(text); m != "" {
		return `\` + text
	}
	return text
}

// listOpening is what Markdown reads as the first line of a list.
var listOpening = regexp.MustCompile(`^([-*+]|[0-9]{1,9}[.)])\s`)

// flattened says a paragraph is what is left of a piece of display mathematics.
//
// Every line of it has to look like mathematics and there have to be few enough
// of them. A longer run of lines that all look like mathematics is a table of
// numbers, and a table is not an equation.
func flattened(c chunk) bool {
	n := 0
	for _, l := range c.lines {
		if l.text == "" {
			continue
		}
		if !mathematical(l.text) {
			return false
		}
		n++
	}
	return n > 0 && n <= mathLines
}

// mathematical says a line is what a piece of mathematics looks like once it has
// been printed and read back.
//
// Measured and not pattern matched, because there is no pattern left. The test
// is the share of the line that is letters of an alphabet somebody writes prose
// in: a sentence is nearly all letters and spaces, and an equation is digits,
// operators, brackets, Greek and single letter variables. The line also has to
// be short, because a long line of mostly symbols is a row of a table.
func mathematical(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	letters, total, sign := 0, 0, false
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		total++
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			letters++
		case operator(r):
			sign = true
		}
	}
	if total == 0 || !sign {
		return false
	}
	return float64(letters) < mathLetters*float64(total)
}

// operator says a rune is one of the things only mathematics is written with.
func operator(r rune) bool {
	switch r {
	case '=', '+', '<', '>', '/', '^', '_', '±', '×', '÷', '∑', '∏', '∫', '≤', '≥', '≠', '≈', '∈', '→', '∞', '∂', '√':
		return true
	}
	switch {
	case r >= 0x0370 && r <= 0x03ff: // Greek, which is most of the variables.
		return true
	case r >= 0x2190 && r <= 0x22ff: // Arrows and mathematical operators.
		return true
	}
	return false
}

// bibitem reads one printed reference.
//
// Parsed no further than the number the paper printed in front of it. What the
// rest of the line means depends on the style the author used, and ax refs build
// is what reads it, exactly as it does for the two paths above this one.
//
// The prefix is the path that read it, which goes into the identifier because the
// two paths that read a printed page share this function and an entry that called
// itself native in a paper read off pictures would be an entry saying a thing that
// is not so.
func bibitem(prefix string, n int, text string) Bibitem {
	b := Bibitem{ID: fmt.Sprintf("%s.bib%d", prefix, n)}
	if m := bibLabel.FindStringSubmatch(text); m != nil {
		b.Label = "[" + m[1] + "]"
		b.Blocks = []string{m[2]}
		return b
	}
	b.Blocks = []string{text}
	return b
}

// encodingOf says whether the PDF's text layer has a broken font encoding, and
// nothing when it does not.
//
// A real and common failure, and one worth naming because it is invisible until
// somebody reads the output. A PDF built with a Type 1 font that carries its own
// character map, which is what several journals still produce, has a text layer
// where the character codes are the font's and not Unicode's. The commonest one
// puts "¼" where the paper printed "=" and "þ" where it printed "+", so an
// equation comes back as "M ¼ 1.188þ0.004" and every sentence around it reads
// perfectly. Nothing here fixes it, because fixing it means guessing at one
// publisher's font map and getting it wrong on the next, and a paper this fires
// on is a paper for the vision path.
func encodingOf(chunks []chunk) string {
	odd, total := 0, 0
	for _, c := range chunks {
		for _, r := range c.text() {
			if unicode.IsSpace(r) {
				continue
			}
			total++
			// The vulgar fractions and the Icelandic letters, which are where
			// the commonest broken map puts the operators, and the control
			// characters, which no font prints at all and which is what a plus
			// or a minus sign comes back as when the map is wrong.
			switch {
			case r == '¼', r == '½', r == '¾', r == 'þ', r == 'ð', r == 'ÿ':
				odd++
			case r < 0x20:
				odd++
			}
		}
	}
	// One in a thousand. A paper that prints a fraction or a Scandinavian name
	// has a handful of these honestly, and a paper whose font map is wrong has
	// one everywhere an operator was printed.
	if total == 0 || odd*1000 < total {
		return ""
	}
	return fmt.Sprintf("the text layer holds %d characters out of %d that are what a broken Type 1 font map prints instead of an operator, so the mathematics in this paper is mojibake and not just flattened, and this one is worth reading on the vision path", odd, total)
}

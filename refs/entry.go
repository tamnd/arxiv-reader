// Package refs turns a paper's reference list into structured entries.
//
// An entry becomes an edge in the graph when it resolves to something the
// corpus knows about, and it is published as a bibliography line either way. A
// reference to a textbook resolves to nothing and that is a fine outcome, so
// nothing in here treats an unresolved entry as a failure.
//
// The parsing is by pattern and never by a model. A bibliography is the most
// regular prose in a paper: the style file laid it out and the same style file
// laid out every other entry in the same list. A model asked to read one would
// cost more than the rest of this pipeline put together and would be wrong in
// ways that are harder to find, because it would be wrong in prose.
package refs

import (
	"regexp"
	"strings"

	"github.com/tamnd/arxiv-reader/extract"
)

// Entry is one reference, read into fields.
//
// Text is kept alongside the fields and is what gets published. The fields are
// a reading of a bibliography style and any of them can be wrong or empty; the
// line the paper printed cannot be, so the reader sees that and the resolver
// works from the rest.
type Entry struct {
	// ID is LaTeXML's anchor, "bib.bibx106", which every citation in the paper
	// points at. It is the key inside this paper and it means nothing outside.
	ID string `yaml:"id"`
	// Label is the refnum the paper prints, "Vaswani et al. (2017)" or "[106]".
	Label   string   `yaml:"label,omitempty"`
	Authors []string `yaml:"authors,omitempty"`
	Title   string   `yaml:"title,omitempty"`
	Venue   string   `yaml:"venue,omitempty"`
	Year    int      `yaml:"year,omitempty"`
	// ArXiv is the id this entry names, when it names one. It is the first
	// thing the resolver tries and it is nearly free, because a modern paper
	// citing a preprint prints the id in the entry.
	ArXiv string `yaml:"arxiv,omitempty"`
	DOI   string `yaml:"doi,omitempty"`
	URL   string `yaml:"url,omitempty"`
	// Text is the whole entry as the paper printed it, which is what is
	// published and what a person checks the fields against.
	Text string `yaml:"text"`
}

// Read turns the rendering's bibliography into entries.
func Read(items []extract.Bibitem) []Entry {
	out := make([]Entry, 0, len(items))
	for _, it := range items {
		out = append(out, entry(it))
	}
	return out
}

func entry(it extract.Bibitem) Entry {
	e := Entry{ID: it.ID, Label: it.Label, Text: it.Text()}
	blocks := it.Blocks
	// The title is the block in quotation marks. Every BibTeX style this has
	// been run against quotes it, and finding it that way rather than by
	// position survives the styles that put the year before the title.
	title := -1
	for i, b := range blocks {
		if s, ok := quoted(b); ok {
			e.Title, title = s, i
			break
		}
	}
	switch {
	case title > 0:
		// Everything before the quoted title is the author block, and
		// everything after it is the venue.
		e.Authors = people(strings.Join(blocks[:title], " "))
		e.Venue = plain(strings.Join(blocks[title+1:], " "))
	case title == 0:
		e.Venue = plain(strings.Join(blocks[1:], " "))
	case len(blocks) > 2:
		// No quotation marks, which is what the numeric styles do. Author,
		// then title, then venue, one block each, and that layout is the same
		// in plain, unsrt, abbrv and plainnat. A book puts the title in
		// italics and a paper does not, so the emphasis cannot be what tells
		// them apart and the position is.
		e.Authors = people(blocks[0])
		e.Title = strings.TrimRight(plain(blocks[1]), ".")
		e.Venue = plain(strings.Join(blocks[2:], " "))
	case len(blocks) == 2:
		// Two blocks is an author line and everything else. Guessing which
		// half of the second block is the title would be inventing a field.
		e.Authors = people(blocks[0])
		e.Venue = plain(blocks[1])
	}
	// The year comes from what the entry printed and the locators come from
	// what it printed plus what it linked to. A DOI is printed as its own text
	// and linked to dx.doi.org, so reading the locators off the raw Markdown
	// gets the DOI with the link syntax still stuck to the end of it, and
	// reading them off the printed text alone loses the href of a link whose
	// text is a title. The year is read without the hrefs because a conference
	// URL ends in something like 2021.acl-long.568 and that is not a year.
	e.Year = year(plain(e.Text))
	loc := locators(e.Text)
	e.ArXiv = arxivID(loc)
	e.DOI = doi(loc)
	e.URL = url(loc)
	return e
}

// locators is the entry with every link written out as its text and then its
// target, which is the form the id patterns are run over.
func locators(s string) string {
	return strings.Join(strings.Fields(mdLink.ReplaceAllString(s, "$1 $2")), " ")
}

// quoted is the text inside quotation marks, if the block is one.
//
// Both the curly quotes LaTeXML emits and the straight ones an author types,
// because a bibliography written by hand uses whatever was on the keyboard.
func quoted(s string) (string, bool) {
	s = plain(strings.TrimSpace(s))
	for _, q := range []struct{ open, shut string }{
		{"“", "”"},
		{`"`, `"`},
	} {
		if !strings.HasPrefix(s, q.open) {
			continue
		}
		if i := strings.LastIndex(s, q.shut); i > len(q.open)-1 {
			return strings.TrimRight(strings.TrimSpace(s[len(q.open):i]), ",."), true
		}
	}
	return "", false
}

// people splits an author block into names.
//
// Split on the separators a style prints and not on anything cleverer. The
// resolver matches surnames, so a name that comes out as "Aidan. Gomez" with
// the initial stuck to it still matches on Gomez, and a block this splits
// badly costs one candidate rather than a wrong answer.
var andSep = regexp.MustCompile(`(?i),?\s+and\s+|,\s*|\s+&\s+`)

func people(s string) []string {
	s = plain(strings.TrimSpace(s))
	if s == "" {
		return nil
	}
	var out []string
	for _, name := range andSep.Split(s, -1) {
		name = strings.Trim(strings.TrimSpace(name), ".,")
		// A one letter piece is an initial the split cut off a name, and a
		// piece with no letter in it is punctuation the style printed.
		if len([]rune(name)) < 2 || !hasLetter(name) {
			continue
		}
		out = append(out, name)
	}
	return out
}

func hasLetter(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool {
		return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r > 127
	}) >= 0
}

// yearPattern is a four digit year in the range a paper can cite.
//
// Anchored on the century rather than on four digits, because a bibliography is
// full of page ranges, volume numbers and conference names with numbers in
// them, and 1120 as in pages 1120 to 1128 is not a year.
var yearPattern = regexp.MustCompile(`\b(1[89]\d\d|20\d\d)\b`)

// year is the year the entry states.
//
// The last one in the entry and not the first. A style that prints the year at
// the end prints exactly one, and a style that prints it after the author also
// prints it in the venue, so taking the last is right in both and taking the
// first picks up a volume number that happens to look like a year.
func year(s string) int {
	m := yearPattern.FindAllString(s, -1)
	if len(m) == 0 {
		return 0
	}
	n := 0
	for _, c := range m[len(m)-1] {
		n = n*10 + int(c-'0')
	}
	return n
}

// arxivPattern matches an arXiv id however the entry writes it.
//
// Both id styles, because a bibliography reaches back further than April 2007.
// The optional version is dropped: a citation names a paper and the version it
// names is the one the author had, which is not a fact about the reference.
var arxivPattern = regexp.MustCompile(`(?i)arxiv[:\s/]*((?:\d{4}\.\d{4,5})|(?:[a-z-]+(?:\.[A-Z]{2})?/\d{7}))`)

func arxivID(s string) string {
	m := arxivPattern.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return m[1]
}

// doiPattern matches a DOI wherever it appears.
//
// The trailing punctuation is trimmed rather than excluded by the pattern,
// because a DOI may legitimately contain a full stop and only the last one can
// be the sentence's.
var doiPattern = regexp.MustCompile(`\b10\.\d{4,9}/[^\s"<>]+`)

func doi(s string) string {
	m := doiPattern.FindString(s)
	return strings.TrimRight(m, ".,);]")
}

var urlPattern = regexp.MustCompile(`https?://[^\s)\]]+`)

func url(s string) string {
	return strings.TrimRight(urlPattern.FindString(s), ".,;")
}

// plain takes the Markdown emphasis off a block.
//
// The markup told the fields apart and has done its job by the time a field is
// stored, and a venue kept as *Nature Methods* would be compared against a
// metadata plane that holds it as Nature Methods.
var emphasis = regexp.MustCompile(`\*{1,2}([^*]+)\*{1,2}`)

func plain(s string) string {
	s = emphasis.ReplaceAllString(s, "$1")
	s = linkText(s)
	return strings.Join(strings.Fields(s), " ")
}

var mdLink = regexp.MustCompile(`\[([^\]]*)\]\(([^)]*)\)`)

// linkText turns a Markdown link into its text, keeping the target when the
// text is empty, which is what a bare URL in a reference comes out as.
func linkText(s string) string {
	return mdLink.ReplaceAllStringFunc(s, func(m string) string {
		p := mdLink.FindStringSubmatch(m)
		if strings.TrimSpace(p[1]) == "" {
			return p[2]
		}
		return p[1]
	})
}

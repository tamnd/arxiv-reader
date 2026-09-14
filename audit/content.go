package audit

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/extract"
)

// ContentRules are the rules that read a content file.
//
// They cost much more than the metadata rules and there are far fewer things to
// run them over, which is the trade the two planes were split on: every paper
// on arXiv has a record and only the ones somebody extracted have content.
//
// Group T is here in full bar T12. That rule fails a Markdown link left in a
// body, and on the render path a link into the paper's own bibliography is
// deliberately left for ax refs to resolve, so the rule cannot tell a leak from
// work in progress until it can read the bibliography. It arrives with group R.
// The other groups in 2166-10 that read content are not here yet for the same
// reason the S rules that read content were not here before: a rule registered
// before it can run is a rule everybody believes is working.
var ContentRules = []Rule{
	{
		ID: "T01", Group: GroupStructure, Hard: true,
		Says: "every content file parses: front matter, then body",
		Why:  "The file's shape and not its fields, which are T02's. Everything else in this group reads a parsed file, so a file that does not cut in two is reported once here and then stepped over rather than failing nine rules in a row.",
	},
	{
		ID: "T02", Group: GroupStructure, Hard: true,
		Says: "every front matter field is known and typed",
		Why:  "The reader ignores a field it does not know, which is what makes a corpus written by a later version of this tool readable by an earlier one. That tolerance is also how a renamed field goes unnoticed for a year, and this is the one place the tolerance is switched off.",
	},
	{
		ID: "T03", Group: GroupStructure, Hard: true,
		Says: "content_sha256 matches the body as it stands",
		Why:  "The hash is what stops the extractor overwriting a hand correction. A file whose body has moved away from it is a correction nobody has accepted yet, and ax split -accept is what accepts it.",
	},
	{
		ID: "T04", Group: GroupStructure, Hard: true,
		Says: "section numbers within a paper are contiguous from 0",
		Why:  "The numbers are this corpus's own and not the paper's, so a gap is a file that failed to be written rather than a paper that starts at 2.",
	},
	{
		ID: "T05", Group: GroupStructure, Hard: true,
		Says: "the heading tree is well formed: no level skipped",
		Why:  "A file is one section and its title is in the front matter, so the first heading in a body is the level below it. A skipped level is a rendering that reads as an outline nobody wrote.",
	},
	{
		ID: "T06", Group: GroupStructure, Hard: true,
		Says: "every paper has a 00_front.md with an abstract",
		Why:  "It is the file that carries the title, the authors and the licence, and a paper without it is a set of sections with nothing saying whose they are.",
	},
	{
		ID: "T07", Group: GroupStructure, Hard: true,
		Says: "every paper with a reference section has it last, or last before its appendices",
		Why:  "A references section in the middle of a paper is a heading the splitter mistook for a section, and everything after it has been filed under the wrong one.",
	},
	{
		ID: "T08", Group: GroupStructure,
		Says: "no section body under 200 characters, which is a split that went wrong",
		Why:  "Soft, because a one paragraph conclusion is a real thing. The front matter file is not counted, because an abstract is as long as its author made it.",
	},
	{
		ID: "T09", Group: GroupStructure,
		Says: "no section body over 40,000 characters, which is a split that did not happen",
	},
	{
		ID: "T10", Group: GroupStructure, Hard: true,
		Says: "no page furniture left in the body: running heads, bare folios, arXiv's own stamp",
		Why:  "arXiv prints its identifier down the left margin of every PDF it serves, and a reader of that PDF gets it back as a column of single characters. It is the most common piece of furniture in this corpus and the native and vision paths both walk into it.",
	},
	{
		ID: "T11", Group: GroupStructure, Hard: true,
		Says: "no raw HTML markup left in a body",
		Why:  "The render path converts LaTeXML's HTML5, and a converter that meets a construct it does not know has an obvious wrong way out. A table left as markup passes every other group: the T rules see a body of the right length, the M rules see no mathematics because nothing has dollars round it, and the F rules see no figure.",
	},
	{
		ID: "T13", Group: GroupStructure,
		Says: "no word left split at the hyphen the page broke it with",
		Why:  "Soft, because a line that ends in a hyphen is occasionally a hyphen. It is a leak of the page the text was read off, and it makes the word unsearchable in every language the corpus is translated into.",
	},
}

// Content runs the content rules over a corpus on disk.
//
// One language, because a rule in this group is about the shape of a file and
// every language has the same shape. The rules that compare a translation with
// what it was translated from are group L and they read two languages at once.
type Content struct {
	// Root is the corpus root and Lang is the language directory under it.
	Root string
	Lang string
	// Cap is how many findings to keep per rule, and Only is the rules to run,
	// empty meaning all of them.
	Cap  int
	Only []string
	// Log is called once per paper, for progress.
	Log func(paper string, files int)
}

func (c Content) lang() string {
	if c.Lang == "" {
		return "en"
	}
	return c.Lang
}

// Papers is every paper with content in this language, in identifier order.
//
// It reads the directory tree rather than the metadata plane, because the
// question this answers is what has been extracted and the plane answers what
// exists. A corpus where those two are the same is a corpus at the end of the
// project.
func (c Content) Papers() ([]axid.ID, error) {
	dir := filepath.Join(c.Root, "content", c.lang())
	shards, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []axid.ID
	for _, shard := range shards {
		if !shard.IsDir() {
			continue
		}
		papers, err := os.ReadDir(filepath.Join(dir, shard.Name()))
		if err != nil {
			return nil, err
		}
		for _, p := range papers {
			if !p.IsDir() {
				continue
			}
			// The path form writes an old style id's slash as a hyphen, so it
			// has to be turned back before it is an identifier again.
			id, err := axid.Parse(strings.Replace(p.Name(), "-", "/", 1))
			if err != nil {
				id, err = axid.Parse(p.Name())
			}
			if err != nil {
				return nil, fmt.Errorf("audit: %s/%s is not a paper: %w", shard.Name(), p.Name(), err)
			}
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Canonical < out[j].Canonical })
	return out, nil
}

// Run reads every paper given and reports what the rules found.
func (c Content) Run(papers []axid.ID) (Report, error) {
	report := Report{Plane: "content", Unit: "file", Scope: "paper"}
	col := &collector{cap: c.Cap, only: c.Only}
	for _, id := range papers {
		files, err := c.read(id)
		if err != nil {
			return report, err
		}
		c.paper(col, id, files)
		report.Records += len(files)
		report.Shards++
		if c.Log != nil {
			c.Log(id.Canonical, len(files))
		}
	}
	report.Results = col.results(ContentRules)
	return report, nil
}

// content is one file as the rules see it.
type content struct {
	// name is the file's own name and path is it under the corpus root, which
	// is the form a finding prints.
	name string
	path string
	raw  []byte
	doc  extract.Document
	// ok says the file parsed. Everything but T01 and T02 needs it.
	ok bool
}

// read loads one paper's files in name order.
func (c Content) read(id axid.ID) ([]content, error) {
	dir := corpus.ContentDir(c.Root, c.lang(), id)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []content
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		f := content{
			name: e.Name(),
			path: path.Join(corpus.ContentDir("", c.lang(), id), e.Name()),
			raw:  b,
		}
		if doc, err := extract.ParseDocument(b); err == nil {
			f.doc, f.ok = doc, true
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// paper runs every rule over one paper.
func (c Content) paper(col *collector, id axid.ID, files []content) {
	shard := corpus.Shard(id)
	for _, f := range files {
		at := func(rule string, line int, what string, args ...any) {
			col.add(Finding{Rule: rule, File: f.path, Shard: shard, Line: line, ID: id.Canonical, What: fmt.Sprintf(what, args...)})
		}

		// T01 is the file's shape and T02 is its fields, which is the split in
		// 2166-10 and is why a mistyped field belongs to the second and not the
		// first. FrontProblems answers both: the error is the file not cutting
		// into front matter and body, and the list is what is wrong with the
		// fields once it has.
		col.checked("T01")
		problems, err := extract.FrontProblems(f.raw)
		if err != nil {
			at("T01", 0, "%v", err)
			continue
		}

		col.checked("T02")
		for _, p := range problems {
			at("T02", 0, "%s", p)
		}
		if !f.ok {
			// Strict decoding refuses everything the reader refuses and more,
			// so this is unreachable. It is here because a silent skip is how
			// a file leaves the audit without any rule having looked at it.
			if len(problems) == 0 {
				_, err := extract.ParseDocument(f.raw)
				at("T02", 0, "%v", err)
			}
			continue
		}

		col.checked("T03")
		switch {
		case f.doc.Front.ContentSHA256 == "":
			at("T03", 0, "records no content hash, so nothing would notice this file being rewritten")
		case f.doc.Corrected():
			at("T03", 0, "has been edited since it was written, and ax split -accept is what records that")
		}

		body := mask(f.doc.Body)
		col.checked("T05")
		c.headings("T05", at, body)
		col.checked("T10")
		c.furniture("T10", at, body)
		col.checked("T11")
		c.markup("T11", at, body)
		col.checked("T13")
		c.hyphens("T13", at, body)

		n := utf8.RuneCountInString(strings.TrimSpace(f.doc.Body))
		if f.doc.Front.Kind != "front" {
			col.checked("T08")
			if n < shortBody {
				at("T08", 0, "has a body of %s", plural(n, "character"))
			}
		}
		col.checked("T09")
		if n > longBody {
			at("T09", 0, "has a body of %s", plural(n, "character"))
		}
	}

	// The rest are about the paper rather than about a file, so they are
	// anchored at the directory and run once.
	dir := path.Join(corpus.ContentDir("", c.lang(), id))
	at := func(rule string, what string, args ...any) {
		col.add(Finding{Rule: rule, File: dir, Shard: shard, ID: id.Canonical, What: fmt.Sprintf(what, args...)})
	}
	parsed := make([]content, 0, len(files))
	for _, f := range files {
		if f.ok {
			parsed = append(parsed, f)
		}
	}
	if len(parsed) == 0 {
		return
	}

	col.checked("T04")
	sections := make([]int, 0, len(parsed))
	for _, f := range parsed {
		sections = append(sections, f.doc.Front.Section)
	}
	sort.Ints(sections)
	for i, n := range sections {
		if n != i {
			at("T04", "numbers its sections %s", numbers(sections))
			break
		}
	}

	col.checked("T06")
	switch front := find(parsed, func(f content) bool { return f.name == frontFile }); {
	case front == nil:
		at("T06", "has no %s", frontFile)
	case front.doc.Front.Kind != "front":
		at("T06", "has a %s of kind %q", frontFile, front.doc.Front.Kind)
	case strings.TrimSpace(front.doc.Body) == "":
		at("T06", "has a %s with no abstract in it", frontFile)
	}

	col.checked("T07")
	for i, f := range parsed {
		if !references(f.doc.Front) {
			continue
		}
		for _, after := range parsed[i+1:] {
			if after.doc.Front.Kind != "appendix" {
				at("T07", "puts %s after its references", after.name)
			}
		}
		break
	}
}

const (
	frontFile = "00_front.md"
	// shortBody and longBody are the two ends of a section that went wrong.
	//
	// Both are from papers, where they were arrived at by looking rather than
	// by reasoning, and both are soft. A sample of two seed papers here runs
	// from 388 characters to 35,980, which is inside them at both ends.
	shortBody = 200
	longBody  = 40000
)

// finder is the signature the per file rules report through.
type finder func(rule string, line int, what string, args ...any)

// headings walks the heading tree of one body.
//
// The file itself is level 1: its title is section_title in the front matter
// and it is never printed in the body. So the first heading a body may carry is
// level 2 and every later one may go at most one level deeper.
func (c Content) headings(rule string, at finder, body string) {
	level := 0
	for i, line := range strings.Split(body, "\n") {
		n := heading(line)
		if n == 0 {
			continue
		}
		switch {
		case level == 0:
			if n != 2 {
				at(rule, i+1, "opens at %s, and a body opens one level under the section's own title", strings.Repeat("#", n))
			}
		case n > level+1:
			at(rule, i+1, "goes from %s to %s", strings.Repeat("#", level), strings.Repeat("#", n))
		}
		level = n
	}
}

// heading is the level of a heading line, or zero.
func heading(line string) int {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n == 0 || n > 6 || n == len(line) || line[n] != ' ' {
		return 0
	}
	return n
}

// furniture looks for what the page the text was read off left behind.
//
// Three things, and not the fourth. A running head is a line that repeats at
// the top of every page and there are no pages here to count it against, so it
// is named in the rule and caught by the two that are findable: the stamp is a
// running head of a sort, and a folio is what sits under one.
func (c Content) furniture(rule string, at finder, body string) {
	lines := strings.Split(body, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if m := stamp.FindString(line); m != "" {
			at(rule, i+1, "carries arXiv's own stamp, %q", m)
			continue
		}
		if folio.MatchString(line) {
			at(rule, i+1, "is a page number on its own, %q", line)
			continue
		}
		// The margin stamp arrives as one character per line, because the page
		// prints it sideways and every reader of that page unwinds it the same
		// way. Six in a row is past anything a body does by accident.
		if j := column(lines, i); j-i >= columnRun {
			at(rule, i+1, "is %d lines of one character each, which is arXiv's identifier printed down the margin", j-i)
			i = j - 1
		}
	}
}

const columnRun = 6

// column is the end of a run of single character lines starting at i.
func column(lines []string, i int) int {
	j := i
	for j < len(lines) && utf8.RuneCountInString(strings.TrimSpace(lines[j])) == 1 {
		j++
	}
	return j
}

// markup looks for HTML that was never converted.
//
// A fixed list of element names rather than anything that looks like a tag,
// because mathematics is full of angle brackets and $a<b>c$ is an inequality
// and not a bold run. Mathematics is masked out before this sees it anyway, and
// the list is the second guard.
func (c Content) markup(rule string, at finder, body string) {
	for i, line := range strings.Split(body, "\n") {
		if m := htmlTag.FindString(line); m != "" {
			at(rule, i+1, "has %s in it", m)
		}
	}
}

// hyphens looks for a word the page broke in half.
func (c Content) hyphens(rule string, at finder, body string) {
	lines := strings.Split(body, "\n")
	for i := 0; i+1 < len(lines); i++ {
		tail := broken.FindStringSubmatch(lines[i])
		if tail == nil {
			continue
		}
		head := opener.FindStringSubmatch(lines[i+1])
		if head == nil {
			continue
		}
		at(rule, i+1, "ends on %s- and the next line starts %s", tail[1], head[1])
	}
}

// mask blanks out the parts of a body the structural rules must not read.
//
// Code and mathematics both carry characters that look like everything this
// group is looking for: a shell session has a bare number on a line, a listing
// has angle brackets, and TeX has hashes. Blanking them keeps every line number
// and every line count exactly where it was, so a finding still points at the
// line somebody has to open.
func mask(body string) string {
	lines := strings.Split(body, "\n")
	fenced, display := false, false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~"):
			fenced = !fenced
			lines[i] = blank(line)
			continue
		case fenced:
			lines[i] = blank(line)
			continue
		case trimmed == "$$":
			display = !display
			lines[i] = blank(line)
			continue
		case display:
			lines[i] = blank(line)
			continue
		}
		lines[i] = inline(line)
	}
	return strings.Join(lines, "\n")
}

// inline blanks the mathematics inside one line, delimiters and all.
func inline(line string) string {
	b := []byte(line)
	open := -1
	for i := 0; i < len(b); i++ {
		if b[i] != '$' || (i > 0 && b[i-1] == '\\') {
			continue
		}
		if open < 0 {
			open = i
			continue
		}
		for j := open; j <= i; j++ {
			b[j] = ' '
		}
		open = -1
	}
	return string(b)
}

// blank is a line of spaces the same length, so line numbers do not move.
func blank(line string) string {
	return strings.Repeat(" ", utf8.RuneCountInString(line))
}

// find is the first file matching, or nil.
func find(files []content, ok func(content) bool) *content {
	for i := range files {
		if ok(files[i]) {
			return &files[i]
		}
	}
	return nil
}

// references reports whether a file is the paper's reference section.
//
// Both by kind and by title. The render path does not emit a references section
// at all, because the bibliography is read into manifests/refs by ax refs, and
// the source and native paths will.
func references(f extract.Front) bool {
	if f.Kind == "references" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(f.SectionTitle)) {
	case "references", "bibliography", "works cited":
		return true
	}
	return false
}

// numbers writes a list of section numbers the way a person would read it out.
func numbers(sections []int) string {
	out := make([]string, len(sections))
	for i, n := range sections {
		out[i] = fmt.Sprint(n)
	}
	return strings.Join(out, " ")
}

// stamp is the identifier arXiv prints on every PDF it serves.
//
// The bracketed category is required and that is the point. A body is allowed
// to say arXiv:2312.00752 in a sentence and papers do. What no sentence says is
// arXiv:2312.00752v2 [cs.LG] 31 May 2024, because that is a stamp and not
// prose.
var stamp = regexp.MustCompile(`(?i)arXiv:\s*[a-z0-9./-]+\s*\[[a-z][a-z.-]*\]`)

// folio is a page number left on a line of its own.
var folio = regexp.MustCompile(`^(?:-\s*)?\d{1,4}(?:\s*-)?$|^(?i:page)\s+\d{1,4}$`)

// htmlTag is markup a converter gave up on.
//
// A list of element names and not a shape. Anything that looks like a tag would
// take $a<b>c$ with it, and while mathematics is masked out before this runs,
// one guard for a hard rule is not enough.
var htmlTag = regexp.MustCompile(`(?i)</?(?:a|abbr|b|blockquote|br|caption|center|cite|code|col|colgroup|dd|div|dl|dt|em|figcaption|figure|font|h1|h2|h3|h4|h5|h6|hr|i|img|li|ol|p|pre|small|span|strong|sub|sup|table|tbody|td|tfoot|th|thead|tr|tt|u|ul)(?:\s[^<>]*)?/?>`)

// broken is a word cut in half at the end of a line, and opener is the rest of
// it at the start of the next.
var (
	broken = regexp.MustCompile(`([a-z]{2,})-\s*$`)
	opener = regexp.MustCompile(`^\s*([a-z]{2,})`)
)

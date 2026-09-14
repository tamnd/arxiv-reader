package audit

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/tags"
)

// tagRules read a paper's register against the bodies its tags were written
// into.
//
// The register is the record and the content files are the copy, which is the
// shape of most of this group: one rule for the register on its own, one for
// the copy on its own, and one for the two disagreeing.
//
// G07 and G08 are in 2166-10 and are not here. G07 is about tombstones and
// nothing writes one until ax tags diff, and G08 reads git history for a tag
// that used to be in a register and is not any more. Both are M4. A rule
// registered before it can run is a rule everybody believes is working.
var tagRules = []Rule{
	{
		ID: "G01", Group: GroupTags, Hard: true,
		Says: "every tag is four characters from the Stacks alphabet",
		Why:  "This is also the rule that reads the register at all, so a line that is not a line is reported here, the same way T01 carries a content file's shape. A tag that is not a tag is a name nothing will ever resolve, and the register is where names are handed out.",
	},
	{
		ID: "G02", Group: GroupTags, Hard: true,
		Says: "every tag reference names a paper as well as a tag",
		Why:  "A bare 03QK is not a reference in this corpus, because 03QK exists in thousands of papers and means something different in each. The reference is 2106.09685#03QK. Nothing emits a page, an EPUB or a .tex yet, so what this reads today is the bodies, which is where a reference written by hand lands first.",
	},
	{
		ID: "G03", Group: GroupTags, Hard: true,
		Says: "no tag appears twice in one paper's register",
		Why:  "Two lines for one tag means two objects in one paper answer to one name, and every reference written against it resolves to whichever the reader loaded last.",
	},
	{
		ID: "G04", Group: GroupTags, Hard: true,
		Says: "no local identifier appears twice in one paper's register",
		Why:  "One object with two tags is two references that are both right pointing at one theorem, and nothing downstream can tell which of them to print or which of them to count.",
	},
	{
		ID: "G05", Group: GroupTags, Hard: true,
		Says: "every tag in a body is in that paper's register, against that object",
		Why:  "A copy that disagrees with the record is what a rewrite that stopped half way leaves behind, and it is the disagreement that makes a tag resolve to the wrong object rather than to nothing at all.",
	},
	{
		ID: "G06", Group: GroupTags, Hard: true,
		Says: "every taggable object in a body carries a tag",
		Why:  "Scanned with the function ax tags assign scans with, so the rule and the command cannot disagree about what a taggable object is. An object with no tag is an object nothing outside this paper can point at, and a paper nobody has run ax tags assign over fails this on every object it has, which is the answer wanted: content is committed tagged.",
	},
}

// objectRules read the object model.
//
// X01 alone, and the other eight of 2166-10 are not here. X02, X03 and X09
// read a result record, X04 through X06 and X08 read the concept layer, and
// X07 reads an artefact record. None of those three things exist until M6.
var objectRules = []Rule{
	{
		ID: "X01", Group: GroupObjects, Hard: true,
		Says: "every object's kind is one of the sixteen",
		Why:  "The kind is what every later group dispatches on: the M rules read an equation, the F rules read a figure and a table, and a kind none of them know is an object they all walk past. A block with an anchor and no class at all is reported too. It still gets a tag, on purpose, because an anchor nothing can be written against is worse than a tag on something that turns out to be a footnote, and it is still a hole in the class mapping.",
	},
}

// tagged runs the tag and object rules over one paper.
//
// It is one method and two groups because they read the same three things: the
// register, the attribute blocks and the front matter. Walking the bodies twice
// to keep the groups apart in the source would be reading a paper twice to
// answer questions that are asked of the same line.
func (c Content) tagged(col *collector, id axid.ID, files []content) error {
	shard := corpus.Shard(id)
	reg, err := readRegister(corpus.TagsPath(c.Root, id))
	if err != nil {
		return err
	}
	byLocal := map[string]tags.Tag{}
	if reg.found {
		at := func(rule string, line int, what string, args ...any) {
			col.add(Finding{Rule: rule, File: corpus.TagsPath("", id), Shard: shard, Line: line, ID: id.Canonical, What: fmt.Sprintf(what, args...)})
		}

		col.checked("G01")
		for _, p := range reg.problems {
			at("G01", p.line, "%s", p.what)
		}
		for _, e := range reg.entries {
			if !e.Tag.Valid() {
				at("G01", e.line, "names %q, and a tag is four characters of %s", e.Tag, tags.Alphabet)
			}
		}

		col.checked("G03")
		col.checked("G04")
		tagLine := map[tags.Tag]int{}
		localLine := map[string]int{}
		for _, e := range reg.entries {
			if first, ok := tagLine[e.Tag]; ok {
				at("G03", e.line, "gives %s to %s, and line %d gave it to %s", e.Tag, e.Local, first, reg.localAt(first))
			} else {
				tagLine[e.Tag] = e.line
			}
			if first, ok := localLine[e.Local]; ok {
				at("G04", e.line, "gives %s a second tag, %s, after the one on line %d", e.Local, e.Tag, first)
			} else {
				localLine[e.Local] = e.line
				byLocal[e.Local] = e.Tag
			}
		}
	}

	for _, f := range files {
		at := func(rule string, line int, what string, args ...any) {
			col.add(Finding{Rule: rule, File: f.path, Shard: shard, Line: line, ID: id.Canonical, What: fmt.Sprintf(what, args...)})
		}

		col.checked("G02")
		c.bare("G02", at, f.masked)

		// The file is itself an object when it is a section, and its attribute
		// block is the front matter: the heading a section that is a whole file
		// carries is section_title and there is no line in the body to hang a
		// block on. The front matter file is not an object, which is why ax
		// tags assign leaves its tag empty.
		body := f.doc.Body
		carried := tags.Tagged(body)
		self := f.doc.Front.LocalID
		if self != "" && f.doc.Front.Kind != "front" && f.doc.Front.Tag != "" {
			carried[self] = tags.Tag(f.doc.Front.Tag)
		}

		col.checked("G05")
		for _, o := range tags.Blocks(f.name, body) {
			c.carries("G05", at, lineOf(body, o.Local), o.Local, carried, byLocal)
		}
		if self != "" && f.doc.Front.Kind != "front" {
			c.carries("G05", at, 0, self, carried, byLocal)
		}

		col.checked("G06")
		if self != "" && f.doc.Front.Kind != "front" && f.doc.Front.Tag == "" {
			at("G06", 0, "is the section %s and its front matter carries no tag", self)
		}
		for _, o := range tags.Objects(f.name, body) {
			if _, ok := carried[o.Local]; !ok {
				at("G06", lineOf(body, o.Local), "%s carries no tag", o.Local)
			}
		}

		col.checked("X01")
		if !extract.FileKinds[f.doc.Front.Kind] {
			at("X01", 0, "is of kind %q, and a content file is the front matter or a section", f.doc.Front.Kind)
		}
		for _, o := range tags.Blocks(f.name, body) {
			switch {
			case o.Class == "":
				at("X01", lineOf(body, o.Local), "%s carries no class, so nothing downstream knows what kind of object it is", o.Local)
			case !extract.Kinds[o.Class]:
				at("X01", lineOf(body, o.Local), "%s is of kind %q, which is not one of the sixteen", o.Local, o.Class)
			}
		}
	}
	return nil
}

// carries is G05 over one object: the tag in the body against the register.
func (c Content) carries(rule string, at finder, line int, local string, carried map[string]tags.Tag, byLocal map[string]tags.Tag) {
	t, ok := carried[local]
	if !ok {
		// An object with no tag at all is G06's, and reporting it here as well
		// would make one missing tag two findings in two rules.
		return
	}
	switch want, known := byLocal[local]; {
	case !t.Valid():
		at(rule, line, "%s carries %q, which is not a tag", local, t)
	case !known:
		at(rule, line, "%s carries %s, which is not in this paper's register", local, t)
	case want != t:
		at(rule, line, "%s carries %s and the register gives it %s", local, t, want)
	}
}

// bare is G02 over one body: a tag written without the paper it belongs to.
func (c Content) bare(rule string, at finder, body string) {
	for i, line := range strings.Split(body, "\n") {
		for _, m := range bareLink.FindAllStringSubmatch(line, -1) {
			at(rule, i+1, "links to %s, and a reference is a paper and a tag, so 2106.09685#%s", m[1], m[1])
		}
		for _, m := range bareTag.FindAllStringSubmatch(line, -1) {
			if !hasLetter(m[1]) {
				continue
			}
			at(rule, i+1, "names %s with no paper in front of it, and a reference is 2106.09685#%s", m[1], m[1])
		}
	}
}

// bareLink is a link into a bare tag and bareTag is one written in prose.
//
// The prose form has to carry a letter and the link form does not. A hash in
// front of four digits is a footnote marker or somebody's issue number far more
// often than it is a tag, and the tags that are all digits are six in a
// thousand, which is a trade worth making for a hard rule. A link is different:
// a local identifier in this corpus is thm-1 or s4-1 or bib-12 and never four
// digits, so nothing legitimate has that shape.
//
// The prose form steps over a hash that opens a bracket so that a link is one
// finding and not two, and over one inside a brace so that an attribute block,
// which is where an identifier is defined rather than referenced, is not read
// as a reference to itself.
var (
	bareLink = regexp.MustCompile(`\]\(#([0-9A-Z]{4})\)`)
	bareTag  = regexp.MustCompile(`(?:^|[^0-9A-Za-z./_{(#-])#([0-9A-Z]{4})(?:[^0-9A-Za-z]|$)`)
)

func hasLetter(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return r >= 'A' && r <= 'Z' })
}

// lineOf is the line a body's attribute block for an object starts on, or zero.
//
// The block and not the object, because the block is the one place the
// identifier is written down. The character after the identifier is checked so
// that thm-1 does not report the line thm-12 sits on, which is the kind of
// wrong line number that sends somebody to the wrong theorem.
func lineOf(body, local string) int {
	want := "{#" + local
	for i := 0; ; {
		j := strings.Index(body[i:], want)
		if j < 0 {
			return 0
		}
		at := i + j
		end := at + len(want)
		if end >= len(body) || body[end] == ' ' || body[end] == '}' || body[end] == '\t' {
			return strings.Count(body[:at], "\n") + 1
		}
		i = end
	}
}

// register is one paper's tag register as the audit reads it.
//
// More tolerantly than tags.ParseRegister reads it, and that is the point. The
// loader refuses a register with a repeated tag in it, which is the right
// answer for a command about to write one and the wrong answer for a rule whose
// job is to say which line the repeat is on. A loader that refuses reports one
// problem and stops. This reports all of them.
type register struct {
	found    bool
	entries  []entry
	problems []problem
}

type entry struct {
	tags.Entry
	line int
}

type problem struct {
	line int
	what string
}

// localAt is the object named on a line, for a message that has to say what the
// other line was.
func (r register) localAt(line int) string {
	for _, e := range r.entries {
		if e.line == line {
			return e.Local
		}
	}
	return "something else"
}

func readRegister(path string) (register, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return register{}, nil
	}
	if err != nil {
		return register{}, err
	}
	reg := register{found: true}
	r := csv.NewReader(bytes.NewReader(b))
	r.Comment = '#'
	r.FieldsPerRecord = -1
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// A file that stops being comma separated part way through is read
			// as far as it parsed and reported once. Going on after it would
			// report the same broken quoting on every line after it.
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				reg.problems = append(reg.problems, problem{line: pe.Line, what: "is not comma separated from here on: " + pe.Err.Error()})
				break
			}
			return register{}, err
		}
		line, _ := r.FieldPos(0)
		if len(rec) < 2 || len(rec) > 4 {
			reg.problems = append(reg.problems, problem{line: line, what: fmt.Sprintf("has %s, and a line is tag,local or tag,local,gone:vN,note", plural(len(rec), "field"))})
			continue
		}
		e := entry{line: line}
		e.Tag, e.Local = tags.Tag(rec[0]), rec[1]
		if len(rec) > 2 {
			e.Gone = strings.TrimPrefix(rec[2], "gone:")
		}
		if len(rec) > 3 {
			e.Note = rec[3]
		}
		reg.entries = append(reg.entries, e)
	}
	return reg, nil
}

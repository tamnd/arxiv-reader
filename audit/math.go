package audit

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
)

// mathRules read the mathematics.
//
// All of them work off one splitter, because a rule that decides for itself
// where the mathematics is will disagree with the next rule that does, and two
// rules disagreeing about where a span starts is how a corpus gets a finding
// nobody can reproduce.
//
// M04 and M06 are in 2166-10 and are not here. M04 is every span parsing under
// KaTeX, which needs KaTeX, and the reader that carries it is M6; running a
// JavaScript engine from this binary to answer it earlier would be a second
// toolchain in CI for one rule. M06 compares a paper's displays per page with
// its category's median, and the baselines it reads are computed over a corpus
// that has more than two papers in it. A rule registered before it can run is a
// rule everybody believes is working.
var mathRules = []Rule{
	{
		ID: "M01", Group: GroupMath, Hard: true,
		Says: "every math span is closed",
		Why:  "An unclosed span swallows the prose after it, so the damage is never limited to the formula. It is also the rule the other twelve stand on: every one of them reads what the splitter found, and a span that runs to the end of the file is a span with half a paragraph in it.",
	},
	{
		ID: "M02", Group: GroupMath, Hard: true,
		Says: "the number sets are \\mathbb, consistently",
		Why:  "Two things, and the paper itself decides the second. A bare \\R is a macro the author defined in a preamble nothing downstream has, so it renders as nothing at all. A paper that writes \\mathbb{R} in one section and \\mathbf{R} in another has had one of the two rewritten by something, and the letter that changed shape is the one to look at.",
	},
	{
		ID: "M03", Group: GroupMath, Hard: true,
		Says: "no character stranded out of its TeX",
		Why:  "A control sequence sitting in the prose is mathematics whose dollars were lost, and it is the one failure that reads as ordinary text to every other group: the T rules see a body of the right length and this group sees one span fewer than the paper has.",
	},
	{
		ID: "M05", Group: GroupMath, Hard: true,
		Says: "no illegible marker left in the corpus",
		Why:  "The markers are fixed here so that a path which cannot read something has one to write. Nothing writes one today and the replacement character arrives on its own, out of a file read with the wrong encoding, which is the case this catches now.",
	},
	{
		ID: "M07", Group: GroupMath, Hard: true,
		Says: "no bracket from the prose closes inside the mathematics",
		Why:  "A bracket opened in a sentence and closed inside a formula is a dollar sign in the wrong place, and it is the shape a span that is off by one delimiter always has. The bracket is what makes it findable, because the prose either side of it still reads.",
	},
	{
		ID: "M08", Group: GroupMath, Hard: true,
		Says: "no matrix left flattened into a pair of scripts",
		Why:  "An ampersand or a row break with no environment around it is a matrix whose \\begin was dropped, and \\atop is a matrix set as two things stacked by hand. Both render as something, which is why neither shows up as a failure anywhere else.",
	},
	{
		ID: "M09", Group: GroupMath,
		Says: "no base carries two superscripts or two subscripts",
		Why:  "Soft, because what this finds is a span TeX would refuse and KaTeX will usually set anyway. It reads two of the same script in a row, which is the shape a lost brace leaves, and it does not chase the base across an intervening script of the other kind.",
	},
	{
		ID: "M10", Group: GroupMath, Hard: true,
		Says: "no relation sign has lost the stroke that negates it",
		Why:  "A formula that means the opposite of what the paper said is the worst thing in this group and the hardest to see, because both halves render. It is found from the stroke's side: the split leaves a combining overlay with nothing under it, or a \\not with no relation after it, and either one is the half that can be looked for.",
	},
	{
		ID: "M11", Group: GroupMath, Hard: true,
		Says: "the mathematics is written between dollars, never \\( or \\[",
		Why:  "One delimiter, because every reader of this corpus has to agree with the splitter above about where a formula starts. The square form is only read when it is a line of its own or a line that opens and closes, since \\[ mid sentence is Markdown escaping a bracket and this corpus writes plenty of those.",
	},
	{
		ID: "M12", Group: GroupMath,
		Says: "an inline formula is written tight against its dollars",
		Why:  "Soft, and it is about the rendering rather than the meaning. A space inside the dollars is set as a space in front of the formula, on top of the space already in the sentence.",
	},
	{
		ID: "M13", Group: GroupMath, Hard: true,
		Says: "no $ inside a fenced code block opened a span",
		Why:  "The splitter above respects fences and not every reader of this corpus does. An odd number of dollars inside a fence is the case where a reader that ignores them sets the prose after the fence as mathematics, and an even number stays inside the block.",
	},
	{
		ID: "M14", Group: GroupMath, Hard: true,
		Says: "a paper with mathematics in its prose has mathematics in its markup",
		Why:  "This is the rule for mathematics that was deleted rather than mangled. Every other rule in the group reads the spans and asks whether they are right, which leaves the worst outcome unexamined: a paper whose formulas were dissolved into prose has no spans to read, so the other twelve report that they had nothing to look at and the audit comes back green over a destroyed paper.",
	},
}

// maths runs the mathematics rules over one paper.
//
// Two of the fourteen are about the paper and not about a file. M02 is, because
// consistency is a thing a paper has and a section cannot; M14 is, because a
// paper is allowed a section with no mathematics in it and is not allowed to be
// a paper with none.
func (c Content) maths(col *collector, id axid.ID, files []content) {
	shard := corpus.Shard(id)
	var sets []numberSet
	spanCount, prose := 0, map[rune]bool{}

	for _, f := range files {
		at := func(rule string, line int, what string, args ...any) {
			col.add(Finding{Rule: rule, File: f.path, Shard: shard, Line: line, ID: id.Canonical, What: fmt.Sprintf(what, args...)})
		}
		b := classify(f.doc.Body)
		found := spans(b)
		spanCount += len(found)

		col.checked("M01")
		for _, s := range found {
			if s.closed {
				continue
			}
			if s.display {
				at("M01", s.line, "opens a display block that nothing closes")
				continue
			}
			at("M01", s.line, "opens a math span at column %d that nothing closes on this line", s.col)
		}

		sets = append(sets, numberSets(f, found)...)

		col.checked("M03")
		for i, line := range strings.Split(f.masked, "\n") {
			for _, m := range stranded.FindAllString(line, -1) {
				at("M03", i+1, "has %s in the prose, which is TeX that lost its dollars", m)
			}
		}

		col.checked("M05")
		c.illegible("M05", at, b)

		col.checked("M07")
		c.brackets("M07", at, b)

		col.checked("M08")
		c.flattened("M08", at, found)

		col.checked("M09")
		c.scripts("M09", at, found)

		col.checked("M10")
		c.negations("M10", at, b, found)

		col.checked("M11")
		c.delimiters("M11", at, b)

		col.checked("M12")
		for _, s := range found {
			if s.display || strings.TrimSpace(s.tex) == "" {
				continue
			}
			if s.tex != strings.TrimLeft(s.tex, " \t") || s.tex != strings.TrimRight(s.tex, " \t") {
				at("M12", s.line, "writes $%s$ with a space inside the dollars", strings.TrimSpace(s.tex))
			}
		}

		col.checked("M13")
		c.fences("M13", at, b)

		for _, r := range f.masked {
			if mathOnly[r] {
				prose[r] = true
			}
		}
	}

	dir := path.Join(corpus.ContentDir("", c.lang(), id))
	col.checked("M02")
	for _, s := range inconsistent(sets) {
		col.add(Finding{Rule: "M02", File: s.file, Shard: shard, Line: s.line, ID: id.Canonical, What: s.what})
	}

	col.checked("M14")
	if spanCount == 0 && len(prose) >= flattenedPaper {
		col.add(Finding{Rule: "M14", File: dir, Shard: shard, ID: id.Canonical,
			What: fmt.Sprintf("uses %d characters in its prose that only mathematics uses, and carries not one math span in the whole paper, so its formulas were flattened rather than extracted", len(prose))})
	}
}

// flattenedPaper is how many mathematical characters in the prose make a paper
// with no spans a paper something flattened.
//
// Three, because one is a degree sign in a temperature and two is a paper that
// mentions alpha and beta by name. Three distinct characters that English does
// not use, in a paper with no mathematics at all, is not a coincidence.
const flattenedPaper = 3

// lineKind is what one line of a body is.
//
// Four kinds and not two, because the rules ask different questions of each: a
// fence is where M13 looks, a display block is mathematics with no dollars on
// the same line as the formula, and prose is the only place a bracket has a
// side to be on.
type lineKind int

const (
	kindProse lineKind = iota
	kindFence
	kindCode
	kindDisplayEdge
	kindDisplay
)

// bodyLines is a body with every line said to be one of the four.
type bodyLines struct {
	text []string
	kind []lineKind
}

// classify reads a body once and says what each line is.
//
// Once, and everything that has to know where the mathematics is reads the
// answer. The masker in content.go reads it too, so the structural rules and
// this group cannot end up with two different opinions about which lines are
// code.
func classify(body string) bodyLines {
	text := strings.Split(body, "\n")
	kind := make([]lineKind, len(text))
	fenced, display := false, false
	for i, line := range text {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~"):
			kind[i] = kindFence
			fenced = !fenced
		case fenced:
			kind[i] = kindCode
		case trimmed == "$$":
			kind[i] = kindDisplayEdge
			display = !display
		case display:
			kind[i] = kindDisplay
		default:
			kind[i] = kindProse
		}
	}
	return bodyLines{text: text, kind: kind}
}

// span is one piece of mathematics as the M rules see it.
type span struct {
	// line is where the span opens and col is the column its opening dollar
	// sits in, one based, which is what an editor jumps to.
	line, col int
	// tex is what is between the delimiters, delimiters excluded.
	tex     string
	display bool
	closed  bool
}

// at is the line an offset into the span's TeX falls on.
//
// A display block opens on its own line, so its TeX starts on the line after
// the one the span is anchored at, and a finding twenty lines into a long
// aligned block should say which of those twenty lines it is on.
func (s span) at(off int) int {
	if off < 0 || off > len(s.tex) {
		return s.line
	}
	n := strings.Count(s.tex[:off], "\n")
	if s.display {
		return s.line + 1 + n
	}
	return s.line + n
}

// spans is every piece of mathematics in a body, in reading order.
//
// Inline spans do not cross a line, and that is a fact about this corpus rather
// than about Markdown: the extractor writes one paragraph per line, so a dollar
// with no partner before the end of its line has no partner at all. Reading it
// any other way would let one stray dollar pair with one in the next paragraph
// and report a span containing two paragraphs of prose.
func spans(b bodyLines) []span {
	var out []span
	open, buf := -1, []string{}
	for i, line := range b.text {
		switch b.kind[i] {
		case kindDisplayEdge:
			if open < 0 {
				open, buf = i+1, nil
				continue
			}
			out = append(out, span{line: open, col: 1, tex: strings.Join(buf, "\n"), display: true, closed: true})
			open = -1
		case kindDisplay:
			buf = append(buf, line)
		case kindProse:
			out = append(out, inlineSpans(i+1, line)...)
		}
	}
	if open >= 0 {
		out = append(out, span{line: open, col: 1, tex: strings.Join(buf, "\n"), display: true})
	}
	return out
}

// inlineSpans pairs the dollars on one line.
func inlineSpans(line int, text string) []span {
	var out []span
	open := -1
	for i := 0; i < len(text); i++ {
		if text[i] != '$' || (i > 0 && text[i-1] == '\\') {
			continue
		}
		if open < 0 {
			open = i
			continue
		}
		out = append(out, span{line: line, col: open + 1, tex: text[open+1 : i], closed: true})
		open = -1
	}
	if open >= 0 {
		out = append(out, span{line: line, col: open + 1, tex: text[open+1:]})
	}
	return out
}

// numberSet is one place a paper named a number set, for M02.
type numberSet struct {
	file string
	line int
	// letter is the set, and form is the command it was written with, so
	// "mathbb" or "mathbf", or the empty string for a bare macro.
	letter byte
	form   string
	what   string
}

// setNames are the sets whose letter is worth arguing about.
//
// Six, and the two that are not here are the ones that collide. \mathbb{E} is
// an expectation far more often than it is anything else, and \mathcal{C} is a
// category, so neither letter can be read as a number set gone wrong.
var setNames = map[byte]string{
	'N': "the naturals", 'Z': "the integers", 'Q': "the rationals",
	'R': "the reals", 'C': "the complex numbers", 'H': "the quaternions",
}

var (
	setCommand = regexp.MustCompile(`\\(mathbb|mathbf|mathrm)\{([NZQRCH])\}`)
	setMacro   = regexp.MustCompile(`\\([NZQRCH])(?:[^A-Za-z]|$)`)
)

// numberSets finds every number set one file names.
func numberSets(f content, found []span) []numberSet {
	var out []numberSet
	for _, s := range found {
		for _, m := range setCommand.FindAllStringSubmatchIndex(s.tex, -1) {
			out = append(out, numberSet{
				file: f.path, line: s.at(m[0]),
				letter: s.tex[m[4]], form: s.tex[m[2]:m[3]],
			})
		}
		for _, m := range setMacro.FindAllStringSubmatchIndex(s.tex, -1) {
			out = append(out, numberSet{file: f.path, line: s.at(m[0]), letter: s.tex[m[2]]})
		}
	}
	return out
}

// inconsistent is M02 over a whole paper.
//
// A bare macro is wrong on its own, because nothing downstream carries the
// preamble that defined it. Two commands for one letter are wrong together, and
// which of the two is reported is decided by the paper: \mathbb is what this
// corpus writes, so the other one is the one that moved.
func inconsistent(sets []numberSet) []numberSet {
	forms := map[byte]map[string]bool{}
	for _, s := range sets {
		if s.form == "" {
			continue
		}
		if forms[s.letter] == nil {
			forms[s.letter] = map[string]bool{}
		}
		forms[s.letter][s.form] = true
	}
	var out []numberSet
	for _, s := range sets {
		switch {
		case s.form == "":
			s.what = fmt.Sprintf(`names %s as \%c, a macro that only the author's preamble defines, and the corpus writes \mathbb{%c}`, setNames[s.letter], s.letter, s.letter)
		case s.form != "mathbb" && forms[s.letter]["mathbb"]:
			s.what = fmt.Sprintf(`writes %s as \%s{%c} here and as \mathbb{%c} elsewhere in the paper`, setNames[s.letter], s.form, s.letter, s.letter)
		default:
			continue
		}
		out = append(out, s)
	}
	return out
}

// stranded is a control sequence left in the prose.
//
// Two letters and not one, because a single letter after a backslash is how
// Markdown escapes a bracket and this corpus is full of \[ and \] in citation
// labels. Nothing in Markdown escapes with a word.
var stranded = regexp.MustCompile(`\\[A-Za-z]{2,}`)

// markers are what a path that could not read something leaves behind.
//
// The list is fixed here, in the rule, so that the vision path in M5 has one to
// write rather than inventing its own. The replacement character is the one
// that arrives without anybody writing it, out of a file read with the wrong
// encoding, and it is the reason this rule can run today.
var markers = []string{"\uFFFD", "[illegible]", "[unreadable]", "[?]"}

func (c Content) illegible(rule string, at finder, b bodyLines) {
	for i, line := range b.text {
		lower := strings.ToLower(line)
		for _, m := range markers {
			if strings.Contains(lower, m) {
				at(rule, i+1, "carries %q, which is a marker for something nothing could read", m)
			}
		}
	}
}

// brackets is M07 over one body.
//
// Round and square only. Braces are TeX's own grouping and a formula is full of
// them, so a brace crossing the delimiter says nothing; a bracket is punctuation
// on one side and a delimiter on the other, and the crossing is the whole
// finding.
func (c Content) brackets(rule string, at finder, b bodyLines) {
	for i, line := range b.text {
		if b.kind[i] != kindProse {
			continue
		}
		type opener struct {
			ch   byte
			col  int
			math bool
		}
		var stack []opener
		math := false
		for j := 0; j < len(line); j++ {
			if j > 0 && line[j-1] == '\\' {
				continue
			}
			switch line[j] {
			case '$':
				math = !math
			case '(', '[':
				stack = append(stack, opener{ch: line[j], col: j + 1, math: math})
			case ')', ']':
				want := byte('(')
				if line[j] == ']' {
					want = '['
				}
				if len(stack) == 0 || stack[len(stack)-1].ch != want {
					continue
				}
				o := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if math && !o.math {
					at(rule, i+1, "opens %c in the prose at column %d and closes it inside the mathematics at column %d", o.ch, o.col, j+1)
				}
			}
		}
	}
}

// flattened is M08 over one file's spans.
func (c Content) flattened(rule string, at finder, found []span) {
	for _, s := range found {
		if i := strings.Index(s.tex, `\atop`); i >= 0 {
			at(rule, s.at(i), `sets two things with \atop, which is what a matrix collapses into when its environment is dropped`)
			continue
		}
		if i := strings.Index(s.tex, `\above`); i >= 0 {
			at(rule, s.at(i), `sets two things with \above, which is what a matrix collapses into when its environment is dropped`)
			continue
		}
		if strings.Contains(s.tex, `\begin{`) {
			continue
		}
		if i := alignment.FindStringIndex(s.tex); i != nil {
			at(rule, s.at(i[0]), "carries %q with no environment around it, so the rows of a matrix are there and the matrix is not", strings.TrimSpace(s.tex[i[0]:i[1]]))
		}
	}
}

// alignment is a matrix separator: a column break or a row break.
//
// The ampersand has to be unescaped, because \& is the character and not the
// separator, and the row break has to be a real pair of backslashes rather than
// the tail of a command.
var alignment = regexp.MustCompile(`(^|[^\\])(&|\\\\)`)

// scripts is M09 over one file's spans.
func (c Content) scripts(rule string, at finder, found []span) {
	for _, s := range found {
		for i := 0; i < len(s.tex); i++ {
			ch := s.tex[i]
			if ch != '^' && ch != '_' {
				continue
			}
			if i > 0 && s.tex[i-1] == '\\' {
				continue
			}
			j := skipArg(s.tex, i+1)
			if j < len(s.tex) && s.tex[j] == ch {
				kind := "superscripts"
				if ch == '_' {
					kind = "subscripts"
				}
				at(rule, s.at(i), "gives one base two %s, at %q", kind, around(s.tex, i))
			}
			i = j - 1
		}
	}
}

// skipArg is the end of the argument a script takes.
func skipArg(tex string, i int) int {
	for i < len(tex) && (tex[i] == ' ' || tex[i] == '\t') {
		i++
	}
	if i >= len(tex) {
		return i
	}
	switch tex[i] {
	case '{':
		depth := 0
		for ; i < len(tex); i++ {
			switch tex[i] {
			case '\\':
				i++
			case '{':
				depth++
			case '}':
				if depth--; depth == 0 {
					return i + 1
				}
			}
		}
		return i
	case '\\':
		i++
		if i < len(tex) && !letter(tex[i]) {
			return i + 1
		}
		for i < len(tex) && letter(tex[i]) {
			i++
		}
		return i
	}
	_, n := utf8.DecodeRuneInString(tex[i:])
	return i + n
}

func letter(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// around is a short window of TeX round an offset, for a message somebody can
// find the place from without opening the file.
func around(tex string, i int) string {
	lo, hi := i-8, i+12
	if lo < 0 {
		lo = 0
	}
	if hi > len(tex) {
		hi = len(tex)
	}
	for lo > 0 && !utf8.RuneStart(tex[lo]) {
		lo--
	}
	for hi < len(tex) && !utf8.RuneStart(tex[hi]) {
		hi++
	}
	return strings.TrimSpace(tex[lo:hi])
}

// negations is M10, over a body and its spans.
//
// The combining overlay is looked for everywhere and the loose \not only inside
// the mathematics, because outside it a backslash and a word is M03's finding
// and reporting it twice helps nobody.
func (c Content) negations(rule string, at finder, b bodyLines, found []span) {
	for i, line := range b.text {
		if strings.ContainsRune(line, overlay) {
			at(rule, i+1, "carries a combining negation stroke with nothing under it, so a relation lost the half that reverses it")
		}
	}
	for _, s := range found {
		for _, m := range loose.FindAllStringSubmatchIndex(s.tex, -1) {
			if relations[s.tex[m[2]:m[3]]] {
				continue
			}
			at(rule, s.at(m[0]), `writes \not in front of %q, which is not a relation to negate`, s.tex[m[2]:m[3]])
		}
	}
}

// overlay is the combining long solidus, which is how a negated relation falls
// in half.
const overlay = '\u0338'

// loose is a \not and whatever it was put in front of.
var loose = regexp.MustCompile(`\\not\s*(\\[A-Za-z]+|[^\s])`)

// relations are what \not is allowed to be in front of.
var relations = map[string]bool{
	"=": true, "<": true, ">": true, "∈": true, "⊂": true, "⊆": true,
	`\in`: true, `\ni`: true, `\subset`: true, `\subseteq`: true, `\supset`: true,
	`\supseteq`: true, `\sim`: true, `\simeq`: true, `\equiv`: true, `\approx`: true,
	`\cong`: true, `\parallel`: true, `\mid`: true, `\leq`: true, `\geq`: true,
	`\le`: true, `\ge`: true, `\to`: true, `\rightarrow`: true, `\models`: true,
	`\vdash`: true, `\preceq`: true, `\succeq`: true, `\prec`: true, `\succ`: true,
	`\perp`: true, `\divides`: true, `\asymp`: true, `\propto`: true,
}

// delimiters is M11 over one body.
//
// The round form is reported wherever it appears, because nothing in Markdown
// escapes a parenthesis and a paper has no other reason to write one. The square
// form is only read when the line is the delimiter or opens and closes on
// itself, since \[ in the middle of a sentence is Markdown escaping a bracket
// and this corpus writes hundreds of those in citation labels and table cells.
func (c Content) delimiters(rule string, at finder, b bodyLines) {
	for i, line := range b.text {
		if b.kind[i] == kindCode || b.kind[i] == kindFence {
			continue
		}
		for _, d := range []string{`\(`, `\)`} {
			if strings.Contains(line, d) {
				at(rule, i+1, `writes %s, and the mathematics of this corpus is written between dollars`, d)
			}
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == `\[`, trimmed == `\]`:
			at(rule, i+1, `opens a display block with %s, and a display block is written between a pair of $$ lines`, trimmed)
		case strings.HasPrefix(trimmed, `\[`) && strings.HasSuffix(trimmed, `\]`):
			at(rule, i+1, `writes a display formula between \[ and \], and a display block is written between a pair of $$ lines`)
		}
	}
}

// fences is M13 over one body.
func (c Content) fences(rule string, at finder, b bodyLines) {
	open, count := 0, 0
	for i, line := range b.text {
		switch b.kind[i] {
		case kindFence:
			if open == 0 {
				open, count = i+1, 0
				continue
			}
			if count%2 == 1 {
				at(rule, open, "is a fenced block with %s in it, so a reader that does not respect fences sets the prose after it as mathematics", plural(count, "dollar sign"))
			}
			open = 0
		case kindCode:
			for j := 0; j < len(line); j++ {
				if line[j] == '$' && (j == 0 || line[j-1] != '\\') {
					count++
				}
			}
		}
	}
}

// mathOnly are the characters M14 counts: ones that appear in mathematics and
// never in English prose.
//
// Greek is in here whole, and that is deliberate even though a paper can write
// out a Greek word. A paper writing Greek words and carrying no math span at
// all is still a paper worth looking at.
var mathOnly = func() map[rune]bool {
	m := map[rune]bool{}
	for _, r := range "∑∏∫∈∉⊂⊃⊆⊇≤≥≠≈≡∞√±×÷⋅→←↔⇒⇔∀∃∇∂⊕⊗∪∩∧∨¬∥⊥⊕" {
		m[r] = true
	}
	for r := 'Α'; r <= 'ω'; r++ {
		m[r] = true
	}
	return m
}()

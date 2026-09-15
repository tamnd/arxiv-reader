// Package objects reads a paper back out of the content plane as objects.
//
// 2166-06 says a paper in this corpus is not a document with sections but a set
// of objects, each with a kind, a permanent tag, a position, a body and a set of
// edges to other objects, and that the sections are one of the kinds. This is
// the package that produces that set, and everything downstream of it, the
// graph, the four emitters and the reading app, joins on what it writes.
//
// It derives, and it is derived from the Markdown rather than the other way
// round. The record under work/objects is a build artefact, the content files
// are the truth, and regenerating the record takes a second. That direction is
// the opposite of what a database first design would do and it is the one every
// prior project here arrived at the hard way: a corpus whose truth is a JSON
// file is a corpus nobody can fix with a text editor, and the fixing is most of
// the work.
//
// So nothing in here is allowed to know anything the Markdown does not say. A
// field that cannot be filled from the file is empty, which is the same rule
// 2166-06 puts on results, and for the same reason.
package objects

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/refs"
	"github.com/tamnd/arxiv-reader/tags"
)

// Record is one object, and it is the line 2166-06 section 6 prints.
//
// The field names are that file's field names. They are not this package's to
// rename, because the emitters, the graph and the reading app all read them and
// the spec is the only place the four of them agree.
type Record struct {
	Tag     string `json:"tag,omitempty"`
	Kind    string `json:"kind"`
	Subkind string `json:"subkind,omitempty"`
	Paper   string `json:"paper"`
	Version string `json:"version"`
	// Local is the name the object has inside its own paper, so thm-1, and File
	// is the content file it was read out of.
	//
	// File is not in the spec's example line and is here anyway. Every other
	// field says what the object is, and this is the only one that says where to
	// go and change it, which is the question somebody reading a record that
	// looks wrong actually has.
	Local string `json:"local"`
	File  string `json:"file"`
	Label string `json:"label,omitempty"`
	// Number is what the paper printed, so the 1 of Theorem 1, and it is a
	// string because papers number theorems 2.3 and appendices A.1.
	Number string `json:"number,omitempty"`
	// Section is the local identifier of the innermost section this sits in,
	// which is the file's own section until a subsection heading opens another.
	Section string   `json:"section,omitempty"`
	Order   int      `json:"order"`
	Title   string   `json:"title,omitempty"`
	BodyMD  string   `json:"body_md,omitempty"`
	Math    []string `json:"math,omitempty"`
	// RefsOut is the local identifiers this object links to inside the paper and
	// Cites is the bibliography anchors it links to, which are the same two
	// halves of the same Markdown links split by where they point.
	RefsOut []string `json:"refs_out,omitempty"`
	Cites   []string `json:"cites,omitempty"`
	// Concepts is always empty here. The concept layer is a model pass over the
	// metadata plane and it is written back into this field by ax concepts, so
	// the field exists and this pass does not fill it rather than the field
	// arriving later and every reader needing two shapes.
	Concepts []string `json:"concepts,omitempty"`
	// Resolved and Via are only ever set on a reference, and they are what the
	// citation half of the graph is built out of.
	Resolved string `json:"resolved,omitempty"`
	Via      string `json:"via,omitempty"`
	Path     string `json:"path"`
	// Confidence is how much the recognition of this object is worth, in the
	// vocabulary of 2166-08: certain, high or medium, and never low.
	Confidence string `json:"confidence"`
}

// Paper is one paper's objects, in reading order.
type Paper struct {
	Paper   string
	Version string
	Lang    string
	Objects []Record
	// Files is the content files that were read, in the order they were read.
	Files []string
}

// path is the path the paper was read on.
//
// Taken off the objects rather than held as a field, because the path is a fact
// about the content files and the content files are the only thing here that is
// allowed to say anything. A bibliography has no path of its own and belongs to
// the paper that printed it, so it takes this one.
func (p Paper) path() string {
	if len(p.Objects) == 0 {
		return ""
	}
	return p.Objects[0].Path
}

// Counts is how many objects of each kind a paper has.
func (p Paper) Counts() map[string]int {
	out := map[string]int{}
	for _, o := range p.Objects {
		out[o.Kind]++
	}
	return out
}

// Kinds is the sixteen kinds in the order 2166-06 tabulates them.
//
// The order is the spec's and not alphabetical, because the table is ordered
// from the structure of a paper down to its furniture, and a count printed in
// that order reads like a paper rather than like a map.
var Kinds = []string{
	"section", "statement", "definition", "remark", "problem", "exercise",
	"proof", "equation", "figure", "table", "code", "result", "artefact",
	"reference", "note", "front",
}

// Build reads one paper out of the content plane.
//
// The language is a parameter because a translation is another copy of the same
// objects with the same local identifiers and the same tags, so the object
// record of the Vietnamese copy is the object record of the paper with
// Vietnamese bodies in it. Nothing about the structure is allowed to differ,
// and an emitter that finds it does has found a translation that has drifted.
func Build(root, lang string, id axid.ID) (Paper, error) {
	dir := corpus.ContentDir(root, lang, id)
	names, err := files(dir)
	if err != nil {
		return Paper{}, err
	}
	if len(names) == 0 {
		return Paper{}, fmt.Errorf("objects: %s holds no content files, so there is nothing to read: run ax extract first", dir)
	}
	p := Paper{Lang: lang, Files: names}
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return Paper{}, err
		}
		doc, err := extract.ParseDocument(b)
		if err != nil {
			return Paper{}, fmt.Errorf("objects: %s: %w", name, err)
		}
		if p.Paper == "" {
			p.Paper, p.Version = doc.Front.Paper, doc.Front.Version
		}
		if doc.Front.Paper != p.Paper || doc.Front.Version != p.Version {
			return Paper{}, fmt.Errorf("objects: %s says it is %s %s and the files beside it say %s %s, so this directory holds two papers", name, doc.Front.Paper, doc.Front.Version, p.Paper, p.Version)
		}
		p.Objects = append(p.Objects, file(name, doc, len(p.Objects))...)
	}
	entries, err := refs.Load(corpus.RefsPath(root, id))
	if err != nil {
		return Paper{}, err
	}
	p.Objects = append(p.Objects, references(p, corpus.RefsPath("", id), entries.Entries, len(p.Objects))...)
	return p, nil
}

// file is one content file's objects, the file's own section first.
func file(name string, doc extract.Document, from int) []Record {
	front := doc.Front
	spans := tags.Spans(doc.Body)
	base := Record{
		Paper: front.Paper, Version: front.Version, File: name,
		Path: front.Path, Confidence: confidence(front.Path),
	}
	var out []Record

	// The file's own object comes first and it is not in the body. A section
	// that is a whole file has its heading in the front matter, which is why
	// local_id and label are fields up there, and the prose before the first
	// attribute block belongs to it.
	self := base
	self.Kind, self.Subkind = fileKind(front.Kind)
	self.Local, self.Label, self.Tag = front.LocalID, front.Label, front.Tag
	self.Title, self.Section = front.SectionTitle, front.LocalID
	self.Order = from
	self.BodyMD = lead(doc.Body, spans)
	if self.Kind == "front" {
		// The front matter is the metadata record, which is a fact arXiv
		// published rather than a reading of a rendering, so it is the one
		// object in a paper whose recognition is certain.
		self.Confidence, self.Section, self.Title = "certain", "", front.Title
	}
	self.fill()
	out = append(out, self)

	section := front.LocalID
	for i, s := range spans {
		r := base
		// A block with no class on it is an object the extractor gave an
		// anchor to and did not classify. It comes through with an empty
		// kind, because a record that quietly drops the objects nothing
		// knows the kind of is a record that hides the gap.
		r.Kind, r.Local, r.Tag = s.Class, s.Local, s.Attrs["tag"]
		r.Subkind, r.Label = s.Attrs["env"], s.Attrs["label"]
		r.Order = from + len(out)
		r.BodyMD = span(doc.Body, spans, i)
		r.Number, r.Title = heading(doc.Body, s)
		if r.Kind == "section" {
			// A subsection heading opens a new section, and everything under it
			// belongs to that section until the next heading. A theorem in
			// section 1.1 is in 1.1 and not in 1.
			section = s.Local
			r.Section = s.Local
		} else {
			r.Section = section
		}
		r.fill()
		out = append(out, r)
	}
	return out
}

// span is the body of the ith object in a file.
//
// An object's body runs from where its own body starts to where the next
// object's does, and the only subtlety is where a body starts. Every kind is
// written under its attribute block except the display equation, which is
// written above one, because the block would otherwise sit between the closing
// dollars and the paragraph that follows and split the display in two.
//
// Reading an equation forwards would give it the paragraph after it and none of
// its own mathematics, and would give the object before it the equation as well.
// Both halves of that are wrong and both are fixed by the same rule.
func span(body string, ss []tags.Span, i int) string {
	s := ss[i]
	end := len(body)
	if i+1 < len(ss) {
		end = edge(body, ss, i+1)
	}
	if end < s.End {
		end = s.End // Two blocks on one line, so this one has no body.
	}
	below := strings.TrimSpace(body[s.End:end])
	if s.Class != "equation" {
		return below
	}
	// An equation's own body is the display, which is the paragraph its block
	// sits at the bottom of, and the block line itself is not part of it. What
	// follows the block is whatever the paper wrote between the display and the
	// next object, which belongs to the equation the same way it would belong to
	// any other object it came after.
	at := tags.LineStart(body, s.Start)
	above := strings.TrimSpace(body[paragraph(body, floor(ss, i), at):at])
	return strings.TrimSpace(above + "\n\n" + below)
}

// lead is the body of the section a whole file is, which is the prose in front
// of everything else in it.
//
// tags.Lead answers nearly this and stops at the line the first block is on,
// which is a line too late when the first object is an equation, since the
// display above the block would come out belonging to the section as well as to
// the equation. The same boundary the objects inside the file are split on is
// the one the file's own object ends at.
func lead(body string, ss []tags.Span) string {
	if len(ss) == 0 {
		return strings.TrimSpace(body)
	}
	return strings.TrimSpace(body[:edge(body, ss, 0)])
}

// edge is the boundary in front of the ith object, which is where the body of
// the object before it stops.
//
// The line an object's attribute block sits on, except for an equation, whose
// display is written above the block and is part of the equation rather than
// part of whatever came before it.
func edge(body string, ss []tags.Span, i int) int {
	at := tags.LineStart(body, ss[i].Start)
	if ss[i].Class != "equation" {
		return at
	}
	return paragraph(body, floor(ss, i), at)
}

// floor is the end of the object in front of the ith one, which is as far back
// as anything here is allowed to walk.
func floor(ss []tags.Span, i int) int {
	if i > 0 {
		return ss[i-1].End
	}
	return 0
}

// paragraph is the start of the run of non-blank lines ending at at.
//
// A display is three lines of dollars and mathematics with the block on a
// fourth, and the four are one paragraph, so this finds the display by finding
// the paragraph. An equation the extractor could not read has a line of prose
// there instead and gets that, which is the right answer: the equation is
// whatever the paper printed in the place an equation goes.
func paragraph(body string, floor, at int) int {
	for at > floor {
		start := tags.LineStart(body, at-1)
		if start < floor || strings.TrimSpace(body[start:at-1]) == "" {
			break
		}
		at = start
	}
	return at
}

// fill works out the fields that are read off the body rather than off the
// block, which is every edge the object has and every piece of mathematics in
// it.
func (r *Record) fill() {
	r.Math = math(r.BodyMD)
	r.RefsOut, r.Cites = links(r.BodyMD)
}

// references is the bibliography, which is the one kind of object that is not
// in the content plane at all.
//
// A reference lives in manifests/refs because it is parsed by pattern from the
// bibliography and then resolved against the metadata plane, and both of those
// are facts about the corpus rather than prose in the paper. It is an object all
// the same: 2166-06 gives it one of the sixteen kinds, it carries a tag, and it
// is what the citation half of the graph hangs off.
func references(p Paper, file string, entries []refs.Entry, from int) []Record {
	out := make([]Record, 0, len(entries))
	for i, e := range entries {
		r := Record{
			Kind: "reference", Paper: p.Paper, Version: p.Version,
			Local: e.ID, File: file, Path: p.path(),
			Number: e.Label, Title: e.Title, BodyMD: e.Text,
			Resolved: e.Resolved, Via: e.Via,
			Order: from + i, Confidence: "high",
		}
		if e.Resolved != "" {
			// A reference that resolved carries the confidence of the match
			// rather than the confidence of the parse, because what anybody
			// reads this field for is how much the edge is worth.
			r.Confidence = e.Confidence()
		}
		out = append(out, r)
	}
	return out
}

// mdLink is a Markdown link to a fragment on the same page, which after the
// extractor has run is a link to another object in the same paper.
var mdLink = regexp.MustCompile(`\]\(#([^)\s]+)\)`)

// links splits an object's outgoing links into the two halves 2166-08 makes
// different predicates of.
//
// A link to a bibliography anchor is a citation and becomes a cites edge from
// this object to a paper. A link to anything else in the file is an internal
// cross reference and becomes a refers-to edge inside the paper. The anchors
// the extractor could not place are left in the prose exactly as the rendering
// wrote them, so a bib.bibx12 here is a citation whose entry ax refs has read
// and a fragment that is neither is a link this corpus does not resolve, which
// is recorded as neither rather than guessed at.
func links(body string) (out, cites []string) {
	seen := map[string]bool{}
	for _, m := range mdLink.FindAllStringSubmatch(body, -1) {
		to := m[1]
		if seen[to] {
			continue
		}
		seen[to] = true
		if strings.HasPrefix(to, "bib.") {
			cites = append(cites, to)
			continue
		}
		out = append(out, to)
	}
	return out, cites
}

// display and inline are the two ways mathematics is written in the body.
var (
	display = regexp.MustCompile(`(?s)\$\$(.+?)\$\$`)
	inline  = regexp.MustCompile(`(?s)(^|[^\\$])\$([^$]+?)\$`)
)

// math is every piece of mathematics in an object, in the order it appears.
//
// Display first and then inline, deduplicated, because the same symbol is
// written five times in a paragraph about it and a list that says so five times
// is a list nothing can join on. The point of the field is what mathematics an
// object contains, not how often.
func math(body string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	rest := body
	for _, m := range display.FindAllStringSubmatch(body, -1) {
		add(m[1])
		rest = strings.Replace(rest, m[0], "", 1)
	}
	for _, m := range inline.FindAllStringSubmatch(rest, -1) {
		add(m[2])
	}
	return out
}

// runin matches the bold heading an object starts with, so **Theorem 1**.
var runin = regexp.MustCompile(`\*\*(.+?)\*\*\s*$`)

// hash matches a Markdown heading, which is how a subsection is written.
var hash = regexp.MustCompile(`^#+\s+(.*?)\s*$`)

// heading is the number the paper printed and whatever it printed after it.
//
// The two shapes are a run-in heading for everything inside a section and a
// hash heading for a subsection. Both are read off the line the attribute block
// sits on, which is where the extractor put them, and a line that is neither
// gives an empty number and an empty title. That is the right answer for a
// display equation, whose number is in the rendering and whose line holds
// nothing but the block.
//
// The two are read by different rules and not by one, because a subsection
// heading is a number and a title and a run-in heading is a kind, a number and
// sometimes a title. Reading a subsection by the run-in rule throws away its
// first word, and a heading like A Run-in Heading loses the article and then
// reads it back as the section number.
func heading(body string, s tags.Span) (number, title string) {
	line := strings.TrimSpace(body[tags.LineStart(body, s.Start):s.Start])
	if m := hash.FindStringSubmatch(line); m != nil {
		return hashHeading(m[1])
	}
	if m := runin.FindStringSubmatch(line); m != nil {
		return runinHeading(m[1])
	}
	return "", ""
}

// hashHeading splits a subsection heading into its number and its title.
func hashHeading(line string) (number, title string) {
	fields := untagged(line)
	if len(fields) > 0 && sectioned(fields[0]) {
		number = strings.TrimRight(fields[0], ".")
		fields = fields[1:]
	}
	return number, strings.Join(fields, " ")
}

// runinHeading splits a run-in heading into its number and whatever follows it.
//
// The word in front is the kind the paper calls this, which is already in
// subkind, so what is left after it is the number and the title.
func runinHeading(line string) (number, title string) {
	fields := untagged(line)
	if len(fields) > 0 && !numbered(fields[0]) {
		fields = fields[1:]
	}
	if len(fields) > 0 && numbered(fields[0]) {
		number = strings.TrimRight(fields[0], ".")
		fields = fields[1:]
	}
	return number, strings.Join(fields, " ")
}

// untagged is a heading's words with the tag taken off the front.
//
// A tagged object has its tag printed in the heading, between the hashes and the
// title, and the tag is not part of either field.
func untagged(line string) []string {
	fields := strings.Fields(line)
	if len(fields) > 0 && isTag(fields[0]) {
		return fields[1:]
	}
	return fields
}

// isTag says whether a word is a permanent tag rather than part of a heading.
//
// A tag is four or more characters of the tag alphabet and a heading word is
// prose, so the two only collide on a word that is all capitals and digits and
// happens to be in a heading position, which is a word like LORA. That is why
// this is asked of the first word of a heading only, where the extractor puts
// the tag, and not of the heading in general.
func isTag(s string) bool {
	if len(s) < 4 {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune(tags.Alphabet, r) {
			return false
		}
	}
	return true
}

// numbered says whether a word is the number a paper printed for an object.
//
// Theorem 1, Theorem 2.3, Figure A.1 and Table B all happen. What does not
// happen is a number that starts with a letter and goes on into prose, so a
// single letter counts and a word does not.
func numbered(s string) bool {
	s = strings.TrimRight(s, ".")
	if s == "" {
		return false
	}
	if len(s) == 1 && s[0] >= 'A' && s[0] <= 'Z' {
		return true
	}
	if s[0] >= '0' && s[0] <= '9' {
		return true
	}
	if len(s) > 1 && s[0] >= 'A' && s[0] <= 'Z' && s[1] == '.' {
		return true
	}
	return false
}

// sectioned says whether a word is the number a paper printed for a section.
//
// Stricter than numbered by one case, which is the bare capital. A run-in
// heading has its kind in front of the number and so a Table B is unambiguous,
// but a subsection heading starts at the number and the first word of A Run-in
// Heading is an article. So a section number has to have a digit in it, and an
// unnumbered appendix keeps its whole heading as the title rather than losing
// the first word of it.
func sectioned(s string) bool {
	s = strings.TrimRight(s, ".")
	digit := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digit = true
		case r >= 'A' && r <= 'Z', r == '.':
		default:
			return false
		}
	}
	return digit
}

// fileKind maps what a content file says it is onto an object kind.
//
// Four file kinds and two object kinds. A references file and an appendix are
// both sections, and which one they are is worth keeping, so it goes in subkind
// where the environment name of a theorem goes: the kind is what this corpus
// calls the object and the subkind is what the paper called it.
func fileKind(s string) (kind, subkind string) {
	switch s {
	case "front":
		return "front", ""
	case "appendix", "references":
		return "section", s
	}
	return "section", ""
}

// confidence is how much the recognition of an object is worth, by the path it
// was read on.
//
// The render and source paths are structural. An object is a theorem there
// because LaTeXML said class ltx_theorem or because the source said
// \begin{theorem}, and that is a match against markup rather than a reading of
// prose, so it is high. The native path finds a theorem by the word Theorem
// followed by a number on a printed page and the vision path finds one by asking
// a model, and both of those can be wrong about a sentence, so they are medium.
//
// Nothing read off a path is ever certain. 2166-08 keeps that word for a fact
// arXiv published, and the one object in a paper that qualifies is the front
// matter, which Build sets by hand. A reference is the other exception and it
// does not come through here at all: its confidence is the confidence of the
// match, which references takes from the refs package.
func confidence(path string) string {
	switch path {
	case "render", "source":
		return "high"
	}
	return "medium"
}

// files is the content files of one paper, in reading order.
//
// Sorted by name, which is the order the extractor numbered them in, so 00_front
// comes before 01_introduction and the object order is the order of the paper.
// Anything that is not a Markdown file is somebody else's, and a notes.md
// somebody left in the directory is read like any other file: it has no front
// matter, so it fails the parse and says so, which is better than being silently
// skipped by a rule about names nobody can see.
func files(dir string) ([]string, error) {
	des, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, de := range des {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".md") {
			continue
		}
		out = append(out, de.Name())
	}
	sort.Strings(out)
	return out, nil
}

// Save writes the record, creating the directory if it is missing.
//
// JSONL and not YAML, for the same reason the metadata plane is: it is a file a
// program reads a line at a time and no person is meant to edit, and the one
// people do edit is the Markdown this was built from.
func (p Paper) Save(path string) error {
	body, err := p.Bytes()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

// Bytes is the record as it would be written.
//
// Separate from Save so that a run can tell whether anything changed without
// writing first, which is what makes a rebuild of an unchanged paper leave the
// file alone.
func (p Paper) Bytes() ([]byte, error) {
	var b strings.Builder
	for _, o := range p.Objects {
		line, err := json.Marshal(o)
		if err != nil {
			return nil, err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return []byte(b.String()), nil
}

// Load reads a record back.
func Load(path string) (Paper, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Paper{}, err
	}
	var p Paper
	for i, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r Record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return Paper{}, fmt.Errorf("objects: %s:%d: %w", path, i+1, err)
		}
		p.Objects = append(p.Objects, r)
		p.Paper, p.Version = r.Paper, r.Version
	}
	return p, nil
}

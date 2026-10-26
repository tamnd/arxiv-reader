// Package tex is the TeX front end of the census.
//
// It reads the source of a submission and gives the text that TeX would read,
// with a map from each offset back to its file and line. It is a tokenizer and
// a small interpreter, not a TeX engine. It must not fail where latexmlc fails,
// because a paper that latexmlc cannot convert still has theorems.
//
// The steps are the steps of AX3.1, in order: unpack, find the main file,
// expand the inclusions, read the local packages, remove what does not print,
// evaluate the simple conditionals, protect verbatim, record the macros, and
// build the tree. Each step writes a line in Steps, and the census record
// keeps them.
//
// All removal is done with spaces. The expanded text keeps the length and the
// lines of what it was made from, so each offset maps to a line exactly.
package tex

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/tamnd/arxiv-reader/source"
)

// Document is one main file of a submission, read by the front end.
type Document struct {
	// Main is the main file, and Mains is each main file of the submission.
	// A submission with more than one main file is multi-main, and each main
	// file gives a Document of its own.
	Main  string
	Mains []string
	// Text is the expanded source. The parts that do not print are spaces.
	Text []byte
	// Body is the offset just after \begin{document}, and End is the offset
	// of \end{document}. Both are zero when the file has no document body.
	Body, End int
	// Class is the document class and Options are its options.
	Class   string
	Options []string
	// Packages is each package that the preamble loads, in order.
	Packages []string
	// Local is each local .sty and .cls file of the submission, cleaned.
	Local map[string][]byte
	// Macros is the macro table.
	Macros *Macros
	// Conditional is each region whose condition the front end could not
	// decide. Both branches stay, and items in them get the flag conditional.
	Conditional []Region
	// Files is each file that the expansion read, in the order of reading.
	Files []string
	// Missing is each inclusion whose file is not in the submission.
	Missing []string
	// Steps says what the front end did, one line for each step.
	Steps []string
	// Tree is the tree of sections and environments of the body.
	Tree *Tree

	segs []segment
}

// Region is a span of the expanded text.
type Region struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// In tells if the offset is in one of the regions.
func In(rs []Region, off int) bool {
	for _, r := range rs {
		if off >= r.From && off < r.To {
			return true
		}
	}
	return false
}

// Pos maps an offset of the expanded text to its file and line.
func (d *Document) Pos(off int) Pos { return pos(d.segs, d.Text, off) }

// ErrNoDocument is the error for a main file with no \begin{document}.
var ErrNoDocument = errors.New("tex: the main file has no \\begin{document}")

// Read runs the front end on each main file of a submission.
func Read(b *source.Bundle) ([]*Document, error) {
	mains, err := b.Mains()
	if err != nil {
		return nil, err
	}
	raw := map[string][]byte{}
	var all [][]byte
	for _, f := range b.Files {
		data := toUTF8(f.Data)
		raw[path.Clean(f.Name)] = data
		all = append(all, data)
	}
	excluded := Excluded(all...)
	cleaned := map[string][]byte{}
	local := map[string][]byte{}
	for name, data := range raw {
		if isTeXFile(name) || isStyle(name) {
			cleaned[name] = Clean(data, excluded)
		}
		if isStyle(name) {
			local[name] = cleaned[name]
		}
	}
	var docs []*Document
	for _, main := range mains {
		d, err := build(path.Clean(main), cleaned, local, excluded)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", main, err)
		}
		d.Mains = mains
		docs = append(docs, d)
	}
	return docs, nil
}

// ReadFile runs the front end on one TeX file with no submission around it.
//
// The tests and the regression set T use it.
func ReadFile(name string, data []byte) (*Document, error) {
	docs, err := Read(&source.Bundle{Files: []source.File{{Name: name, Data: data}}})
	if err != nil {
		return nil, err
	}
	return docs[0], nil
}

func isStyle(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	return ext == ".sty" || ext == ".cls" || ext == ".def"
}

// toUTF8 reads a file that is not UTF-8 as Latin-1.
//
// Papers from the 1990s are often Latin-1. A Latin-1 file is never valid UTF-8
// when it has accents, so the test is safe.
func toUTF8(b []byte) []byte {
	if utf8.Valid(b) {
		return b
	}
	var out bytes.Buffer
	for _, c := range b {
		out.WriteRune(rune(c))
	}
	return out.Bytes()
}

func build(main string, files, local map[string][]byte, excluded map[string]bool) (*Document, error) {
	if _, ok := files[main]; !ok {
		return nil, fmt.Errorf("tex: the submission has no file %s", main)
	}
	d := &Document{Main: main, Local: local}
	d.step("unpack: %d files", len(files))
	d.step("main: %s", main)

	e := &expander{files: files, seen: map[string]bool{main: true}}
	e.expand(main, 0)
	if e.tooBig {
		return nil, fmt.Errorf("tex: the expanded source is larger than %d bytes", maxExpanded)
	}
	d.Text, d.segs, d.Files, d.Missing = e.out.Bytes(), e.segs, e.read, e.missing
	d.step("include: %d files read, %d not found", len(d.Files), len(d.Missing))
	d.step("local: %d style files", len(local))
	d.step("clean: comments, verbatim and %d comment environments", len(excluded))

	blankFalse(d.Text)
	t := d.toggles()
	d.findBody()
	n := d.conditionals()
	d.step("conditionals: %d decided, %d flag tests decided, %d kept", n, t, len(d.Conditional))
	d.Macros = ReadMacros(d.Text[:d.preambleEnd()], local)
	d.Macros.Add(ReadMacros(d.bodyText(), nil))
	d.step("macros: %d commands, %d environments", len(d.Macros.Commands), len(d.Macros.Envs))
	d.findPreamble()
	d.Tree = BuildTree(d.Text, d.Body, d.End)
	d.step("tree: %d nodes", len(d.Tree.Nodes))
	if d.Body == 0 {
		return d, ErrNoDocument
	}
	return d, nil
}

func (d *Document) step(format string, args ...any) {
	d.Steps = append(d.Steps, fmt.Sprintf(format, args...))
}

var (
	beginDocument = regexp.MustCompile(`\\begin\s*\{document\}`)
	endDocument   = regexp.MustCompile(`\\end\s*\{document\}`)
)

func (d *Document) findBody() {
	if m := beginDocument.FindIndex(d.Text); m != nil {
		d.Body = m[1]
		d.End = len(d.Text)
		if e := endDocument.FindIndex(d.Text[d.Body:]); e != nil {
			d.End = d.Body + e[0]
		}
	}
}

func (d *Document) preambleEnd() int {
	if d.Body == 0 {
		return len(d.Text)
	}
	return d.Body
}

func (d *Document) bodyText() []byte {
	if d.Body == 0 {
		return nil
	}
	return d.Text[d.Body:d.End]
}

// Preamble is the text before \begin{document}.
func (d *Document) Preamble() []byte { return d.Text[:d.preambleEnd()] }

var (
	classCmd   = regexp.MustCompile(`\\document(?:class|style)\s*(?:\[([^\]]*)\])?\s*\{([^}]*)\}`)
	packageCmd = regexp.MustCompile(`\\(?:usepackage|RequirePackage)\s*(?:\[[^\]]*\])?\s*\{([^}]*)\}`)
)

func (d *Document) findPreamble() {
	pre := d.Preamble()
	if m := classCmd.FindSubmatch(pre); m != nil {
		d.Class = strings.TrimSpace(string(m[2]))
		for o := range strings.SplitSeq(string(m[1]), ",") {
			if o = strings.TrimSpace(o); o != "" {
				d.Options = append(d.Options, o)
			}
		}
	}
	seen := map[string]bool{}
	for _, m := range packageCmd.FindAllSubmatch(pre, -1) {
		for p := range strings.SplitSeq(string(m[1]), ",") {
			if p = strings.TrimSpace(p); p != "" && !seen[p] {
				seen[p] = true
				d.Packages = append(d.Packages, p)
			}
		}
	}
	// A 2.09 paper loads its packages as options of \documentstyle.
	if strings.Contains(string(pre), `\documentstyle`) {
		for _, o := range d.Options {
			if !seen[o] {
				seen[o] = true
				d.Packages = append(d.Packages, o)
			}
		}
	}
}

// HasPackage tells if the preamble loads the package.
func (d *Document) HasPackage(name string) bool {
	for _, p := range d.Packages {
		if p == name {
			return true
		}
	}
	return false
}

// sortedKeys is the keys of m in order, so that a run reads them the same way
// each time.
func sortedKeys(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

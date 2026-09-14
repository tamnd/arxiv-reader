package extract

import (
	"fmt"
	"strings"
	"unicode"
)

// File is one file of the content plane, named and ready to write.
type File struct {
	Name string
	Doc  Document
}

// Files cuts a paper into the files the content plane stores.
//
// One file per top level section, plus the front. That is the unit a person
// reads, the unit a translator is given and the unit a diff is reviewed in, and
// a whole paper in one file is none of those: a forty page paper as one
// Markdown file is a file nobody reviews and a translation job nobody can
// retry.
//
// The base carries every field that is the same in every file of the paper, and
// this fills in the ones that are not.
func Files(p *Paper, base Front) ([]File, error) {
	if p == nil {
		return nil, fmt.Errorf("extract: there is no paper to split")
	}
	if r, bad := p.Reject(); bad {
		return nil, r
	}
	// One namer for the whole paper, walked in reading order, because a local
	// identifier is unique within a paper and not within a file.
	n := newNames()

	front := base
	front.Section = 0
	front.Kind = "front"
	front.SectionTitle = "Front matter"
	front.LocalID = "front"
	w := &body{names: n}
	w.objects++ // The front itself, which is one of the sixteen kinds.
	w.blocks(p.Abstract, within{})
	out := []File{{Name: "00_front.md", Doc: Document{Front: count(front, w), Body: w.String()}}}

	// A paper with no sections at all is a short note, and it gets one file
	// holding the whole of it. That is not an error and it is not worth a
	// special case anywhere downstream.
	if len(p.Sections) == 0 {
		f := base
		f.Section = 1
		f.Kind = "section"
		f.SectionTitle = "Body"
		f.LocalID = "s1"
		w := &body{names: n}
		out = append(out, File{Name: "01_body.md", Doc: Document{Front: count(f, w), Body: w.String()}})
		return out, nil
	}

	for i, s := range p.Sections {
		f := base
		f.Section = i + 1
		f.Kind = "section"
		if s.Kind == "appendix" {
			f.Kind = "appendix"
		}
		f.SectionTitle = s.Title
		f.Tag = "" // The section's permanent tag, filled in by ax tags assign.
		w := &body{names: n}
		w.objects++ // The section itself, whose heading lives in the front matter.
		// The heading of a top level section is in the front matter rather than
		// in the body, so there is no line for its attribute block to sit on,
		// but references to it still have to land somewhere. Naming it here
		// gives it an identifier the rest of the paper can point at.
		f.LocalID = n.section(s, []int{i + 1})
		w.blocks(s.Blocks, within{})
		for j, sub := range s.Sections {
			w.section(sub, 2, []int{i + 1, j + 1})
		}
		out = append(out, File{
			Name: fmt.Sprintf("%02d_%s.md", i+1, slug(s.Title)),
			Doc:  Document{Front: count(f, w), Body: w.String()},
		})
	}
	// Last, because a reference in section 1 to a figure in section 4 can only
	// be rewritten once section 4 has been walked and its figure named.
	for i := range out {
		out[i].Doc.Body = link(out[i].Doc.Body, n.anchors)
	}
	return out, nil
}

// count copies what the body found into the front matter.
func count(f Front, w *body) Front {
	f.Objects = w.objects
	f.Equations = w.equations
	f.CodeBlocks = w.code
	f.Figures = w.figures
	f.Tables = w.tables
	f.Statements = w.statements
	if f.Figures == nil {
		f.Figures = []string{}
	}
	if f.Tables == nil {
		f.Tables = []string{}
	}
	if f.Statements == nil {
		f.Statements = []string{}
	}
	return f
}

// slug is a section title turned into the part of a file name that says which
// section it is.
//
// Ascii only, words joined with underscores, capped in length. The number in
// front of it is what orders the files and what makes them unique, so this only
// has to be readable. A title that is entirely mathematics, which happens,
// leaves nothing behind and the file is called section.
func slug(title string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if space && b.Len() > 0 {
				b.WriteByte('_')
			}
			space = false
			b.WriteRune(r)
		case unicode.IsLetter(r), unicode.IsDigit(r):
			// A non-ascii letter is dropped rather than transliterated. A file
			// name is not the place to guess at somebody's alphabet.
			space = true
		default:
			space = true
		}
	}
	s := b.String()
	const most = 40
	if len(s) > most {
		s = s[:most]
		if i := strings.LastIndex(s, "_"); i > 0 {
			s = s[:i]
		}
	}
	s = strings.Trim(s, "_")
	if s == "" {
		return "section"
	}
	return s
}

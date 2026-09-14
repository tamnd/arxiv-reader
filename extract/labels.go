package extract

import (
	"encoding/xml"
	"io"
	"strings"
)

// The author's own \label, which is the one name in a paper that survives a
// revision.
//
// Numbering does not. An author who inserts a section renumbers every theorem
// after it without touching a character of them, so matching last year's
// Theorem 3 to this year's Theorem 4 by number matches the wrong theorem.
// Relabelling is manual and renumbering is automatic, so \label{thm:main} is
// still \label{thm:main} in v7, and it is what ax tags diff follows when it
// decides which object in the new extraction is which object in the old one.
//
// It is not in the HTML. LaTeXML resolves every \label into a cross reference
// while it converts and the name the author wrote does not reach the page, on
// arXiv's rendering or on ours, which was worth checking before building on it.
// It is in LaTeXML's intermediate XML, as a labels attribute beside the xml:id,
// and the xml:id is the same string as the id in the HTML. So the source path
// keeps the XML, reads the labels out of it, and hands this package a map from
// one to the other. The render path has no XML and no way to get one, so a paper
// read from arXiv's rendering carries no labels and ax tags diff falls to its
// second pass for it.

// Labels reads the author's label for every element of a LaTeXML document.
//
// The key is the element's xml:id, which is the anchor the HTML carries, and
// the value is the label with LaTeXML's LABEL: prefix taken off.
//
// An element can carry more than one label, because \label twice in one
// environment is legal and authors do it. The first is kept, since the passes
// this feeds want one name per object and the first is the one the author wrote
// first.
func Labels(doc []byte) (map[string]string, error) {
	out := map[string]string{}
	d := xml.NewDecoder(strings.NewReader(string(doc)))
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		el, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		var id, labels string
		for _, a := range el.Attr {
			switch {
			case a.Name.Local == "id":
				id = a.Value
			case a.Name.Local == "labels":
				labels = a.Value
			}
		}
		if id == "" || labels == "" {
			continue
		}
		if name := first(labels); name != "" {
			out[id] = name
		}
	}
}

// first is the first label of a labels attribute, without its prefix.
//
// LaTeXML writes them space separated and prefixed, so "LABEL:thm:main
// LABEL:thm:alt" is two labels and the first is thm:main. Anything without the
// prefix is not a \label the author wrote, so it is stepped over rather than
// guessed at.
func first(labels string) string {
	for _, f := range strings.Fields(labels) {
		if name, ok := strings.CutPrefix(f, "LABEL:"); ok && name != "" {
			return name
		}
	}
	return ""
}

// Label fills in the author's label on every block and section that has one,
// and says how many it filled.
//
// Matched by LaTeXML's anchor, which both the XML and the HTML carry and which
// is the same string in each. A block with no anchor cannot be matched and does
// not need to be: an object nothing can link to is an object no tag follows.
func (p *Paper) Label(byID map[string]string) int {
	if len(byID) == 0 {
		return 0
	}
	n := 0
	var blocks func([]Block)
	blocks = func(bs []Block) {
		for i := range bs {
			if name, ok := byID[bs[i].ID]; ok {
				bs[i].Label = name
				n++
			}
			blocks(bs[i].Blocks)
		}
	}
	var sections func([]Section)
	sections = func(ss []Section) {
		for i := range ss {
			if name, ok := byID[ss[i].ID]; ok {
				ss[i].Label = name
				n++
			}
			blocks(ss[i].Blocks)
			sections(ss[i].Sections)
		}
	}
	blocks(p.Abstract)
	sections(p.Sections)
	return n
}

// Labelled is how many blocks and sections carry the author's own label.
//
// Worth printing next to the block counts. It is the share of a paper that ax
// tags diff can follow across a revision by name rather than by guessing, so a
// paper where it is zero is a paper where every tag rests on the weaker passes.
func (p Paper) Labelled() int {
	n := 0
	var blocks func([]Block)
	blocks = func(bs []Block) {
		for _, b := range bs {
			if b.Label != "" {
				n++
			}
			blocks(b.Blocks)
		}
	}
	var sections func([]Section)
	sections = func(ss []Section) {
		for _, s := range ss {
			if s.Label != "" {
				n++
			}
			blocks(s.Blocks)
			sections(s.Sections)
		}
	}
	blocks(p.Abstract)
	sections(p.Sections)
	return n
}

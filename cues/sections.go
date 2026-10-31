package cues

import (
	"strings"

	"github.com/tamnd/arxiv-reader/tex"
)

// Section is the running text of one section, as the sentences that the
// prose pass of the understand role reads (AX7.4 step 2). It has the text of
// the paper, so it is never written to the corpus: ax prose prints it for
// one run of the pass.
type Section struct {
	// Section is the section, as sec-3, in the form of the object records.
	// The text before the first section has no section.
	Section   string     `json:"section"`
	Sentences []Sentence `json:"sentences"`
}

// Sentence is one sentence of the running text. N is its number in the
// paper, from 1, so that a span of the pass is a range of numbers that code
// can check. File and Line are where it starts in the source.
type Sentence struct {
	N    int    `json:"n"`
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text"`
	Cues []Mark `json:"cues,omitempty"`
}

// Sections gives the running text of one main file, section by section, with
// the cues of each sentence marked. First is the number of the first sentence,
// so that the sentences of the second main file go on from the first.
func (c *Cues) Sections(d *tex.Document, items []tex.Region, prefix string, first int) []Section {
	var out []Section
	for i, s := range c.read(d, items, prefix) {
		if len(out) == 0 || out[len(out)-1].Section != s.Section {
			out = append(out, Section{Section: s.Section})
		}
		p := d.Pos(s.From)
		sec := &out[len(out)-1]
		sec.Sentences = append(sec.Sentences, Sentence{N: first + i, File: p.File, Line: p.Line, Text: strings.Join(strings.Fields(string(d.Text[s.From:s.To])), " "), Cues: s.Cues})
	}
	return out
}

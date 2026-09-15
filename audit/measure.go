package audit

import (
	"unicode/utf8"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/refs"
)

// Measurement is one paper's numbers, for the baselines.
//
// Counts and not rates, because a rate is a division and the divisor is worth
// keeping: a paper with two references, one of them resolved, has a resolution
// rate of a half and says almost nothing, and the only thing that can tell that
// paper from one with two hundred is the count.
type Measurement struct {
	ID string
	// Displays is how many display blocks the paper's bodies hold, and
	// Characters is how long those bodies are.
	Displays, Characters int
	// Entries is how many bibliography entries the paper has and Resolved is
	// how many of them were matched to a paper in the metadata plane.
	Entries, Resolved int
}

// Measure reads the papers and counts what the baselines are built from.
//
// It is here and not in the baselines package because it reads a content file,
// which is the thing this package knows how to do, and it deliberately does not
// read the metadata plane: the category a paper belongs to is a fact about the
// record and the join is made by the caller, one read of the plane for the
// whole corpus rather than one per paper.
//
// Nothing here is a rule and nothing here reports a finding. A paper whose
// files do not parse is measured on the ones that do, because a baseline is a
// description of the corpus and a corpus with a broken file in it is still the
// corpus every threshold has to be fair to.
func (c Content) Measure(papers []axid.ID) ([]Measurement, error) {
	out := make([]Measurement, 0, len(papers))
	for _, id := range papers {
		files, err := c.read(id)
		if err != nil {
			return nil, err
		}
		m := Measurement{ID: id.Canonical}
		for _, f := range files {
			if !f.ok {
				continue
			}
			m.Characters += utf8.RuneCountInString(f.doc.Body)
			for _, s := range spans(classify(f.doc.Body)) {
				if s.display {
					m.Displays++
				}
			}
		}
		bib, err := refs.Load(corpus.RefsPath(c.Root, id))
		// A paper nobody has run ax refs over has no bibliography and no
		// resolution rate, which is not the same as a rate of nought. It is
		// left out of that metric and counted in the category all the same.
		if err == nil {
			m.Entries = len(bib.Entries)
			for _, e := range bib.Entries {
				if e.Resolved != "" {
					m.Resolved++
				}
			}
		}
		if c.Log != nil {
			c.Log(id.Canonical, len(files))
		}
		out = append(out, m)
	}
	return out, nil
}

package seed

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/internal/prose"
)

// Text is what ax select seed --propose prints.
//
// The counts first, because the number that matters most about a proposal is
// not how many papers it offers but how much of arXiv it drew them from. A
// thousand candidates out of a plane where nobody has resolved a licence is a
// thousand candidates out of the handful of papers somebody happened to look
// at, and that is a different file from the same thousand out of three million.
func (p Proposal) Text() string {
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "records\t%s\tscanned in the metadata plane\n", prose.Thousands(p.Counts.Records))
	fmt.Fprintf(tw, "unresolved\t%s\t%s, nobody has run ax licence resolve over them\n", prose.Thousands(p.Counts.Unresolved), prose.Percent(prose.Share(p.Counts.Unresolved, p.Counts.Records)))
	fmt.Fprintf(tw, "refused\t%s\t%s, the corpus may publish the record and nothing else\n", prose.Thousands(p.Counts.Refused), prose.Percent(prose.Share(p.Counts.Refused, p.Counts.Records)))
	fmt.Fprintf(tw, "eligible\t%s\t%s, %s\n", prose.Thousands(p.Counts.Eligible), prose.Percent(prose.Share(p.Counts.Eligible, p.Counts.Records)), classes(p.Classes))
	fmt.Fprintf(tw, "offered\t%s\tthe top %d of each archive and year, over %s\n", prose.Thousands(p.Counts.Offered), p.Top, prose.Count(len(p.Groups), "group"))
	tw.Flush()

	if len(p.Groups) > 0 {
		fmt.Fprintln(&b)
		gw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
		for _, a := range archives(p.Groups) {
			fmt.Fprintf(gw, "%s\t%s\t%s\tfrom %s\n", a.Archive, prose.Count(a.Taken, "paper"), a.Span, prose.Count(a.Eligible, "eligible paper"))
		}
		gw.Flush()
	}

	fmt.Fprintln(&b)
	if p.Counts.Offered == 0 {
		fmt.Fprintln(&b, "No paper in the metadata plane both has a licence on record and is in one of those classes, so there is nothing to propose.")
		fmt.Fprintln(&b, "Run ax harvest to fill the plane and ax licence resolve to work out what each paper permits, and then run this again.")
		return b.String()
	}
	fmt.Fprintln(&b, "This is a candidate list and not a selection.")
	fmt.Fprintln(&b, "Every row is a paper the licence gate says this corpus may publish, which is the only claim the file makes.")
	fmt.Fprintln(&b, "Three of the score's four terms are nought at seed time, so the order inside a group is oldest first and is not a ranking of anything.")
	fmt.Fprintln(&b, "Delete the rows that do not belong, and then run ax select seed --commit.")
	return b.String()
}

// archive is one archive with its years folded together, which is how somebody
// reads a proposal: cs and math and hep-th, not cs in 2007 and cs in 2008.
type archive struct {
	Archive  string
	Taken    int
	Eligible int
	Span     string
}

func archives(groups []Group) []archive {
	var out []archive
	at := map[string]int{}
	lo, hi := map[string]int{}, map[string]int{}
	for _, g := range groups {
		i, ok := at[g.Archive]
		if !ok {
			out = append(out, archive{Archive: g.Archive})
			i = len(out) - 1
			at[g.Archive] = i
			lo[g.Archive], hi[g.Archive] = g.Year, g.Year
		}
		out[i].Taken += g.Taken
		out[i].Eligible += g.Eligible
		if g.Year < lo[g.Archive] {
			lo[g.Archive] = g.Year
		}
		if g.Year > hi[g.Archive] {
			hi[g.Archive] = g.Year
		}
	}
	for i := range out {
		a := out[i].Archive
		if lo[a] == hi[a] {
			out[i].Span = fmt.Sprintf("%d", lo[a])
			continue
		}
		out[i].Span = fmt.Sprintf("%d to %d", lo[a], hi[a])
	}
	return out
}

// classes is the access class filter as a sentence.
func classes(in []corpus.Access) string {
	names := make([]string, 0, len(in))
	for _, c := range in {
		names = append(names, string(c))
	}
	switch len(names) {
	case 0:
		return "no class at all"
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

package licence

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/scope"
)

// Class is a licence class of spec 9.3, which is what math2 may take from a
// version (AX2.3). It is not the access class of the corpus: CC BY-SA is
// share-alike for the corpus and open for math2.
type Class string

const (
	ClassOpen         Class = "open"
	ClassShareAlikeNC Class = "share-alike-nc"
	ClassVerbatim     Class = "verbatim"
	ClassRecord       Class = "record"
)

// Classes is the classes in the order of the report, most open first.
var Classes = []Class{ClassOpen, ClassShareAlikeNC, ClassVerbatim, ClassRecord}

// ClassOf is the spec 9.3 class of a licence. A version with no licence, or
// an unknown one, is a record version.
func ClassOf(l corpus.Licence) Class {
	switch l {
	case corpus.LicenceCC0, corpus.LicenceCCBY, corpus.LicenceCCBYSA:
		return ClassOpen
	case corpus.LicenceCCBYNCSA:
		return ClassShareAlikeNC
	case corpus.LicenceCCBYNCND:
		return ClassVerbatim
	}
	return ClassRecord
}

// ScopeRow is the count of each class for one year or one category.
type ScopeRow struct {
	Key     string
	Records int
	Classes map[Class]int
}

// Share is the share of a class in the row.
func (r ScopeRow) Share(c Class) float64 { return prose.Share(r.Classes[c], r.Records) }

// ScopeCensus is the licence census of a scope (AX-M1, AQ03).
type ScopeCensus struct {
	Reason    string
	Generated time.Time
	Total     ScopeRow
	// Licences is the count of each licence.
	Licences map[corpus.Licence]int
	// Years is one row for each year that the id names, oldest first.
	Years []ScopeRow
	// Categories is one row for each primary category in scope, largest
	// first, and one last row for the papers in scope by a cross-list only.
	Categories []ScopeRow
	// Cross is the papers in scope by a cross-list only.
	Cross int
}

// ScopeCounter counts a scope one record at a time.
type ScopeCounter struct {
	reason   string
	total    ScopeRow
	licences map[corpus.Licence]int
	years    map[string]*ScopeRow
	cats     map[string]*ScopeRow
	cross    int
}

// CrossOnly is the key of the category row for the papers that are in scope
// by a cross-list only.
const CrossOnly = "cross-list only"

// NewScopeCounter returns an empty count for a reason.
func NewScopeCounter(reason string) *ScopeCounter {
	return &ScopeCounter{reason: reason, total: ScopeRow{Key: "all", Classes: map[Class]int{}}, licences: map[corpus.Licence]int{}, years: map[string]*ScopeRow{}, cats: map[string]*ScopeRow{}}
}

// Add counts one record when it is in the math scope, and tells if it was.
func (c *ScopeCounter) Add(shard string, r metadata.Record) (bool, error) {
	in, cross := scope.Of(r.Categories)
	if !in {
		return false, nil
	}
	month, err := metadata.ShardMonth(shard)
	if err != nil {
		return false, fmt.Errorf("licence: %w", err)
	}
	l, _ := Latest(r)
	class := ClassOf(l)
	c.licences[l]++
	add := func(m map[string]*ScopeRow, key string) {
		row := m[key]
		if row == nil {
			row = &ScopeRow{Key: key, Classes: map[Class]int{}}
			m[key] = row
		}
		row.Records++
		row.Classes[class]++
	}
	c.total.Records++
	c.total.Classes[class]++
	add(c.years, fmt.Sprint(month.Year()))
	if cross {
		c.cross++
		add(c.cats, CrossOnly)
	} else {
		add(c.cats, r.Primary())
	}
	return true, nil
}

// Census freezes the count.
func (c *ScopeCounter) Census() ScopeCensus {
	out := ScopeCensus{Reason: c.reason, Total: c.total, Licences: c.licences, Cross: c.cross}
	for _, r := range c.years {
		out.Years = append(out.Years, *r)
	}
	sort.Slice(out.Years, func(i, j int) bool { return out.Years[i].Key < out.Years[j].Key })
	for _, r := range c.cats {
		out.Categories = append(out.Categories, *r)
	}
	sort.Slice(out.Categories, func(i, j int) bool {
		a, b := out.Categories[i], out.Categories[j]
		if (a.Key == CrossOnly) != (b.Key == CrossOnly) {
			return b.Key == CrossOnly
		}
		if a.Records != b.Records {
			return a.Records > b.Records
		}
		return a.Key < b.Key
	})
	return out
}

// Markdown is the report as it is committed to reports/licence-math.md.
func (c ScopeCensus) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# The licence census of %s\n\n", c.Reason)
	b.WriteString(c.headline())
	fmt.Fprintf(&b, "Counted on %s from the metadata plane. The list of the papers is manifests/scope/math.txt.\n\n", c.Generated.UTC().Format("2 January 2006"))

	b.WriteString("## How to read it\n\n")
	b.WriteString("A paper is in scope when one of its categories is a math category, math-ph, or one of the old archives alg-geom, dg-ga, funct-an and q-alg (AX1.2).\n")
	b.WriteString("The class is the class of spec 9.3 for the licence of the latest version (AX2.3). CC0, CC BY and CC BY-SA are open. CC BY-NC-SA is share-alike-nc. CC BY-NC-ND is verbatim. The arXiv licence and no licence are record.\n")
	b.WriteString("The bulk surfaces give one licence for each paper, and it is the licence of the latest version. So the count of an earlier version can be wrong. ax licence resolve reads the licence of each version when a paper is selected.\n")
	b.WriteString("The year is the year that the id names, so the year when arXiv announced the paper.\n\n")

	b.WriteString("## By class\n\n")
	b.WriteString("| Class | Papers | Share |\n| --- | ---: | ---: |\n")
	for _, cl := range Classes {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", cl, prose.Thousands(c.Total.Classes[cl]), prose.Percent(c.Total.Share(cl)))
	}

	b.WriteString("\n## By licence\n\n")
	b.WriteString("| Licence | Class | Papers | Share |\n| --- | --- | ---: | ---: |\n")
	for _, l := range append(append([]corpus.Licence{}, corpus.Licences...), "") {
		label := string(l)
		if l == "" {
			label = "not recorded"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", label, ClassOf(l), prose.Thousands(c.Licences[l]), prose.Percent(prose.Share(c.Licences[l], c.Total.Records)))
	}

	b.WriteString("\n## By year\n\n")
	c.table(&b, "Year", c.Years)
	b.WriteString("\n## By category\n\n")
	b.WriteString("Each paper is counted once, under its primary category. A paper whose primary category is not in scope is in the last row.\n\n")
	c.table(&b, "Category", c.Categories)
	return b.String()
}

func (c ScopeCensus) table(b *strings.Builder, key string, rows []ScopeRow) {
	fmt.Fprintf(b, "| %s | Papers | open | share-alike-nc | verbatim | record | open share |\n", key)
	b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, r := range rows {
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %s |\n", r.Key, prose.Thousands(r.Records),
			prose.Thousands(r.Classes[ClassOpen]), prose.Thousands(r.Classes[ClassShareAlikeNC]),
			prose.Thousands(r.Classes[ClassVerbatim]), prose.Thousands(r.Classes[ClassRecord]),
			prose.Percent(r.Share(ClassOpen)))
	}
}

// Text is the short form, for a terminal.
func (c ScopeCensus) Text() string {
	var b strings.Builder
	b.WriteString(c.headline())
	for _, cl := range Classes {
		fmt.Fprintf(&b, "  %-15s %10s  %6s\n", cl, prose.Thousands(c.Total.Classes[cl]), prose.Percent(c.Total.Share(cl)))
	}
	return b.String()
}

func (c ScopeCensus) headline() string {
	if c.Total.Records == 0 {
		return "Nothing counted, because no paper in the metadata plane is in scope.\n"
	}
	return fmt.Sprintf("%s are in %s, and %s of them are in scope by a cross-list only. %s are open, and %s are record.\n",
		prose.Count(c.Total.Records, "paper"), c.Reason, prose.Thousands(c.Cross),
		prose.Thousands(c.Total.Classes[ClassOpen]), prose.Thousands(c.Total.Classes[ClassRecord]))
}

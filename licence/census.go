// Package licence counts what the corpus is permitted to do with what it
// holds, over the metadata plane and without touching the network.
//
// The count is an upper bound and the report says so in its first paragraph.
// Every surface that serves arXiv metadata at three million scale, which is
// Kaggle, the Hugging Face mirror and OAI-PMH alike, carries one licence per
// paper and not one per version, and that one licence is the latest version's.
// A paper whose author relicensed on the way from v1 to v3 counts here under
// the v3 licence for all three versions. Measured over a random sample of forty
// multi-version papers in September 2026, five disagreed between v1 and the
// latest, and four of those five had a latest version more permissive than the
// first. Roughly two fifths of arXiv is multi-version, so the overstatement is
// on the order of five percent of the corpus and it runs in the direction that
// matters. That is what ax licence resolve is for, and it runs per paper at
// selection rather than over three million abs pages.
package licence

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/metadata"
)

// VersionCap is the last row of the version distribution, which counts every
// paper with that many versions or more.
const VersionCap = 5

// Census is the finished count.
type Census struct {
	// Generated is when the count was taken.
	Generated time.Time
	// Records is how many papers were counted.
	Records int
	// Months is how many shards were read.
	Months int
	// Licences is one row per licence, most permissive first, with the papers
	// that carry no licence at all in a final row.
	Licences []Row
	// Access is one row per access class.
	Access []AccessRow
	// Years is one row per announcement year, oldest first.
	Years []YearRow
	// Archives is one row per primary archive, largest first.
	Archives []ArchiveRow
	// Authorities is who said so, which surface the licence was harvested from.
	Authorities []AuthorityRow
	// Versions is the distribution of version counts, 1 up to VersionCap.
	Versions []VersionRow
	// MultiVersion is how many papers have more than one version, which is the
	// population the overstatement above applies to.
	MultiVersion int
}

// Row is one licence.
type Row struct {
	// Licence is empty for the papers no surface gave a licence for, which is
	// not the same thing as a licence of unknown. Unknown is a paper that was
	// looked at, and empty is a paper that was not.
	Licence corpus.Licence
	Access  corpus.Access
	Records int
	Share   float64
}

// Label is the licence as the report prints it.
func (r Row) Label() string {
	if r.Licence == "" {
		return "not recorded"
	}
	return string(r.Licence)
}

// MayTranslate reports whether the papers in this row may be translated.
func (r Row) MayTranslate() bool { return r.Access.MayTranslate() }

// MayPublishText reports whether their English text may be republished.
func (r Row) MayPublishText() bool { return r.Access.MayPublishText() }

// AccessRow is one access class.
type AccessRow struct {
	Access  corpus.Access
	Records int
	Share   float64
}

// YearRow is one announcement year.
//
// The year is the one the identifier names, so the year the paper was
// announced, which is how the plane is filed. It is not always the year the
// paper was submitted, and for a paper held in moderation over a new year it is
// not the same year at all.
type YearRow struct {
	Year         int
	Records      int
	Text         int
	Translatable int
}

// Share is the translatable fraction of the year.
func (y YearRow) Share() float64 { return prose.Share(y.Translatable, y.Records) }

// ArchiveRow is one primary archive, so cs rather than cs.CL.
type ArchiveRow struct {
	Archive      string
	Records      int
	Text         int
	Translatable int
}

// Share is the translatable fraction of the archive.
func (a ArchiveRow) Share() float64 { return prose.Share(a.Translatable, a.Records) }

// AuthorityRow is one harvest surface.
type AuthorityRow struct {
	Source  metadata.Source
	Records int
}

// VersionRow is one bar of the version distribution.
type VersionRow struct {
	// Versions is the count, and VersionCap means that many or more.
	Versions int
	Records  int
	Share    float64
}

// Label is the row as the report prints it.
func (v VersionRow) Label() string {
	if v.Versions >= VersionCap {
		return fmt.Sprintf("%d or more", VersionCap)
	}
	return fmt.Sprintf("%d", v.Versions)
}

// Republishable is how many papers may have their English text republished.
func (c Census) Republishable() int { return c.access(corpus.Access.MayPublishText) }

// Translatable is how many papers may be translated, as an upper bound.
func (c Census) Translatable() int { return c.access(corpus.Access.MayTranslate) }

func (c Census) access(permits func(corpus.Access) bool) int {
	n := 0
	for _, r := range c.Access {
		if permits(r.Access) {
			n += r.Records
		}
	}
	return n
}

// Counter accumulates a census one record at a time.
//
// Streaming rather than a slice of records, because the plane is three million
// records and the whole point of the JSON Lines layout is that nothing has to
// hold it all at once.
type Counter struct {
	records      int
	months       map[string]bool
	licences     map[corpus.Licence]int
	access       map[corpus.Access]int
	years        map[int]*bucket
	archives     map[string]*bucket
	authorities  map[metadata.Source]int
	versions     map[int]int
	multiVersion int
}

type bucket struct {
	records      int
	text         int
	translatable int
}

// NewCounter returns an empty count.
func NewCounter() *Counter {
	return &Counter{
		months:      map[string]bool{},
		licences:    map[corpus.Licence]int{},
		access:      map[corpus.Access]int{},
		years:       map[int]*bucket{},
		archives:    map[string]*bucket{},
		authorities: map[metadata.Source]int{},
		versions:    map[int]int{},
	}
}

// Add counts one record, filed under the month it was read from.
func (c *Counter) Add(shard string, r metadata.Record) error {
	month, err := metadata.ShardMonth(shard)
	if err != nil {
		return fmt.Errorf("licence: %w", err)
	}
	c.records++
	c.months[shard] = true

	l, from := Latest(r)
	access := corpus.AccessFor(l)
	c.licences[l]++
	c.access[access]++
	if from != "" {
		c.authorities[from]++
	}

	fold(c.years, month.Year(), access)
	fold(c.archives, r.Archive(), access)

	n := len(r.Versions)
	if n > VersionCap {
		n = VersionCap
	}
	c.versions[n]++
	if len(r.Versions) > 1 {
		c.multiVersion++
	}
	return nil
}

// fold adds one paper to a group, keyed by year or by archive.
func fold[K comparable](m map[K]*bucket, key K, access corpus.Access) {
	b := m[key]
	if b == nil {
		b = &bucket{}
		m[key] = b
	}
	b.records++
	if access.MayPublishText() {
		b.text++
	}
	if access.MayTranslate() {
		b.translatable++
	}
}

// Latest is the licence of a record's latest version, and the surface that
// said so.
//
// Both are empty when the record has no versions or when its latest version
// carries no licence, which is a record nobody has resolved rather than a
// record resolved to unknown.
func Latest(r metadata.Record) (corpus.Licence, metadata.Source) {
	v, ok := r.Latest()
	if !ok {
		return "", ""
	}
	return v.Licence, v.LicenceFrom
}

// Census freezes the count.
func (c *Counter) Census() Census {
	out := Census{
		Records:      c.records,
		Months:       len(c.months),
		MultiVersion: c.multiVersion,
	}

	// The licence rows follow the spec's order, most permissive first, with the
	// papers nobody has resolved last. A row with nothing in it is still
	// printed, because a corpus holding no CC0 at all is a fact about the
	// corpus and a missing row reads as an oversight.
	for _, l := range append(append([]corpus.Licence{}, corpus.Licences...), "") {
		out.Licences = append(out.Licences, Row{
			Licence: l,
			Access:  corpus.AccessFor(l),
			Records: c.licences[l],
			Share:   prose.Share(c.licences[l], c.records),
		})
	}

	for _, a := range []corpus.Access{corpus.AccessOpen, corpus.AccessShareAlike, corpus.AccessVerbatim, corpus.AccessRecord} {
		out.Access = append(out.Access, AccessRow{
			Access:  a,
			Records: c.access[a],
			Share:   prose.Share(c.access[a], c.records),
		})
	}

	for year, b := range c.years {
		out.Years = append(out.Years, YearRow{Year: year, Records: b.records, Text: b.text, Translatable: b.translatable})
	}
	sort.Slice(out.Years, func(i, j int) bool { return out.Years[i].Year < out.Years[j].Year })

	for archive, b := range c.archives {
		out.Archives = append(out.Archives, ArchiveRow{Archive: archive, Records: b.records, Text: b.text, Translatable: b.translatable})
	}
	sort.Slice(out.Archives, func(i, j int) bool {
		if out.Archives[i].Records != out.Archives[j].Records {
			return out.Archives[i].Records > out.Archives[j].Records
		}
		return out.Archives[i].Archive < out.Archives[j].Archive
	})

	for _, s := range []metadata.Source{metadata.SourceKaggle, metadata.SourceHF, metadata.SourceOAI} {
		if c.authorities[s] > 0 {
			out.Authorities = append(out.Authorities, AuthorityRow{Source: s, Records: c.authorities[s]})
		}
	}

	for n := 1; n <= VersionCap; n++ {
		out.Versions = append(out.Versions, VersionRow{
			Versions: n,
			Records:  c.versions[n],
			Share:    prose.Share(c.versions[n], c.records),
		})
	}
	return out
}

// Take counts a whole plane, or the months named.
func Take(p metadata.Plane, shards []string) (Census, error) {
	if len(shards) == 0 {
		found, err := p.Shards()
		if err != nil {
			return Census{}, err
		}
		shards = found
	}
	c := NewCounter()
	for _, shard := range shards {
		err := p.Scan(shard, func(r metadata.Record) error { return c.Add(shard, r) })
		if err != nil {
			return Census{}, err
		}
	}
	return c.Census(), nil
}

// Markdown is the report as it is committed to reports/licence.md.
func (c Census) Markdown() string {
	var b strings.Builder
	b.WriteString("# The licence census\n\n")
	b.WriteString(c.headline())
	fmt.Fprintf(&b, "Counted on %s over %s.\n\n",
		c.Generated.UTC().Format("2 January 2006"), prose.Count(c.Months, "month"))

	b.WriteString("## Why this is an upper bound\n\n")
	b.WriteString("Every surface that serves arXiv metadata in bulk carries one licence per paper rather than one per version, and the one it carries is the latest version's.\n")
	b.WriteString("A paper whose author relicensed between v1 and v3 is counted here under the v3 licence for all three of its versions.\n")
	if c.Records > 0 {
		carry := "papers counted here carry"
		if c.MultiVersion == 1 {
			carry = "paper counted here carries"
		}
		fmt.Fprintf(&b, "%s %s more than one version, which is %s of the corpus.\n",
			prose.Thousands(c.MultiVersion), carry, prose.Percent(prose.Share(c.MultiVersion, c.Records)))
	}
	b.WriteString("A random sample of forty multi-version papers taken in September 2026 found five whose v1 licence differs from their latest, and four of those five had a latest version more permissive than the first.\n")
	b.WriteString("That is the direction that costs something: it is the case where a corpus reads a permissive latest licence and republishes an earlier version that was never offered under it.\n")
	b.WriteString("Resolving all of it means one page per version, which is over five million pages at arXiv's fifteen second pace, so the resolution happens per paper when a paper is selected and ax licence resolve is the command that does it.\n\n")

	b.WriteString("## By licence\n\n")
	b.WriteString("| Licence | Papers | Share | Access | Text | Translation |\n")
	b.WriteString("| --- | ---: | ---: | --- | --- | --- |\n")
	for _, r := range c.Licences {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
			r.Label(), prose.Thousands(r.Records), prose.Percent(r.Share), r.Access,
			yesno(r.MayPublishText()), yesno(r.MayTranslate()))
	}

	b.WriteString("\n## By access class\n\n")
	b.WriteString("| Access | Papers | Share |\n")
	b.WriteString("| --- | ---: | ---: |\n")
	for _, r := range c.Access {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", r.Access, prose.Thousands(r.Records), prose.Percent(r.Share))
	}

	if len(c.Authorities) > 0 {
		b.WriteString("\n## Who said so\n\n")
		b.WriteString("| Surface | Papers |\n")
		b.WriteString("| --- | ---: |\n")
		for _, r := range c.Authorities {
			fmt.Fprintf(&b, "| %s | %s |\n", r.Source, prose.Thousands(r.Records))
		}
		b.WriteString("\nNo bulk surface carries a per version licence, so every row above is a latest version licence whichever surface it came from.\n")
	}

	b.WriteString("\n## Versions\n\n")
	b.WriteString("| Versions | Papers | Share |\n")
	b.WriteString("| --- | ---: | ---: |\n")
	for _, r := range c.Versions {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", r.Label(), prose.Thousands(r.Records), prose.Percent(r.Share))
	}

	b.WriteString("\n## By year\n\n")
	b.WriteString("The year an identifier names is the year the paper was announced, which for a paper held in moderation over a new year is not the year it was submitted.\n\n")
	b.WriteString("| Year | Papers | Text | Translatable | Share |\n")
	b.WriteString("| --- | ---: | ---: | ---: | ---: |\n")
	for _, y := range c.Years {
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %s |\n",
			y.Year, prose.Thousands(y.Records), prose.Thousands(y.Text), prose.Thousands(y.Translatable), prose.Percent(y.Share()))
	}

	b.WriteString("\n## By archive\n\n")
	b.WriteString("| Archive | Papers | Text | Translatable | Share |\n")
	b.WriteString("| --- | ---: | ---: | ---: | ---: |\n")
	for _, a := range c.Archives {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
			a.Archive, prose.Thousands(a.Records), prose.Thousands(a.Text), prose.Thousands(a.Translatable), prose.Percent(a.Share()))
	}
	return b.String()
}

// Text is the short form, for a terminal.
func (c Census) Text() string {
	var b strings.Builder
	b.WriteString(c.headline())
	for _, r := range c.Licences {
		if r.Records == 0 {
			continue
		}
		fmt.Fprintf(&b, "  %-14s %10s  %6s  %s\n", r.Label(), prose.Thousands(r.Records), prose.Percent(r.Share), r.Access)
	}
	return b.String()
}

// headline is the one sentence the whole report exists to produce.
func (c Census) headline() string {
	if c.Records == 0 {
		return "Nothing counted, because the metadata plane is empty.\n"
	}
	return fmt.Sprintf("Of %s counted, %s may be translated and %s may be republished in English, both as upper bounds.\n",
		prose.Count(c.Records, "paper"), prose.Thousands(c.Translatable()), prose.Thousands(c.Republishable()))
}

func yesno(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

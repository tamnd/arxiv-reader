package harvest

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/metadata"
)

// Coverage is what the corpus holds set against what arXiv says it announced.
//
// The two numbers are close and they are not the same number, and the report
// says so rather than pretending otherwise. arXiv counts a paper in the month
// it was submitted and the corpus files it under the month its identifier
// names, which is the month it was announced. A paper held in moderation is
// announced later than it was submitted, so a month's two counts differ by the
// papers that moved across its edges in either direction.
type Coverage struct {
	// Generated is when the report was made.
	Generated time.Time
	// Rows is one per month, oldest first.
	Rows []CoverageRow
	// Source says where the published numbers came from.
	Source string
}

// CoverageRow is one month.
type CoverageRow struct {
	Shard string
	Month time.Time
	// Held is how many records the corpus has for that month.
	Held int
	// Published is arXiv's own count, zero valued for a month arXiv does not
	// list at all.
	Published Published
	// Listed says whether arXiv lists the month.
	Listed bool
}

// Live is the published count after arXiv's own correction.
func (r CoverageRow) Live() int { return r.Published.Live() }

// Short is how many the corpus is missing, negative when it holds more.
func (r CoverageRow) Short() int { return r.Live() - r.Held }

// Coverage is the fraction held, and -1 for a month arXiv gives no count for.
func (r CoverageRow) Coverage() float64 {
	if r.Live() <= 0 {
		return -1
	}
	return float64(r.Held) / float64(r.Live())
}

// Empty reports whether arXiv announced papers that month and the corpus has
// none of them, which is the one case that is unambiguously a gap rather than
// an edge effect.
func (r CoverageRow) Empty() bool { return r.Held == 0 && r.Live() > 0 }

// Compare sets the corpus's counts against arXiv's.
//
// Every month either side knows about gets a row, including a month the corpus
// holds that arXiv does not list, because that is a real finding and dropping
// it would hide it.
func Compare(held map[string]int, published []Published) Coverage {
	rows := make(map[string]*CoverageRow)
	for _, p := range published {
		rows[p.Shard] = &CoverageRow{Shard: p.Shard, Month: p.Month, Published: p, Listed: true}
	}
	for shard, n := range held {
		row, ok := rows[shard]
		if !ok {
			month, err := metadata.ShardMonth(shard)
			if err != nil {
				continue
			}
			row = &CoverageRow{Shard: shard, Month: month}
			rows[shard] = row
		}
		row.Held = n
	}

	out := make([]CoverageRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Month.Before(out[j].Month) })
	return Coverage{Rows: out}
}

// Held is the corpus total.
func (c Coverage) Held() int { return c.sum(func(r CoverageRow) int { return r.Held }) }

// Live is the published total.
func (c Coverage) Live() int { return c.sum(func(r CoverageRow) int { return r.Live() }) }

func (c Coverage) sum(of func(CoverageRow) int) int {
	n := 0
	for _, r := range c.Rows {
		n += of(r)
	}
	return n
}

// Fraction is how much of arXiv the corpus holds.
func (c Coverage) Fraction() float64 {
	if c.Live() == 0 {
		return 0
	}
	return float64(c.Held()) / float64(c.Live())
}

// Empty is every month arXiv announced papers in and the corpus has none of.
func (c Coverage) Empty() []CoverageRow {
	var out []CoverageRow
	for _, r := range c.Rows {
		if r.Empty() {
			out = append(out, r)
		}
	}
	return out
}

// Unlisted is every month the corpus holds that arXiv does not count.
//
// arXiv's file starts in July 1991 and ends with the month in progress, so a
// month outside that range is a record with an identifier from a month that
// does not exist.
func (c Coverage) Unlisted() []CoverageRow {
	var out []CoverageRow
	for _, r := range c.Rows {
		if !r.Listed && r.Held > 0 {
			out = append(out, r)
		}
	}
	return out
}

// Shortest is the months furthest below their published count, worst first.
func (c Coverage) Shortest(n int) []CoverageRow {
	var out []CoverageRow
	for _, r := range c.Rows {
		if r.Listed && r.Held > 0 && r.Short() > 0 {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Short() > out[j].Short() })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// Years rolls the months up, because 423 rows is a table nobody reads first.
func (c Coverage) Years() []YearRow {
	var out []YearRow
	index := map[int]int{}
	for _, r := range c.Rows {
		y := r.Month.Year()
		i, ok := index[y]
		if !ok {
			index[y] = len(out)
			out = append(out, YearRow{Year: y})
			i = len(out) - 1
		}
		out[i].Held += r.Held
		out[i].Live += r.Live()
		out[i].Months++
		if r.Held > 0 {
			out[i].Harvested++
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Year < out[j].Year })
	return out
}

// YearRow is one year of the roll-up.
type YearRow struct {
	Year              int
	Months, Harvested int
	Held, Live        int
}

// Fraction is how much of the year the corpus holds.
func (y YearRow) Fraction() float64 {
	if y.Live == 0 {
		return 0
	}
	return float64(y.Held) / float64(y.Live)
}

// Markdown is the committed report, which is reports/harvest.md.
func (c Coverage) Markdown() string {
	var b strings.Builder
	b.WriteString("# The harvest\n\n")
	fmt.Fprintf(&b, "%s of the %s arXiv says it has announced, which is %s.\n",
		prose.Count(c.Held(), "record"), prose.Count(c.Live(), "record"), prose.Percent(c.Fraction()))
	fmt.Fprintf(&b, "Read on %s against %s.\n\n", c.Generated.UTC().Format("2 January 2006"), c.Source)

	b.WriteString("arXiv counts a paper in the month it was submitted and this corpus files it under the month its identifier names, which is the month it was announced.\n")
	b.WriteString("A paper held in moderation is announced later than it was submitted, so a month's two counts differ by the papers that crossed its edges, and a difference of a percent or two either way is that and not a gap.\n")
	b.WriteString("A month with nothing in it is a gap.\n\n")

	if empty := c.Empty(); len(empty) > 0 {
		fmt.Fprintf(&b, "## %s with nothing in them\n\n", prose.Count(len(empty), "month"))
		b.WriteString("```\n")
		for _, r := range capRows(empty, 50) {
			fmt.Fprintf(&b, "%s  %d announced, none held\n", r.Shard, r.Live())
		}
		b.WriteString("```\n")
		if n := len(empty) - 50; n > 0 {
			fmt.Fprintf(&b, "\nAnd %d more.\n", n)
		}
		b.WriteString("\n")
	}

	if odd := c.Unlisted(); len(odd) > 0 {
		b.WriteString("## Months arXiv does not count\n\n")
		b.WriteString("arXiv's file runs from July 1991 to the month in progress, so a month outside it holds records whose identifiers name a month that does not exist.\n\n")
		b.WriteString("```\n")
		for _, r := range capRows(odd, 50) {
			fmt.Fprintf(&b, "%s  %d held, not announced\n", r.Shard, r.Held)
		}
		b.WriteString("```\n\n")
	}

	if short := c.Shortest(20); len(short) > 0 {
		b.WriteString("## The months furthest short\n\n")
		b.WriteString("| Month | Held | Announced | Short | Coverage |\n")
		b.WriteString("| --- | ---: | ---: | ---: | ---: |\n")
		for _, r := range short {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
				r.Month.Format("2006-01"), prose.Thousands(r.Held), prose.Thousands(r.Live()), prose.Thousands(r.Short()), prose.Percent(r.Coverage()))
		}
		b.WriteString("\n")
	}

	b.WriteString("## By year\n\n")
	b.WriteString("| Year | Months | Held | Announced | Coverage |\n")
	b.WriteString("| --- | ---: | ---: | ---: | ---: |\n")
	for _, y := range c.Years() {
		fmt.Fprintf(&b, "| %d | %d of %d | %s | %s | %s |\n",
			y.Year, y.Harvested, y.Months, prose.Thousands(y.Held), prose.Thousands(y.Live), prose.Percent(y.Fraction()))
	}

	b.WriteString("\n## By month\n\n")
	b.WriteString("| Month | Held | Announced | Difference | Coverage |\n")
	b.WriteString("| --- | ---: | ---: | ---: | ---: |\n")
	for _, r := range c.Rows {
		announced := prose.Thousands(r.Live())
		if !r.Listed {
			announced = "not counted"
		}
		// Held minus announced, signed either way, so a month holding more than
		// arXiv counted reads as the surplus it is rather than as a negative gap.
		difference := prose.Thousands(r.Held - r.Live())
		if r.Held > r.Live() {
			difference = "+" + difference
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
			r.Month.Format("2006-01"), prose.Thousands(r.Held), announced, difference, prose.Percent(r.Coverage()))
	}
	return b.String()
}

// Text is the summary a person reads in the terminal, which is the totals and
// the gaps and nothing else.
func (c Coverage) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s of %s announced, %s\n", prose.Count(c.Held(), "record"), prose.Count(c.Live(), "record"), prose.Percent(c.Fraction()))

	held := 0
	for _, r := range c.Rows {
		if r.Held > 0 {
			held++
		}
	}
	fmt.Fprintf(&b, "%s held, %s announced\n", prose.Count(held, "month"), prose.Count(len(c.Rows), "month"))

	if empty := c.Empty(); len(empty) > 0 {
		fmt.Fprintf(&b, "%s with nothing in them, first %s, last %s\n",
			prose.Count(len(empty), "month"), empty[0].Shard, empty[len(empty)-1].Shard)
	}
	if odd := c.Unlisted(); len(odd) > 0 {
		fmt.Fprintf(&b, "%s arXiv does not count: %s\n", prose.Count(len(odd), "month"), shards(odd))
	}
	return b.String()
}

func capRows(rows []CoverageRow, n int) []CoverageRow {
	if len(rows) > n {
		return rows[:n]
	}
	return rows
}

func shards(rows []CoverageRow) string {
	out := make([]string, 0, len(rows))
	for _, r := range capRows(rows, 10) {
		out = append(out, r.Shard)
	}
	return strings.Join(out, " ")
}

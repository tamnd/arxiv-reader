// Package report writes the committed answers to the questions somebody
// actually asks about this corpus.
//
// Every report here is generated, committed and regenerated, and none of them
// is a log. A log says what happened during one run and is read once. A report
// says what is true of the corpus now, and it is in the repository so that the
// diff between two of them is the month's news.
package report

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/selection"
)

// Paths is reports/paths.md.
//
// It is the running answer to whether the cheap paths are doing the work the
// economics of this project assume they do. Three of the four paths cost local
// work and one costs a model call a page, so the share on the vision path is
// the budget, and the month it starts drifting is the month to find out rather
// than the quarter after.
type Paths struct {
	// Built is the date this was computed, as YYYY-MM-DD.
	Built string
	// Papers is every paper in the selection, decided or not.
	Papers int
	// Rows is one per path and then one for the papers with no path yet.
	Rows []PathRow
	// Months is one per month the selection has papers from, in month order.
	Months []MonthRow
	// Demoted is how many papers are on the source path because a rendering
	// was rejected rather than because the submission asked for it.
	Demoted int
	// Undecided is what would settle the papers that have no path, rolled up by
	// the sentence rather than listed per paper.
	Undecided []Blocker
}

// PathRow is one path's line.
type PathRow struct {
	// Path is one of the four, or empty for the papers nothing has decided yet.
	Path selection.Path
	// Papers is how many are on it and Share is that over the whole selection.
	Papers int
	Share  float64
	// Reached is how many of them have got to each rung, counted as reached and
	// not as stopped at, so a paper that is tagged counts under extracted too.
	//
	// The ladder and not the current rung, because the question a reader has is
	// how many papers on this path have been read, and a paper that has been
	// read and tagged has been read.
	Reached map[selection.Status]int
}

// Furthest is the rung the whole of a path has got to.
//
// The rung every paper on the path has reached, which is the honest headline
// for a path: a path where one paper of forty is tagged has not been tagged.
func (r PathRow) Furthest() selection.Status {
	out := selection.Status("")
	for _, s := range selection.Statuses {
		if r.Reached[s] < r.Papers || r.Papers == 0 {
			break
		}
		out = s
	}
	return out
}

// MonthRow is one month of the selection, by path.
type MonthRow struct {
	// Month is the YYMM the ids encode.
	Month string
	// Papers is the month's total and Counts is it split by path, with the
	// empty path holding the papers nothing has decided yet.
	Papers int
	Counts map[selection.Path]int
}

// Blocker is one thing standing between papers and a path.
type Blocker struct {
	// Says is the sentence ax path decide printed, and Papers is how many
	// papers it was printed for.
	Says   string
	Papers int
}

// Expected is what the spec expects each path to carry.
//
// Words and not numbers, because the spec says what each path is for and
// deliberately does not assert a share. The share is what this report is for,
// and a number in this column would be a target somebody would then hit.
func Expected(p selection.Path) string {
	switch p {
	case selection.PathRender:
		return "most of everything since 2024, and much of the backfill"
	case selection.PathSource:
		return "the errored conversions, and the older papers arXiv never converted"
	case selection.PathNative:
		return "the PDF only submissions that have a real text layer"
	case selection.PathVision:
		return "scanned PDF only papers, mostly 1991 to 2000, and the whole budget"
	}
	return "papers nobody has decided about yet"
}

// BuildPaths reads the selection into the report.
//
// The selection and nothing else. A paper's path is a field of its selection
// entry, how far it has got is another, and a report that went to the content
// plane to check would be answering a different question, which is whether the
// selection is true. That question has a rule of its own.
func BuildPaths(m selection.Manifest, built string) Paths {
	out := Paths{Built: built, Papers: len(m.Selected)}
	rows := map[selection.Path]*PathRow{}
	order := append(append([]selection.Path{}, selection.Paths...), "")
	for _, p := range order {
		rows[p] = &PathRow{Path: p, Reached: map[selection.Status]int{}}
	}
	months := map[string]*MonthRow{}
	blockers := map[string]int{}
	for _, e := range m.Selected {
		p := e.Path
		if !selection.KnownPath(p) {
			p = ""
		}
		row := rows[p]
		row.Papers++
		for _, s := range selection.Statuses {
			if selection.Reached(e.Status, s) {
				row.Reached[s]++
			}
		}
		if p == selection.PathSource && e.PathWhy == selection.DemotedWhy {
			out.Demoted++
		}
		month := monthOf(e.ID)
		if months[month] == nil {
			months[month] = &MonthRow{Month: month, Counts: map[selection.Path]int{}}
		}
		months[month].Papers++
		months[month].Counts[p]++
		if p == "" {
			blockers[e.PathWhy]++
		}
	}
	for _, p := range order {
		row := *rows[p]
		row.Share = prose.Share(row.Papers, out.Papers)
		// The undecided line is there when there is something on it and gone
		// when there is not, because a corpus where every paper has a path
		// should not carry a row of noughts saying so.
		if row.Papers == 0 && p == "" {
			continue
		}
		out.Rows = append(out.Rows, row)
	}
	for _, row := range months {
		out.Months = append(out.Months, *row)
	}
	sort.Slice(out.Months, func(i, j int) bool { return out.Months[i].Month < out.Months[j].Month })
	for says, n := range blockers {
		out.Undecided = append(out.Undecided, Blocker{Says: says, Papers: n})
	}
	// Most papers first, because the blocker standing in front of four hundred
	// papers is the one worth clearing and the order is what says so.
	sort.Slice(out.Undecided, func(i, j int) bool {
		if out.Undecided[i].Papers != out.Undecided[j].Papers {
			return out.Undecided[i].Papers > out.Undecided[j].Papers
		}
		return out.Undecided[i].Says < out.Undecided[j].Says
	})
	return out
}

// monthOf is the YYMM a paper's id encodes.
//
// Read off the id rather than parsed, because an id this report cannot read is
// a finding of the metadata audit and not a reason to stop counting. An old
// style id carries its month after the slash and a new style one carries it in
// front of the dot, and both are four digits.
func monthOf(id string) string {
	if i := strings.IndexByte(id, '/'); i >= 0 {
		id = id[i+1:]
	}
	if len(id) < 4 {
		return "unknown"
	}
	for i := 0; i < 4; i++ {
		if id[i] < '0' || id[i] > '9' {
			return "unknown"
		}
	}
	return id[:4]
}

// Markdown is the committed report.
func (p Paths) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# How the selection divides between the four extraction paths\n\n")
	fmt.Fprintf(&b, "Written by `ax report paths` on %s, over %s in `manifests/selected.yaml`.\n",
		p.Built, prose.Count(p.Papers, "paper"))
	b.WriteString("Three of the four paths cost local work and the fourth costs a model call a page, so the share on the vision path is the budget.\n\n")

	b.WriteString("| Path | Papers | Share | Furthest | What it is for |\n")
	b.WriteString("| --- | ---: | ---: | --- | --- |\n")
	for _, r := range p.Rows {
		name := string(r.Path)
		if name == "" {
			name = "undecided"
		}
		fmt.Fprintf(&b, "| %s | %d | %s | %s | %s |\n", name, r.Papers, prose.Percent(r.Share), furthest(r), Expected(r.Path))
	}
	b.WriteString("\n")
	b.WriteString("Furthest is the rung every paper on the path has reached, and not the rung the best of them has.\n")
	b.WriteString("A path where one paper of forty is tagged has not been tagged.\n\n")

	b.WriteString("## How far each path has got\n\n")
	b.WriteString("| Path |")
	for _, s := range selection.Statuses {
		fmt.Fprintf(&b, " %s |", s)
	}
	b.WriteString("\n| --- |")
	for range selection.Statuses {
		b.WriteString(" ---: |")
	}
	b.WriteString("\n")
	for _, r := range p.Rows {
		name := string(r.Path)
		if name == "" {
			name = "undecided"
		}
		fmt.Fprintf(&b, "| %s |", name)
		for _, s := range selection.Statuses {
			fmt.Fprintf(&b, " %d |", r.Reached[s])
		}
		b.WriteString("\n")
	}
	b.WriteString("\nCounted as reached and not as stopped at, so a paper that is tagged is counted under extracted as well.\n\n")

	fmt.Fprintf(&b, "## Month by month\n\n")
	b.WriteString("| Month | Papers |")
	for _, s := range selection.Paths {
		fmt.Fprintf(&b, " %s |", s)
	}
	b.WriteString(" undecided |\n| --- | ---: |")
	for range selection.Paths {
		b.WriteString(" ---: |")
	}
	b.WriteString(" ---: |\n")
	for _, m := range p.Months {
		fmt.Fprintf(&b, "| %s | %d |", m.Month, m.Papers)
		for _, s := range selection.Paths {
			fmt.Fprintf(&b, " %d |", m.Counts[s])
		}
		fmt.Fprintf(&b, " %d |\n", m.Counts[""])
	}
	b.WriteString("\nThe month the vision column starts growing is the month the budget needs rewriting, which is the reason this table is by month and not by year.\n\n")

	b.WriteString("## Demotions\n\n")
	if p.Demoted == 0 {
		b.WriteString("No paper in the selection is on the source path because a rendering was rejected.\n\n")
	} else {
		fmt.Fprintf(&b, "%s on the source path %s there because arXiv's rendering held errors the reject rule would not accept, and not because the submission asked for it.\n\n",
			prose.Count(p.Demoted, "paper"), isare(p.Demoted))
	}
	b.WriteString("A demotion is the one fact about a path that cannot be known before the reading, so it is the one number here that a run can change without anybody deciding anything.\n\n")

	b.WriteString("## What is holding the rest up\n\n")
	if len(p.Undecided) == 0 {
		b.WriteString("Every paper in the selection has a path.\n")
		return b.String()
	}
	b.WriteString("| Papers | What would settle it |\n| ---: | --- |\n")
	for _, u := range p.Undecided {
		says := u.Says
		if says == "" {
			says = "nothing recorded, which means ax path decide has not been run over this paper"
		}
		fmt.Fprintf(&b, "| %d | %s |\n", u.Papers, says)
	}
	return b.String()
}

// Text is the same thing for a terminal, without the table markup.
func (p Paths) Text() string {
	var b strings.Builder
	for _, r := range p.Rows {
		name := string(r.Path)
		if name == "" {
			name = "undecided"
		}
		fmt.Fprintf(&b, "%s\t%d\t%s\t%s\n", name, r.Papers, prose.Percent(r.Share), furthest(r))
	}
	fmt.Fprintf(&b, "  papers\t%d\n", p.Papers)
	fmt.Fprintf(&b, "  months\t%d\n", len(p.Months))
	fmt.Fprintf(&b, "  demoted\t%d, from the render path to the source path\n", p.Demoted)
	return b.String()
}

func furthest(r PathRow) string {
	if s := r.Furthest(); s != "" {
		return string(s)
	}
	return "nowhere yet"
}

func isare(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

// Save writes a report, creating the directory if it is missing.
func Save(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o644)
}

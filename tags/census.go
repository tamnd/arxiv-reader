package tags

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/arxiv-reader/internal/prose"
)

// Census is every register in the corpus counted, and every revision of a paper
// the cache holds both sides of, matched again.
//
// The number this exists for is the fall through rate. Passes one to three match
// on equality and pass four matches on similarity, so an object that reaches
// pass four is an object whose permanent name was decided by a guess. The gate
// in ax tags diff refuses to write when more than a fifth of a paper falls that
// far, and this is how anybody finds out whether the corpus is anywhere near
// that line before a run hits it.
//
// The revisions are matched again at report time rather than read from a record
// of what the matcher once said, because the matcher is the thing being
// measured. A number kept from the run that carried a register would say what
// last year's matcher did, which is not the question.
type Census struct {
	// Generated is when the report was made.
	Generated time.Time
	// Papers is one per paper with a register, in identifier order.
	Papers []PaperTags
}

// PaperTags is one paper's register and the revisions of it that could be
// compared.
type PaperTags struct {
	ID string
	// Version is the version the register was last assigned against, and zero
	// for a register written before the version line existed.
	Version int
	// Live is the tags naming an object the paper still has, and Buried is the
	// tombstones.
	Live, Buried int
	// Runs is how many assignments have written into the register.
	Runs int
	// Revisions is each version compared against the next, oldest first.
	Revisions []Revision
	// Cached is every version of the paper the work directory holds, which is
	// what says why a paper with four versions has one revision in it.
	Cached []int
}

// Tags is every tag the register holds, live and buried.
func (p PaperTags) Tags() int { return p.Live + p.Buried }

// Revision is one version of a paper matched against the next.
type Revision struct {
	From, To int
	// Was is the object count of the older version and Now is the newer one's.
	Was, Now int
	// Label, Number, Content and Sequence are how many objects each pass
	// matched, and Added and Gone are what no pass could match in either
	// direction.
	Label, Number, Content, Sequence int
	Added, Gone                      int
}

// Fell is the share of the newer version's objects that the first three passes
// did not account for, which is the same number ax tags diff prints and the same
// one the gate reads.
func (r Revision) Fell() float64 { return prose.Share(r.Sequence+r.Added, r.Now) }

// Over threshold reports whether this revision is one ax tags diff would refuse
// to write without -force.
func (r Revision) Over() bool { return r.Fell() > TooMuch }

// Revise counts one comparison.
func Revise(from, to, was int, d Diff) Revision {
	return Revision{
		From: from, To: to, Was: was, Now: d.Total,
		Label:    d.Count(ByLabel),
		Number:   d.Count(ByNumber),
		Content:  d.Count(ByContent),
		Sequence: d.Count(BySequence),
		Added:    len(d.Added),
		Gone:     len(d.Gone),
	}
}

// Revisions is every comparison in the corpus, oldest paper first.
func (c Census) Revisions() []Revision {
	var out []Revision
	for _, p := range c.Papers {
		out = append(out, p.Revisions...)
	}
	return out
}

// Matched is how many objects the corpus has carried across a revision, which is
// the denominator of the rate.
func (c Census) Matched() int {
	n := 0
	for _, r := range c.Revisions() {
		n += r.Now
	}
	return n
}

// Fell is how many of those reached pass four or reached the end unmatched.
func (c Census) Fell() int {
	n := 0
	for _, r := range c.Revisions() {
		n += r.Sequence + r.Added
	}
	return n
}

// Rate is the corpus's fall through rate, and -1 when nothing has been compared.
func (c Census) Rate() float64 { return prose.Share(c.Fell(), c.Matched()) }

// Pass is how many objects one pass has matched across the whole corpus.
func (c Census) Pass(p Pass) int {
	n := 0
	for _, r := range c.Revisions() {
		switch p {
		case ByLabel:
			n += r.Label
		case ByNumber:
			n += r.Number
		case ByContent:
			n += r.Content
		case BySequence:
			n += r.Sequence
		}
	}
	return n
}

// Over is every revision a run would have refused to write, worst first.
func (c Census) Over() []Revision {
	var out []Revision
	for _, r := range c.Revisions() {
		if r.Over() {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Fell() > out[j].Fell() })
	return out
}

// Tags and Buried are the corpus totals.
func (c Census) Tags() int   { return c.total(func(p PaperTags) int { return p.Tags() }) }
func (c Census) Buried() int { return c.total(func(p PaperTags) int { return p.Buried }) }

func (c Census) total(of func(PaperTags) int) int {
	n := 0
	for _, p := range c.Papers {
		n += of(p)
	}
	return n
}

// Comparable is how many papers had two versions in the cache to compare.
func (c Census) Comparable() int {
	n := 0
	for _, p := range c.Papers {
		if len(p.Revisions) > 0 {
			n++
		}
	}
	return n
}

// Markdown is the committed report, which is reports/tags.md.
func (c Census) Markdown() string {
	var b strings.Builder
	b.WriteString("# The tags\n\n")
	fmt.Fprintf(&b, "%s over %s, %s of them tombstones.\n",
		prose.Count(c.Tags(), "tag"), prose.Count(len(c.Papers), "paper"), prose.Thousands(c.Buried()))
	if c.Matched() == 0 {
		b.WriteString("Nothing has been compared across a revision yet, so there is no fall through rate to report.\n")
		fmt.Fprintf(&b, "Read on %s.\n\n", c.Generated.UTC().Format("2 January 2006"))
		b.WriteString(why())
		b.WriteString("\nA revision is compared when the work directory holds both versions of a paper.\n")
		b.WriteString("The content plane holds one extraction of a paper and a comparison needs two, which is what the cache is for.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "%s carried across %s of %s, and %s of them fell through to pass four or matched nothing at all.\n",
		prose.Count(c.Matched(), "object"), prose.Count(len(c.Revisions()), "revision"),
		prose.Count(c.Comparable(), "paper"), prose.Percent(c.Rate()))
	fmt.Fprintf(&b, "Read on %s.\n\n", c.Generated.UTC().Format("2 January 2006"))

	b.WriteString(why())

	b.WriteString("\n## The four passes\n\n")
	b.WriteString("| Pass | Matched on | Objects | Share |\n")
	b.WriteString("| --- | --- | ---: | ---: |\n")
	for _, row := range []struct {
		pass Pass
		on   string
	}{
		{ByLabel, "the author's own label"},
		{ByNumber, "the same kind and number in the same section"},
		{ByContent, "the normalised content hash"},
		{BySequence, "the alignment of the two lists in reading order"},
	} {
		n := c.Pass(row.pass)
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", row.pass, row.on, prose.Thousands(n), prose.Percent(prose.Share(n, c.Matched())))
	}
	added, gone := c.added(), c.gone()
	fmt.Fprintf(&b, "| new | nothing in the older version is it | %s | %s |\n", prose.Thousands(added), prose.Percent(prose.Share(added, c.Matched())))
	fmt.Fprintf(&b, "| gone | nothing in the newer version is it | %s | n/a |\n", prose.Thousands(gone))

	if over := c.Over(); len(over) > 0 {
		fmt.Fprintf(&b, "\n## %s a run would refuse to write\n\n", prose.Count(len(over), "revision"))
		fmt.Fprintf(&b, "More than %s of the objects fell through, which is the line 03-tags.md draws.\n", prose.Percent(TooMuch))
		b.WriteString("A revision here is not a defect, it is a paper that changed enough that somebody should read the diff before a permanent name is moved.\n\n")
		b.WriteString("| Paper | Revision | Objects | Fell through |\n")
		b.WriteString("| --- | --- | ---: | ---: |\n")
		for _, p := range c.Papers {
			for _, r := range p.Revisions {
				if r.Over() {
					fmt.Fprintf(&b, "| %s | v%d to v%d | %d | %s |\n", p.ID, r.From, r.To, r.Now, prose.Percent(r.Fell()))
				}
			}
		}
	}

	b.WriteString("\n## By revision\n\n")
	b.WriteString("| Paper | Revision | Objects | Label | Number | Content | Sequence | New | Gone | Fell through |\n")
	b.WriteString("| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, p := range c.Papers {
		for _, r := range p.Revisions {
			fmt.Fprintf(&b, "| %s | v%d to v%d | %d | %d | %d | %d | %d | %d | %d | %s |\n",
				p.ID, r.From, r.To, r.Now, r.Label, r.Number, r.Content, r.Sequence, r.Added, r.Gone, prose.Percent(r.Fell()))
		}
	}

	b.WriteString("\n## By paper\n\n")
	b.WriteString("| Paper | Assigned against | Tags | Live | Tombstoned | Runs | Cached versions |\n")
	b.WriteString("| --- | --- | ---: | ---: | ---: | ---: | --- |\n")
	for _, p := range c.Papers {
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %d | %s |\n",
			p.ID, assigned(p.Version), p.Tags(), p.Live, p.Buried, p.Runs, versions(p.Cached))
	}
	return b.String()
}

// why is the paragraph that says what the number in the headline means, which
// both shapes of the report print.
func why() string {
	return "Passes one to three match on equality and pass four matches on similarity, so an object that reaches pass four is an object whose permanent name was decided by a guess.\n" +
		"That is why the rate is the number worth watching: it is the share of the corpus's names that were carried on judgement rather than read off.\n" +
		"An object no pass could match at all is counted with them, because a new tag on an object that is really an old one is the same mistake arrived at from the other side.\n" +
		"Every revision here is matched again at report time rather than read back from what the matcher said when the register was carried, because the matcher is the thing being measured.\n"
}

func (c Census) added() int {
	n := 0
	for _, r := range c.Revisions() {
		n += r.Added
	}
	return n
}

func (c Census) gone() int {
	n := 0
	for _, r := range c.Revisions() {
		n += r.Gone
	}
	return n
}

// assigned prints the version a register was assigned against, and says so
// plainly for a register that does not carry one.
func assigned(v int) string {
	if v == 0 {
		return "not said"
	}
	return fmt.Sprintf("v%d", v)
}

// versions prints a paper's cached versions as v1 v2 v3.
func versions(vs []int) string {
	if len(vs) == 0 {
		return "none"
	}
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, fmt.Sprintf("v%d", v))
	}
	return strings.Join(out, " ")
}

// Text is the summary a person reads in the terminal.
func (c Census) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s over %s, %d tombstoned\n", prose.Count(c.Tags(), "tag"), prose.Count(len(c.Papers), "paper"), c.Buried())
	if c.Matched() == 0 {
		b.WriteString("no revision has both versions in the cache, so nothing was compared\n")
		return b.String()
	}
	fmt.Fprintf(&b, "%s across %s, %s fell through to pass four\n",
		prose.Count(c.Matched(), "object"), prose.Count(len(c.Revisions()), "revision"), prose.Percent(c.Rate()))
	for _, p := range []Pass{ByLabel, ByNumber, ByContent, BySequence} {
		fmt.Fprintf(&b, "  %-9s %d\n", p, c.Pass(p))
	}
	fmt.Fprintf(&b, "  %-9s %d\n", "new", c.added())
	fmt.Fprintf(&b, "  %-9s %d\n", "gone", c.gone())
	if over := c.Over(); len(over) > 0 {
		fmt.Fprintf(&b, "%s over the fifth a run will write without -force\n", prose.Count(len(over), "revision"))
	}
	return b.String()
}

package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/arxiv-reader/audit"
	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/policy"
	"github.com/tamnd/arxiv-reader/selection"
)

// Quiet is the share of the content plane a rule has to stop applying to before
// this report names it.
//
// A fifth, which is a threshold and not a measurement, so it is here with the
// reasoning next to it rather than buried in the loop. Under a fifth is a rule
// that does not fit a corner of the corpus, which is ordinary and is what the
// policy file is for. Over a fifth is a rule most of the corpus is no longer
// checked by, and the number of those is the honest limit on what a green audit
// means.
const Quiet = 0.2

// Coverage is reports/coverage.md.
//
// Two questions, and they are the same question asked of the corpus and of the
// rules. How far has the corpus got, and how much of it is still being checked.
// A corpus can reach a thousand papers with a clean audit and have got there by
// asking fewer and fewer questions, and this is the report that would say so.
type Coverage struct {
	// Built is the date this was computed, as YYYY-MM-DD.
	Built string
	// Papers is every paper in the selection and Read is the ones that have
	// reached extracted, which is the content plane and the denominator for
	// everything about rules below.
	Papers int
	Read   int
	// Rules is how many rules read a content file.
	Rules int
	// Langs is one row per language, in the order the corpus got them, English
	// first.
	Langs []LangRow
	// Paths is one row per path that has papers in the content plane.
	Paths []PathCoverage
	// Silent is every rule that has stopped applying to more than Quiet of the
	// content plane, most of the corpus first.
	Silent []RuleCoverage
	// Unknown is a rule the policy file names that no rule in the set has.
	//
	// A typo in that file is silent by construction: it excuses nothing, so
	// nothing changes, and the rule the author meant to excuse goes on firing.
	// This is where that gets said.
	Unknown []string
}

// LangRow is one language at each of the four depths.
//
// Full is the body text, stub is the title and the abstract, record is a paper
// whose licence lets this corpus hold the metadata and nothing else, and none
// is a paper that should have more and does not yet.
type LangRow struct {
	Lang                     string
	Full, Stub, Record, None int
}

// PathCoverage is one path and how much of the rule set it is still asked.
type PathCoverage struct {
	Path selection.Path
	// Papers is how many papers on this path are in the content plane, and
	// Share is that over the content plane.
	Papers int
	Share  float64
	// Asked is how many content rules this path is expected to satisfy and
	// Skipped names the rest, in rule order.
	Asked   int
	Skipped []string
}

// AskedShare is how much of the rule set this path is still checked by.
func (p PathCoverage) AskedShare(rules int) float64 { return prose.Share(p.Asked, rules) }

// RuleCoverage is one rule and the part of the corpus it no longer reads.
type RuleCoverage struct {
	Rule  string
	Group audit.Group
	Says  string
	// Papers is how many papers in the content plane do not ask it, Share is
	// that over the content plane, and Paths are the paths responsible.
	Papers int
	Share  float64
	Paths  []selection.Path
}

// BuildCoverage reads the selection and the policy into the report.
//
// The two files that decide it, and neither plane. What a rule found is the
// audit's answer and it changes with every run, and what a rule is asked is
// this one, which changes when somebody edits a file. Reading the audit here
// would make the report a log of the last run.
func BuildCoverage(m selection.Manifest, pol policy.Audit, rules []audit.Rule, built string) Coverage {
	out := Coverage{Built: built, Papers: len(m.Selected), Rules: len(rules)}
	onPath := map[selection.Path]int{}
	langs := map[string]*LangRow{}
	order := []string{"en"}
	langs["en"] = &LangRow{Lang: "en"}
	for _, e := range m.Selected {
		read := selection.Reached(e.Status, selection.StatusExtracted)
		if read {
			out.Read++
			p := e.Path
			if !selection.KnownPath(p) {
				p = ""
			}
			onPath[p]++
		}
		// English is the status ladder, because the extraction is what writes
		// the English and there is no other way for it to appear.
		if read {
			langs["en"].Full++
		} else {
			langs["en"].None++
		}
		for _, l := range e.Languages {
			if l == "en" {
				continue
			}
			if langs[l] == nil {
				langs[l] = &LangRow{Lang: l}
				order = append(order, l)
			}
			langs[l].Full++
		}
	}
	// A language is counted over the whole selection and not over the papers
	// that have it, so a language nobody has started shows the work left rather
	// than a column of noughts that reads as nothing to do.
	for _, l := range order[1:] {
		langs[l].None = out.Papers - langs[l].Full
	}
	sort.Strings(order[1:])
	for _, l := range order {
		out.Langs = append(out.Langs, *langs[l])
	}

	known := map[string]audit.Rule{}
	for _, r := range rules {
		known[r.ID] = r
	}
	quiet := map[string]*RuleCoverage{}
	seen := map[string]bool{}
	for _, p := range append(append([]selection.Path{}, selection.Paths...), "") {
		skipped := pol.Skipped(p)
		for _, id := range skipped {
			if _, ok := known[strings.ToUpper(id)]; !ok && !seen[id] {
				seen[id] = true
				out.Unknown = append(out.Unknown, id)
			}
		}
		if onPath[p] == 0 {
			continue
		}
		asked := 0
		var names []string
		for _, r := range rules {
			if pol.Expects(p, r.ID) {
				asked++
				continue
			}
			names = append(names, r.ID)
			if quiet[r.ID] == nil {
				quiet[r.ID] = &RuleCoverage{Rule: r.ID, Group: r.Group, Says: r.Says}
			}
			quiet[r.ID].Papers += onPath[p]
			quiet[r.ID].Paths = append(quiet[r.ID].Paths, p)
		}
		out.Paths = append(out.Paths, PathCoverage{
			Path:    p,
			Papers:  onPath[p],
			Share:   prose.Share(onPath[p], out.Read),
			Asked:   asked,
			Skipped: names,
		})
	}
	sort.Strings(out.Unknown)
	for _, r := range quiet {
		r.Share = prose.Share(r.Papers, out.Read)
		if r.Share <= Quiet {
			continue
		}
		out.Silent = append(out.Silent, *r)
	}
	// Most of the corpus first, because the rule that has stopped reading four
	// fifths of it is the one worth an argument and the order is what says so.
	sort.Slice(out.Silent, func(i, j int) bool {
		if out.Silent[i].Papers != out.Silent[j].Papers {
			return out.Silent[i].Papers > out.Silent[j].Papers
		}
		return out.Silent[i].Rule < out.Silent[j].Rule
	})
	return out
}

// Markdown is the committed report.
func (c Coverage) Markdown() string {
	var b strings.Builder
	b.WriteString("# How far the corpus has got, and how much of it is still checked\n\n")
	fmt.Fprintf(&b, "Written by `ax report coverage` on %s, over %s in `manifests/selected.yaml` and the policy in `manifests/selection.yaml`.\n\n",
		c.Built, prose.Count(c.Papers, "paper"))
	b.WriteString("A corpus can reach a thousand papers with a clean audit and have got there by asking fewer and fewer questions of them.\n")
	b.WriteString("The first half of this report is how far it has got and the second half is what is still reading it, and the second half is the one that stops the first from being a number anybody can move.\n\n")

	b.WriteString("## What exists, by language\n\n")
	b.WriteString("| Language | Full | Stub | Record | None |\n| --- | ---: | ---: | ---: | ---: |\n")
	for _, l := range c.Langs {
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d |\n", l.Lang, l.Full, l.Stub, l.Record, l.None)
	}
	b.WriteString("\nFull is body text, stub is the title and the abstract, record is a paper whose licence lets this corpus hold the metadata and the structure and nothing else, and none is a paper that should have more and does not yet.\n")
	b.WriteString("The record column counts a paper as done and not as a paper somebody forgot, which is the whole point of having four words here rather than a percentage.\n")
	b.WriteString("Stub and record are both nought for now because both are read off the metadata plane and the join that fills them arrives with `reports/licence.md`.\n\n")

	b.WriteString("## What is still reading it, by path\n\n")
	if c.Read == 0 {
		b.WriteString("No paper in the selection has been extracted yet, so there is nothing for a content rule to be asked of.\n\n")
	} else {
		fmt.Fprintf(&b, "| Path | Papers | Share of the corpus | Rules asked | Share of the %d | Not asked |\n| --- | ---: | ---: | ---: | ---: | --- |\n", c.Rules)
		for _, p := range c.Paths {
			name := string(p.Path)
			if name == "" {
				name = "undecided"
			}
			not := strings.Join(p.Skipped, ", ")
			if not == "" {
				not = "none, every rule is asked"
			}
			fmt.Fprintf(&b, "| %s | %d | %s | %d | %s | %s |\n",
				name, p.Papers, prose.Percent(p.Share), p.Asked, prose.Percent(p.AskedShare(c.Rules)), not)
		}
		b.WriteString("\nA rule not asked of a path is a rule that comes back `n/a` in the audit rather than `pass`, and `manifests/selection.yaml` is where that is said.\n")
		b.WriteString("Not asked means the thing the rule checks cannot exist on that path, so a finding from it would be an artefact of the path rather than a fact about the paper.\n\n")
	}

	b.WriteString("## The rules most of the corpus no longer answers\n\n")
	fmt.Fprintf(&b, "Every rule that has stopped applying to more than %s of the content plane, with the paths responsible.\n\n", prose.Percent(Quiet))
	if c.Read == 0 {
		b.WriteString("The content plane is empty, so there is nothing to say yet.\n\n")
	} else if len(c.Silent) == 0 {
		fmt.Fprintf(&b, "None. Every one of the %d rules is asked of more than %s of the content plane.\n\n", c.Rules, prose.Percent(1-Quiet))
	} else {
		b.WriteString("| Rule | Papers | Share | Paths | What it says |\n| --- | ---: | ---: | --- | --- |\n")
		for _, r := range c.Silent {
			var paths []string
			for _, p := range r.Paths {
				name := string(p)
				if name == "" {
					name = "undecided"
				}
				paths = append(paths, name)
			}
			fmt.Fprintf(&b, "| %s | %d | %s | %s | %s |\n",
				r.Rule, r.Papers, prose.Percent(r.Share), strings.Join(paths, ", "), cell(r.Says))
		}
		b.WriteString("\nThis is the table to read before believing a green audit.\n")
		b.WriteString("A rule here is not wrong and it is not broken, it is a question most of this corpus is no longer being asked, and the way a corpus gets quietly worse is one more row appearing here every quarter.\n\n")
	}

	if len(c.Unknown) > 0 {
		b.WriteString("## Rules the policy names and this project does not have\n\n")
		b.WriteString("A typo in `manifests/selection.yaml` is silent by construction: it excuses nothing, so nothing changes, and the rule somebody meant to excuse goes on firing.\n\n")
		for _, id := range c.Unknown {
			fmt.Fprintf(&b, "- `%s`\n", id)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// cell is a rule's sentence, safe inside a table.
//
// F11 says a table exists twice as <n>.md and as <n>.tex, and a browser reads
// that as a tag it does not know and shows nothing at all. The sentence is the
// thing somebody arguing with a row reads, so losing half of it to the renderer
// is losing the row.
func cell(says string) string {
	says = strings.ReplaceAll(says, "<", "&lt;")
	return strings.ReplaceAll(says, "|", "\\|")
}

// Text is the same thing for a terminal, without the table markup.
func (c Coverage) Text() string {
	var b strings.Builder
	for _, p := range c.Paths {
		name := string(p.Path)
		if name == "" {
			name = "undecided"
		}
		fmt.Fprintf(&b, "%s\t%d\t%s\t%d of %d rules\t%s\n",
			name, p.Papers, prose.Percent(p.Share), p.Asked, c.Rules, prose.Percent(p.AskedShare(c.Rules)))
	}
	fmt.Fprintf(&b, "  papers\t%d\n", c.Papers)
	fmt.Fprintf(&b, "  extracted\t%d, which is the content plane\n", c.Read)
	fmt.Fprintf(&b, "  languages\t%d\n", len(c.Langs))
	fmt.Fprintf(&b, "  quiet rules\t%d, asked of under %s of the content plane\n", len(c.Silent), prose.Percent(1-Quiet))
	if len(c.Unknown) > 0 {
		fmt.Fprintf(&b, "  unknown\t%s, named by the policy and not by this project\n", strings.Join(c.Unknown, ", "))
	}
	return b.String()
}

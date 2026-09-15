package audit

import (
	"fmt"
	"strings"
	"text/tabwriter"
)

// Text is the report as it reads in a terminal.
//
// The scoreboard and nothing else when everything passed, because a run that
// found nothing has nothing to say and a wall of green is how a person learns
// to stop reading output.
func (r Report) Text() string {
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "rule\tstate\tchecked\tskipped\tfindings\t\n")
	for _, res := range r.Results {
		found := ""
		if res.Total > 0 {
			found = fmt.Sprint(res.Total)
		}
		// Skipped is printed only where there is one, because a column of
		// noughts down every rule of a corpus with no native papers in it is a
		// column nobody reads and this one is worth noticing.
		skipped := ""
		if res.Skipped > 0 {
			skipped = fmt.Sprint(res.Skipped)
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t\n", res.Rule.ID, res.State(), res.Checked, skipped, found)
	}
	tw.Flush()

	fmt.Fprintf(&b, "\n%s over %s, %s\n", plural(r.Records, r.unit()), plural(r.Shards, r.scope()), summary(r))

	for _, res := range r.Results {
		if res.Total == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n%s %s\n", res.Rule.ID, res.Rule.Says)
		for _, f := range res.Findings {
			fmt.Fprintf(&b, "  %s\n", f)
		}
		if n := res.Dropped(); n > 0 {
			fmt.Fprintf(&b, "  and %s\n", more(n, "finding"))
		}
	}
	return b.String()
}

// Markdown is the report as it is committed, which is what reports/audit.md
// holds.
func (r Report) Markdown() string {
	var b strings.Builder
	b.WriteString("# The audit\n\n")
	fmt.Fprintf(&b, "%s over %s of the %s plane, %s.\n\n", plural(r.Records, r.unit()), plural(r.Shards, r.scope()), r.Plane, summary(r))

	b.WriteString("| Group | Rules | Pass | Fail | Not run | N/A | Findings |\n")
	b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, t := range r.byGroup() {
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %d |\n",
			t.Group.Name(), t.Rules, t.Pass, t.Fail, t.NotRun, t.NA, t.Findings)
	}

	b.WriteString("\n## The rules\n\n")
	b.WriteString("| ID | State | Checked | Skipped | Findings | Rule |\n")
	b.WriteString("| --- | --- | ---: | ---: | ---: | --- |\n")
	for _, res := range r.Results {
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %s |\n",
			res.Rule.ID, res.State(), res.Checked, res.Skipped, res.Total, res.Rule.Says)
	}

	if r.Findings() == 0 {
		return b.String()
	}

	b.WriteString("\n## The findings\n")
	for _, res := range r.Results {
		if res.Total == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n### %s\n\n%s.\n", res.Rule.ID, sentence(res.Rule.Says))
		if res.Rule.Why != "" {
			fmt.Fprintf(&b, "\n%s\n", res.Rule.Why)
		}
		b.WriteString("\n```\n")
		for _, f := range res.Findings {
			fmt.Fprintf(&b, "%s\n", f)
		}
		b.WriteString("```\n")
		if n := res.Dropped(); n > 0 {
			fmt.Fprintf(&b, "\nAnd %s, not listed.\n", more(n, "finding"))
		}
	}
	return b.String()
}

func summary(r Report) string {
	switch n := r.Findings(); {
	// An empty plane has not passed anything. Saying "nothing found" over no
	// records is the sentence that lets a corpus believe in a rule that has
	// never run, which is the whole reason there are four states and not two.
	case r.Records == 0:
		return "nothing to check"
	case n == 0:
		return "nothing found"
	case r.Failed():
		return fmt.Sprintf("%s, and the build fails", plural(n, "finding"))
	default:
		return plural(n, "finding")
	}
}

func sentence(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// more counts the findings the cap threw away, in the order the words go in.
// plural would put it as "1 finding more", which is not English.
func more(n int, what string) string {
	if n == 1 {
		return fmt.Sprintf("1 more %s", what)
	}
	return fmt.Sprintf("%d more %ss", n, what)
}

func plural(n int, what string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", what)
	}
	return fmt.Sprintf("%d %ss", n, what)
}

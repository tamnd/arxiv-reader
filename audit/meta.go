package audit

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
)

// MetaRules are the rules that read the metadata plane.
//
// They are cheap, they read one JSONL line each, and they run over all 3.17
// million papers on every push. This is the half that catches a bad merge of a
// month's manifest, which is the failure this plane is most exposed to: a merge
// writes a whole month at once and a wrong one is three thousand quiet errors
// rather than a loud one.
//
// S01 through S04 and S06 through S12 are in 2166-10 and are not here, because
// every one of them reads a content file and there is no content plane yet.
// They arrive with it. Registering them now as rules that never run would be
// the same lie in the other direction.
var MetaRules = []Rule{
	{
		ID: "S05", Group: GroupSources, Hard: true,
		Says: "every licence field holds one of the values arXiv actually issues",
		Why:  "A licence this tool does not recognise is a licence it cannot reason about, and the access class it would fall back to is the permissive one.",
	},
	{
		ID: "S13", Group: GroupSources, Hard: true,
		Says: "every record's identifier is one arXiv could have issued, in either scheme",
		Why:  "There are hundreds of thousands of old style identifiers, they are case sensitive and they have a slash in them, and every tool that has got this wrong got it wrong on math.GT/0309136 rather than on 2106.09685.",
	},
	{
		ID: "S14", Group: GroupSources, Hard: true,
		Says: "every record is filed in the month its identifier names",
		Why:  "The month file is the only index this plane has. A record in the wrong one is a record no lookup will find, and it is also what makes checking for a repeated identifier a shard at a time enough.",
	},
	{
		ID: "S15", Group: GroupSources, Hard: true,
		Says: "no identifier appears twice in a month",
		Why:  "Two lines for one paper means a merge appended where it should have replaced, and the second copy is whatever the first one used to be.",
	},
	{
		ID: "S16", Group: GroupSources, Hard: true,
		Says: "every record has a title, an abstract, a category and at least one version",
		Why:  "A record missing any of these cannot be published, cannot be placed in the tree and cannot be licensed, so it is not a record.",
	},
	{
		ID: "S17", Group: GroupSources, Hard: true,
		Says: "a record's versions are numbered from v1 with no gaps",
		Why:  "arXiv has no v0 and skips no numbers, so a gap means the harvest dropped a version, and a dropped version is a licence nobody read.",
	},
	{
		ID: "S18", Group: GroupSources, Hard: true,
		Says: "a record's versions are dated in the order they were announced",
		Why:  "The licence census walks versions in order and stops at the one a paper was extracted from. Out of order dates make the version it stops at the wrong one.",
	},
	{
		ID: "S19", Group: GroupSources, Hard: true,
		Says: "no version is dated after the day the record was harvested",
		Why:  "A date in the future is a parse that went wrong, and the two year window it usually lands in is wide enough to look plausible.",
	},
	{
		ID: "S20", Group: GroupSources, Hard: true,
		Says: "no paper's first version is dated after the month its identifier names",
		Why:  "A paper can be announced months after it was submitted, because moderation holds it, and on a sample of five thousand the gap ran to four months. It is never negative: arXiv issues the number when it announces, so a v1 dated after its own number is a record that has been edited or merged wrong.",
	},
	{
		ID: "S21", Group: GroupSources, Hard: true,
		Says: "every record names the surface it was read from",
		Why:  "It is what tells S10 that a licence came from a snapshot that states one licence for a whole paper, and a record with no source reads as one that was never checked.",
	},
	{
		ID: "S22", Group: GroupSources, Hard: true,
		Says: "every category is an archive, or an archive and a subclass",
		Why:  "The primary category picks the baseline every soft rule is measured against, and a category that is not a category has no baseline.",
	},
	{
		ID: "S23", Group: GroupSources, Hard: true,
		Says: "every month's file is in identifier order",
		Why:  "The file is committed to git. Out of order lines mean the next merge rewrites the whole file, and a diff of three thousand moved lines hides the one line that changed.",
	},
}

// Meta runs the metadata plane rules.
type Meta struct {
	// Plane is what to read.
	Plane metadata.Plane
	// Now defaults to time.Now, and is what S19 measures against.
	Now func() time.Time
	// Cap is how many findings to keep per rule, and zero means fifty.
	Cap int
	// Only, if set, restricts the run to these rule IDs.
	Only []string
	// Log, if set, is called once per month with the records in it.
	Log func(shard string, records int)
}

// Run walks the shards and returns what the rules found.
//
// One pass over the plane feeding every rule, rather than a pass per rule.
// Twelve passes over 3.17 million records is twelve times the reading for the
// same answer, and the answer is wanted on every push.
func (m Meta) Run(shards []string) (Report, error) {
	c := &collector{cap: m.Cap, only: m.Only}
	if c.cap == 0 {
		c.cap = 50
	}
	now := time.Now
	if m.Now != nil {
		now = m.Now
	}
	today := now().UTC()

	report := Report{Plane: "meta"}
	for _, shard := range shards {
		month, err := monthOf(shard)
		if err != nil {
			return report, err
		}
		// Reset per shard. A repeated identifier only has to be looked for
		// inside a month, because S14 says a record is filed in the month its
		// identifier names, so two records with one identifier in two different
		// months is already a finding there. That keeps this to one month's
		// worth of identifiers rather than three million.
		seen := make(map[string]int)
		var previous string
		records := 0
		line := 0

		err = m.Plane.Scan(shard, func(r metadata.Record) error {
			line++
			records++
			at := func(rule, what string, args ...any) {
				c.add(Finding{Rule: rule, Shard: shard, Line: line, ID: r.ID, What: fmt.Sprintf(what, args...)})
			}

			// S13 first, because most of the rest need the identifier to have
			// parsed. A record whose id is not an id is reported once and then
			// stepped over, rather than reported by every rule in turn.
			c.checked("S13")
			id, err := axid.Parse(r.ID)
			if err != nil {
				at("S13", "%v", err)
				return nil
			}

			c.checked("S14")
			if got := corpus.Shard(id); got != shard {
				at("S14", "belongs in %s", got)
			}

			c.checked("S15")
			if first, ok := seen[r.ID]; ok {
				at("S15", "is already on line %d", first)
			} else {
				seen[r.ID] = line
			}

			c.checked("S16")
			for _, missing := range []struct {
				what string
				gone bool
			}{
				{"title", strings.TrimSpace(r.Title) == ""},
				{"abstract", strings.TrimSpace(r.Abstract) == ""},
				{"category", len(r.Categories) == 0},
				{"version", len(r.Versions) == 0},
			} {
				if missing.gone {
					at("S16", "has no %s", missing.what)
				}
			}

			c.checked("S17")
			for i, v := range r.Versions {
				if v.Version != i+1 {
					at("S17", "is numbered %s", versionRun(r.Versions))
					break
				}
			}

			c.checked("S18")
			for i := 1; i < len(r.Versions); i++ {
				if r.Versions[i].Created.Before(r.Versions[i-1].Created) {
					at("S18", "v%d is dated %s, before v%d at %s",
						r.Versions[i].Version, day(r.Versions[i].Created),
						r.Versions[i-1].Version, day(r.Versions[i-1].Created))
					break
				}
			}

			c.checked("S19")
			for _, v := range r.Versions {
				if v.Created.After(today) {
					at("S19", "v%d is dated %s, which has not happened", v.Version, day(v.Created))
					break
				}
			}

			c.checked("S20")
			if len(r.Versions) > 0 {
				// The end of the month the identifier names, which is the last
				// instant a v1 of that number could have been submitted.
				if first := r.Versions[0].Created; !first.Before(month.AddDate(0, 1, 0)) {
					at("S20", "v1 is dated %s, after the month %s names", day(first), r.ID)
				}
			}

			c.checked("S05")
			for _, v := range r.Versions {
				// Empty is not a finding. Empty means nobody has looked yet,
				// which is the ordinary state of this field until the M2
				// census, and it is the state S10 exists to keep distinct from
				// a licence somebody read off a snapshot.
				if v.Licence == "" {
					continue
				}
				if _, err := corpus.ParseLicence(string(v.Licence)); err != nil {
					at("S05", "v%d: %v", v.Version, err)
				}
			}

			c.checked("S21")
			if !r.Source.Valid() {
				at("S21", "was read from %q, which is not a surface this tool reads", r.Source)
			}

			c.checked("S22")
			for _, cat := range r.Categories {
				if !categoryPattern.MatchString(cat) {
					at("S22", "is in %q, which is not the shape of a category", cat)
				}
			}

			c.checked("S23")
			if key := r.SortKey(); previous != "" && key < previous {
				at("S23", "comes after %s", previous)
			} else {
				previous = key
			}
			return nil
		})
		if err != nil {
			return report, err
		}

		report.Records += records
		report.Shards++
		if m.Log != nil {
			m.Log(shard, records)
		}
	}

	report.Results = c.results()
	return report, nil
}

// collector holds what the rules found, in rule order and under the cap.
type collector struct {
	cap      int
	only     []string
	checks   map[string]int
	findings map[string][]Finding
	totals   map[string]int
}

func (c *collector) wanted(rule string) bool {
	if len(c.only) == 0 {
		return true
	}
	for _, want := range c.only {
		if strings.EqualFold(want, rule) {
			return true
		}
	}
	return false
}

func (c *collector) checked(rule string) {
	if !c.wanted(rule) {
		return
	}
	if c.checks == nil {
		c.checks = map[string]int{}
	}
	c.checks[rule]++
}

func (c *collector) add(f Finding) {
	if !c.wanted(f.Rule) {
		return
	}
	if c.totals == nil {
		c.totals = map[string]int{}
		c.findings = map[string][]Finding{}
	}
	c.totals[f.Rule]++
	// Counted always, kept up to the cap. The count is the number that says
	// whether a rule is wrong or the corpus is, and the kept ones are the
	// examples somebody reads.
	if len(c.findings[f.Rule]) < c.cap {
		c.findings[f.Rule] = append(c.findings[f.Rule], f)
	}
}

func (c *collector) results() []Result {
	out := make([]Result, 0, len(MetaRules))
	for _, rule := range MetaRules {
		if !c.wanted(rule.ID) {
			continue
		}
		found := c.findings[rule.ID]
		sortFindings(found)
		out = append(out, Result{
			Rule:     rule,
			Checked:  c.checks[rule.ID],
			Findings: found,
			Total:    c.totals[rule.ID],
		})
	}
	return out
}

// categoryPattern is an archive, or an archive and a subclass.
//
// Deliberately a shape and not a list. arXiv adds categories, the list would be
// out of date the first time it did, and a rule that fails every paper in a new
// archive is worse than one that misses a typo.
var categoryPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z-]*(\.[a-zA-Z][a-zA-Z-]*)?$`)

// monthOf turns a shard into the first instant of the month it names.
//
// The two digit year is arXiv's. 91 through 99 are the 1990s, everything else
// is the 2000s, and this stops working in 2091, by which time the identifier
// scheme will have changed twice more.
func monthOf(shard string) (time.Time, error) {
	m, err := metadata.ShardMonth(shard)
	if err != nil {
		return time.Time{}, fmt.Errorf("audit: %w", err)
	}
	return m, nil
}

func day(t time.Time) string { return t.UTC().Format("2006-01-02") }

func versionRun(versions []metadata.Version) string {
	out := make([]string, len(versions))
	for i, v := range versions {
		out[i] = fmt.Sprintf("v%d", v.Version)
	}
	return strings.Join(out, " ")
}

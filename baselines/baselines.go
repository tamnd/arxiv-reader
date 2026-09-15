// Package baselines is what ordinary looks like, one category at a time.
//
// The soft rules in papers compared against constants, because papers held a
// hundred computer science papers and a constant was a fair description of all
// of them. Here a constant is wrong in both directions at once. A pure
// mathematics paper has fifteen displays a page and two figures in total, a
// structural biology paper has two displays and a figure on every page, and one
// threshold over both either fails thousands of ordinary papers or catches
// nothing.
//
// So a soft rule with a threshold takes it from the median of the paper's
// primary category and reports the paper's distance from that rather than its
// absolute value. A hard rule never does: a hard rule states an invariant of the
// corpus, and an invariant that moves with the neighbourhood is not one.
package baselines

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// Floor is how many papers a category needs before its numbers are a baseline.
//
// Under it there is a number and there is no baseline, and the difference
// matters more here than anywhere else in this project: a median taken over
// three papers is the middle of three papers, a rule that compared the fourth
// against it would be comparing a paper with its three neighbours, and the
// findings would say more about which papers were extracted first than about
// the paper. A category under the floor is written down with its count and
// marked unusable rather than left out, because how far a category is from
// having a baseline is the thing somebody reading this file wants to know.
const Floor = 30

// Metric is one thing measured about a paper.
//
// Each of them is a quantity a soft rule needs and nothing else. A metric with
// no rule behind it is a number nobody checks, which goes stale without anybody
// finding out.
type Metric string

const (
	// DisplaysPerPage is what M06 reads.
	DisplaysPerPage Metric = "displays_per_page"
	// ReferencesResolved is what R09 reads, as a share between 0 and 1.
	ReferencesResolved Metric = "references_resolved"
)

// Metrics is every metric, in the order they are written.
var Metrics = []Metric{DisplaysPerPage, ReferencesResolved}

// Page is how many characters of body count as a page.
//
// A Markdown corpus has no pages, and displays per page is the quantity the
// rule is about, so one is defined here: three thousand characters, which is
// near enough what a single column page of a paper holds. It is a convention
// and not a measurement, and it is the same convention for every paper, so the
// comparison it supports is between papers of a category and never between a
// paper and a printed page.
const Page = 3000

// Stat is one metric over one category.
type Stat struct {
	// Median is the middle value, and Spread is how far from it is ordinary.
	Median float64 `yaml:"median"`
	Spread float64 `yaml:"spread"`
	// Papers is how many papers carried this metric, which is not always the
	// category's paper count: a paper with no bibliography says nothing about
	// how well bibliographies resolve.
	Papers int `yaml:"papers"`
}

// Distance is how far a value is from the median, counted in spreads.
//
// A category where every paper agrees has a spread of zero, and there a value
// that differs at all is infinitely far, which is the honest answer: nothing in
// the category has ever been anywhere else.
func (s Stat) Distance(v float64) float64 {
	if s.Spread == 0 {
		if v == s.Median {
			return 0
		}
		return math.Inf(1)
	}
	return math.Abs(v-s.Median) / s.Spread
}

// Far says whether a value is more than k spreads from the median.
func (s Stat) Far(v, k float64) bool { return s.Distance(v) > k }

// Category is one primary category's numbers.
type Category struct {
	Category string `yaml:"category"`
	// Papers is how many papers of this category the corpus has read.
	Papers int             `yaml:"papers"`
	Stats  map[Metric]Stat `yaml:"stats,omitempty"`
}

// Manifest is manifests/baselines.yaml.
type Manifest struct {
	// Floor is the floor these numbers were built under, written down because a
	// file read by a later version of this tool should be judged by the rule it
	// was made with and not by one that moved since.
	Floor int `yaml:"floor"`
	// Built is the date, as YYYY-MM-DD, and Papers is what was measured.
	Built  string `yaml:"built"`
	Papers int    `yaml:"papers"`
	// Categories is one entry per primary category, in category order.
	Categories []Category `yaml:"categories"`
}

// Sample is one paper's measurements.
//
// A metric a paper has nothing to say about is absent rather than zero. A paper
// with no bibliography has no resolution rate, and counting it as nought would
// pull every category's median towards the papers nobody has run ax refs over.
type Sample struct {
	ID       string
	Category string
	Values   map[Metric]float64
}

// Values turns one paper's counts into the metrics this file holds.
//
// The definition of each metric lives here rather than where the counting is
// done, so that the file and the rules that read it cannot disagree about what
// a number in it means.
//
// A paper with no body says nothing about displays per page and a paper with no
// bibliography says nothing about how well bibliographies resolve, and in both
// cases the metric is absent rather than nought.
func Values(displays, characters, entries, resolved int) map[Metric]float64 {
	out := map[Metric]float64{}
	if characters > 0 {
		out[DisplaysPerPage] = round(float64(displays) / (float64(characters) / Page))
	}
	if entries > 0 {
		out[ReferencesResolved] = round(float64(resolved) / float64(entries))
	}
	return out
}

// Build computes the medians over a set of samples.
//
// The spread is the median absolute deviation, scaled so that it means on a
// normal distribution what a standard deviation means. A mean and a standard
// deviation would be the obvious pair and they are the wrong one here, because
// one review paper with forty displays a page moves both, and the rule that
// reads them would then be looser for every other paper in the category because
// of that one.
func Build(samples []Sample, built string) Manifest {
	m := Manifest{Floor: Floor, Built: built, Papers: len(samples)}
	papers := map[string]int{}
	values := map[string]map[Metric][]float64{}
	for _, s := range samples {
		if s.Category == "" {
			continue
		}
		papers[s.Category]++
		for _, metric := range Metrics {
			v, ok := s.Values[metric]
			if !ok {
				continue
			}
			if values[s.Category] == nil {
				values[s.Category] = map[Metric][]float64{}
			}
			values[s.Category][metric] = append(values[s.Category][metric], v)
		}
	}
	for cat, n := range papers {
		c := Category{Category: cat, Papers: n}
		for _, metric := range Metrics {
			vs := values[cat][metric]
			if len(vs) == 0 {
				continue
			}
			if c.Stats == nil {
				c.Stats = map[Metric]Stat{}
			}
			c.Stats[metric] = Stat{Median: round(median(vs)), Spread: round(spread(vs)), Papers: len(vs)}
		}
		m.Categories = append(m.Categories, c)
	}
	sort.Slice(m.Categories, func(i, j int) bool { return m.Categories[i].Category < m.Categories[j].Category })
	return m
}

// Of is the baseline a rule should compare a paper against.
//
// It says no for a category nobody has extracted enough of, which is most of
// them for most of this project, and a rule that gets no here reports that it
// had nothing to measure against rather than passing.
func (m Manifest) Of(category string, metric Metric) (Stat, bool) {
	floor := m.Floor
	if floor < 1 {
		floor = Floor
	}
	for _, c := range m.Categories {
		if c.Category != category {
			continue
		}
		if c.Papers < floor {
			return Stat{}, false
		}
		s, ok := c.Stats[metric]
		if !ok || s.Papers < floor {
			return Stat{}, false
		}
		return s, true
	}
	return Stat{}, false
}

// Find is one category's row, whether or not it has enough papers to be a
// baseline.
func (m Manifest) Find(category string) (Category, bool) {
	for _, c := range m.Categories {
		if c.Category == category {
			return c, true
		}
	}
	return Category{}, false
}

// Usable is how many categories have enough papers behind them to judge one.
func (m Manifest) Usable() int {
	floor := m.Floor
	if floor < 1 {
		floor = Floor
	}
	n := 0
	for _, c := range m.Categories {
		if c.Papers >= floor {
			n++
		}
	}
	return n
}

const header = `# What ordinary looks like, one primary category at a time.
#
# Written by ax audit -baselines. The soft rules with a threshold read this and
# report a paper's distance from its category's median rather than its absolute
# value, because one constant over all of arXiv either fails thousands of
# ordinary papers or catches nothing. A category with fewer papers than the
# floor is written down with its count and is not a baseline: a median over
# three papers says more about which three were extracted first than about the
# category.
`

// Load reads the manifest.
//
// A file that is not there is an empty manifest and not an error, because a
// corpus nobody has computed the baselines for yet is an ordinary state, and
// the rules that read it say they had nothing to measure against.
func Load(path string) (Manifest, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Manifest{}, nil
	}
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := yaml.Unmarshal(b, &m); err != nil {
		return Manifest{}, fmt.Errorf("baselines: %s: %w", path, err)
	}
	return m, nil
}

// Save writes the manifest, creating the directory if it is missing.
func (m Manifest) Save(path string) error {
	body, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(header), body...), 0o644)
}

// median is the middle value, or the mean of the middle two.
func median(in []float64) float64 {
	vs := append([]float64(nil), in...)
	sort.Float64s(vs)
	n := len(vs)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return vs[n/2]
	}
	return (vs[n/2-1] + vs[n/2]) / 2
}

// spread is the median absolute deviation, scaled to a standard deviation.
//
// The constant is 1.4826, which is what makes the two agree on a normal
// distribution, so a rule written as three sigma means what its author meant by
// it and does not have to be rewritten for this.
func spread(in []float64) float64 {
	mid := median(in)
	away := make([]float64, len(in))
	for i, v := range in {
		away[i] = math.Abs(v - mid)
	}
	return 1.4826 * median(away)
}

// round keeps four decimal places, because a baseline is a threshold and not a
// measurement, and sixteen digits of float in a committed file is a diff on
// every run.
func round(v float64) float64 { return math.Round(v*10000) / 10000 }

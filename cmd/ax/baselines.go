package main

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/tamnd/arxiv-reader/audit"
	"github.com/tamnd/arxiv-reader/baselines"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/selection"
)

// runBaselines recomputes manifests/baselines.yaml.
//
// It reads the content plane for the numbers and the metadata plane for the
// category each paper belongs to, because the primary category is a fact about
// the record and nothing in a content file is allowed to be the authority on
// it. The plane is read once for the whole corpus rather than once per paper.
func runBaselines(lang, shard string, dry, quiet bool) error {
	root := corpusRoot()
	c := audit.Content{Root: root, Lang: lang}
	papers, err := c.Papers()
	if err != nil {
		return err
	}
	if shard != "" {
		if !metadata.ValidShard(shard) {
			return fmt.Errorf("%q is not a month, want four digits like 2106", shard)
		}
		papers = keepMonth(papers, shard)
		if len(papers) == 0 {
			return fmt.Errorf("no paper has been extracted into content/%s from %s", lang, shard)
		}
	}
	if len(papers) == 0 {
		return fmt.Errorf("nothing has been extracted into content/%s, and a baseline is a description of papers", lang)
	}
	if !quiet && len(papers) > 1 {
		done := 0
		c.Log = func(paper string, files int) {
			done++
			if done%50 == 0 || done == len(papers) {
				fmt.Fprintf(os.Stderr, "%d of %d papers\n", done, len(papers))
			}
		}
	}
	measured, err := c.Measure(papers)
	if err != nil {
		return err
	}

	plane := metadata.Plane{Root: root}
	want := make(map[string]bool, len(measured))
	for _, m := range measured {
		want[m.ID] = true
	}
	category := map[string]string{}
	if err := plane.ScanAll(func(_ string, r metadata.Record) error {
		if want[r.ID] {
			category[r.ID] = r.Primary()
		}
		return nil
	}); err != nil {
		return err
	}

	samples := make([]baselines.Sample, 0, len(measured))
	var orphans []string
	for _, m := range measured {
		cat, ok := category[m.ID]
		if !ok || cat == "" {
			// A paper the plane has never heard of is group S's finding and not
			// this command's, so it is named and left out rather than counted
			// under an empty category that would then have a baseline of its own.
			orphans = append(orphans, m.ID)
			continue
		}
		samples = append(samples, baselines.Sample{
			ID:       m.ID,
			Category: cat,
			Values:   baselines.Values(m.Displays, m.Characters, m.Entries, m.Resolved),
		})
	}
	built := baselines.Build(samples, selection.Today())

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "category\tpapers\tdisplays per page\treferences resolved\n")
	for _, cat := range built.Categories {
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n", cat.Category, cat.Papers,
			stat(built, cat, baselines.DisplaysPerPage), stat(built, cat, baselines.ReferencesResolved))
	}
	tw.Flush()
	sort.Strings(orphans)
	for _, id := range orphans {
		fmt.Fprintf(os.Stderr, "%s: the metadata plane has no record of this paper, so it has no category and is not in the baselines\n", id)
	}
	fmt.Fprintf(os.Stderr, "  papers      %d over %d categories\n", built.Papers, len(built.Categories))
	// The number that says whether this file is usable yet, and it is nought
	// until the corpus is thousands of papers. Saying it every run is how nobody
	// ends up reading a median of three papers as though it described a field.
	fmt.Fprintf(os.Stderr, "  baselines   %d, which is the categories with %d papers or more\n", built.Usable(), baselines.Floor)
	if dry {
		fmt.Fprintln(os.Stderr, "nothing written, because this was a dry run")
		return nil
	}
	path := corpus.BaselinesPath(root)
	if err := built.Save(path); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "written to %s\n", corpus.BaselinesPath(""))
	return nil
}

// stat is one cell of the table, and it says what the number is worth.
//
// A median from a category with too few papers behind it is printed in brackets,
// because leaving it out would hide what the corpus has and printing it plain
// would be offering it as a threshold.
func stat(m baselines.Manifest, c baselines.Category, metric baselines.Metric) string {
	s, ok := c.Stats[metric]
	if !ok {
		return "none"
	}
	if _, usable := m.Of(c.Category, metric); !usable {
		return fmt.Sprintf("(%.2f)", s.Median)
	}
	return fmt.Sprintf("%.2f, %.2f either way", s.Median, s.Spread)
}

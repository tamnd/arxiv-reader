package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/audit"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
)

// runAudit runs the numbered rules and exits non-zero if a hard one found
// something.
//
// One plane per run. The two read different files, cost different amounts and
// have different rule sets, and a flag that quietly ran both would make the
// cheap one wait for the expensive one on every push.
func runAudit(args []string) error {
	fs := flag.NewFlagSet("ax audit", flag.ContinueOnError)
	plane := fs.String("plane", "meta", "which half to read, meta or content")
	shard := fs.String("shard", "", "one month, so 2106")
	lang := fs.String("lang", "en", "which language of the content plane to read")
	rules := fs.String("rules", "", "a comma separated list of rule ids, so S14,S20")
	hard := fs.Bool("hard", false, "only the rules that fail a build")
	report := fs.String("report", "", "write the markdown report here as well")
	limit := fs.Int("cap", 50, "findings to list per rule, the rest counted")
	quiet := fs.Bool("q", false, "no per month progress")
	medians := fs.Bool("baselines", false, "recompute manifests/baselines.yaml instead of running the rules")
	dry := fs.Bool("n", false, "with -baselines, print the medians and write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("ax audit takes no arguments, so drop %s", fs.Args()[0])
	}
	// The baselines are what the soft thresholds are read from and computing
	// them is not a rule, so it is this command's other job rather than another
	// command: it reads the same plane, with the same flags, over the same
	// papers, and a rule that wants a median is asking about the corpus the
	// audit just walked.
	if *medians {
		return runBaselines(*lang, *shard, *dry, *quiet)
	}
	if *limit < 1 {
		return errors.New("-cap has to be at least 1")
	}
	var known []audit.Rule
	switch *plane {
	case "meta":
		known = audit.MetaRules
	case "content":
		known = audit.ContentRules
	default:
		return fmt.Errorf("%q is not a plane, want meta or content", *plane)
	}

	only, err := chooseRules(*rules, *hard, *plane, known)
	if err != nil {
		return err
	}

	var result audit.Report
	if *plane == "content" {
		result, err = auditContent(*lang, *shard, *limit, only, *quiet)
	} else {
		result, err = auditMeta(*shard, *limit, only, *quiet)
	}
	if err != nil {
		return err
	}
	fmt.Print(result.Text())

	if *report != "" {
		if err := os.MkdirAll(filepath.Dir(*report), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(*report, []byte(result.Markdown()), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "written to %s\n", *report)
	}

	// The exit code is the whole point of the command in CI. A findings list
	// nobody reads and a green build is worse than no audit.
	if result.Failed() {
		return errors.New("a hard rule found something")
	}
	return nil
}

// auditMeta reads the metadata plane.
func auditMeta(shard string, limit int, only []string, quiet bool) (audit.Report, error) {
	plane := metadata.Plane{Root: corpusRoot()}
	shards, err := plane.Shards()
	if err != nil {
		return audit.Report{}, err
	}
	if shard != "" {
		if !metadata.ValidShard(shard) {
			return audit.Report{}, fmt.Errorf("%q is not a month, want four digits like 2106", shard)
		}
		shards = keepShard(shards, shard)
		if len(shards) == 0 {
			return audit.Report{}, fmt.Errorf("the plane at %s has no %s", plane.Dir(), shard)
		}
	}

	m := audit.Meta{Plane: plane, Now: time.Now, Cap: limit, Only: only}
	if !quiet && len(shards) > 1 {
		done := 0
		m.Log = func(shard string, records int) {
			done++
			// Every fiftieth month, because there are about four hundred of
			// them and a line each is four hundred lines of nothing.
			if done%50 == 0 || done == len(shards) {
				fmt.Fprintf(os.Stderr, "%d of %d months\n", done, len(shards))
			}
		}
	}
	return m.Run(shards)
}

// auditContent reads one language of the content plane.
func auditContent(lang, shard string, limit int, only []string, quiet bool) (audit.Report, error) {
	c := audit.Content{Root: corpusRoot(), Lang: lang, Cap: limit, Only: only}
	papers, err := c.Papers()
	if err != nil {
		return audit.Report{}, err
	}
	if shard != "" {
		if !metadata.ValidShard(shard) {
			return audit.Report{}, fmt.Errorf("%q is not a month, want four digits like 2106", shard)
		}
		papers = keepMonth(papers, shard)
		if len(papers) == 0 {
			return audit.Report{}, fmt.Errorf("no paper has been extracted into content/%s from %s", lang, shard)
		}
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
	return c.Run(papers)
}

// chooseRules turns -rules and -hard into the list to run.
func chooseRules(rules string, hard bool, plane string, known []audit.Rule) ([]string, error) {
	byID := map[string]audit.Rule{}
	for _, r := range known {
		byID[strings.ToUpper(r.ID)] = r
	}

	var only []string
	if rules != "" {
		for _, name := range strings.Split(rules, ",") {
			name = strings.ToUpper(strings.TrimSpace(name))
			if name == "" {
				continue
			}
			if _, ok := byID[name]; !ok {
				return nil, fmt.Errorf("%s is not a rule this tool runs over the %s plane", name, plane)
			}
			only = append(only, name)
		}
		if len(only) == 0 {
			return nil, errors.New("-rules was given and named nothing")
		}
	}
	if !hard {
		return only, nil
	}
	var out []string
	for _, r := range known {
		if !r.Hard {
			continue
		}
		if len(only) > 0 && !contains(only, strings.ToUpper(r.ID)) {
			continue
		}
		out = append(out, r.ID)
	}
	if len(out) == 0 {
		return nil, errors.New("-hard and -rules together named no rules")
	}
	return out, nil
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func keepShard(shards []string, want string) []string {
	for _, s := range shards {
		if s == want {
			return []string{s}
		}
	}
	return nil
}

// keepMonth is the same filter over papers, which carry their month in their
// identifier rather than in a file name.
func keepMonth(papers []axid.ID, want string) []axid.ID {
	var out []axid.ID
	for _, id := range papers {
		if corpus.Shard(id) == want {
			out = append(out, id)
		}
	}
	return out
}

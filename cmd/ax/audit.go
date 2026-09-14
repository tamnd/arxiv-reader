package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tamnd/arxiv-reader/audit"
	"github.com/tamnd/arxiv-reader/metadata"
)

// runAudit runs the numbered rules and exits non-zero if a hard one found
// something.
//
// Only the metadata plane so far. The content plane rules read content files
// and there is no content plane until M3, so --plane content says so rather
// than reporting a clean run over nothing, which is the failure the whole
// three state design exists to avoid.
func runAudit(args []string) error {
	fs := flag.NewFlagSet("ax audit", flag.ContinueOnError)
	plane := fs.String("plane", "meta", "which half to read, meta or content")
	shard := fs.String("shard", "", "one month, so 2106")
	rules := fs.String("rules", "", "a comma separated list of rule ids, so S14,S20")
	hard := fs.Bool("hard", false, "only the rules that fail a build")
	report := fs.String("report", "", "write the markdown report here as well")
	limit := fs.Int("cap", 50, "findings to list per rule, the rest counted")
	quiet := fs.Bool("q", false, "no per month progress")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("ax audit takes no arguments, so drop %s", fs.Args()[0])
	}
	if *limit < 1 {
		return errors.New("-cap has to be at least 1")
	}
	switch *plane {
	case "meta":
	case "content":
		return errors.New("the content plane arrives in milestone M3, and the rules that read it arrive with it")
	default:
		return fmt.Errorf("%q is not a plane, want meta or content", *plane)
	}

	only, err := chooseRules(*rules, *hard)
	if err != nil {
		return err
	}

	plane2 := metadata.Plane{Root: corpusRoot()}
	shards, err := plane2.Shards()
	if err != nil {
		return err
	}
	if *shard != "" {
		if !metadata.ValidShard(*shard) {
			return fmt.Errorf("%q is not a month, want four digits like 2106", *shard)
		}
		shards = keepShard(shards, *shard)
		if len(shards) == 0 {
			return fmt.Errorf("the plane at %s has no %s", plane2.Dir(), *shard)
		}
	}

	m := audit.Meta{Plane: plane2, Now: time.Now, Cap: *limit, Only: only}
	if !*quiet && len(shards) > 1 {
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

	result, err := m.Run(shards)
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

// chooseRules turns -rules and -hard into the list to run.
func chooseRules(rules string, hard bool) ([]string, error) {
	known := map[string]audit.Rule{}
	for _, r := range audit.MetaRules {
		known[strings.ToUpper(r.ID)] = r
	}

	var only []string
	if rules != "" {
		for _, name := range strings.Split(rules, ",") {
			name = strings.ToUpper(strings.TrimSpace(name))
			if name == "" {
				continue
			}
			if _, ok := known[name]; !ok {
				return nil, fmt.Errorf("%s is not a rule this tool runs over the metadata plane", name)
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
	// Every metadata plane rule is hard today, so -hard is a filter that
	// currently removes nothing. It is wired up anyway, because the soft rules
	// arrive with the content plane and a flag that starts working silently is
	// a flag nobody trusts.
	var out []string
	for _, r := range audit.MetaRules {
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

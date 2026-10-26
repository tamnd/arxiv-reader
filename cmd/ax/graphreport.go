package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/analysis"
	"github.com/tamnd/arxiv-reader/edges"
	"github.com/tamnd/arxiv-reader/gold"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/objects"
)

// graphReport rebuilds reports/graph.md from analysis/edges and
// analysis/objects. These are the graph checks of AX6.7.
func graphReport(args []string) error {
	fs := flag.NewFlagSet("ax graph report", flag.ContinueOnError)
	out := fs.String("o", "", "where to write the report, and the default is reports/graph.md in the corpus")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root := corpusRoot()
	plane := metadata.Plane{Root: root}
	codes := map[string]map[string]string{}
	codeOf := func(shard, paper string) string {
		m, ok := codes[shard]
		if !ok {
			m = map[string]string{}
			_ = plane.Scan(shard, func(r metadata.Record) error {
				m[r.ID] = gold.CodeOf(r)
				return nil
			})
			codes[shard] = m
		}
		if c := m[paper]; c != "" {
			return c
		}
		return "unknown"
	}
	files, err := filepath.Glob(filepath.Join(root, "analysis", "edges", "*", "*.jsonl"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	rp := edges.NewReport()
	rp.Generated = time.Now()
	for _, f := range files {
		es, err := edges.ReadFile(f)
		if err != nil {
			return err
		}
		if len(es) == 0 {
			continue
		}
		paper, _, _ := strings.Cut(es[0].From, "#")
		id, err := axid.Parse(paper)
		if err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		objs, err := readObjectsOf(root, id)
		if err != nil {
			return err
		}
		shard := filepath.Base(filepath.Dir(f))
		code := codeOf(shard, paper)
		byVersion := map[int][]edges.Edge{}
		for _, e := range es {
			byVersion[e.Evidence.Version] = append(byVersion[e.Evidence.Version], e)
		}
		for v, ves := range byVersion {
			var vobjs []objects.Record
			for _, o := range objs {
				if o.Path == "source" && o.Version == strconv.Itoa(v) {
					vobjs = append(vobjs, o)
				}
			}
			rp.Add(analysis.MonthOf(shard), code, vobjs, ves)
		}
	}
	if *out == "" {
		*out = filepath.Join(root, "reports", "graph.md")
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(*out, []byte(rp.Markdown()), 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: %d versions, %d edges\n", *out, rp.All.Papers, rp.All.Edges)
	return nil
}

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	iofs "io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/analysis"
	"github.com/tamnd/arxiv-reader/census"
	"github.com/tamnd/arxiv-reader/gold"
	"github.com/tamnd/arxiv-reader/licence"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/objects"
	"github.com/tamnd/arxiv-reader/scope"
	"github.com/tamnd/arxiv-reader/witness"
)

// censusReport rebuilds reports/census.md from the analysis plane.
func censusReport(args []string) error {
	fs := flag.NewFlagSet("ax census report", flag.ContinueOnError)
	since := fs.String("since", "", "read only the papers from this month, as YYMM")
	out := fs.String("o", "", "where to write the report, and the default is reports/census.md in the corpus")
	noScope := fs.Bool("no-scope", false, "do not count the open papers in scope, which reads the whole metadata plane")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root := corpusRoot()
	from := ""
	if *since != "" {
		from = analysis.MonthOf(*since)
	}
	rp := analysis.NewReport(from)
	rp.Generated = time.Now()
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

	files, err := filepath.Glob(filepath.Join(root, "analysis", "census", "*", "*.json"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	wit := &witness.Cache{Root: root}
	extracted := map[string]bool{}
	objs := map[string][]objects.Record{}
	for _, f := range files {
		r, err := readCensusFile(f)
		if err != nil {
			return err
		}
		extracted[fmt.Sprintf("%sv%d", r.Paper, r.Version)] = true
		id, err := axid.Parse(r.Paper)
		if err != nil {
			return err
		}
		all, ok := objs[r.Paper]
		if !ok {
			if all, err = readObjectsOf(root, id); err != nil {
				return err
			}
			objs[r.Paper] = all
		}
		var mine []objects.Record
		for _, o := range all {
			if o.Path == "source" && o.Version == fmt.Sprint(r.Version) {
				mine = append(mine, o)
			}
		}
		shard := filepath.Base(filepath.Dir(f))
		rp.Add(shard, codeOf(shard, r.Paper), r, mine)
		for _, index := range witness.Indexes {
			w, ok, err := wit.Get(index, shard, r.Paper)
			if err != nil {
				return err
			}
			if ok {
				rp.AddWitness(shard, index, r, w)
			}
		}
	}
	stops, err := filepath.Glob(filepath.Join(root, "analysis", "stopped", "*", "*.json"))
	if err != nil {
		return err
	}
	stopped := map[string]bool{}
	for _, f := range stops {
		s, err := analysis.ReadStop(f)
		if err != nil {
			return err
		}
		if from != "" && analysis.MonthOf(filepath.Base(filepath.Dir(f))) < from {
			continue
		}
		stopped[fmt.Sprintf("%sv%d", s.Paper, s.Version)] = true
		rp.AddStop(s)
	}

	if !*noScope {
		in, err := scope.Read(scope.Path(root, scope.Math))
		if err != nil && !errors.Is(err, iofs.ErrNotExist) {
			return err
		}
		if len(in) > 0 {
			err = plane.ScanAll(func(shard string, r metadata.Record) error {
				if !in[r.ID] || (from != "" && analysis.MonthOf(shard) < from) {
					return nil
				}
				latest, ok := r.Latest()
				if !ok {
					return nil
				}
				l := latest.Licence
				if latest.LicenceFrom != metadata.SourceAbs {
					l, _ = licence.Latest(r)
				}
				if c := licence.ClassOf(l); c != licence.ClassOpen && c != licence.ClassShareAlikeNC {
					// A paper that math2 cannot read has its structure from
					// TheoremGraph by way R1 (AX2.5), when the index has it.
					rp.Closed++
					if _, ok, err := wit.Get(witness.TheoremGraph, shard, r.ID); err != nil {
						return err
					} else if ok {
						rp.ClosedOutside++
					}
					return nil
				}
				rp.Scope++
				ref := fmt.Sprintf("%sv%d", r.ID, latest.Version)
				switch {
				case extracted[ref]:
					rp.Extracted++
				case stopped[ref]:
					rp.Halted++
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
	}

	for _, part := range []string{"census", "objects", "stopped", "witness"} {
		z := analysis.PlaneSize{Part: part}
		err := filepath.WalkDir(filepath.Join(root, "analysis", part), func(p string, d iofs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type().IsRegular() {
				info, err := d.Info()
				if err != nil {
					return err
				}
				z.Files++
				z.Bytes += info.Size()
			}
			return nil
		})
		if err != nil && !errors.Is(err, iofs.ErrNotExist) {
			return err
		}
		rp.Size = append(rp.Size, z)
	}

	md := rp.Markdown()
	path := *out
	if path == "" {
		path = filepath.Join(root, "reports", "census.md")
	}
	if err := writeFile(path, []byte(md)); err != nil {
		return err
	}
	// The counts go next to the report as JSON, for m2 corpus status.
	sum, err := json.MarshalIndent(rp.Summary(!*noScope && rp.Scope > 0), "", "  ")
	if err != nil {
		return err
	}
	jsonPath := strings.TrimSuffix(path, filepath.Ext(path)) + ".json"
	if err := writeFile(jsonPath, append(sum, '\n')); err != nil {
		return err
	}
	fmt.Printf("%d versions with a census, %d stopped", rp.All.Papers, len(stopped))
	if rp.Scope > 0 {
		fmt.Printf(", %d of %d open papers in scope done", rp.Extracted+rp.Halted, rp.Scope)
	}
	fmt.Println()
	fmt.Fprintf(os.Stderr, "written to %s and %s\n", path, jsonPath)
	return nil
}

func readCensusFile(file string) (*census.Record, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var r census.Record
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return &r, nil
}

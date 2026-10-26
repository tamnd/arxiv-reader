package analysis

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/objects"
	"github.com/tamnd/arxiv-reader/source"
)

// TestSetT runs the regression set T of AX11.2 (testdata/sett/README.md).
func TestSetT(t *testing.T) {
	files, err := filepath.Glob("../testdata/sett/*.tex")
	if err != nil || len(files) == 0 {
		t.Fatalf("set T has no files: %v", err)
	}
	m := DefaultManifests()
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".tex")
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			wants, rules := readWants(string(data))
			b := &source.Bundle{Files: []source.File{{Name: "main.tex", Data: data}}}
			res, err := Analyze(b, Paper{ID: "test." + name, Version: 1, Extractor: "test"}, m)
			if err != nil {
				t.Fatal(err)
			}
			got := res.Objects
			for i, w := range wants {
				if i >= len(got) {
					t.Errorf("want %s, and there are only %d objects", w["local"], len(got))
					continue
				}
				checkWant(t, w, got[i])
			}
			for _, o := range got[min(len(got), len(wants)):] {
				t.Errorf("extra object %s %s %s", o.Local, o.Item, o.Number)
			}
			for _, r := range res.Census {
				for _, g := range r.Gaps {
					t.Errorf("gap %s %s %s line %d", g.Occurrence, g.Result, g.Pattern, g.Line)
				}
				for _, p := range r.Phantoms {
					t.Errorf("phantom %s", p)
				}
				var found []string
				for _, f := range r.Findings {
					found = append(found, f.Rule)
					if !slices.Contains(rules, f.Rule) {
						t.Errorf("finding %s %s: %s", f.Rule, f.Occurrence, f.Message)
					}
				}
				for _, rule := range rules {
					if !slices.Contains(found, rule) {
						t.Errorf("want a finding %s, and the census has none", rule)
					}
				}
			}
		})
	}
}

// readWants reads the % want and % finding lines of a file of set T.
func readWants(text string) ([]map[string]string, []string) {
	var wants []map[string]string
	var rules []string
	for _, line := range strings.Split(text, "\n") {
		if r, ok := strings.CutPrefix(line, "% finding "); ok {
			rules = append(rules, strings.TrimSpace(r))
			continue
		}
		rest, ok := strings.CutPrefix(line, "% want ")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 3 {
			continue
		}
		w := map[string]string{"local": f[0], "item": f[1], "number": f[2]}
		for _, kv := range f[3:] {
			k, v, _ := strings.Cut(kv, "=")
			w[k] = v
		}
		wants = append(wants, w)
	}
	return wants, rules
}

func checkWant(t *testing.T, w map[string]string, o objects.Record) {
	t.Helper()
	number := o.Number
	if number == "" {
		number = "-"
	}
	got := map[string]string{
		"local": o.Local, "item": o.Item, "number": number, "proves": o.Proves,
		"part": o.ProvesPart, "status": o.ProofStatus, "origin": o.Origin,
		"labels": strings.Join(o.Labels, ","), "parent": o.Parent,
	}
	for k, v := range w {
		if k == "flag" {
			if !slices.Contains(o.Flags, v) {
				t.Errorf("%s: want the flag %s, got %v", w["local"], v, o.Flags)
			}
			continue
		}
		if got[k] != v {
			t.Errorf("%s: %s is %q, want %q", w["local"], k, got[k], v)
		}
	}
}

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"

	"github.com/tamnd/arxiv-reader/analysis"
	"github.com/tamnd/arxiv-reader/census"
	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/source"
)

// runCensus takes the census of one version from its e-print (AX04).
//
// It prints the counts, the gaps and the findings. With -json it prints the
// census record of AX4.7 instead.
func runCensus(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "batch":
			return censusBatch(args[1:])
		case "report":
			return censusReport(args[1:])
		}
	}
	fs := flag.NewFlagSet("ax census", flag.ContinueOnError)
	render := fs.String("render", "", "the LaTeXML render of the same version, as the second witness")
	asJSON := fs.Bool("json", false, "print the census record as JSON")
	items := fs.Bool("items", false, "print each item occurrence")
	write := fs.Bool("write", false, "write the census record and the object records under analysis/ in the corpus")
	class := fs.String("class", "auto", "the licence class of the version: open, record, or auto to read it from the metadata plane")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return errors.New("usage: ax census [-render page.html] [-class open|record|auto] [-write] [-json] [-items] <id>v<n> <e-print file>")
	}
	id, version, err := splitVersion(fs.Arg(0))
	if err != nil {
		return err
	}
	b, err := source.Read(fs.Arg(1))
	if err != nil {
		return err
	}
	p := analysis.Paper{ID: id, Version: version, Extractor: "ax/" + Version}
	waivers, err := analysis.ReadWaivers(corpus.WaiversPath(corpusRoot()))
	if err != nil {
		return err
	}
	p.Waivers = waivers.For(id, version)
	switch *class {
	case "open":
		p.Text = true
	case "record":
	case "auto":
		// A version with no known licence is a record version, because that
		// is the safe side (rule D06).
		lic := licences{plane: metadata.Plane{Root: corpusRoot()}}
		p.Text, _ = lic.text(id, version)
	default:
		return fmt.Errorf("%q is not a class, want open, record or auto", *class)
	}
	if *render != "" {
		page, err := os.ReadFile(*render)
		if err != nil {
			return err
		}
		parsed, err := extract.Parse(page, id, version)
		if err != nil {
			return err
		}
		p.Render = census.FromRender(parsed)
	}
	res, err := analysis.Analyze(b, p, nil)
	if err != nil {
		return err
	}
	if *write {
		if err := writeAnalysis(corpusRoot(), id, version, res); err != nil {
			return err
		}
	}
	for _, r := range res.Census {
		if *asJSON {
			out, err := r.JSON()
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			continue
		}
		fmt.Printf("paper       %sv%d\n", r.Paper, r.Version)
		fmt.Printf("main        %s\n", strings.Join(r.Main, " "))
		fmt.Printf("occurrences env %d, proof %d, head %d, restate %d, label %d, textref %d\n", r.Occurrences["env"], r.Occurrences["proof"], r.Occurrences["head"], r.Occurrences["restate"], r.Occurrences["label"], r.Occurrences["textref"])
		fmt.Printf("matched     env %d, proof %d, head %d, restate %d\n", r.Matched["env"], r.Matched["proof"], r.Matched["head"], r.Matched["restate"])
		fmt.Printf("numbers     aux %d, render %d, counter %d, head %d, agree %d, disagree %d, contiguity gaps %d\n", r.Numbers.FromAux, r.Numbers.FromRender, r.Numbers.FromCounter, r.Numbers.FromHead, r.Numbers.Agree, r.Numbers.Disagree, r.Numbers.ContiguityGaps)
		fmt.Printf("textrefs    resolved %d, unresolved %d\n", r.Textrefs.Resolved, r.Textrefs.Unresolved)
		if r.Render != nil {
			fmt.Printf("render      theorems %d, proofs %d, agree %v\n", r.Render.Theorems, r.Render.Proofs, r.Render.Agree)
		}
		fmt.Printf("gaps        %d\n", len(r.Gaps))
		for _, g := range r.Gaps {
			fmt.Printf("  gap       %s %s %s %s:%d\n", g.Occurrence, g.Result, g.Pattern, g.File, g.Line)
		}
		for _, ph := range r.Phantoms {
			fmt.Printf("  phantom   %s\n", ph)
		}
		for _, f := range r.Findings {
			fmt.Printf("  finding   %s %s %s\n", f.Rule, f.Occurrence, f.Message)
		}
		fmt.Printf("strength    %s\n", r.Strength)
		if *items {
			for _, o := range r.Items {
				fmt.Printf("  item      %-28s %-12s %-22s %-8s %-7s %s:%d-%d %s\n", o.ID, o.Env, o.Item, o.Number, o.NumberFrom, o.Pos.File, o.Pos.Line, o.Last, o.Result)
			}
		}
	}
	if *items && !*asJSON {
		for _, o := range res.Objects {
			fmt.Printf("  object    %-14s %-10s %-24s %-6s proves=%s attach=%d status=%s origin=%s labels=%v flags=%v\n", o.Local, o.Kind, o.Item, o.Number, o.Proves, o.Attach, o.ProofStatus, o.Origin, o.Labels, o.Flags)
		}
	}
	return nil
}

// writeAnalysis writes the census record of the version and its object
// records. The object file of the paper keeps the records of the other
// versions.
func writeAnalysis(root, paper string, version int, res *analysis.Result) error {
	id, err := axid.Parse(paper)
	if err != nil {
		return err
	}
	if len(res.Census) > 0 {
		out, err := res.Census[0].JSON()
		if err != nil {
			return err
		}
		if err := writeFile(corpus.CensusPath(root, id, version), append(out, '\n')); err != nil {
			return err
		}
	}
	file := corpus.AnalysisObjectsPath(root, id)
	var buf bytes.Buffer
	if old, err := os.ReadFile(file); err == nil {
		v := fmt.Sprint(version)
		for line := range bytes.SplitSeq(old, []byte("\n")) {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var r struct {
				Version string `json:"version"`
			}
			if json.Unmarshal(line, &r) == nil && r.Version == v {
				continue
			}
			buf.Write(line)
			buf.WriteByte('\n')
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, r := range res.Objects {
		line, err := json.Marshal(r)
		if err != nil {
			return err
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return writeFile(file, buf.Bytes())
}

func writeFile(file string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return os.WriteFile(file, b, 0o644)
}

// splitVersion reads 2312.00752v3 into the id and the version.
func splitVersion(s string) (string, int, error) {
	i := strings.LastIndex(s, "v")
	if i <= 0 {
		return "", 0, fmt.Errorf("%q has no version, write it as <id>v<n>", s)
	}
	var v int
	if _, err := fmt.Sscan(s[i+1:], &v); err != nil || v < 1 {
		return "", 0, fmt.Errorf("%q has no version, write it as <id>v<n>", s)
	}
	return s[:i], v, nil
}

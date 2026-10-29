package audit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/tamnd/arxiv-reader/analysis"
	"github.com/tamnd/arxiv-reader/census"
	"github.com/tamnd/arxiv-reader/edges"
	"github.com/tamnd/arxiv-reader/mdrec"
	"github.com/tamnd/arxiv-reader/objects"
	"github.com/tamnd/arxiv-reader/theorems"
)

// GroupAnalysis is D, the group that reads the analysis plane: the census
// records and the object records of the source analysis.
const GroupAnalysis Group = "D"

// AnalysisRules are the rules of group D that read analysis/census,
// analysis/objects and analysis/stopped. The rules that read the mentions or
// the nodes (D05, D11, D12, D14, D16, D17, D18) are not here. They arrive
// with the files they read. D10 and D19 read analysis/edges.
var AnalysisRules = []Rule{
	{
		ID: "D01", Group: GroupAnalysis, Hard: true,
		Says: "each paper in scope (AX1.2) has a paper state: a stage of the ladder of 2166-04, or a reason why it stops",
		Why:  "A paper in scope with a metadata record is at least selected. This rule checks the states that the analysis plane holds: each stop record names a reason from the closed list of AX1.6, and no version is both extracted and stopped.",
	},
	{
		ID: "D02", Group: GroupAnalysis, Hard: true,
		Says: "each extracted version has a census record, and the census record names the extractor version",
		Why:  "The census is the denominator. A version with objects and no census is a version where nobody can say what is missing.",
	},
	{
		ID: "D03", Group: GroupAnalysis, Hard: true,
		Says: "each counted occurrence of a theorem-like environment, proof or manual head in the census has an object record, or a waiver with a reason",
		Why:  "This is the rule that makes the corpus complete. A phantom, an object that no occurrence accounts for, is a finding of this rule too.",
	},
	{
		ID: "D04", Group: GroupAnalysis, Hard: true,
		Says: "each statement object has a proof_status, and a proof_status of here has a proves edge from a proof object",
	},
	{
		ID: "D06", Group: GroupAnalysis, Hard: true,
		Says: "no object record from a version of class record holds text of the paper, in any field",
		Why:  "A record version gives structure only. The check is mechanical: the body, the math and the name in the head must be empty.",
	},
	{
		ID: "D07", Group: GroupAnalysis,
		Says: "a counter whose numbers have a gap that the census does not explain",
	},
	{
		ID: "D08", Group: GroupAnalysis,
		Says: "a paper whose source and render counts differ by more than the threshold of AX4.5",
	},
	{
		ID: "D09", Group: GroupAnalysis,
		Says: "an in-paper reference to a theorem-like number or label that resolves to no object",
	},
	{
		ID: "D10", Group: GroupAnalysis, Hard: true,
		Says: "each edge has a method and a confidence from the closed lists of AX6.1, and no edge whose method is model has the confidence certain or high",
		Why:  "A model cannot make an edge certain. Only a person can raise a model edge, and the raise is in the edge history.",
	},
	{
		ID: "D13", Group: GroupAnalysis, Hard: true,
		Says: "each object record names its path, its extractor version and its confidence",
	},
	{
		ID: "D15", Group: GroupAnalysis,
		Says: "an environment name or title that manifests/environments.yaml does not map",
	},
	{
		ID: "D19", Group: GroupAnalysis,
		Says: "a cycle in the in-paper uses graph (AX6.3)",
		Why:  "The in-paper uses graph must be acyclic. In most cases a refers-to was read as uses, and the fix is in the cue lists.",
	},
}

// Analysis is the audit of the analysis plane.
type Analysis struct {
	Root string
	Cap  int
	Only []string
	// Shard keeps one month when it is not empty.
	Shard string
	// Text tells if a version may hold text of the paper. known is false when
	// the licence of the version is not known, and then D06 counts the version
	// as a record version, because that is the safe side.
	Text func(paper string, version int) (text, known bool)
	Log  func(paper string)
}

// Run reads each census record and each objects file under the root.
func (a Analysis) Run() (Report, error) {
	report := Report{Plane: "analysis", Unit: "record", Scope: "paper"}
	col := &collector{cap: a.Cap, only: a.Only}
	censusFiles, err := a.glob("census", "*.json")
	if err != nil {
		return report, err
	}
	objectFiles, err := a.glob("objects", "*"+mdrec.Ext)
	if err != nil {
		return report, err
	}
	have := map[string]bool{}
	for _, f := range censusFiles {
		rec, err := readCensus(f)
		if err != nil {
			return report, err
		}
		have[fmt.Sprintf("%sv%d", rec.Paper, rec.Version)] = true
		a.census(col, a.rel(f), rec)
		report.Records++
	}
	for _, f := range objectFiles {
		recs, err := readObjects(f)
		if err != nil {
			return report, err
		}
		a.objects(col, a.rel(f), recs, have)
		report.Records += len(recs)
		report.Shards++
		if a.Log != nil && len(recs) > 0 {
			a.Log(recs[0].Paper)
		}
	}
	edgeFiles, err := a.glob("edges", "*"+mdrec.Ext)
	if err != nil {
		return report, err
	}
	for _, f := range edgeFiles {
		es, err := edges.ReadFile(f)
		if err != nil {
			return report, err
		}
		a.edges(col, a.rel(f), es)
		report.Records += len(es)
	}
	stops, err := a.glob("stopped", "*.json")
	if err != nil {
		return report, err
	}
	for _, f := range stops {
		s, err := analysis.ReadStop(f)
		if err != nil {
			return report, err
		}
		report.Records++
		a.stop(col, a.rel(f), s, have)
	}
	report.Results = col.results(AnalysisRules)
	return report, nil
}

func (a Analysis) stop(col *collector, file string, s analysis.Stop, have map[string]bool) {
	ref := fmt.Sprintf("%sv%d", s.Paper, s.Version)
	add := func(what string) {
		col.add(Finding{Rule: "D01", File: file, Shard: shardOf(file), ID: ref, What: what})
	}
	col.checked("D01")
	if s.State != analysis.Stopped {
		add(fmt.Sprintf("the stop record has the state %q, want stopped", s.State))
	}
	if !analysis.KnownReason(s.Reason) {
		add(fmt.Sprintf("the reason %q is not in the closed list of AX1.6", s.Reason))
	}
	if have[ref] {
		add("the version has a census record and a stop record, so it is extracted and stopped")
	}
	if s.By == "" {
		add("the stop record names no extractor version")
	}
}

func (a Analysis) glob(plane, pattern string) ([]string, error) {
	shard := "*"
	if a.Shard != "" {
		shard = a.Shard
	}
	files, err := mdrec.Glob(filepath.Join(a.Root, "analysis", plane, shard, pattern))
	sort.Strings(files)
	return files, err
}

func (a Analysis) rel(f string) string {
	if r, err := filepath.Rel(a.Root, f); err == nil {
		return filepath.ToSlash(r)
	}
	return f
}

func shardOf(file string) string {
	return path.Base(path.Dir(file))
}

func (a Analysis) census(col *collector, file string, r *census.Record) {
	paper := fmt.Sprintf("%sv%d", r.Paper, r.Version)
	add := func(rule, what string) {
		col.add(Finding{Rule: rule, File: file, Shard: shardOf(file), ID: paper, What: what})
	}
	col.checked("D02")
	if r.Extractor == "" {
		add("D02", "the census record names no extractor version")
	}
	col.checked("D03")
	for _, g := range r.Gaps {
		add("D03", fmt.Sprintf("occurrence %s is %s (%s) at %s:%d", g.Occurrence, g.Result, g.Pattern, g.File, g.Line))
	}
	for _, p := range r.Phantoms {
		add("D03", fmt.Sprintf("object %s accounts for no occurrence", p))
	}
	for _, w := range r.Waivers {
		switch {
		case !analysis.KnownWaiverReason(w.Reason):
			add("D03", fmt.Sprintf("occurrence %s is waived with the reason %q, which is not in the closed list", w.Occurrence, w.Reason))
		case strings.TrimSpace(w.By) == "":
			add("D03", fmt.Sprintf("occurrence %s is waived, and the waiver names nobody", w.Occurrence))
		}
	}
	for _, rule := range []string{"D07", "D08", "D09"} {
		col.checked(rule)
	}
	for _, f := range r.Findings {
		switch f.Rule {
		case "D07", "D08", "D09":
			what := f.Message
			if f.Occurrence != "" {
				what = f.Occurrence + ": " + what
			}
			add(f.Rule, what)
		}
	}
	if r.Render != nil && !r.Render.Agree && !r.Render.Truncated && !hasRule(r.Findings, "D08") {
		msg := fmt.Sprintf("the render has %d theorems and %d proofs, and the source counts do not agree", r.Render.Theorems, r.Render.Proofs)
		if r.Render.SourceTheorems+r.Render.SourceProofs > 0 {
			// A record from before v0.5.11 has no source counts.
			msg = fmt.Sprintf("the render has %d theorems and %d proofs, and the source has %d and %d", r.Render.Theorems, r.Render.Proofs, r.Render.SourceTheorems, r.Render.SourceProofs)
		}
		add("D08", msg)
	}
	col.checked("D15")
	for _, d := range r.Declarations {
		if d.TitleArg {
			// Each use gives the title. The items below are checked instead.
			continue
		}
		if d.Item == "" || d.Item == theorems.Unknown {
			add("D15", fmt.Sprintf("environment %s with the title %q has no item kind", d.Env, d.Title))
		}
	}
	for _, o := range r.Items {
		if o.Type == census.Env && o.Item == theorems.Unknown {
			add("D15", fmt.Sprintf("occurrence %s of %s with the title %q has no item kind", o.ID, o.Env, o.Title))
		}
	}
}

func hasRule(fs []census.Finding, rule string) bool {
	for _, f := range fs {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

func (a Analysis) objects(col *collector, file string, recs []objects.Record, have map[string]bool) {
	proved := map[string]bool{}
	versions := map[string]bool{}
	for _, r := range recs {
		if r.Proves != "" {
			proved[r.Version+"/"+r.Proves] = true
		}
		if r.Path == "source" {
			versions[r.Paper+"v"+r.Version] = true
		}
	}
	add := func(rule, id, what string) {
		col.add(Finding{Rule: rule, File: file, Shard: shardOf(file), ID: id, What: what})
	}
	for v := range versions {
		col.checked("D02")
		if !have[v] {
			add("D02", v, "the version has object records and no census record")
		}
	}
	for _, r := range recs {
		id := r.Paper + "v" + r.Version + "#" + r.Local
		col.checked("D13")
		var none []string
		if r.Path == "" {
			none = append(none, "path")
		}
		if r.Extractor == "" && r.Path == "source" {
			none = append(none, "extractor")
		}
		if r.Confidence == "" {
			none = append(none, "confidence")
		}
		if len(none) > 0 {
			add("D13", id, "the record names no "+strings.Join(none, ", no "))
		}
		if r.Path != "source" {
			continue
		}
		if strings.HasPrefix(r.Item, "statement") || r.Kind == "statement" {
			col.checked("D04")
			switch {
			case r.ProofStatus == "":
				add("D04", id, "the statement has no proof_status")
			case r.ProofStatus == "here" && !proved[r.Version+"/"+r.Local] && !provedByRestatement(recs, r):
				add("D04", id, "the proof_status is here and no proof object proves it")
			}
		}
		col.checked("D06")
		text, known := true, true
		if a.Text != nil {
			text, known = a.Text(r.Paper, atoi(r.Version))
		}
		if text && known {
			continue
		}
		var held []string
		if r.BodyMD != "" {
			held = append(held, "body_md")
		}
		if len(r.Math) > 0 {
			held = append(held, "math")
		}
		if r.Name != "" {
			held = append(held, "name")
		}
		if len(held) > 0 {
			add("D06", id, "a version of class record holds text in "+strings.Join(held, ", "))
		}
	}
}

// provedByRestatement tells if a proof proves a restatement of the record,
// which proves the first print too (AX3.6).
func provedByRestatement(recs []objects.Record, first objects.Record) bool {
	for _, r := range recs {
		if r.Restates != first.Local || r.Version != first.Version {
			continue
		}
		for _, p := range recs {
			if p.Proves == r.Local && p.Version == r.Version {
				return true
			}
		}
	}
	return false
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func (a Analysis) edges(col *collector, file string, es []edges.Edge) {
	add := func(rule, id, what string) {
		col.add(Finding{Rule: rule, File: file, Shard: shardOf(file), ID: id, What: what})
	}
	cycles := map[int]int{}
	paper := ""
	for _, e := range es {
		paper, _, _ = strings.Cut(e.From, "#")
		col.checked("D10")
		id := fmt.Sprintf("%s %s %s", e.From, e.Predicate, e.To)
		switch {
		case !slices.Contains(edges.Methods, e.Method):
			add("D10", id, fmt.Sprintf("the method %q is not in the closed list", e.Method))
		case !slices.Contains(edges.Confidences, e.Confidence):
			add("D10", id, fmt.Sprintf("the confidence %q is not in the closed list", e.Confidence))
		case e.Method == edges.Model && (e.Confidence == edges.Certain || e.Confidence == edges.High):
			add("D10", id, "a model edge has the confidence "+e.Confidence)
		}
		if slices.Contains(e.Flags, edges.FlagCycle) {
			cycles[e.Evidence.Version]++
		}
	}
	col.checked("D19")
	for v, n := range cycles {
		add("D19", fmt.Sprintf("%sv%d", paper, v), fmt.Sprintf("%d uses edges are in a cycle", n))
	}
}

func readCensus(file string) (*census.Record, error) {
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

func readObjects(file string) ([]objects.Record, error) {
	b, err := mdrec.ReadLines(file)
	if err != nil {
		return nil, err
	}
	var out []objects.Record
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var r objects.Record
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", file, n, err)
		}
		out = append(out, r)
	}
	return out, sc.Err()
}

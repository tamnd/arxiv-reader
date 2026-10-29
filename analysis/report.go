package analysis

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tamnd/arxiv-reader/census"
	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/objects"
	"github.com/tamnd/arxiv-reader/theorems"
	"github.com/tamnd/arxiv-reader/witness"
)

// Measures are the counts of AX11.4 over a set of census records.
type Measures struct {
	Papers int
	// Occurrences is the counted environments, proofs, manual heads and
	// restatements. Gaps and Waivers are the gaps and the waivers in them.
	Occurrences, Gaps, Waivers int
	// WithGap and WithWaiver are the papers with at least one.
	WithGap, WithWaiver int
	// D07, D08 and D09 are the papers with at least one finding of the rule.
	D07, D08, D09 int
	Strength      map[string]int
	// Statements and NoProof are the statement objects and the ones with the
	// proof_status none. Proofs and Orphans are the proof objects and the ones
	// that prove nothing. Items and Unknown are the census items and the ones
	// with the kind unknown.
	Statements, NoProof int
	Proofs, Orphans     int
	Items, Unknown      int
}

func (m *Measures) add(r *census.Record, objs []objects.Record) {
	m.Papers++
	if m.Strength == nil {
		m.Strength = map[string]int{}
	}
	m.Strength[r.Strength]++
	for _, k := range []string{"env", "proof", "head", "restate"} {
		m.Occurrences += r.Occurrences[k]
	}
	gaps := len(r.Gaps) + len(r.Phantoms)
	m.Gaps += gaps
	m.Waivers += len(r.Waivers)
	if gaps > 0 {
		m.WithGap++
	}
	if len(r.Waivers) > 0 {
		m.WithWaiver++
	}
	seen := map[string]bool{}
	for _, f := range r.Findings {
		seen[f.Rule] = true
	}
	if r.Render != nil && !r.Render.Agree {
		seen["D08"] = true
	}
	if seen["D07"] {
		m.D07++
	}
	if seen["D08"] {
		m.D08++
	}
	if seen["D09"] {
		m.D09++
	}
	for _, o := range r.Items {
		m.Items++
		if o.Item == theorems.Unknown {
			m.Unknown++
		}
	}
	for _, o := range objs {
		switch o.Kind {
		case "statement":
			m.Statements++
			if o.ProofStatus == "none" {
				m.NoProof++
			}
		case "proof":
			m.Proofs++
			if o.Proves == "" {
				m.Orphans++
			}
		}
	}
}

// Row is the measures of one key: an extractor version, a month or a code.
type Row struct {
	Key string
	Measures
}

// Pattern is one gap pattern of AX4.9, with the papers that have it.
type Pattern struct {
	Pattern string
	Gaps    int
	Papers  []string
}

// Report is reports/census.md (AX4.7, AX11.4).
type Report struct {
	Generated time.Time
	// Since is the first month that the report reads, as YYYY-MM, or empty.
	Since string
	All   Measures
	// Stopped is the count of the stop records by reason.
	Stopped map[string]int
	// Scope is the open and share-alike papers in scope, when it is known.
	// Extracted and Halted are the ones that have a census record and a stop
	// record for the latest version.
	Scope, Extracted, Halted int
	// Closed is the papers in scope that are not open or share-alike, and
	// ClosedOutside the ones with a structure from TheoremGraph (way R1).
	// ClosedExtracted and ClosedHalted are the ones with a census record and a
	// stop record for the latest version: way R2 reads them (AX2.5, AQ01).
	Closed, ClosedOutside, ClosedExtracted, ClosedHalted int
	// Areas is the papers in scope of each MSC code at two digits, when the
	// scope is known.
	Areas map[string]*AreaScope

	// Size is the files and the bytes of each part of the analysis plane, for
	// AQ10.
	Size []PlaneSize
	// Witness is the comparison with each outside index (AX4.5, AQ21).
	Witness map[string]*WitnessRow

	versions, months, codes map[string]*Row
	patterns                map[string]*Pattern
}

// AreaScope is the papers in scope of one MSC code at two digits: the open
// and share-alike papers, and the verbatim and record papers.
type AreaScope struct {
	Scope, Extracted, Halted              int
	Closed, ClosedExtracted, ClosedHalted int
}

func (rp *Report) area(code string) *AreaScope {
	if rp.Areas == nil {
		rp.Areas = map[string]*AreaScope{}
	}
	a := rp.Areas[code]
	if a == nil {
		a = &AreaScope{}
		rp.Areas[code] = a
	}
	return a
}

// AddClosed counts one verbatim or record paper in scope, with its MSC code at
// two digits, whether TheoremGraph has its structure (way R1), and whether
// its latest version has a census record or a stop record.
func (rp *Report) AddClosed(code string, outside, extracted, stopped bool) {
	a := rp.area(code)
	rp.Closed++
	a.Closed++
	if outside {
		rp.ClosedOutside++
	}
	switch {
	case extracted:
		rp.ClosedExtracted++
		a.ClosedExtracted++
	case stopped:
		rp.ClosedHalted++
		a.ClosedHalted++
	}
}

// AddScope counts one open or share-alike paper in scope, with its MSC code at
// two digits and whether its latest version has a census record or a stop
// record.
func (rp *Report) AddScope(code string, extracted, stopped bool) {
	a := rp.area(code)
	rp.Scope++
	a.Scope++
	switch {
	case extracted:
		rp.Extracted++
		a.Extracted++
	case stopped:
		rp.Halted++
		a.Halted++
	}
}

// PlaneSize is the size of one part of the analysis plane.
type PlaneSize struct {
	Part         string
	Files, Bytes int64
}

// WitnessRow compares the census with one outside index.
type WitnessRow struct {
	Index string
	// Both is the versions with a census record that the index has. Other is
	// the ones where the index read another version, and they are not
	// compared.
	Both, Other int
	// Same, More and Fewer count the versions where the index has the same
	// count of results as the census has items, more, or fewer.
	Same, More, Fewer int
	// Excess is the versions where the index has more than one result more
	// than the census has items. They go to the queue census-diff.
	Excess []Diff
}

// Diff is one version where an index has more results than the census.
type Diff struct {
	Paper         string
	Census, Index int
}

// CensusItems is the items that the census counts for a version: the
// theorem-like environments and the manual heads.
func CensusItems(r *census.Record) int { return r.Occurrences[census.Env] + r.Occurrences[census.Head] }

// AddWitness compares one census record with what an index gives for it.
func (rp *Report) AddWitness(shard, index string, r *census.Record, w witness.Record) {
	if rp.Since != "" && MonthOf(shard) < rp.Since {
		return
	}
	row := rp.Witness[index]
	if row == nil {
		row = &WitnessRow{Index: index}
		rp.Witness[index] = row
	}
	if w.Version != 0 && w.Version != r.Version {
		row.Other++
		return
	}
	row.Both++
	n := CensusItems(r)
	switch {
	case w.Count == n:
		row.Same++
	case w.Count > n:
		row.More++
		if w.Count-n > 1 {
			row.Excess = append(row.Excess, Diff{Paper: fmt.Sprintf("%sv%d", r.Paper, r.Version), Census: n, Index: w.Count})
		}
	default:
		row.Fewer++
	}
}

// NewReport returns an empty report.
func NewReport(since string) *Report {
	return &Report{Since: since, Stopped: map[string]int{}, Witness: map[string]*WitnessRow{}, versions: map[string]*Row{}, months: map[string]*Row{}, codes: map[string]*Row{}, patterns: map[string]*Pattern{}}
}

// MonthOf is the month that a shard names, as YYYY-MM. A shard is YYMM, and
// the years from 91 are 1991 to 1999.
func MonthOf(shard string) string {
	if len(shard) != 4 {
		return shard
	}
	yy, err := strconv.Atoi(shard[:2])
	if err != nil {
		return shard
	}
	year := 2000 + yy
	if yy >= 91 {
		year = 1900 + yy
	}
	return fmt.Sprintf("%d-%s", year, shard[2:])
}

// Add counts one census record, with the object records of the same version
// and the MSC code at two digits of the paper.
func (rp *Report) Add(shard, code string, r *census.Record, objs []objects.Record) {
	month := MonthOf(shard)
	if rp.Since != "" && month < rp.Since {
		return
	}
	rp.All.add(r, objs)
	row := func(m map[string]*Row, key string) {
		x := m[key]
		if x == nil {
			x = &Row{Key: key}
			m[key] = x
		}
		x.add(r, objs)
	}
	row(rp.versions, r.Extractor)
	row(rp.months, month)
	row(rp.codes, code)
	paper := fmt.Sprintf("%sv%d", r.Paper, r.Version)
	for _, g := range r.Gaps {
		key := g.Pattern
		if key == "" {
			key = g.Result
		}
		p := rp.patterns[key]
		if p == nil {
			p = &Pattern{Pattern: key}
			rp.patterns[key] = p
		}
		p.Gaps++
		if len(p.Papers) == 0 || p.Papers[len(p.Papers)-1] != paper {
			p.Papers = append(p.Papers, paper)
		}
	}
}

// AddStop counts one stop record.
func (rp *Report) AddStop(s Stop) { rp.Stopped[s.Reason]++ }

func sortedRows(m map[string]*Row) []Row {
	out := make([]Row, 0, len(m))
	for _, r := range m {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Patterns is the gap patterns, the most papers first.
func (rp *Report) Patterns() []Pattern {
	out := make([]Pattern, 0, len(rp.patterns))
	for _, p := range rp.patterns {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Papers) != len(out[j].Papers) {
			return len(out[i].Papers) > len(out[j].Papers)
		}
		return out[i].Pattern < out[j].Pattern
	})
	return out
}

func share(n, of int) string { return prose.Percent(prose.Share(n, of)) }

// Markdown is the report as it is committed.
func (rp *Report) Markdown() string {
	var b strings.Builder
	a := rp.All
	b.WriteString("# The census of the analysis plane\n\n")
	halted := 0
	for _, n := range rp.Stopped {
		halted += n
	}
	fmt.Fprintf(&b, "%s have a census record, and %s are stopped. %s of the papers with a census have a gap, and %s are strong or normal.\n",
		prose.Count(a.Papers, "version"), prose.Thousands(halted), share(a.WithGap, a.Papers), share(a.Strength["strong"]+a.Strength["normal"], a.Papers))
	if rp.Since != "" {
		fmt.Fprintf(&b, "The report reads the papers from %s.\n", rp.Since)
	}
	fmt.Fprintf(&b, "Counted on %s from analysis/census, analysis/objects and analysis/stopped.\n\n", rp.Generated.UTC().Format("2 January 2006"))

	if rp.Scope > 0 {
		b.WriteString("## The open papers in scope\n\n")
		b.WriteString("AX-M3 is done when each open and share-alike paper in scope has the state extracted or stopped (rule D01).\n\n")
		b.WriteString("| State | Papers | Share |\n| --- | ---: | ---: |\n")
		left := rp.Scope - rp.Extracted - rp.Halted
		fmt.Fprintf(&b, "| extracted | %s | %s |\n", prose.Thousands(rp.Extracted), share(rp.Extracted, rp.Scope))
		fmt.Fprintf(&b, "| stopped | %s | %s |\n", prose.Thousands(rp.Halted), share(rp.Halted, rp.Scope))
		fmt.Fprintf(&b, "| not yet | %s | %s |\n", prose.Thousands(left), share(left, rp.Scope))
		fmt.Fprintf(&b, "| all | %s | 100.0%% |\n\n", prose.Thousands(rp.Scope))
	}
	if rp.Closed > 0 {
		b.WriteString("## The verbatim and record papers in scope\n\n")
		fmt.Fprintf(&b, "AQ01 allows way R2 of AX2.5: the e-print of a verbatim or record paper is read in memory, and the analysis plane keeps only its structure and fingerprints, with no text of the paper (rule D06). AX-M8 is done when each of them has the state extracted or stopped. TheoremGraph also gives the structure of %s of them (%s), which is way R1.\n\n",
			prose.Thousands(rp.ClosedOutside), share(rp.ClosedOutside, rp.Closed))
		b.WriteString("| State | Papers | Share |\n| --- | ---: | ---: |\n")
		left := rp.Closed - rp.ClosedExtracted - rp.ClosedHalted
		fmt.Fprintf(&b, "| extracted | %s | %s |\n", prose.Thousands(rp.ClosedExtracted), share(rp.ClosedExtracted, rp.Closed))
		fmt.Fprintf(&b, "| stopped | %s | %s |\n", prose.Thousands(rp.ClosedHalted), share(rp.ClosedHalted, rp.Closed))
		fmt.Fprintf(&b, "| not yet | %s | %s |\n", prose.Thousands(left), share(left, rp.Closed))
		fmt.Fprintf(&b, "| all | %s | 100.0%% |\n\n", prose.Thousands(rp.Closed))
	}
	if len(rp.Areas) > 0 {
		b.WriteString("## The papers in scope of each area\n\n")
		b.WriteString("Each paper in scope of each MSC code at two digits, of each licence class. The research layer is complete when the papers with no state and the census gaps are zero for each code, and the open rows of math2 are zero too (AX1.5).\n\n")
		b.WriteString("| Code | Papers | Extracted | Stopped | No state | Census gaps |\n| --- | ---: | ---: | ---: | ---: | ---: |\n")
		codes := make([]string, 0, len(rp.Areas))
		for k := range rp.Areas {
			codes = append(codes, k)
		}
		sort.Strings(codes)
		for _, k := range codes {
			x := rp.Areas[k]
			all, done, halted := x.Scope+x.Closed, x.Extracted+x.ClosedExtracted, x.Halted+x.ClosedHalted
			gaps := 0
			if r := rp.codes[k]; r != nil {
				gaps = r.Gaps
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n", k, prose.Thousands(all), prose.Thousands(done), prose.Thousands(halted), prose.Thousands(all-done-halted), prose.Thousands(gaps))
		}
		b.WriteString("\n")
	}

	b.WriteString("## The measures of each extractor version\n\n")
	b.WriteString("These are the measures of AX11.4. A new extractor version may not raise any of them by more than 0.5 points on the papers that both versions read.\n\n")
	b.WriteString("| Extractor | Papers | Gap | Waiver | D07 | D08 | D09 | proof_status none | Orphan proofs | Kind unknown |\n")
	b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, r := range sortedRows(rp.versions) {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", r.Key, prose.Thousands(r.Papers),
			share(r.WithGap, r.Papers), share(r.WithWaiver, r.Papers), share(r.D07, r.Papers), share(r.D08, r.Papers), share(r.D09, r.Papers),
			share(r.NoProof, r.Statements), share(r.Orphans, r.Proofs), share(r.Unknown, r.Items))
	}

	b.WriteString("\n## The strength\n\n")
	b.WriteString("| Strength | Papers | Share |\n| --- | ---: | ---: |\n")
	for _, s := range []string{"strong", "normal", "weak", "none"} {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", s, prose.Thousands(a.Strength[s]), share(a.Strength[s], a.Papers))
	}

	b.WriteString("\n## The stopped versions\n\n")
	b.WriteString("| Reason | Versions |\n| --- | ---: |\n")
	for _, r := range StopReasons {
		fmt.Fprintf(&b, "| %s | %s |\n", r, prose.Thousands(rp.Stopped[r]))
	}

	table := func(title, key string, rows []Row) {
		fmt.Fprintf(&b, "\n## %s\n\n", title)
		fmt.Fprintf(&b, "| %s | Papers | Occurrences | Gaps | Waivers | D07 | D08 | D09 | Strong or normal |\n", key)
		b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
		for _, r := range rows {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", r.Key, prose.Thousands(r.Papers), prose.Thousands(r.Occurrences),
				prose.Thousands(r.Gaps), prose.Thousands(r.Waivers), prose.Thousands(r.D07), prose.Thousands(r.D08), prose.Thousands(r.D09),
				share(r.Strength["strong"]+r.Strength["normal"], r.Papers))
		}
	}
	if len(rp.Witness) > 0 {
		b.WriteString("\n## The outside witnesses\n\n")
		b.WriteString("TheoremSearch and TheoremGraph are a third count (AX4.5). They are not ground truth, because they split and merge items in their own ways, and TheoremSearch has no definitions. A version where an index has more than one result more than the census has items goes to the queue census-diff. The comparison answers AQ21.\n\n")
		b.WriteString("| Index | Versions in both | Same count | Index has more | Index has fewer | Excess over 1 | Other version |\n| --- | ---: | ---: | ---: | ---: | ---: | ---: |\n")
		var names []string
		for k := range rp.Witness {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			w := rp.Witness[k]
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s |\n", k, prose.Thousands(w.Both), share(w.Same, w.Both), share(w.More, w.Both), share(w.Fewer, w.Both), prose.Thousands(len(w.Excess)), prose.Thousands(w.Other))
		}
		for _, k := range names {
			w := rp.Witness[k]
			if len(w.Excess) == 0 {
				continue
			}
			ex := append([]Diff(nil), w.Excess...)
			sort.Slice(ex, func(i, j int) bool {
				di, dj := ex[i].Index-ex[i].Census, ex[j].Index-ex[j].Census
				if di != dj {
					return di > dj
				}
				return ex[i].Paper < ex[j].Paper
			})
			if len(ex) > 50 {
				ex = ex[:50]
			}
			fmt.Fprintf(&b, "\n### census-diff from %s\n\n%s with the largest excess.\n\n| Version | Census items | %s results |\n| --- | ---: | ---: |\n", k, prose.Count(len(ex), "version"), k)
			for _, d := range ex {
				fmt.Fprintf(&b, "| %s | %s | %s |\n", d.Paper, prose.Thousands(d.Census), prose.Thousands(d.Index))
			}
		}
	}

	if len(rp.Size) > 0 {
		b.WriteString("\n## The size of the analysis plane\n\n")
		b.WriteString("This is the measure of AQ10. The estimate for the open papers in scope scales the census and the objects by the versions with a census.\n\n")
		b.WriteString("| Part | Files | Bytes | Estimate for the open papers in scope |\n| --- | ---: | ---: | ---: |\n")
		for _, z := range rp.Size {
			est := "-"
			if (z.Part == "census" || z.Part == "objects") && a.Papers > 0 && rp.Scope > 0 {
				est = prose.Bytes(z.Bytes * int64(rp.Scope) / int64(a.Papers))
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", z.Part, prose.Thousands(int(z.Files)), prose.Bytes(z.Bytes), est)
		}
	}

	table("By month", "Month", sortedRows(rp.months))
	table("By code", "Code", sortedRows(rp.codes))

	b.WriteString("\n## The gap patterns\n\n")
	pats := rp.Patterns()
	if len(pats) == 0 {
		b.WriteString("No gap is open.\n")
		return b.String()
	}
	b.WriteString("Each pattern gets an issue with three example papers, and the fix gets a test (AX4.9).\n\n")
	b.WriteString("| Pattern | Papers | Gaps | Examples |\n| --- | ---: | ---: | --- |\n")
	for _, p := range pats {
		ex := p.Papers
		if len(ex) > 3 {
			ex = ex[:3]
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", strings.ReplaceAll(p.Pattern, "|", "\\|"), prose.Thousands(len(p.Papers)), prose.Thousands(p.Gaps), strings.Join(ex, ", "))
	}
	return b.String()
}

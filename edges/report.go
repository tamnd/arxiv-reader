package edges

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/mdrec"
	"github.com/tamnd/arxiv-reader/objects"
)

// MonthChange is the change of a share from one month to the next that is a
// finding (AX6.7), in points.
const MonthChange = 5.0

// minPapers is how many papers a month needs before its change is a finding.
// A month of few papers moves by more than five points by chance.
const minPapers = 20

// Measures are the graph checks of AX6.7 over a set of paper versions.
type Measures struct {
	Papers, Edges int
	// Statements and Used are the statement objects and the ones with at
	// least one incoming uses or proves edge.
	Statements, Used int
	// Proofs and Bare are the proof objects and the ones with no outgoing
	// uses edge.
	Proofs, Bare int
	// WithCycle is the papers with a cycle in the in-paper uses graph.
	WithCycle int
	// Mismatch is the cross-paper uses edges to an item of a kind that is
	// not a statement or a definition. It stays zero until AX6.5 gives the
	// cross-paper edges.
	Mismatch    int
	Methods     map[string]int
	Confidences map[string]int
	Predicates  map[string]int
}

func (m *Measures) add(objs []objects.Record, es []Edge) {
	if m.Methods == nil {
		m.Methods, m.Confidences, m.Predicates = map[string]int{}, map[string]int{}, map[string]int{}
	}
	m.Papers++
	m.Edges += len(es)
	in := map[string]bool{}
	out := map[string]bool{}
	cyc := map[string]bool{}
	for _, e := range es {
		m.Methods[e.Method]++
		m.Confidences[e.Confidence]++
		m.Predicates[e.Predicate]++
		if e.Predicate == Uses || e.Predicate == Proves {
			in[local(e.To)] = true
		}
		if e.Predicate == Uses {
			out[local(e.From)] = true
		}
		for _, f := range e.Flags {
			if f == FlagCycle {
				cyc[local(e.From)] = true
			}
		}
	}
	for _, o := range objs {
		switch o.Kind {
		case "statement":
			m.Statements++
			if in[o.Local] {
				m.Used++
			}
		case "proof":
			m.Proofs++
			if !out[o.Local] {
				m.Bare++
			}
		}
	}
	if len(cyc) > 0 {
		m.WithCycle++
	}
}

func local(id string) string {
	_, l, ok := strings.Cut(id, "#")
	if !ok {
		return id
	}
	return l
}

// Report is reports/graph.md.
type Report struct {
	Generated time.Time
	All       Measures
	codes     map[string]*Measures
	months    map[string]*Measures
	// monthCodes is the measures of each code in each month, for the
	// month changes.
	monthCodes map[string]map[string]*Measures
}

// NewReport is an empty report.
func NewReport() *Report {
	return &Report{codes: map[string]*Measures{}, months: map[string]*Measures{}, monthCodes: map[string]map[string]*Measures{}}
}

// Add counts one paper version, with the objects and the edges of that
// version.
func (rp *Report) Add(month, code string, objs []objects.Record, es []Edge) {
	get := func(m map[string]*Measures, k string) *Measures {
		x, ok := m[k]
		if !ok {
			x = &Measures{}
			m[k] = x
		}
		return x
	}
	rp.All.add(objs, es)
	get(rp.codes, code).add(objs, es)
	get(rp.months, month).add(objs, es)
	mc, ok := rp.monthCodes[code]
	if !ok {
		mc = map[string]*Measures{}
		rp.monthCodes[code] = mc
	}
	get(mc, month).add(objs, es)
}

// Change is a month whose share moved by more than MonthChange points from
// the month before, for one code.
type Change struct {
	Code, Measure, From, To string
	Before, After           float64
}

// Changes are the findings of AX6.7.
func (rp *Report) Changes() []Change {
	var out []Change
	for _, code := range keys(rp.monthCodes) {
		mc := rp.monthCodes[code]
		var prev string
		for _, month := range keys(mc) {
			m := mc[month]
			if m.Papers < minPapers {
				continue
			}
			if prev != "" {
				p := mc[prev]
				for _, c := range []struct {
					name       string
					a, b, x, y int
				}{
					{"statements used", p.Used, p.Statements, m.Used, m.Statements},
					{"proofs with no uses", p.Bare, p.Proofs, m.Bare, m.Proofs},
				} {
					if c.b == 0 || c.y == 0 {
						continue
					}
					before, after := 100*prose.Share(c.a, c.b), 100*prose.Share(c.x, c.y)
					if d := after - before; d > MonthChange || d < -MonthChange {
						out = append(out, Change{Code: code, Measure: c.name, From: prev, To: month, Before: before, After: after})
					}
				}
			}
			prev = month
		}
	}
	return out
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func share(n, of int) string { return prose.Percent(prose.Share(n, of)) }

// Markdown is the report as it is committed.
func (rp *Report) Markdown() string {
	var b strings.Builder
	a := rp.All
	b.WriteString("# The graph of the analysis plane\n\n")
	fmt.Fprintf(&b, "%s have edges, and the edges total %s. %s of the statements have an incoming uses or proves edge, and %s of the proofs use nothing. %s have a cycle in the in-paper uses graph.\n",
		prose.Count(a.Papers, "version"), prose.Thousands(a.Edges), share(a.Used, a.Statements), share(a.Bare, a.Proofs), prose.Count(a.WithCycle, "version"))
	fmt.Fprintf(&b, "Counted on %s from analysis/edges and analysis/objects. These are the checks of AX6.7.\n\n", rp.Generated.UTC().Format("2 January 2006"))

	b.WriteString("## The methods\n\n| Method | Edges | Share |\n| --- | ---: | ---: |\n")
	for _, k := range Methods {
		if n := a.Methods[k]; n > 0 {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", k, prose.Thousands(n), share(n, a.Edges))
		}
	}
	b.WriteString("\n## The confidences\n\n| Confidence | Edges | Share |\n| --- | ---: | ---: |\n")
	for _, k := range Confidences {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", k, prose.Thousands(a.Confidences[k]), share(a.Confidences[k], a.Edges))
	}
	b.WriteString("\n## The predicates\n\n| Predicate | Edges | Share |\n| --- | ---: | ---: |\n")
	for _, k := range []string{Uses, Proves, RefersTo, Restates, CitesResult, Cites, Generalizes, Specializes} {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", k, prose.Thousands(a.Predicates[k]), share(a.Predicates[k], a.Edges))
	}

	table := func(title, key string, m map[string]*Measures) {
		fmt.Fprintf(&b, "\n## %s\n\n", title)
		fmt.Fprintf(&b, "| %s | Papers | Edges | Statements used | Proofs with no uses | With a cycle | ref | text | cite | term | certain or high |\n", key)
		b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
		for _, k := range keys(m) {
			r := m[k]
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", k, prose.Thousands(r.Papers), prose.Thousands(r.Edges),
				share(r.Used, r.Statements), share(r.Bare, r.Proofs), prose.Thousands(r.WithCycle),
				share(r.Methods[Ref], r.Edges), share(r.Methods[Text], r.Edges), share(r.Methods[Cite], r.Edges), share(r.Methods[Term], r.Edges),
				share(r.Confidences[Certain]+r.Confidences[High], r.Edges))
		}
	}
	table("By code", "Code", rp.codes)
	table("By month", "Month", rp.months)

	b.WriteString("\n## The month changes\n\n")
	fmt.Fprintf(&b, "A share that moves by more than %.0f points from one month to the next, for one code, is a finding. It is more often an extractor change than a change in mathematics. A month with fewer than %d papers is not compared.\n\n", MonthChange, minPapers)
	ch := rp.Changes()
	if len(ch) == 0 {
		b.WriteString("No share moved by more than that.\n")
	} else {
		b.WriteString("| Code | Measure | From | To | Before | After |\n| --- | --- | --- | --- | ---: | ---: |\n")
		for _, c := range ch {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %.1f%% | %.1f%% |\n", c.Code, c.Measure, c.From, c.To, c.Before, c.After)
		}
	}
	b.WriteString("\n## The cross-paper edges\n\n")
	if a.Mismatch == 0 {
		b.WriteString("No cross-paper uses edge goes to an item of the wrong kind. The cross-paper edges of AX6.5 are not resolved yet, so this check reads nothing yet.\n")
	} else {
		fmt.Fprintf(&b, "%s go to an item that is not a statement or a definition.\n", prose.Count(a.Mismatch, "cross-paper uses edge"))
	}
	return b.String()
}

// ReadFile reads an edges file of analysis/edges.
func ReadFile(file string) ([]Edge, error) {
	b, err := mdrec.ReadLines(file)
	if err != nil {
		return nil, err
	}
	var out []Edge
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var e Edge
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", file, n, err)
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

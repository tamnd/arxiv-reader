package census

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/arxiv-reader/theorems"
)

// Ref is what the census needs to know of one object record.
type Ref struct {
	Local  string
	Census string
	// Head is environment, manual, section or prose.
	Head   string
	Origin string
	Path   string
}

// Waiver is one waiver of AX4.8.
type Waiver struct {
	Occurrence string `json:"occurrence"`
	Reason     string `json:"reason"`
	// By is the person or the rule that wrote it. A rule is rule:<name>.
	By string `json:"by"`
	// Env is set on a waiver rule instead of Occurrence. The rule waives each
	// occurrence of the environment that no object accounts for.
	Env string `json:"-"`
}

// Gap is one occurrence that no object accounts for, or that two objects
// account for, with the pattern of AX4.9.
type Gap struct {
	Occurrence string `json:"occurrence"`
	Result     string `json:"result"`
	Pattern    string `json:"pattern"`
	File       string `json:"file,omitempty"`
	Line       int    `json:"line,omitempty"`
}

// Record is the census record of AX4.7.
type Record struct {
	Paper        string         `json:"paper"`
	Version      int            `json:"version"`
	Extractor    string         `json:"extractor"`
	Run          string         `json:"run,omitempty"`
	Main         []string       `json:"main"`
	Class        string         `json:"class,omitempty"`
	Packages     []string       `json:"packages,omitempty"`
	Steps        []string       `json:"steps,omitempty"`
	Declarations []Declaration  `json:"declarations"`
	Occurrences  map[string]int `json:"occurrences"`
	Matched      map[string]int `json:"matched"`
	Gaps         []Gap          `json:"gaps"`
	Phantoms     []string       `json:"phantoms,omitempty"`
	Waivers      []Waiver       `json:"waivers"`
	Numbers      Numbers        `json:"numbers"`
	Textrefs     Textrefs       `json:"textrefs"`
	Render       *RenderCount   `json:"render,omitempty"`
	Outside      map[string]int `json:"outside,omitempty"`
	Findings     []Finding      `json:"findings,omitempty"`
	Strength     string         `json:"strength"`
	Items        []*Occurrence  `json:"items,omitempty"`
}

// Declaration is a declaration as the census record keeps it.
type Declaration struct {
	Env      string `json:"env"`
	Title    string `json:"title"`
	Item     string `json:"item"`
	Counter  string `json:"counter,omitempty"`
	Shared   string `json:"shared,omitempty"`
	Within   string `json:"within,omitempty"`
	Numbered bool   `json:"numbered"`
	Proof    bool   `json:"proof,omitempty"`
	From     string `json:"from"`
	Form     string `json:"form,omitempty"`
	// TitleArg is true when each use gives the title, as a wrapper with an
	// argument for the head.
	TitleArg bool `json:"title_arg,omitempty"`
}

// Numbers is the check of agreement of AX4.4.
type Numbers struct {
	FromAux        int `json:"from_aux"`
	FromRender     int `json:"from_render"`
	FromCounter    int `json:"from_counter"`
	FromHead       int `json:"from_head"`
	Agree          int `json:"agree"`
	Disagree       int `json:"disagree"`
	ContiguityGaps int `json:"contiguity_gaps"`
}

// Textrefs is the count of text references and how they resolve.
type Textrefs struct {
	Resolved   int `json:"resolved"`
	Unresolved int `json:"unresolved"`
}

// RenderCount is the cross-path count of AX4.5.
type RenderCount struct {
	Theorems int  `json:"theorems"`
	Proofs   int  `json:"proofs"`
	Agree    bool `json:"agree"`
	// SourceTheorems and SourceProofs are the counts of the source path.
	SourceTheorems int `json:"source_theorems"`
	SourceProofs   int `json:"source_proofs"`
	// Envs is each environment whose two counts differ, when the counts of
	// the paper do not agree. It is the input of the queue census-diff.
	Envs []EnvCount `json:"envs,omitempty"`
}

// EnvCount is the two counts of one environment, by its name in the render.
type EnvCount struct {
	Env    string `json:"env"`
	Source int    `json:"source"`
	Render int    `json:"render"`
}

// Match matches the occurrences to the object records, and gives the census
// record. The waivers name occurrences by id.
func (c *Census) Match(objs []Ref, waivers []Waiver) *Record {
	in := c.In
	r := &Record{
		Paper: in.Paper, Version: in.Version, Extractor: in.Extractor, Run: in.Run,
		Occurrences: map[string]int{}, Matched: map[string]int{}, Gaps: []Gap{}, Waivers: []Waiver{},
		Outside: in.Outside,
	}
	if d := in.Doc; d != nil {
		r.Main = []string{d.Main}
		if len(d.Mains) > 0 {
			r.Main = d.Mains
		}
		r.Class, r.Packages, r.Steps = d.Class, d.Packages, d.Steps
	}
	if in.Set != nil {
		for _, decl := range in.Set.List() {
			r.Declarations = append(r.Declarations, declOf(decl))
		}
	}
	waived := map[string]Waiver{}
	byEnv := map[string]Waiver{}
	for _, w := range waivers {
		if w.Occurrence == "" && w.Env != "" {
			byEnv[w.Env] = w
			continue
		}
		waived[w.Occurrence] = w
	}
	named := map[string]int{}
	labels := map[string]bool{}
	for _, o := range objs {
		if o.Census != "" {
			named[o.Census]++
		}
	}
	for _, o := range c.Occurrences {
		r.Occurrences[o.Type]++
		switch o.Type {
		case Env, Proof, Head, Restate:
			n := named[o.ID]
			switch {
			case n == 1:
				o.Result = Matched
				r.Matched[o.Type]++
				for _, l := range o.Labels {
					labels[l] = true
				}
			case n > 1:
				o.Result = Double
			default:
				o.Result = Missing
			}
			w, ok := waived[o.ID]
			if !ok && o.Type == Env {
				if w, ok = byEnv[o.Env]; ok {
					w.Occurrence = o.ID
				}
			}
			if ok && o.Result != Matched {
				o.Result, o.Waiver = Waived, w.Reason
				r.Waivers = append(r.Waivers, w)
			}
			if o.Result == Missing || o.Result == Double {
				r.Gaps = append(r.Gaps, Gap{Occurrence: o.ID, Result: o.Result, Pattern: c.pattern(o), File: o.Pos.File, Line: o.Pos.Line})
			}
		}
	}
	for _, o := range c.Occurrences {
		if o.Type == Label {
			if labels[o.Target] {
				r.Matched[Label]++
			}
		}
		if o.Type == Textref {
			if strings.HasPrefix(o.Result, "resolved") {
				r.Textrefs.Resolved++
			} else {
				r.Textrefs.Unresolved++
			}
		}
	}
	ids := map[string]bool{}
	for _, o := range c.Occurrences {
		ids[o.ID] = true
	}
	for _, o := range objs {
		if o.Census == "" && (o.Head == "prose" || o.Path != "source") {
			continue
		}
		if !ids[o.Census] {
			r.Phantoms = append(r.Phantoms, o.Local)
		}
	}
	for _, o := range c.Items() {
		switch o.NumberFrom {
		case "aux":
			r.Numbers.FromAux++
		case "render":
			r.Numbers.FromRender++
		case "counter":
			r.Numbers.FromCounter++
		case "head":
			r.Numbers.FromHead++
		}
		witness := o.Aux
		if witness == "" {
			witness = o.Printed
		}
		if witness != "" && o.Model != "" {
			if witness == o.Model {
				r.Numbers.Agree++
			} else {
				r.Numbers.Disagree++
				c.Findings = append(c.Findings, Finding{Rule: "D07", Occurrence: o.ID, Message: fmt.Sprintf("the counter model gives %s %s and the second witness gives %s", o.Title, o.Model, witness)})
			}
		}
	}
	if in.Render != nil {
		r.Render = c.renderCounts()
	}
	for _, f := range c.Findings {
		if f.Rule == "D07" && (strings.Contains(f.Message, "comes after") || strings.Contains(f.Message, "first in its scope")) {
			r.Numbers.ContiguityGaps++
		}
	}
	r.Findings = c.Findings
	r.Strength = c.strength(r)
	r.Items = c.Items()
	return r
}

// strength is the strength of AX4.6.
func (c *Census) strength(r *Record) string {
	if c.In.Doc == nil || c.In.Doc.Body == 0 {
		return "none"
	}
	witness := r.Numbers.FromAux + r.Numbers.FromRender
	numbered := 0
	for _, o := range c.Items() {
		if o.Type == Env && o.Counter != "" {
			numbered++
		}
	}
	renderOK := r.Render == nil || r.Render.Agree
	switch {
	case numbered == 0 && r.Render != nil && r.Render.Agree:
		return "strong"
	case numbered > 0 && r.Numbers.Disagree == 0 && renderOK && witness > 0 && (r.Render != nil || r.Numbers.FromAux == numbered):
		return "strong"
	}
	return "normal"
}

// pattern is the pattern of AX4.9 that groups gaps: the declaration form, the
// class, the package and the head form.
func (c *Census) pattern(o *Occurrence) string {
	form := "head"
	if decl := c.In.Set.Get(o.Env); decl != nil {
		form = decl.Form
		if form == "" {
			form = decl.From
		}
	}
	return fmt.Sprintf("%s/%s/%s", o.Type, form, c.In.Doc.Class)
}

func declOf(d *theorems.Declaration) Declaration {
	return Declaration{Env: d.Env, Title: d.Title, Item: d.Item, Counter: d.Counter, Shared: d.Shared, Within: d.Within, Numbered: d.Numbered, Proof: d.Proof, From: d.From, Form: d.Form, TitleArg: d.TitleArg}
}

// JSON is the census record as one JSON document, with the items sorted.
func (r *Record) JSON() ([]byte, error) {
	sort.SliceStable(r.Gaps, func(i, j int) bool { return r.Gaps[i].Occurrence < r.Gaps[j].Occurrence })
	return json.MarshalIndent(r, "", " ")
}

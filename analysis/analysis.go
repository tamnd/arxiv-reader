package analysis

import (
	"fmt"
	"path"
	"strings"

	"github.com/tamnd/arxiv-reader/census"
	"github.com/tamnd/arxiv-reader/cues"
	"github.com/tamnd/arxiv-reader/edges"
	"github.com/tamnd/arxiv-reader/objects"
	"github.com/tamnd/arxiv-reader/source"
	"github.com/tamnd/arxiv-reader/tex"
	"github.com/tamnd/arxiv-reader/theorems"
)

// Paper is what the analysis needs to know of a version.
type Paper struct {
	ID        string
	Version   int
	Extractor string
	Run       string
	// Text is true when the licence class of the version allows text in the
	// analysis plane: open and share-alike.
	Text bool
	// Render is the second reading from the LaTeXML render, or nil.
	Render *census.Render
	// Waivers is each waiver for the version.
	Waivers []census.Waiver
	// Outside is the count of results of each outside index for the paper
	// (AX4.5), or nil.
	Outside map[string]int
}

// Result is the analysis of one version.
type Result struct {
	Objects []objects.Record
	// Census is one census record for each main file. The first is the
	// record of the paper.
	Census []*census.Record
	// Edges is the edges of each main file (AX06), and Cycles each cycle of
	// the in-paper uses graph.
	Edges  []edges.Edge
	Cycles [][]string
	// Prose is the candidate sentences of the prose items (AX7.4 step 1).
	Prose []cues.Candidate
}

// Manifests is the four manifests that the analysis reads.
type Manifests struct {
	Resolver *theorems.Resolver
	Phrases  theorems.Phrases
	Cues     *cues.Cues
}

// DefaultManifests is the first version of the four manifests.
func DefaultManifests() *Manifests {
	b, _ := theorems.Manifest("prose.yaml")
	c, err := cues.Parse(b)
	if err != nil {
		panic(err)
	}
	return &Manifests{Resolver: theorems.NewResolver(), Phrases: theorems.DefaultPhrases(), Cues: c}
}

// Analyze runs the front end, the builder and the census on an e-print.
func Analyze(b *source.Bundle, p Paper, m *Manifests) (*Result, error) {
	if m == nil {
		m = DefaultManifests()
	}
	docs, err := tex.Read(b)
	if err != nil {
		return nil, err
	}
	var aux [][]byte
	for _, f := range b.Files {
		if strings.EqualFold(path.Ext(f.Name), ".aux") {
			aux = append(aux, f.Data)
		}
	}
	res := &Result{}
	for i, d := range docs {
		set := m.Resolver.Resolve(d)
		prefix := ""
		if i > 0 {
			prefix = fmt.Sprintf("m%d-", i+1)
		}
		var render *census.Render
		var outside map[string]int
		if i == 0 {
			render, outside = p.Render, p.Outside
		}
		c := census.Run(census.Input{Paper: p.ID + prefixPaper(i), Version: p.Version, Extractor: p.Extractor, Run: p.Run, Doc: d, Set: set, Titles: m.Resolver.Titles, Aux: aux, Render: render, Outside: outside})
		// Each record names the census occurrence that starts where it starts.
		at := map[int]*census.Occurrence{}
		numbers := map[int]string{}
		for _, o := range c.Items() {
			if _, ok := at[o.Off]; !ok {
				at[o.Off] = o
				numbers[o.Off] = o.Number
			}
		}
		bld := &Builder{Paper: p.ID, Version: p.Version, Extractor: p.Extractor, Text: p.Text, Titles: m.Resolver.Titles, Phrases: m.Phrases, Prefix: prefix, Numbers: numbers}
		recs, ext := bld.Build(d, set)
		refs := make([]census.Ref, len(recs))
		for k := range recs {
			r := &recs[k]
			if o, ok := at[ext[k].Off]; ok {
				r.Census = o.ID
				if r.Number == "" || o.NumberFrom != "head" {
					r.Number, r.NumberFrom = o.Number, o.NumberFrom
				}
			}
			refs[k] = census.Ref{Local: r.Local, Census: r.Census, Head: r.Head, Origin: r.Origin, Path: r.Path}
		}
		// A restatement prints the number of the item that it restates.
		byLocal := map[string]*objects.Record{}
		for k := range recs {
			byLocal[recs[k].Local] = &recs[k]
		}
		for k := range recs {
			r := &recs[k]
			if f, ok := byLocal[r.Restates]; ok && r.Number == "" && f.Number != "" {
				r.Number, r.NumberFrom = f.Number, f.NumberFrom
			}
		}
		rec := c.Match(refs, p.Waivers)
		items := make([]edges.Item, len(recs))
		for k, r := range recs {
			x := ext[k]
			items[k] = edges.Item{Local: r.Local, Kind: r.Item, Title: r.Title, Number: r.Number, Name: r.Name, Labels: r.Labels, Parts: r.Parts, Proof: x.Proof, Parent: r.Parent, InProof: r.Scope == "local", Proves: r.Proves, ProvesPart: r.ProvesPart, Restates: r.Restates, Off: x.Off, Inner: x.Inner, Close: x.Close, End: x.End}
		}
		g := edges.Resolve(edges.Input{Paper: p.ID, Version: p.Version, Extractor: p.Extractor, Prefix: prefix, Doc: d, Items: items, Titles: m.Resolver.Titles})
		res.Edges = append(res.Edges, g.Edges...)
		res.Cycles = append(res.Cycles, g.Cycles...)
		if m.Cues != nil {
			spans := make([]tex.Region, len(ext))
			for k, x := range ext {
				spans[k] = tex.Region{From: x.Off, To: x.End}
			}
			res.Prose = append(res.Prose, m.Cues.Find(d, spans, prefix)...)
		}
		res.Objects = append(res.Objects, recs...)
		res.Census = append(res.Census, rec)
	}
	return res, nil
}

// prefixPaper marks the census ids of the second and later main files.
func prefixPaper(i int) string {
	if i == 0 {
		return ""
	}
	return fmt.Sprintf(".m%d", i+1)
}

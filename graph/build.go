package graph

import (
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/objects"
	"github.com/tamnd/arxiv-reader/refs"
)

// The stages that assert an edge, which is the other half of what 2166-08
// section 3 requires every edge to carry.
const (
	// StageHarvest is the metadata plane, so every edge that exists for all of
	// arXiv rather than for the papers somebody extracted.
	StageHarvest = "harvest"
	// StageRefs is the bibliography, parsed and resolved.
	StageRefs = "refs"
	// StageObjects is the content plane read as objects.
	StageObjects = "objects"
)

// The ways an edge is found, which is the Via field.
const (
	ViaMetadata = "metadata"
	ViaRef      = "ref"
	ViaProof    = "proof"
	ViaLocator  = "locator"
)

// Metadata is the edges one metadata record asserts.
//
// These are the edges that exist for the whole of arXiv rather than for the
// papers somebody extracted, because arXiv publishes its metadata under CC0 and
// the licence gate has nothing to say about it. That is the single best argument
// for filling the metadata plane before extracting anything: a graph over the
// thousand papers of the seed is not interesting, and a graph over three
// million with a thousand of them readable is.
//
// Every edge here is certain, because every one of them is a fact arXiv
// published rather than a reading of anything.
func Metadata(rec metadata.Record, at string) []Edge {
	if rec.ID == "" {
		return nil
	}
	b := builder{at: at}
	paper := Paper(rec.ID)
	for _, a := range rec.Authors {
		if name := Name(a.String()); name != "" {
			b.add(Edge{S: paper, P: AuthoredBy, O: name, Conf: Certain, Via: ViaMetadata, Stage: StageHarvest})
		}
	}
	for _, c := range rec.Categories {
		if c == "" {
			continue
		}
		b.add(Edge{S: paper, P: InCategory, O: Category(c), Conf: Certain, Via: ViaMetadata, Stage: StageHarvest})
	}
	for _, v := range rec.Versions {
		if v.Version < 1 {
			continue
		}
		b.add(Edge{S: Version(rec.ID, v.Version), P: VersionOf, O: paper, Conf: Certain, Via: ViaMetadata, Stage: StageHarvest})
	}
	return b.edges
}

// Objects finds another paper's objects, and reports whether the corpus has them.
//
// A function rather than a corpus root, because what this needs is one question
// answered and the caller is the one that knows whether the answer comes off
// disk, out of a cache or out of a test fixture. It is what makes a uses edge
// across two papers possible at all: the locator says Theorem 2 and only the
// cited paper can say which object that is.
type Objects func(paper string) (objects.Paper, bool)

// Content is the edges one extracted paper asserts.
//
// The object record is the whole input. Everything the content plane knows about
// citations is in it already, because a reference is an object and it carries
// what it resolved to, so this reads one file per paper rather than joining
// three, and a paper whose objects have been rebuilt has a graph that agrees
// with them by construction.
//
// The locators are the exception and they are read here rather than taken from
// manifests/cites, because that file records a citation per section and a uses
// edge runs from an object. Scanning each object's own body is what says which
// theorem named Theorem 2 of the paper it cites, rather than which file did.
func Content(p objects.Paper, pat *refs.Locators, find Objects, at string) []Edge {
	if p.Paper == "" {
		return nil
	}
	b := builder{at: at}
	local := make(map[string]objects.Record, len(p.Objects))
	bib := map[string]objects.Record{}
	for _, o := range p.Objects {
		local[o.Local] = o
		if o.Kind == "reference" {
			bib[o.Local] = o
		}
	}
	// The paper level citation first, which is the common case and the easy one.
	// It stands whether or not the citing object carried a tag, and it is what
	// the closure figure is computed over.
	for _, o := range p.Objects {
		if o.Kind != "reference" || o.Resolved == "" {
			continue
		}
		b.add(Edge{S: Paper(p.Paper), P: Cites, O: Paper(o.Resolved), Conf: o.Confidence, Via: o.Via, Stage: StageRefs})
	}
	for _, o := range p.Objects {
		if o.Tag == "" || o.Kind == "reference" {
			continue
		}
		b.internal(p.Paper, o, local)
		b.citations(p.Paper, o, bib, pat, find)
	}
	return b.edges
}

// internal is the edges an object asserts inside its own paper.
//
// A link the renderer resolved is a structural match and not a reading of
// anything, so it is high. It is not certain: nothing inferred is, and the link
// being resolved says the target exists rather than saying the author meant it.
func (b *builder) internal(paper string, o objects.Record, local map[string]objects.Record) {
	for _, l := range o.RefsOut {
		t, ok := local[l]
		if !ok || t.Tag == "" || t.Tag == o.Tag {
			continue
		}
		from, to := Object(paper, o.Tag), Object(paper, t.Tag)
		conf := structural(o.Path)
		b.add(Edge{S: from, P: RefersTo, O: to, Conf: conf, Via: ViaRef, Stage: StageObjects})
		// 2166-08 section 5: a refers-to between a proof and a statement in the
		// same paper is promoted to uses. Both edges are kept rather than one
		// replacing the other, because the link is a fact about the file and the
		// dependency is a reading of it, and the reading is the one that can be
		// wrong. A proof that says "unlike Lemma 2" is a proof this gets wrong.
		if o.Kind == "proof" && (t.Kind == "statement" || t.Kind == "definition") {
			b.add(Edge{S: from, P: Uses, O: to, Conf: conf, Via: ViaProof, Stage: StageObjects})
		}
	}
}

// citations is the edges an object asserts about the papers it cites.
//
// Every citation inside a tagged object is already more than a paper level
// citation graph knows: it says which theorem needed the work rather than which
// paper mentioned it. A citation that names a particular result goes further
// still, and that is the uses edge, which is the hard one and the one worth the
// most.
func (b *builder) citations(paper string, o objects.Record, bib map[string]objects.Record, pat *refs.Locators, find Objects) {
	from := Object(paper, o.Tag)
	for _, c := range refs.Scan(o.Local, o.BodyMD, pat) {
		e, ok := bib[c.Entry]
		if !ok || e.Resolved == "" {
			continue
		}
		cite := Edge{S: from, P: Cites, O: Paper(e.Resolved), Conf: e.Confidence, Via: e.Via, Stage: StageRefs}
		loc := c.Locator()
		if loc.Empty() {
			b.add(cite)
			continue
		}
		if t, ok := located(e.Resolved, loc, find); ok {
			b.add(cite)
			// Medium, always, and 2166-08 section 5 is three paragraphs on why.
			// The target can have renumbered between the version that was cited
			// and the version the corpus extracted, the citing author can have
			// been reading the journal version, and the pattern can have read
			// the wrong words. So the edge is a proposal, the page says so, and
			// the reading app words it as "cites, and appears to use".
			b.add(Edge{S: from, P: Uses, O: Object(e.Resolved, t.Tag), Conf: Medium, Via: ViaLocator, Locator: loc.String(), Stage: StageRefs})
			continue
		}
		// The locator could not be resolved, which is usually because the cited
		// paper is not in the content plane. It is kept on the citation rather
		// than dropped: a paper whose Theorem 2 is named by nine papers already
		// here is a paper worth extracting, and that is a better selection
		// signal than a citation count because somebody used a specific result.
		cite.Locator = loc.String()
		b.add(cite)
	}
}

// located finds the object a locator names in the paper it was cited from.
//
// Nothing is returned when the cited paper is not in the content plane, when no
// object matches, or when two do. Two matching is the appendix case: a paper
// with a Theorem 2 and an appendix Theorem B.2 printed as 2 is a paper where the
// locator names one of them and nothing here can say which, and an edge that
// guesses is worse than no edge.
func located(paper string, loc refs.Locator, find Objects) (objects.Record, bool) {
	if find == nil || loc.Number == "" {
		return objects.Record{}, false
	}
	p, ok := find(paper)
	if !ok {
		return objects.Record{}, false
	}
	var found objects.Record
	n := 0
	for _, o := range p.Objects {
		if o.Tag == "" || o.Number != loc.Number {
			continue
		}
		if o.Subkind != loc.Kind && o.Kind != loc.Kind {
			continue
		}
		found, n = o, n+1
	}
	return found, n == 1
}

// structural is how much a match against the markup is worth on this path.
//
// The render and source paths have markup that says what an object is and what a
// link points at, so a match against it is structural. The native and vision
// paths have a printed page and a model, and both of those can be wrong about a
// sentence.
func structural(path string) string {
	if path == "render" || path == "source" {
		return High
	}
	return Medium
}

// builder collects edges and keeps one of each claim.
type builder struct {
	at    string
	edges []Edge
	seen  map[string]int
}

// add records one claim, keeping the better confidence when the same claim
// arrives twice.
func (b *builder) add(e Edge) {
	if e.Conf == "" || e.Via == "" {
		return
	}
	e.At = b.at
	if b.seen == nil {
		b.seen = map[string]int{}
	}
	if i, held := b.seen[e.Key()]; held {
		b.edges[i].Conf = Strongest(b.edges[i].Conf, e.Conf)
		return
	}
	b.seen[e.Key()] = len(b.edges)
	b.edges = append(b.edges, e)
}

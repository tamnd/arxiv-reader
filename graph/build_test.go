package graph

import (
	"testing"
	"time"

	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/objects"
	"github.com/tamnd/arxiv-reader/refs"
)

const at = "2026-10-14"

// find is what an edge set is read back with, because an edge set is unordered
// to a reader even though it is sorted on disk.
func find(t *testing.T, edges []Edge, s, p, o string) Edge {
	t.Helper()
	for _, e := range edges {
		if e.S == s && e.P == p && e.O == o {
			return e
		}
	}
	t.Fatalf("no %s from %s to %s in %+v", p, s, o, edges)
	return Edge{}
}

// nothingLanded is the assertion of every locator test that expects no use
// across papers. It cannot be "no uses edge at all", because the proof inside
// the paper promotes one and that one is right.
func nothingLanded(t *testing.T, edges []Edge) {
	t.Helper()
	for _, e := range edges {
		if e.P == Uses && e.Via == ViaLocator {
			t.Fatalf("a locator landed, %+v", e)
		}
	}
}

// valid is run on every edge every test builds, because the point of validating
// on the way to disk is that nothing ever reaches it invalid.
func valid(t *testing.T, edges []Edge) []Edge {
	t.Helper()
	for _, e := range edges {
		if err := e.Validate(); err != nil {
			t.Fatalf("%+v: %v", e, err)
		}
		if e.At != at {
			t.Fatalf("%+v is dated %q", e, e.At)
		}
	}
	return edges
}

func record() metadata.Record {
	return metadata.Record{
		ID:         "2312.00752",
		Title:      "Mamba: Linear-Time Sequence Modeling with Selective State Spaces",
		Authors:    []metadata.Author{{Surname: "Gu", Forename: "Albert"}, {Surname: "Dao", Forename: "Tri"}},
		Categories: []string{"cs.LG", "cs.AI"},
		Versions: []metadata.Version{
			{Version: 1, Created: time.Date(2023, 12, 1, 0, 0, 0, 0, time.UTC)},
			{Version: 2, Created: time.Date(2024, 5, 31, 0, 0, 0, 0, time.UTC)},
		},
	}
}

func TestTheMetadataPlaneAssertsAuthorsCategoriesAndVersions(t *testing.T) {
	edges := valid(t, Metadata(record(), at))
	if len(edges) != 6 {
		t.Fatalf("%d edges, want two authors, two categories and two versions: %+v", len(edges), edges)
	}
	find(t, edges, Paper("2312.00752"), AuthoredBy, Name("Albert Gu"))
	find(t, edges, Paper("2312.00752"), InCategory, Category("cs.AI"))
	find(t, edges, Version("2312.00752", 2), VersionOf, Paper("2312.00752"))
}

// Every one of these is a fact arXiv published rather than a reading of
// anything, which is what certain means and the only place it is allowed.
func TestEveryMetadataEdgeIsCertain(t *testing.T) {
	for _, e := range Metadata(record(), at) {
		if e.Conf != Certain {
			t.Errorf("%s is %q", e.P, e.Conf)
		}
		if e.Stage != StageHarvest || e.Via != ViaMetadata {
			t.Errorf("%s came from %s by %s", e.P, e.Stage, e.Via)
		}
	}
}

func TestAPaperWithNoIDAssertsNothing(t *testing.T) {
	if edges := Metadata(metadata.Record{Title: "Nothing"}, at); edges != nil {
		t.Errorf("got %+v", edges)
	}
}

// Two spellings of one name fold together, so the paper says the same thing
// twice and the graph records it once.
func TestTheSameAuthorTwiceIsOneEdge(t *testing.T) {
	rec := record()
	rec.Authors = []metadata.Author{{Surname: "Gu", Forename: "Albert"}, {Surname: "Gu", Forename: "Albert"}}
	n := 0
	for _, e := range Metadata(rec, at) {
		if e.P == AuthoredBy {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d authored-by edges", n)
	}
}

// paper is one extracted paper, built the way the object record has it.
func paper() objects.Paper {
	return objects.Paper{
		Paper: "2501.00001", Version: "v3", Lang: "en",
		Objects: []objects.Record{
			{Kind: "front", Local: "front", Paper: "2501.00001", Path: "render", Confidence: "certain", Order: 0},
			{Kind: "section", Local: "s1", Tag: "AAAA", Paper: "2501.00001", Path: "render", Order: 1,
				BodyMD: `The argument is the one of \[[3](#bib.bibx3), Theorem 2.1\], and the bound is Lemma 4 of \[[4](#bib.bibx4)\]. Everything else follows [Fifth et al. (2022)](#bib.bibx5).`},
			{Kind: "statement", Subkind: "theorem", Local: "thm-1", Tag: "BBBB", Number: "1", Paper: "2501.00001", Path: "render", Order: 2},
			{Kind: "proof", Local: "proof-u1", Tag: "CCCC", Paper: "2501.00001", Path: "render", Order: 3,
				RefsOut: []string{"thm-1"}},
			{Kind: "reference", Local: "bib.bibx3", Paper: "2501.00001", Order: 4,
				Resolved: "2401.01234", Via: "arxiv", Confidence: "certain"},
			{Kind: "reference", Local: "bib.bibx4", Paper: "2501.00001", Order: 5,
				Resolved: "2303.00002", Via: "title", Confidence: "medium"},
			{Kind: "reference", Local: "bib.bibx5", Paper: "2501.00001", Order: 6},
		},
	}
}

// target is the cited paper, in the content plane, with the theorem the locator
// names.
func target() objects.Paper {
	return objects.Paper{
		Paper: "2401.01234", Version: "v1", Lang: "en",
		Objects: []objects.Record{
			{Kind: "statement", Subkind: "theorem", Local: "thm-2-1", Tag: "QQ11", Number: "2.1", Paper: "2401.01234", Path: "render", Order: 0},
		},
	}
}

func lookup(papers ...objects.Paper) Objects {
	return func(id string) (objects.Paper, bool) {
		for _, p := range papers {
			if p.Paper == id {
				return p, true
			}
		}
		return objects.Paper{}, false
	}
}

func content(t *testing.T, find Objects) []Edge {
	t.Helper()
	return valid(t, Content(paper(), refs.DefaultLocators(), find, at))
}

func TestAResolvedReferenceIsACitationFromThePaper(t *testing.T) {
	edges := content(t, nil)
	e := find(t, edges, Paper("2501.00001"), Cites, Paper("2401.01234"))
	if e.Conf != Certain || e.Via != "arxiv" || e.Stage != StageRefs {
		t.Errorf("got %+v", e)
	}
	if e := find(t, edges, Paper("2501.00001"), Cites, Paper("2303.00002")); e.Conf != Medium {
		t.Errorf("a title match is %q", e.Conf)
	}
}

// A reference to a textbook resolves to nothing and is published as a
// bibliography line all the same. It just does not become an edge.
func TestAReferenceThatResolvedToNothingIsNotAnEdge(t *testing.T) {
	for _, e := range content(t, nil) {
		if e.O == Paper("") || e.O == "ax://paper/" {
			t.Errorf("%+v points at nothing", e)
		}
	}
}

// This is the thing a paper level citation graph does not know: which theorem
// needed the work, rather than which paper mentioned it.
func TestACitationInsideAnObjectRunsFromTheObject(t *testing.T) {
	e := find(t, content(t, nil), Object("2501.00001", "AAAA"), Cites, Paper("2401.01234"))
	if e.Conf != Certain || e.Via != "arxiv" {
		t.Errorf("got %+v", e)
	}
}

func TestALinkInsideThePaperIsARefersToEdge(t *testing.T) {
	e := find(t, content(t, nil), Object("2501.00001", "CCCC"), RefersTo, Object("2501.00001", "BBBB"))
	if e.Conf != High || e.Via != ViaRef || e.Stage != StageObjects {
		t.Errorf("got %+v", e)
	}
}

// Inside one paper this is mechanical, so it is high. It is still a reading: a
// proof that says "unlike Lemma 2" is a proof this gets wrong, which is why the
// refers-to edge stays beside it rather than being replaced by it.
func TestAProofThatRefersToAStatementUsesIt(t *testing.T) {
	edges := content(t, nil)
	e := find(t, edges, Object("2501.00001", "CCCC"), Uses, Object("2501.00001", "BBBB"))
	if e.Conf != High || e.Via != ViaProof {
		t.Errorf("got %+v", e)
	}
	find(t, edges, Object("2501.00001", "CCCC"), RefersTo, Object("2501.00001", "BBBB"))
}

// A section that links to a theorem is not using it, and only a proof promotes.
func TestALinkFromSomethingThatIsNotAProofIsNotUse(t *testing.T) {
	p := paper()
	p.Objects[1].RefsOut = []string{"thm-1"}
	edges := Content(p, refs.DefaultLocators(), nil, at)
	for _, e := range edges {
		if e.P == Uses && e.S == Object("2501.00001", "AAAA") {
			t.Errorf("a section used something, %+v", e)
		}
	}
	find(t, edges, Object("2501.00001", "AAAA"), RefersTo, Object("2501.00001", "BBBB"))
}

// The native and vision paths read a printed page rather than markup, so a match
// against what they produced is a reading and not a structural fact.
func TestALinkFoundOnThePrintedPageIsOnlyMedium(t *testing.T) {
	p := paper()
	for i := range p.Objects {
		p.Objects[i].Path = "native"
	}
	e := find(t, Content(p, refs.DefaultLocators(), nil, at), Object("2501.00001", "CCCC"), RefersTo, Object("2501.00001", "BBBB"))
	if e.Conf != Medium {
		t.Errorf("got %q", e.Conf)
	}
}

// The hard one, the novel one, and the one worth the most.
func TestALocatorResolvedInTheCitedPaperIsAUsesEdge(t *testing.T) {
	edges := content(t, lookup(target()))
	e := find(t, edges, Object("2501.00001", "AAAA"), Uses, Object("2401.01234", "QQ11"))
	if e.Conf != Medium || e.Via != ViaLocator || e.Locator != "theorem 2.1" || e.Stage != StageRefs {
		t.Errorf("got %+v", e)
	}
	// The citation stands beside it and carries no dangling locator, because
	// this one landed.
	if e := find(t, edges, Object("2501.00001", "AAAA"), Cites, Paper("2401.01234")); e.Locator != "" {
		t.Errorf("the citation still carries %q", e.Locator)
	}
}

// The strongest selection signal the corpus has: a paper whose Theorem 2 is
// named by nine papers already here is worth extracting, and that is better than
// a citation count because somebody used a specific result.
func TestALocatorNothingCanResolveIsKeptOnTheCitation(t *testing.T) {
	edges := content(t, nil)
	e := find(t, edges, Object("2501.00001", "AAAA"), Cites, Paper("2401.01234"))
	if e.Locator != "theorem 2.1" {
		t.Errorf("the dangling locator is %q", e.Locator)
	}
	nothingLanded(t, edges)
}

// Two objects numbered the same is the appendix case, and an edge that guesses
// which one was meant is worse than no edge.
func TestALocatorThatMatchesTwoObjectsResolvesToNeither(t *testing.T) {
	two := target()
	two.Objects = append(two.Objects, objects.Record{
		Kind: "statement", Subkind: "theorem", Local: "thm-a-2-1", Tag: "QQ22",
		Number: "2.1", Paper: "2401.01234", Path: "render", Order: 1,
	})
	edges := content(t, lookup(two))
	nothingLanded(t, edges)
	if e := find(t, edges, Object("2501.00001", "AAAA"), Cites, Paper("2401.01234")); e.Locator != "theorem 2.1" {
		t.Errorf("the locator is %q, and it should have stayed on the citation", e.Locator)
	}
}

// The locator says lemma and the paper it points at has a theorem of that
// number, which is not the same object.
func TestALocatorOfAnotherKindDoesNotResolve(t *testing.T) {
	lemma := target()
	lemma.Objects[0].Number = "4"
	edges := content(t, lookup(lemma))
	nothingLanded(t, edges)
}

// An object with no tag has no URI, so nothing can be said about it. The paper
// level citation stands all the same, which is why it is built separately.
func TestAnObjectWithNoTagAssertsNothing(t *testing.T) {
	p := paper()
	p.Objects[1].Tag = ""
	edges := Content(p, refs.DefaultLocators(), lookup(target()), at)
	for _, e := range edges {
		if e.S == Object("2501.00001", "") || e.S == "ax://paper/2501.00001#" {
			t.Fatalf("%+v", e)
		}
	}
	find(t, edges, Paper("2501.00001"), Cites, Paper("2401.01234"))
}

func TestAPaperNobodyExtractedAssertsNothing(t *testing.T) {
	if edges := Content(objects.Paper{}, refs.DefaultLocators(), nil, at); edges != nil {
		t.Errorf("got %+v", edges)
	}
}

// Two entries of one bibliography resolving to the same paper is one citation,
// and it is worth the best reason for it rather than the worst.
func TestTheSameCitationTwiceKeepsTheBetterConfidence(t *testing.T) {
	p := paper()
	p.Objects[5].Resolved = "2401.01234"
	p.Objects[5].Via = "arxiv"
	p.Objects[5].Confidence = Certain
	p.Objects[4].Via = "arxiv"
	edges := Content(p, refs.DefaultLocators(), nil, at)
	n := 0
	for _, e := range edges {
		if e.S == Paper("2501.00001") && e.P == Cites && e.O == Paper("2401.01234") {
			n++
			if e.Conf != Certain {
				t.Errorf("kept %q", e.Conf)
			}
		}
	}
	if n != 1 {
		t.Errorf("%d citations of one paper", n)
	}
}

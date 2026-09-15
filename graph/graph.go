// Package graph connects the corpus.
//
// 2166-08 says the ask was to publish a connected web, and that a pile of three
// million papers is not connected while a corpus where a theorem links to the
// definition it uses, in another paper, by another author, is. This package is
// the edge set that makes the difference, and it is deliberately small: seven
// predicates, three confidences, and one rule that nothing inferred is ever
// certain.
//
// The node space is arxiv-cli's. That tool already defines ax:// in its own
// pkg/graph and both tools import it, so nothing new is minted here beyond the
// three kinds the reader has and the harvester does not: an object inside a
// paper, a concept and an artefact. A node kind with two spellings is two nodes,
// and the day one of them is written by hand in a parser is the day half the
// edges stop joining.
package graph

import (
	"regexp"
	"strings"

	cli "github.com/tamnd/arxiv-cli/pkg/graph"
	"github.com/tamnd/arxiv-reader/tags"
)

// Scheme is the URI scheme every node lives under.
const Scheme = cli.Scheme

// The node kinds this graph has ends in.
//
// 2166-08 section 2 gives seven rows and this is six of them plus one rename.
// The author row is a name row here, because the metadata plane holds author
// strings and not arXiv's registered author identifiers, and arxiv-cli already
// draws that distinction in the URI space: ax://name/john-baez is the papers
// whose author string normalises to john baez, which may be one person or
// three, and ax://author/baez_j_1 is the person who registered that identifier.
// Pointing an authored-by edge at the second when the corpus only knows the
// first would be a claim nobody made. The author space fills in when the author
// identifiers are harvested, and the edges that point at it will be different
// edges.
const (
	KindPaper    = cli.KindPaper
	KindObject   = "object"
	KindVersion  = "version"
	KindName     = cli.KindName
	KindCategory = cli.KindCategory
	KindConcept  = "concept"
	KindArtefact = "artefact"
)

// Kinds is every node kind, in the order 2166-08 section 2 lists them.
var Kinds = []string{KindPaper, KindObject, KindName, KindCategory, KindConcept, KindArtefact, KindVersion}

// Paper names a paper, always without the version.
func Paper(id string) string { return cli.Paper(id) }

// Object names one tagged object inside a paper.
//
// The fragment is the tag from 2166-03 and not the local identifier, because the
// local identifier is LaTeXML's and changes when the paper is extracted again
// while the tag is assigned once and never changes. An object URI is therefore
// readable, typeable and resolvable by hand, which is the whole argument for
// tags and the reason the reading app can serve a page per theorem.
func Object(id string, tag string) string { return Paper(id) + "#" + tag }

// Version names one version of a paper.
//
// A fragment on the paper and not a path segment under it. 2166-08 section 1
// prints this one as ax://paper/2106.09685/v2 and arxiv-cli mints it as
// ax://paper/2106.09685#v2, and the tool that already writes the node wins:
// version-of edges are the one place this graph and the harvester's graph can
// join, and two spellings of a node is two nodes. The two fragment spaces do not
// collide, because a tag is four characters of digits and capitals and a version
// fragment starts with a lowercase v.
func Version(id string, n int) string { return cli.Version(id, n) }

// Name names an author string, which is not the same claim as a person.
func Name(name string) string { return cli.Name(name) }

// Category names an arXiv category.
func Category(code string) string { return cli.Category(code) }

// Concept names a concept, which is minted per 2166-06 section 4.
func Concept(name string) string { return mint(KindConcept, name) }

// Artefact names a dataset, benchmark, model or tool, per 2166-06 section 3.
func Artefact(name string) string { return mint(KindArtefact, name) }

// mint builds the URI of a derived node from its name.
//
// The name is folded by arxiv-cli's own normaliser rather than by a second one
// written here, because low-rank adaptation and Low Rank Adaptation are one
// concept and the fold that brings them together is the fold that brings two
// spellings of an author together.
func mint(kind, name string) string {
	slug := cli.NormalizeName(name)
	if slug == "" {
		return ""
	}
	return Scheme + kind + "/" + slug
}

// version matches the fragment of a version node.
var version = regexp.MustCompile(`^v[1-9][0-9]*$`)

// Kind reports which space a URI is in, and whether it is one of this graph's
// URIs at all.
//
// A fragment does change the kind here, which is the one place this parts
// company with arxiv-cli. That tool has no object nodes, so a fragment there is
// always a version and a paper with a fragment is still a paper. Here the
// fragment is the difference between a paper and a theorem inside it, and an
// edge that points at the wrong one of those joins to nothing and looks like
// missing data rather than like a fault.
func Kind(uri string) (string, bool) {
	rest, ok := strings.CutPrefix(uri, Scheme)
	if !ok {
		return "", false
	}
	space, tail, ok := strings.Cut(rest, "/")
	if !ok || tail == "" {
		return "", false
	}
	switch space {
	case KindPaper:
		return paperKind(tail)
	case KindName, KindCategory, KindConcept, KindArtefact:
		return space, true
	}
	return "", false
}

// paperKind tells a paper, one of its versions and one object inside it apart.
func paperKind(tail string) (string, bool) {
	id, frag, has := strings.Cut(tail, "#")
	switch {
	case id == "":
		return "", false
	case !has:
		return KindPaper, true
	case tags.Tag(frag).Valid():
		return KindObject, true
	case version.MatchString(frag):
		return KindVersion, true
	}
	return "", false
}

// PaperOf is the arXiv id at the paper end of a URI, and empty for a URI that is
// not in the paper space.
//
// This is what the edge files are sharded by, so it is also what says which
// paper's claims a rebuild is allowed to replace.
func PaperOf(uri string) string {
	kind, ok := Kind(uri)
	if !ok {
		return ""
	}
	switch kind {
	case KindPaper, KindObject, KindVersion:
		id, _, _ := strings.Cut(strings.TrimPrefix(uri, Scheme+KindPaper+"/"), "#")
		return id
	}
	return ""
}

// TagOf is the tag of an object URI, and empty for anything else.
func TagOf(uri string) string {
	if kind, ok := Kind(uri); !ok || kind != KindObject {
		return ""
	}
	_, frag, _ := strings.Cut(uri, "#")
	return frag
}

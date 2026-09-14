package corpus

import (
	"fmt"
	"path"
	"strings"

	"github.com/tamnd/arxiv-cli/pkg/axid"
)

// Shard is the directory a paper's files live under.
//
// It is the YYMM the id encodes and it is never the category. A paper's
// category can be changed by a moderator years later, and a directory that
// moves takes every path, every tag reference and every published URL with it.
// The month in the id cannot change, because it is part of the id.
func Shard(id axid.ID) string {
	return fmt.Sprintf("%02d%02d", id.Year%100, id.Month)
}

// PathID is the id as it appears in a file path.
//
// Old-style ids carry a slash, which is a directory separator, so the path form
// writes it as a hyphen. This is a path encoding and nothing else: the
// canonical id keeps its slash everywhere else, and the two forms are converted
// at the filesystem boundary rather than being allowed to spread.
func PathID(id axid.ID) string {
	return strings.ReplaceAll(id.Canonical, "/", "-")
}

// ContentDir is the directory holding one paper's sections in one language.
func ContentDir(root, lang string, id axid.ID) string {
	return path.Join(root, "content", lang, Shard(id), PathID(id))
}

// FiguresDir is the directory holding one paper's figures.
//
// Figures are not per language. A figure is the same picture in every
// translation and a caption is a line of prose in the section that references
// it, so there is one copy and four captions.
func FiguresDir(root string, id axid.ID) string {
	return path.Join(root, "figures", Shard(id), PathID(id))
}

// TablesDir is the directory holding one paper's tables.
//
// Not per language, for the same reason figures are not. The Markdown a
// translator works on is the one inside the section file, because that is where
// the sentence around the table is, and these two files are the English
// extraction the translation is checked against.
func TablesDir(root string, id axid.ID) string {
	return path.Join(root, "tables", Shard(id), PathID(id))
}

// TableName is what one table's two files are called, without the extension.
//
// Numbered by position in the paper and not by the number the paper prints,
// because a paper can print Table 1 twice in an appendix, can print no number
// at all, and can call one A.1. Position is the one thing every table has.
func TableName(n int) string { return fmt.Sprintf("t%02d", n) }

// TagsPath is one paper's tag register.
//
// One file per paper rather than one register for the corpus. A single register
// over millions of papers would conflict on every parallel extraction, and
// removing a paper after a takedown would mean rewriting a file that everything
// else in the corpus points into.
func TagsPath(root string, id axid.ID) string {
	return path.Join(root, "tags", Shard(id), PathID(id)+".tags")
}

// RunsPath records where one assignment of a paper's tags stopped and the next
// began.
//
// Beside the register and not inside it, because the register is a set and this
// is a history. A line here is two tags and everything between them in the
// register is one run, which is what tells a correct edit apart from a tag
// somebody pasted in the wrong place.
func RunsPath(root string, id axid.ID) string {
	return path.Join(root, "tags", Shard(id), PathID(id)+".runs")
}

// AliasesPath is the corpus wide file for local identifiers that moved.
//
// One file for everything, unlike the registers, because it is the one tag file
// that is read by a question rather than by a paper: somebody followed a link to
// #thm-3 and needs to know it is #thm-4 now, and they do not have the paper open
// to look in. It stays small because renumbering is rare and only the objects
// that actually moved go in it.
func AliasesPath(root string) string {
	return path.Join(root, "tags", "aliases")
}

// MetadataPath is the metadata plane's file for one month.
//
// Under metadata/ and not under manifests/. They were the same directory in the
// first draft and that was a mistake worth undoing before three million records
// landed on top of it: manifests/ is configuration, a person writes it and CI
// checks it, and it is about thirty lines. metadata/ is harvested data, nobody
// edits it by hand, and it is the largest thing in the repository. Two kinds of
// file with two lifetimes should not share a directory.
func MetadataPath(root, shard string) string {
	return path.Join(root, "metadata", shard+".jsonl")
}

// RenderPath is where arXiv's own HTML rendering of one version is cached.
//
// Under work/, which is gitignored here and in the corpus. These are arXiv's
// bytes and not this project's: a rendering runs to a few hundred kilobytes and
// a corpus that committed one per paper would be a mirror of arXiv rather than
// a reading of it. What gets committed is the manifest entry saying where the
// bytes came from and what they hashed to, which is enough to fetch them again
// and know they are the same bytes.
//
// An empty root gives the path relative to the corpus, which is the form the
// manifest records.
func RenderPath(root string, id axid.ID, version int) string {
	return path.Join(root, "work", "html", Shard(id), fmt.Sprintf("%sv%d.html", PathID(id), version))
}

// EPrintPath is where the submitter's own files for one version are cached.
//
// Under work/ with the rendering and for the same reason, and these bytes are
// the ones it matters most to keep out of the corpus: an e-print is the paper
// as its author wrote it, the whole of it, and a repository holding one per
// paper is a mirror of arXiv's submission store.
//
// The name says gzip and not tar, because arXiv serves one gzip stream and what
// is inside it is usually a tar of the submission and sometimes a single TeX
// file. Naming the file for the thing it turns out to contain would mean
// deciding that before the bytes have arrived.
func EPrintPath(root string, id axid.ID, version int) string {
	return path.Join(root, "work", "source", Shard(id), fmt.Sprintf("%sv%d.gz", PathID(id), version))
}

// EPrintDir is where one e-print is unpacked.
//
// The directory beside the file, named the same thing without the extension,
// because the two are the same submission in two states and a person looking
// at work/source/2006 should be able to see that without being told.
func EPrintDir(root string, id axid.ID, version int) string {
	return path.Join(root, "work", "source", Shard(id), fmt.Sprintf("%sv%d", PathID(id), version))
}

// ConvertedDir is where this project's own conversion of one submission goes.
//
// Separate from the unpacked submission and not inside it. LaTeXML copies every
// picture a paper uses next to the document it writes, so a conversion written
// into the submission would leave the tool's output mixed in with the author's
// files with nothing saying which was which, and the next run would convert
// whatever the last one left behind.
func ConvertedDir(root string, id axid.ID, version int) string {
	return path.Join(root, "work", "converted", Shard(id), fmt.Sprintf("%sv%d", PathID(id), version))
}

// ConvertedPath is the document that conversion writes.
func ConvertedPath(root string, id axid.ID, version int) string {
	return path.Join(ConvertedDir(root, id, version), "paper.html")
}

// SourcesPath is the manifest recording where every fetched byte came from.
func SourcesPath(root string) string {
	return path.Join(root, "manifests", "sources.yaml")
}

// FiguresPath is the manifest of what was decided about one month's figures.
//
// Sharded rather than one file for the corpus, for the same reason the metadata
// plane is: a manifest of every figure on arXiv is tens of millions of lines and
// every extraction would rewrite it.
//
// Separate from sources.yaml, which holds one entry per artefact keyed by paper,
// version and route. A paper has one rendering and forty figures, so figures do
// not fit that key, and they need fields no other artefact has: what the caption
// said, what the picture measured, and which rule withheld it.
func FiguresPath(root, shard string) string {
	return path.Join(root, "manifests", "figures", shard+".yaml")
}

// FigureFile is where one figure's bytes are committed, relative to the corpus
// root.
//
// Committed, unlike a rendering, because a figure is part of the paper and not
// a copy of arXiv's working file. It is also the path the content plane points
// at, which is why it is rooted: the corpus root is the reading app's web root,
// and a path relative to a content file would be four levels of dot dot from
// every one of them and would break the moment a file moved.
func FigureFile(id axid.ID, name string) string {
	return "/" + path.Join("figures", Shard(id), PathID(id), name)
}

// RefsPath is one paper's parsed bibliography.
//
// One file per paper, under a shard directory, and not one file per month the
// way figures are. A bibliography runs to a hundred entries and a month runs to
// twenty thousand papers, so a month of references is a two million line file
// that every extraction in that month rewrites. Figures get away with it
// because a manifest entry is one picture and most papers have a handful.
func RefsPath(root string, id axid.ID) string {
	return path.Join(root, "manifests", "refs", Shard(id), PathID(id)+".yaml")
}

// CitesPath is where one paper's citations sit, with the locator each of them
// carries.
//
// Beside the bibliography and not inside it, because the two are counted
// differently. A bibliography has one entry per work cited and this has one
// record per place in the paper that cites it, so a single entry cited in nine
// proofs is one line there and nine lines here.
func CitesPath(root string, id axid.ID) string {
	return path.Join(root, "manifests", "cites", Shard(id), PathID(id)+".yaml")
}

// LocatorsPath is the pattern set a locator is read with.
//
// It sits in the corpus rather than in the binary because every field writes a
// locator differently, and the person who notices that a field writes them in a
// shape nothing here matches is the person reading that field's papers, not
// whoever is editing this package that week. The binary carries a default set
// and this file replaces it.
func LocatorsPath(root string) string {
	return path.Join(root, "manifests", "graph.yaml")
}

// GraphPath is the edge file for one month.
//
// Sharded by the subject's paper, so extracting one paper writes one file.
func GraphPath(root, shard string) string {
	return path.Join(root, "graph", shard+".jsonl")
}

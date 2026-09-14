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

// TagsPath is one paper's tag register.
//
// One file per paper rather than one register for the corpus. A single register
// over millions of papers would conflict on every parallel extraction, and
// removing a paper after a takedown would mean rewriting a file that everything
// else in the corpus points into.
func TagsPath(root string, id axid.ID) string {
	return path.Join(root, "tags", Shard(id), PathID(id)+".tags")
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

// GraphPath is the edge file for one month.
//
// Sharded by the subject's paper, so extracting one paper writes one file.
func GraphPath(root, shard string) string {
	return path.Join(root, "graph", shard+".jsonl")
}

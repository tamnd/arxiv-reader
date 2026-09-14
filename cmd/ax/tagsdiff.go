package main

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/fetch"
	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/latexml"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/tags"
)

// tagsDiff matches two versions of a paper against each other and says which
// pass decided what.
//
// This is the command 03-tags.md section 6 asks for, and it exists so that the
// decision a machine made about a permanent name is one a person can read and
// argue with before it is written down. With -w it carries the register onto the
// new version, which is the only way a tag assigned against v2 keeps naming the
// same theorem in v3.
//
// Both versions are read out of work/, which is where every conversion and every
// rendering is kept with its version in the name. That is the reason they are
// kept: the content plane holds one extraction of a paper and a comparison needs
// two.
func tagsDiff(args []string) error {
	// The identifier comes off the front before the flags are parsed, because
	// 03-tags.md writes this command with the identifier first and Go's flag
	// package stops reading flags at the first argument that is not one. A
	// person typing the command the way it is documented should not have to find
	// that out.
	var ref string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		ref, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("ax tags diff", flag.ContinueOnError)
	from := fs.String("from", "", "the version the register was last assigned against, so v2")
	to := fs.String("to", "", "the version now in the corpus, so v3")
	write := fs.Bool("w", false, "carry the register onto the new version and tombstone what is gone")
	force := fs.Bool("force", false, "write even when too much of the paper fell through")
	all := fs.Bool("v", false, "print the label and number matches as well")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if ref == "" {
		if len(rest) != 1 {
			return errors.New("usage: ax tags diff <id> -from v2 -to v3")
		}
		ref, rest = rest[0], nil
	}
	if len(rest) > 0 {
		return fmt.Errorf("this compares two versions of one paper and %s is a second one", rest[0])
	}
	id, err := axid.Parse(ref)
	if err != nil {
		return err
	}
	older, err := version(*from)
	if err != nil {
		return err
	}
	newer, err := version(*to)
	if err != nil {
		return err
	}
	if older >= newer {
		return fmt.Errorf("v%d is not older than v%d, and this compares an earlier version against a later one", older, newer)
	}
	plane := metadata.Plane{Root: corpusRoot()}
	was, err := objectsAt(plane.Root, id, older)
	if err != nil {
		return err
	}
	now, err := objectsAt(plane.Root, id, newer)
	if err != nil {
		return err
	}
	d := tags.Match(was, now)
	reportDiff(id, older, newer, len(was), d, *all)
	if !*write {
		return nil
	}
	if err := d.Err(); err != nil && !*force {
		return err
	}
	// The gate, at the point something is about to be written into the corpus,
	// the same as everywhere else.
	rec, err := record(plane, id)
	if err != nil {
		return err
	}
	if err := fetch.Gate(rec, newer); err != nil {
		return err
	}
	path := corpus.TagsPath(plane.Root, id)
	old, err := tags.LoadRegister(path)
	if err != nil {
		return err
	}
	// Carrying a register from a version it was not assigned against would point
	// every tag in it at whatever happens to have that identifier now, which is
	// the failure this command exists to prevent rather than one to commit.
	if old.Version != 0 && old.Version != older && !*force {
		return fmt.Errorf("the register of %s was assigned against v%d and this would carry it from v%d, so run it with -from v%d", id.Canonical, old.Version, older, old.Version)
	}
	m := old.Move(d, newer)
	changed, err := m.Register.Save(path)
	if err != nil {
		return err
	}
	for _, local := range m.Stray {
		fmt.Fprintf(os.Stderr, "%s: the register names %s and neither version of the paper has it, so its tag was left alone\n", id.Canonical, local)
	}
	fmt.Fprintf(os.Stderr, "%s carried and %s tombstoned on %s, %d unmoved%s\n",
		prose.Count(len(m.Carried), "tag"), prose.Count(len(m.Buried), "tag"), id.Canonical, m.Kept, unchanged(changed))
	return nil
}

// version reads a version off the command line, written either way round.
//
// The spec writes the flags as -from v2 and a person typing -from 2 means the
// same thing, so both are read rather than one of them being an error about
// punctuation.
func version(s string) (int, error) {
	if s == "" {
		return 0, errors.New("usage: ax tags diff <id> -from v2 -to v3")
	}
	var n int
	if _, err := fmt.Sscanf(strings.TrimPrefix(s, "v"), "%d", &n); err != nil || n < 1 {
		return 0, fmt.Errorf("%q is not a version, and a version is written v2", s)
	}
	return n, nil
}

// objectsAt is one version of a paper as the list of objects in it.
//
// The same split the content plane is written from, run over the cached document
// and thrown away. Nothing is written, because the point of this command is to
// look at a version that is not the one the corpus is holding.
func objectsAt(root string, id axid.ID, v int) ([]tags.Object, error) {
	p, err := paperAt(root, id, v)
	if err != nil {
		return nil, err
	}
	files, err := extract.Files(p, extract.Front{}, nil)
	if err != nil {
		return nil, fmt.Errorf("v%d of %s: %w", v, id.Canonical, err)
	}
	var out []tags.Object
	for _, f := range files {
		// A top level section is an object whose heading is in the front matter,
		// so it has no attribute block of its own and its text is whatever comes
		// before the first thing inside it.
		if f.Doc.Front.LocalID != "" && f.Doc.Front.Kind != "front" {
			out = append(out, tags.Object{
				File:  f.Name,
				Local: f.Doc.Front.LocalID,
				Class: f.Doc.Front.Kind,
				Label: f.Doc.Front.Label,
				Text:  f.Doc.Front.SectionTitle + " " + tags.Lead(f.Doc.Body),
			})
		}
		out = append(out, tags.Objects(f.Name, f.Doc.Body)...)
	}
	return out, nil
}

// paperAt reads one version of a paper out of the cache.
//
// The conversion first and the rendering second, because a conversion is this
// project's own reading of the author's files and carries the labels pass one
// wants, and a rendering is arXiv's reading of them and carries none. A paper
// compared on the render path is a paper matched by number, content and
// sequence, which works and is weaker.
func paperAt(root string, id axid.ID, v int) (*extract.Paper, error) {
	converted := corpus.ConvertedPath(root, id, v)
	if b, err := os.ReadFile(converted); err == nil {
		p, err := extract.Parse(b, id.Canonical, v)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", converted, err)
		}
		x, err := os.ReadFile(latexml.XMLPath(converted))
		if err != nil {
			return p, nil
		}
		byID, err := extract.Labels(x)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: the conversion's XML does not read, so v%d carries no labels: %v\n", id.Canonical, v, err)
			return p, nil
		}
		p.Label(byID)
		return p, nil
	}
	rendered := corpus.RenderPath(root, id, v)
	b, err := os.ReadFile(rendered)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("v%d of %s is not in the cache, so neither %s nor %s is there, and comparing two versions needs both of them fetched first", v, id.Canonical, converted, rendered)
	}
	if err != nil {
		return nil, err
	}
	p, err := extract.Parse(b, id.Canonical, v)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rendered, err)
	}
	return p, nil
}

// reportDiff prints the four passes' decisions.
//
// The counts first, then the decisions worth arguing with, which are every
// object that moved, every match the first two passes could not make, and
// everything left over at the end. An object that a label or a number match put
// back exactly where it was is not a decision anybody needs to read, and there
// are hundreds of those, so they are counted and printed only when asked for.
func reportDiff(id axid.ID, from, to, was int, d tags.Diff, all bool) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "%s\tv%d to v%d, %d objects to %d\n", id.Canonical, from, to, was, d.Total)
	for _, p := range []tags.Pass{tags.ByLabel, tags.ByNumber, tags.ByContent, tags.BySequence} {
		fmt.Fprintf(tw, "  %s\t%d\n", p, d.Count(p))
	}
	fmt.Fprintf(tw, "  new\t%d\n", len(d.Added))
	fmt.Fprintf(tw, "  gone\t%d\n", len(d.Gone))
	fmt.Fprintf(tw, "  fell through\t%d per cent\n", int(math.Round(d.Fell()*100)))
	for _, p := range d.Pairs {
		read := p.Pass == tags.ByLabel || p.Pass == tags.ByNumber
		if !all && read && p.Old.Local == p.New.Local {
			continue
		}
		fmt.Fprintf(tw, "  %s\t%s\n", p.Pass, moved(p))
	}
	for _, o := range d.Added {
		fmt.Fprintf(tw, "  new\t%s is in v%d and nothing in v%d is it\n", o.Local, to, from)
	}
	for _, o := range d.Gone {
		fmt.Fprintf(tw, "  gone\t%s was in v%d and nothing in v%d is it\n", o.Local, from, to)
	}
	tw.Flush()
}

// moved says where one matched object went, and says so plainly when it did not
// go anywhere.
func moved(p tags.Pair) string {
	if p.Old.Local == p.New.Local {
		return p.Old.Local + " stays"
	}
	return p.Old.Local + " becomes " + p.New.Local
}

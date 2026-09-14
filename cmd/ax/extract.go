package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/fetch"
)

func runExtract(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: ax extract render -n <id>v<n> [...]")
	}
	switch args[0] {
	case "render":
		return extractRender(args[1:])
	default:
		return fmt.Errorf("unknown extract subcommand %q, and render is the only path that is built", args[0])
	}
}

// extractRender reads a cached rendering and says what is in it.
//
// Reading only, for now. The writer is the next change, and the two are
// separate because the reject rule sits between them: a rendering LaTeXML could
// not finish is thrown away and the paper falls through to the source path, and
// a writer that had already put half a paper into the content plane would have
// to take it out again.
func extractRender(args []string) error {
	fs := flag.NewFlagSet("ax extract render", flag.ContinueOnError)
	dry := fs.Bool("n", false, "report what the rendering holds and write nothing")
	outline := fs.Bool("outline", false, "print every heading and block, and not just the counts")
	if err := fs.Parse(args); err != nil {
		return err
	}
	refs := fs.Args()
	if len(refs) == 0 {
		return errors.New("usage: ax extract render -n <id>v<n> [...]")
	}
	if !*dry {
		return errors.New("ax extract render writes nothing yet: the reader is here and the writer is the next change, so pass -n to see what a rendering holds")
	}

	root := corpusRoot()
	manifest, err := fetch.Load(corpus.SourcesPath(root))
	if err != nil {
		return err
	}

	rejected := 0
	for _, ref := range refs {
		p, err := readRendering(root, manifest, ref)
		if err != nil {
			return err
		}
		report(p, *outline)
		if r, bad := p.Reject(); bad {
			rejected++
			fmt.Fprintf(os.Stderr, "%sv%d: %v\n", p.ID, p.Version, r)
			for _, f := range r.Faults {
				fmt.Fprintf(os.Stderr, "  at %s: %s\n", f.Where, f.Text)
			}
		}
	}
	if rejected > 0 {
		return fmt.Errorf("%d of %d renderings are too broken to use, and those papers are on the source path", rejected, len(refs))
	}
	return nil
}

// readRendering finds a cached rendering and parses it.
//
// The manifest and not the disk says where the file is. A rendering nobody
// recorded is a rendering nobody knows the licence of, and reading one off the
// disk because it happens to be there is how a corpus ends up publishing
// something it was never given.
func readRendering(root string, m fetch.Manifest, ref string) (*extract.Paper, error) {
	id, err := axid.Parse(ref)
	if err != nil {
		return nil, err
	}
	if id.Version < 1 {
		return nil, fmt.Errorf("%s names no version, and a rendering is of one version", ref)
	}
	entry, ok := m.Find(id.Canonical, id.Version, fetch.RouteRender)
	if !ok {
		return nil, fmt.Errorf("the manifest has no rendering of %sv%d, so run ax fetch render %sv%d first", id.Canonical, id.Version, id.Canonical, id.Version)
	}
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(entry.Path)))
	if err != nil {
		return nil, fmt.Errorf("%s is in the manifest but not on disk, so run ax fetch render %s again: %w", entry.Ref(), entry.Ref(), err)
	}
	p, err := extract.Parse(body, id.Canonical, id.Version)
	if err != nil {
		return nil, err
	}
	// arXiv serves /html/<id>v<n> and a request for a version it has no
	// rendering of can land on a different one. Extracting v4 into a corpus
	// that decided v1 was the cc-by version would be republishing something
	// nobody was given the right to republish, so this stops rather than
	// reports.
	if got := p.StampVersion(); got != 0 && got != id.Version {
		return nil, fmt.Errorf("the rendering cached for %s says it is of v%d, so either arXiv served a different version or the file has been swapped: %q", entry.Ref(), got, p.Stamp)
	}
	return p, nil
}

func report(p *extract.Paper, outline bool) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "%sv%d\t%s\n", p.ID, p.Version, p.Stamp)
	fmt.Fprintf(tw, "  licence\t%s\n", p.Licence)
	fmt.Fprintf(tw, "  title\t%s\n", p.Title)
	fmt.Fprintf(tw, "  authors\t%s\n", authorNames(p.Authors))
	fmt.Fprintf(tw, "  abstract\t%d block\n", len(p.Abstract))
	fmt.Fprintf(tw, "  headings\t%d\n", p.Headings())
	fmt.Fprintf(tw, "  blocks\t%s\n", counted(p.Counts()))
	fmt.Fprintf(tw, "  unparsed\t%d\n", p.Unparsed)
	fmt.Fprintf(tw, "  faults\t%s\n", where(p.Faults))
	tw.Flush()
	if outline {
		printOutline(p)
	}
}

func authorNames(as []extract.Author) string {
	var out []string
	for _, a := range as {
		out = append(out, a.Name)
	}
	if len(out) == 0 {
		return "none in the rendering, and the metadata plane has the list that counts"
	}
	return strings.Join(out, ", ")
}

func counted(counts map[extract.Kind]int) string {
	var kinds []string
	for k := range counts {
		kinds = append(kinds, string(k))
	}
	sort.Strings(kinds)
	var out []string
	for _, k := range kinds {
		out = append(out, fmt.Sprintf("%d %s", counts[extract.Kind(k)], k))
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, ", ")
}

// where says how many conversion errors there are and which ones matter.
//
// The count on its own says nothing. One in the bibliography is a reference
// that prints badly and one in a section body is a sentence of the paper that
// is gone, so the two are counted apart.
func where(faults []extract.Fault) string {
	if len(faults) == 0 {
		return "none"
	}
	body := 0
	for _, f := range faults {
		if f.Where != "bibliography" {
			body++
		}
	}
	return fmt.Sprintf("%d, %d of them inside the body of the paper", len(faults), body)
}

func printOutline(p *extract.Paper) {
	var blocks func(bs []extract.Block, pad string)
	blocks = func(bs []extract.Block, pad string) {
		for _, b := range bs {
			fmt.Printf("%s%s\n", pad, describe(b))
			blocks(b.Blocks, pad+"  ")
		}
	}
	var sections func(ss []extract.Section, pad string)
	sections = func(ss []extract.Section, pad string) {
		for _, s := range ss {
			fmt.Printf("%s%s %s [%s]\n", pad, s.Tag, s.Title, s.ID)
			blocks(s.Blocks, pad+"  ")
			sections(s.Sections, pad+"  ")
		}
	}
	fmt.Println()
	blocks(p.Abstract, "  ")
	sections(p.Sections, "")
}

// describe is one line about a block, short enough to read a whole paper's
// worth of them.
func describe(b extract.Block) string {
	var about string
	switch {
	case len(b.Rows) > 0:
		about = fmt.Sprintf("%d rows", len(b.Rows))
	case len(b.Images) > 0:
		about = fmt.Sprintf("%d images", len(b.Images))
	case len(b.Items) > 0:
		about = fmt.Sprintf("%d items", len(b.Items))
	case b.Caption != "":
		about = b.Caption
	case b.Text == "" && len(b.Blocks) > 0:
		about = fmt.Sprintf("%d nested", len(b.Blocks))
	default:
		about = b.Text
	}
	// A theorem prints as whatever the author called it, so a paper full of
	// lemmas and claims reads as one rather than as a column of the word
	// theorem.
	kind := string(b.Kind)
	if b.Env != "" {
		kind = b.Env
	}
	return fmt.Sprintf("%-10s %-8s %s", kind, b.Tag, clip(about, 96))
}

func clip(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

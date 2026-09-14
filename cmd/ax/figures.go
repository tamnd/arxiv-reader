package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/fetch"
	"github.com/tamnd/arxiv-reader/figures"
	"github.com/tamnd/arxiv-reader/internal/prose"
)

// runFigures is the figure gate, and for now it is only the gate.
//
// Downloading the pictures, converting them and committing them comes next.
// The order is deliberate: rule F09 fails a build where a suspected figure's
// bytes are committed, so the rules that decide what may be committed have to
// exist, and have to be tested, before the first figure does.
func runFigures(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: ax figures -n <id>v<n> [...], or ax figures check <file> [...]")
	}
	if args[0] == "check" {
		return figuresCheck(args[1:])
	}
	return figuresPaper(args)
}

// figuresPaper reads a cached rendering and says what it would do with each of
// the paper's pictures.
//
// The captions are in the rendering and the pictures are not, so this answers
// the half of rule F09 that needs no bandwidth at all. Running it before a
// fetch says which figures are going to be withheld and why, which is worth
// knowing before spending an hour of paced requests on them.
func figuresPaper(args []string) error {
	fs := flag.NewFlagSet("ax figures", flag.ContinueOnError)
	dry := fs.Bool("n", false, "report what the captions say and download nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	refs := fs.Args()
	if len(refs) == 0 {
		return errors.New("usage: ax figures -n <id>v<n> [...]")
	}
	if !*dry {
		return errors.New("only ax figures -n is written, because the rules that decide what may be committed have to be in place before anything is")
	}

	root := corpusRoot()
	manifest, err := fetch.Load(corpus.SourcesPath(root))
	if err != nil {
		return err
	}
	for _, ref := range refs {
		id, err := axid.Parse(ref)
		if err != nil {
			return err
		}
		p, _, err := readRendering(root, manifest, id, ref)
		if err != nil {
			return err
		}
		reportCaptions(p)
	}
	return nil
}

// picture is one figure as the rendering has it, before anything is downloaded.
type picture struct {
	// name is what the paper calls it, so "Figure 4" or "Figure 4 panel 2" for
	// one of the subfigures under it.
	name   string
	files  []string
	sus    figures.Suspicion
	reason string
}

func reportCaptions(p *extract.Paper) {
	var found []picture
	// A figure of six subfigures is one number in the paper and six files on
	// disk, and each of those files is a separate decision about who owns it. A
	// panel carries its own caption, which is where an author writes the credit,
	// so the panel is what is read and the parent is only what it is called.
	var walk func(bs []extract.Block, parent string)
	walk = func(bs []extract.Block, parent string) {
		for i, b := range bs {
			name := parent
			if b.Kind == extract.KindFigure {
				switch {
				case b.Tag != "":
					name = "Figure " + b.Tag
				case parent != "":
					name = fmt.Sprintf("%s panel %d", parent, i+1)
				default:
					name = "an unnumbered figure"
				}
			}
			if b.Kind == extract.KindFigure && len(b.Images) > 0 {
				pic := picture{name: name}
				for _, im := range b.Images {
					pic.files = append(pic.files, im.Src)
				}
				pic.sus, pic.reason = figures.InspectCaption(b.Caption)
				found = append(found, pic)
			}
			walk(b.Blocks, name)
		}
	}
	var sections func([]extract.Section)
	sections = func(ss []extract.Section) {
		for _, s := range ss {
			walk(s.Blocks, "")
			sections(s.Sections)
		}
	}
	sections(p.Sections)

	withheld := 0
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "%sv%d\t%s with a picture in it\n", p.ID, p.Version, prose.Count(len(found), "figure"))
	for _, pic := range found {
		files := strings.Join(pic.files, ", ")
		if pic.sus == figures.Owned {
			fmt.Fprintf(tw, "  %s\towned\t%s\n", pic.name, files)
			continue
		}
		withheld++
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", pic.name, string(pic.sus), pic.reason)
	}
	tw.Flush()
	if withheld > 0 {
		fmt.Printf("%s withheld by rule F09, and the caption, the number and the tag are published anyway\n", prose.Count(withheld, "figure"))
	}
}

// figuresCheck reads figure files off disk and prints the verdict on each.
//
// The gate on its own, over files somebody already has. It is what the tests in
// CI run, and it is the answer to the question a person actually asks about a
// picture, which is whether this corpus is allowed to publish it.
func figuresCheck(args []string) error {
	fs := flag.NewFlagSet("ax figures check", flag.ContinueOnError)
	caption := fs.String("caption", "", "the caption the figure carries, which is what rule F09 reads")
	if err := fs.Parse(args); err != nil {
		return err
	}
	paths := fs.Args()
	if len(paths) == 0 {
		return errors.New("usage: ax figures check [-caption <text>] <file> [...]")
	}
	refused := 0
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		im, err := figures.Read(b)
		if err != nil {
			return err
		}
		d := figures.Decide(*caption, im)
		fmt.Fprintf(tw, "%s\t%s\t%d by %d\t%s\n", path, im.Format, im.Width, im.Height, prose.Bytes(im.Bytes))
		if im.Measured() {
			fmt.Fprintf(tw, "  page\t%s of a page\tat %.2f by %.2f inches, from %s\n", prose.Percent(d.PageFraction), im.WidthIn, im.HeightIn, im.Stated)
		} else {
			fmt.Fprintf(tw, "  page\tnot measured\tthe file states no resolution, so rule F06 has nothing to go on\n")
		}
		if d.Commit {
			fmt.Fprintf(tw, "  verdict\tcommit\t\n")
			continue
		}
		refused++
		fmt.Fprintf(tw, "  verdict\twithhold\t%s: %s\n", d.Rule, d.Why)
	}
	tw.Flush()
	if refused > 0 {
		return fmt.Errorf("%d of %s may not be committed", refused, prose.Count(len(paths), "file"))
	}
	return nil
}

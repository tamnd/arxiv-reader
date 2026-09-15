package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/fetch"
	"github.com/tamnd/arxiv-reader/figures"
	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/metadata"
)

// runFigures decides which of a paper's pictures may be published, and puts
// the ones that may into the corpus.
//
// The gate came first and the fetching second, deliberately: rule F09 fails a
// build where a suspected figure's bytes are committed, so the rules that
// decide what may be committed had to exist, and had to be tested, before the
// first figure did.
func runFigures(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: ax figures <id>v<n> [...], or ax figures check <file> [...], or ax figures tikz <id>v<n> [...]")
	}
	switch args[0] {
	case "check":
		return figuresCheck(args[1:])
	case "tikz":
		return figuresTikZ(args[1:])
	}
	return figuresPaper(args)
}

// figuresPaper reads a cached rendering and deals with the paper's pictures.
//
// With -n it says what it would do and downloads nothing. The captions are in
// the rendering and the pictures are not, so the dry run answers the half of
// rule F09 that needs no bandwidth at all, and knowing which figures are going
// to be withheld is worth having before spending an hour of paced requests on
// them.
//
// Without -n it fetches each picture, runs the whole gate over the bytes,
// commits the ones that pass and writes down what it decided about all of them.
// It does not touch the content plane. ax extract render reads the manifest and
// writes the image lines, because if this command edited the Markdown then the
// next extraction would put the arXiv paths back and the two would take turns
// undoing each other.
func figuresPaper(args []string) error {
	fs := flag.NewFlagSet("ax figures", flag.ContinueOnError)
	dry := fs.Bool("n", false, "report what the captions say and download nothing")
	recheck := fs.Bool("recheck", false, "fetch and decide again about pictures the manifest already has")
	from := fs.String("from", "", "read the pictures out of a directory instead of off the website")
	pace := fs.Duration("pace", fetch.Pace, "the gap to leave between requests")
	if err := fs.Parse(args); err != nil {
		return err
	}
	refs := fs.Args()
	if len(refs) == 0 {
		return errors.New("usage: ax figures <id>v<n> [...], or -n to decide without downloading")
	}
	if *pace < fetch.Pace {
		return fmt.Errorf("a pace of %s is faster than the fifteen seconds arXiv asks for on the website, and a figure comes off the website", *pace)
	}
	// Checked here rather than when the first picture is looked for, because a
	// run where every picture was already decided reads nothing and would say
	// nothing about a directory that is not there.
	if *from != "" {
		if info, err := os.Stat(*from); err != nil || !info.IsDir() {
			return fmt.Errorf("%s is not a directory to read pictures out of", *from)
		}
	}

	plane := metadata.Plane{Root: corpusRoot()}
	sources, err := fetch.Load(corpus.SourcesPath(plane.Root))
	if err != nil {
		return err
	}
	f := &fetch.Fetcher{
		UserAgent: userAgent(),
		Pace:      *pace,
		Log:       func(ref string) { fmt.Fprintf(os.Stderr, "fetching %s\n", ref) },
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	for _, ref := range refs {
		id, err := axid.Parse(ref)
		if err != nil {
			return err
		}
		p, _, err := readRendering(plane.Root, sources, id, ref)
		if err != nil {
			return err
		}
		if *dry {
			reportCaptions(p)
			continue
		}
		if err := commitFigures(ctx, f, plane, id, p, order{from: *from, recheck: *recheck}); err != nil {
			return err
		}
	}
	return nil
}

// order is what a run was asked to do, kept together so that adding a flag does
// not mean threading another argument through three functions.
type order struct {
	from    string
	recheck bool
}

// commitFigures fetches one version's pictures, decides about each and writes
// the figure manifest.
//
// The manifest is saved even when the run fails partway. A run that stopped has
// still spent the requests, and a manifest that forgot them would spend them
// again on the next attempt, which is the one thing a fifteen second pace
// cannot afford.
func commitFigures(ctx context.Context, f *fetch.Fetcher, plane metadata.Plane, id axid.ID, p *extract.Paper, o order) error {
	// The gate runs here as well as in ax fetch. A figure is bytes going into
	// the corpus, the licence belongs to the version rather than to the paper,
	// and a command that writes bytes should not be trusting another command to
	// have checked.
	rec, err := record(plane, id)
	if err != nil {
		return err
	}
	if err := fetch.Gate(rec, p.Version); err != nil {
		return err
	}
	v, ok := rec.VersionAt(p.Version)
	if !ok {
		return fmt.Errorf("the metadata plane has no v%d of %s", p.Version, rec.ID)
	}

	file := corpus.FiguresPath(plane.Root, corpus.Shard(id))
	m, err := figures.Load(file)
	if err != nil {
		return err
	}
	tally, err := decideEach(ctx, f, &m, plane.Root, id, p, v.Licence, o)
	if tally.fetched > 0 {
		if werr := m.Save(file); werr != nil {
			return werr
		}
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%sv%d: %d committed, %d withheld, %d already decided, %s in %s\n",
		id.Canonical, p.Version, tally.committed, tally.withheld, tally.known, prose.Count(len(m.Figures), "figure"), file)
	return nil
}

type figureTally struct {
	fetched   int
	committed int
	withheld  int
	known     int
}

// decideEach walks the paper's pictures in the order they are printed in.
//
// One picture at a time and in order, rather than in parallel, because the pace
// is the point. Fifteen seconds between requests is not a rate that concurrency
// improves, it is a rate that concurrency breaks.
func decideEach(ctx context.Context, f *fetch.Fetcher, m *figures.Manifest, root string, id axid.ID, p *extract.Paper, licence corpus.Licence, o order) (figureTally, error) {
	var tally figureTally
	dir := corpus.FiguresDir(root, id)
	// A recheck decides again about every picture in the version, so the old
	// entries go. They are copied to one side first, because a picture whose
	// bytes did not change keeps the time it was first read, and that is what
	// makes a recheck that finds nothing new leave the manifest exactly as it
	// was. A committed file is not a place to record that somebody looked at it
	// again.
	prior := figures.Manifest{Figures: append([]figures.Figure(nil), m.Figures...)}
	if o.recheck {
		m.Forget(id.Canonical, p.Version)
	}
	// The index is what the cross paper half of rule F09 reads, and it is built
	// once from everything the shard already holds. Figures committed by this
	// run are added to it as they land, so a picture that appears twice in one
	// month under two licences is caught the second time it is seen.
	index := m.Index()
	// same is rule F05, the one that says no two figures in one paper have the
	// same bytes. The second copy is not committed again, it points at the file
	// the first one wrote, so the paper has one picture and two figure numbers
	// that resolve to it.
	same := map[string]string{}

	for _, pic := range pictures(p) {
		for _, src := range pic.files {
			was, had := prior.Find(id.Canonical, p.Version, src)
			if had && !o.recheck {
				tally.known++
				continue
			}
			body, err := readPicture(ctx, f, o.from, src, pic.name)
			if err != nil {
				return tally, err
			}
			tally.fetched++

			sum := figures.Digest(body)
			entry := figures.Figure{
				Paper:   id.Canonical,
				Version: p.Version,
				Number:  pic.tag,
				Source:  src,
				SHA256:  sum,
				Bytes:   len(body),
				Caption: pic.caption,
				Licence: licence,
				Fetched: time.Now().UTC().Truncate(time.Second),
			}
			if had && was.SHA256 == sum && !was.Fetched.IsZero() {
				entry.Fetched = was.Fetched
			}
			d, im := judge(body, pic.caption, sum, id.Canonical, string(licence), index)
			entry.Format, entry.Width, entry.Height = im.Format, im.Width, im.Height
			entry.PageFraction, entry.ThirdParty = d.PageFraction, d.Suspicion
			entry.Rule, entry.Why = d.Rule, d.Why
			if im.Measured() {
				entry.PageFrom = im.Stated
			}

			switch {
			case !d.Commit:
				// A picture that was committed by an earlier run and is
				// withheld by this one has to lose its bytes as well as its
				// manifest entry, or the audit fails on a file rule F09 says
				// may not be there.
				if err := os.Remove(filepath.Join(dir, path.Base(src))); err != nil && !errors.Is(err, fs.ErrNotExist) {
					return tally, err
				}
				tally.withheld++
			case same[sum] != "":
				entry.File = same[sum]
				tally.committed++
			default:
				name := path.Base(src)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return tally, err
				}
				if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
					return tally, err
				}
				entry.File = corpus.FigureFile(id, name)
				same[sum] = entry.File
				index[sum] = figures.Owner{Paper: id.Canonical, Licence: string(licence)}
				tally.committed++
			}
			m.Put(entry)
		}
	}
	return tally, nil
}

// judge runs the whole gate over one picture's bytes.
//
// A format this does not read is withheld rather than fatal. One figure saved
// as something unusual should cost the corpus that figure, and not the paper.
func judge(body []byte, caption, sum, paper, licence string, index map[string]figures.Owner) (figures.Decision, figures.Image) {
	im, err := figures.Read(body)
	if err != nil {
		d := figures.Decision{Commit: true, Suspicion: figures.Owned}
		return d.Withhold("", figures.Owned, "it is in a format this does not read yet, so nothing has measured it"), im
	}
	d := figures.Decide(caption, im)
	if !d.Commit {
		return d, im
	}
	if s, why := figures.Elsewhere(sum, paper, licence, index); s != figures.Owned {
		return d.Withhold("F09", s, why), im
	}
	return d, im
}

// readPicture gets one figure's bytes, off the website or out of a directory.
//
// The directory is for somebody who already has the source tarball unpacked,
// and it is what the tests use, because CI does not touch the network and a
// committing path nothing exercises is a committing path nobody has run.
func readPicture(ctx context.Context, f *fetch.Fetcher, from, src, name string) ([]byte, error) {
	if from != "" {
		b, err := os.ReadFile(filepath.Join(from, filepath.FromSlash(path.Base(src))))
		if err != nil {
			return nil, fmt.Errorf("%s is not in %s: %w", path.Base(src), from, err)
		}
		return b, nil
	}
	return f.Get(ctx, f.URL(src), name+" "+path.Base(src))
}

// picture is one figure as the rendering has it, before anything is downloaded.
type picture struct {
	// name is what the paper calls it, so "Figure 4" or "Figure 4 panel 2" for
	// one of the subfigures under it.
	name string
	// tag is the number the paper prints, empty for a panel the author did not
	// number, and it is what goes in the manifest.
	tag     string
	caption string
	files   []string
	sus     figures.Suspicion
	reason  string
}

// pictures is every figure in the paper that has a file, in printed order.
//
// A figure of six subfigures is one number in the paper and six files on disk,
// and each of those files is a separate decision about who owns it. A panel
// carries its own caption, which is where an author writes the credit, so the
// panel is what is read and the parent is only what it is called.
func pictures(p *extract.Paper) []picture {
	var found []picture
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
				pic := picture{name: name, tag: b.Tag, caption: b.Caption}
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
	// The abstract first, because a paper that opens with a teaser figure puts
	// it there and the front matter is written from it. Rule F01 found this:
	// KAN's Figure 0.1 was the one picture of the paper still pointing at
	// arXiv, because nothing here had ever offered it a decision.
	walk(p.Abstract, "")
	sections(p.Sections)
	return found
}

func reportCaptions(p *extract.Paper) {
	found := pictures(p)
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
		fmt.Fprintf(tw, "%s\t%s\t%d by %d\t%s\n", path, im.Format, im.Width, im.Height, prose.Bytes(int64(im.Bytes)))
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

// decided is what ax figures wrote down about one version's pictures, in the
// form the writer wants it.
//
// Keyed by the path the rendering gave, because that is what the extractor has
// in its hand when it comes to write an image line and it is the only thing the
// two commands agree on before a file has been committed.
func decided(root string, id axid.ID, version int) (extract.Pictures, error) {
	m, err := figures.Load(corpus.FiguresPath(root, corpus.Shard(id)))
	if err != nil {
		return nil, err
	}
	pics := extract.Pictures{}
	for _, f := range m.Figures {
		if f.Paper != id.Canonical || f.Version != version {
			continue
		}
		pics[f.Source] = extract.Picture{
			File:     f.File,
			Withheld: f.Why,
			At:       fetch.HTMLBase + f.Source,
		}
	}
	return pics, nil
}

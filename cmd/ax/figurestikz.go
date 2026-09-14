package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/fetch"
	"github.com/tamnd/arxiv-reader/figures"
	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/source"
	"github.com/tamnd/arxiv-reader/tikz"
)

// figuresTikZ compiles the pictures a submission describes rather than the ones
// arXiv rasterised.
//
// This is the best figure the pipeline can produce and the only one it makes
// itself. It is vector, it has real text in it, and it came from the author's
// description of the drawing. Every other picture in the corpus started as
// something arXiv had already turned into pixels.
//
// Nothing here touches the network. The drawings are in the e-print the fetch
// side already cached, which is also why this is a separate subcommand: ax
// figures reads a rendering and ax figures tikz reads a submission, and they are
// two different files about the same paper.
func figuresTikZ(args []string) error {
	fs := flag.NewFlagSet("ax figures tikz", flag.ContinueOnError)
	dry := fs.Bool("n", false, "find the drawings, say what they are, and compile nothing")
	from := fs.String("from", "", "read the submission out of a directory instead of unpacking the cached e-print")
	named := fs.String("main", "", "the file the preamble comes from, for a submission that holds more than one document")
	recheck := fs.Bool("recheck", false, "compile and decide again about drawings the manifest already has")
	budget := fs.Duration("timeout", tikz.Timeout, "how long one drawing gets before it is stopped")
	bin := fs.String("latex", tikz.LaTeX, "the TeX to run, for a machine with more than one or with it somewhere unusual")
	svg := fs.String("dvisvgm", tikz.DVISVGM, "the dvisvgm to run, which ships with the same install")
	quiet := fs.Bool("q", false, "do not print what TeX says while it compiles")
	if err := fs.Parse(args); err != nil {
		return err
	}
	refs := fs.Args()
	if len(refs) == 0 {
		return errors.New("usage: ax figures tikz [-n] <id>v<n> [...]")
	}
	if len(refs) > 1 && (*named != "" || *from != "") {
		return errors.New("-main and -from both name one submission, so they take one paper at a time")
	}
	if *from != "" {
		if info, err := os.Stat(*from); err != nil || !info.IsDir() {
			return fmt.Errorf("%s is not a directory to read a submission out of", *from)
		}
	}
	// Every reference is read before TeX is looked for, so that a typo is a
	// refusal about the typo rather than a refusal about the machine.
	ids := make([]axid.ID, len(refs))
	for i, ref := range refs {
		id, err := axid.Parse(ref)
		if err != nil {
			return err
		}
		if id.Version < 1 {
			return fmt.Errorf("%s names no version, and a drawing is in one version of a submission", ref)
		}
		ids[i] = id
	}

	c := &tikz.Compiler{LaTeX: *bin, DVISVGM: *svg, Timeout: *budget}
	if !*quiet {
		c.Log = func(line string) { fmt.Fprintln(os.Stderr, line) }
	}
	// A dry run asks nothing of TeX, which is the point of it: knowing which
	// drawings a paper has is worth having on a machine that cannot compile them.
	if !*dry {
		if err := c.Available(); err != nil {
			return err
		}
	}

	root := corpusRoot()
	plane := metadata.Plane{Root: root}
	sources, err := fetch.Load(corpus.SourcesPath(root))
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	for i, id := range ids {
		ref := refs[i]
		dir, main, err := submission(root, sources, id, ref, *from, *named)
		if err != nil {
			return err
		}
		found, preamble, err := drawings(dir, main)
		if err != nil {
			return err
		}
		if len(found) == 0 {
			fmt.Fprintf(os.Stderr, "%s: %s\n", ref, nothingToDraw(dir))
			continue
		}
		if *dry {
			reportDrawings(ref, found)
			continue
		}
		if err := commitDrawings(ctx, c, plane, id, dir, preamble, found, *recheck); err != nil {
			return err
		}
	}
	return nil
}

// submission is the directory the drawings are compiled in, and the file the
// preamble comes out of.
//
// The cached e-print is unpacked again rather than reused where it sits, for the
// reason ax extract source unpacks it again: the bytes are a hash in the manifest
// and the files under work/ are not, so a submission somebody has edited by hand
// would otherwise be compiled as though it were what arXiv served.
func submission(root string, m fetch.Manifest, id axid.ID, ref, from, named string) (dir, main string, err error) {
	if from != "" {
		return from, named, nil
	}
	entry, ok := m.Find(id.Canonical, id.Version, fetch.RouteSource)
	if !ok {
		return "", "", fmt.Errorf("the manifest has no e-print of %s, so run ax fetch source %s first", ref, ref)
	}
	bundle, err := source.Read(filepath.Join(root, filepath.FromSlash(entry.Path)))
	if err != nil {
		return "", "", fmt.Errorf("%s: %w", entry.Ref(), err)
	}
	if named == "" {
		named, err = bundle.Main()
		if err != nil {
			return "", "", fmt.Errorf("%s: %w, which -main does", entry.Ref(), err)
		}
	} else if _, ok := bundle.Find(named); !ok {
		return "", "", fmt.Errorf("%s holds no %s, and it holds %v", entry.Ref(), named, bundle.Names())
	}
	dir = corpus.EPrintDir(root, id, id.Version)
	if err := os.RemoveAll(dir); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	if err := bundle.Write(dir); err != nil {
		return "", "", err
	}
	return dir, named, nil
}

// drawing is one picture found in a submission, and which file it came out of.
type drawing struct {
	// file is where the picture is, relative to the submission, so a message
	// about it names somewhere the author would recognise.
	file string
	// nth is which drawing it is in that file, one based.
	nth int
	pic tikz.Picture
}

// source is what the manifest calls a drawing.
//
// There is no path on arXiv to name it by, because the picture is not a file: it
// is a few lines in the middle of the author's TeX. The file and the line are
// what somebody would need to go and look at it, and they are the same on a
// re-run over the same submission, which is what the manifest needs of a key.
func (d drawing) source() string {
	return fmt.Sprintf("tikz:%s:%d", filepath.ToSlash(d.file), d.pic.Line)
}

// name is the file the picture is committed to.
//
// Named for the file it was drawn in rather than for its number, because the
// number of a figure is a TeX counter and a source file does not know it. A
// submission's own file names are what the author chose, so the separators go
// and everything else stays.
func (d drawing) name() string {
	stem := strings.TrimSuffix(filepath.ToSlash(d.file), ".tex")
	stem = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_':
			return r
		}
		return '-'
	}, stem)
	return fmt.Sprintf("tikz-%s-%d.svg", strings.Trim(stem, "-"), d.nth)
}

// drawings is every picture in a submission, and the preamble to compile them
// with.
//
// Every file and not only the main one, because an author who has fifty figures
// keeps them in fifty files and \input them, and a run that read the main
// document alone would find none of them. The preamble comes from the main
// document either way: an included file has no preamble of its own and the
// macros a drawing uses were defined in the one that includes it.
func drawings(dir, main string) ([]drawing, string, error) {
	names, err := texFiles(dir)
	if err != nil {
		return nil, "", err
	}
	if main == "" {
		main = mainDocument(dir, names)
	}
	var preamble string
	if main != "" {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(main)))
		if err != nil {
			return nil, "", err
		}
		preamble = tikz.Preamble(string(b))
	}
	// The main document first and then the rest by name. A submission spreads its
	// drawings over as many files as the author felt like and there is no reading
	// order to recover from a directory listing, so this is an order rather than
	// the order.
	var found []drawing
	for _, name := range append([]string{main}, without(names, main)...) {
		if name == "" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			return nil, "", err
		}
		for i, p := range tikz.Pictures(string(b)) {
			found = append(found, drawing{file: name, nth: i + 1, pic: p})
		}
	}
	return found, preamble, nil
}

// texFiles is every TeX file in a submission, by name, deepest last.
func texFiles(dir string) ([]string, error) {
	var names []string
	err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() || strings.ToLower(filepath.Ext(p)) != ".tex" {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		names = append(names, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}

// document is the file the paper starts from, for a submission read out of a
// directory rather than out of an e-print.
//
// A bundle knows its own main file and a directory does not, so this is the same
// question answered from less: a document has a class and a body, and an included
// file has neither.
func mainDocument(dir string, names []string) string {
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			continue
		}
		s := string(b)
		if strings.Contains(s, `\documentclass`) && strings.Contains(s, `\begin{document}`) {
			return name
		}
	}
	return ""
}

func without(names []string, name string) []string {
	out := names[:0:0]
	for _, n := range names {
		if n != name {
			out = append(out, n)
		}
	}
	return out
}

// nothingToDraw says which kind of nothing this is.
//
// A paper that never loads TikZ and a paper that loads it and draws nothing with
// it are different facts, and the second one is the one worth looking at, because
// it is usually a drawing written with the \tikz shorthand rather than as an
// environment and this does not read those yet.
func nothingToDraw(dir string) string {
	names, err := texFiles(dir)
	if err != nil {
		return "there is nothing here to compile"
	}
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err == nil && tikz.Uses(string(b)) {
			return "it loads TikZ and has no tikzpicture environment in it, so there is nothing here to compile"
		}
	}
	return "it draws none of its own figures"
}

func reportDrawings(ref string, found []drawing) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "%s\t%s of its own\n", ref, prose.Count(len(found), "drawing"))
	for _, d := range found {
		where := fmt.Sprintf("%s line %d", d.file, d.pic.Line)
		switch {
		case d.pic.Label != "":
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", where, d.pic.Label, d.pic.Caption)
		case d.pic.Caption != "":
			fmt.Fprintf(tw, "  %s\tno label\t%s\n", where, d.pic.Caption)
		default:
			fmt.Fprintf(tw, "  %s\tno label\tdrawn in the prose, so the paper gives it no caption and no number\n", where)
		}
	}
	tw.Flush()
}

// drawTally is what one version's drawings came to.
type drawTally struct {
	committed int
	withheld  int
	known     int
	failed    int
}

func (t drawTally) wrote() bool { return t.committed+t.withheld > 0 }

// commitDrawings compiles a version's pictures, decides about each and writes the
// figure manifest.
//
// The same gate as every other picture. A drawing is the author's own by
// construction, which answers rule F09 about who owns it and answers nothing
// about the licence of the version it is in, and a figure is bytes going into the
// corpus either way.
func commitDrawings(ctx context.Context, c *tikz.Compiler, plane metadata.Plane, id axid.ID, dir, preamble string, found []drawing, recheck bool) error {
	rec, err := record(plane, id)
	if err != nil {
		return err
	}
	if err := fetch.Gate(rec, id.Version); err != nil {
		return err
	}
	v, ok := rec.VersionAt(id.Version)
	if !ok {
		return fmt.Errorf("the metadata plane has no v%d of %s", id.Version, rec.ID)
	}

	file := corpus.FiguresPath(plane.Root, corpus.Shard(id))
	m, err := figures.Load(file)
	if err != nil {
		return err
	}
	dest := corpus.FiguresDir(plane.Root, id)
	index := m.Index()
	// same is rule F05, the one that says no two figures in one paper have the
	// same bytes. A paper that draws the same picture twice is a paper with one
	// picture in it and two places it appears, so the second entry points at the
	// file the first one wrote.
	same := map[string]string{}
	var tally drawTally

	for _, d := range found {
		src := d.source()
		was, had := m.Find(id.Canonical, id.Version, src)
		if had && !recheck {
			tally.known++
			continue
		}
		entry, err := compileDrawing(ctx, c, dir, dest, preamble, d, id, v.Licence, index, same, was)
		if err != nil {
			// A drawing TeX will not compile costs the corpus that drawing and not
			// the paper. The rendering's version of the figure is already there,
			// so this path is the better copy of a picture and never the only one.
			tally.failed++
			fmt.Fprintf(os.Stderr, "%s: %v\n", src, err)
			continue
		}
		if entry.Committed() {
			tally.committed++
		} else {
			tally.withheld++
		}
		m.Put(entry)
	}
	if tally.wrote() {
		if err := m.Save(file); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "%sv%d: %s, %d committed, %d withheld, %d already decided, %d TeX would not draw\n",
		id.Canonical, id.Version, prose.Count(len(found), "drawing"), tally.committed, tally.withheld, tally.known, tally.failed)
	// One drawing TeX will not compile is that drawing's problem. Every drawing in
	// the paper failing is the installation's, and reporting it as a hundred
	// separate bad pictures would send somebody looking at the pictures.
	if tally.failed > 0 && !tally.wrote() {
		return fmt.Errorf("%s in %sv%d, and none of them compiled, which is usually a TeX installation that cannot build this paper rather than a paper full of bad drawings",
			prose.Count(len(found), "drawing"), id.Canonical, id.Version)
	}
	return nil
}

// compileDrawing turns one picture into a manifest entry, and into a file when it
// is allowed to be one.
func compileDrawing(ctx context.Context, c *tikz.Compiler, dir, dest, preamble string, d drawing, id axid.ID, licence corpus.Licence, index map[string]figures.Owner, same map[string]string, was figures.Figure) (figures.Figure, error) {
	compiled, err := c.Compile(ctx, dir, tikz.Standalone(preamble, d.pic.Body))
	if err != nil {
		return figures.Figure{}, err
	}
	shaped, err := figures.Scalable(compiled)
	if err != nil {
		return figures.Figure{}, err
	}
	// The picture is measured as it was compiled and committed as it will be
	// published. dvisvgm states the size in points, which is the size the drawing
	// would print at and is what rule F06 reads, and Scalable takes that off
	// because a size an author cannot override is what breaks a picture on an
	// EPUB reader. So the physical size comes from the one and the coordinates
	// and the bytes from the other, and both are true of the same drawing.
	im, err := figures.Read(shaped)
	if err != nil {
		return figures.Figure{}, err
	}
	if as, err := figures.Read(compiled); err == nil {
		im.WidthIn, im.HeightIn, im.Stated = as.WidthIn, as.HeightIn, as.Stated
	}

	sum := figures.Digest(shaped)
	entry := figures.Figure{
		Paper:   id.Canonical,
		Version: id.Version,
		Source:  d.source(),
		SHA256:  sum,
		Bytes:   len(shaped),
		Caption: d.pic.Caption,
		Licence: licence,
		Fetched: time.Now().UTC().Truncate(time.Second),
		Format:  im.Format,
		Width:   im.Width,
		Height:  im.Height,
	}
	// A drawing whose bytes did not change keeps the time it was first compiled,
	// which is what makes a recheck that finds nothing new leave the manifest
	// exactly as it was. A committed file is not a place to record that somebody
	// looked at it again.
	if was.SHA256 == sum && !was.Fetched.IsZero() {
		entry.Fetched = was.Fetched
	}
	dec := figures.Decide(d.pic.Caption, im)
	if dec.Commit {
		if s, why := figures.Elsewhere(sum, id.Canonical, string(licence), index); s != figures.Owned {
			dec = dec.Withhold("F09", s, why)
		}
	}
	entry.PageFraction, entry.ThirdParty = dec.PageFraction, dec.Suspicion
	entry.Rule, entry.Why = dec.Rule, dec.Why
	if im.Measured() {
		entry.PageFrom = im.Stated
	}
	if !dec.Commit {
		// A drawing an earlier run committed and this one withholds has to lose
		// its bytes as well as its entry, or the audit fails on a file rule F09
		// says may not be there.
		if err := os.Remove(filepath.Join(dest, d.name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return figures.Figure{}, err
		}
		return entry, nil
	}
	if at := same[sum]; at != "" {
		entry.File = at
		return entry, nil
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return figures.Figure{}, err
	}
	if err := os.WriteFile(filepath.Join(dest, d.name()), shaped, 0o644); err != nil {
		return figures.Figure{}, err
	}
	entry.File = corpus.FigureFile(id, d.name())
	same[sum] = entry.File
	index[sum] = figures.Owner{Paper: id.Canonical, Licence: string(licence)}
	return entry, nil
}

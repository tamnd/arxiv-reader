package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/fetch"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/pdftext"
	"github.com/tamnd/arxiv-reader/raster"
	"github.com/tamnd/arxiv-reader/vision"
)

// extractVision reads a paper by looking at pictures of its pages.
//
// The fourth path and the last one. A paper gets here when arXiv never rendered
// it, its submission is not TeX this project could convert, and its PDF has no
// text layer worth reading, which is a scan or a PDF whose glyphs carry no
// characters. Before this path existed those papers were simply missing, and most
// of them are from the 1990s.
//
// Three programs and a ladder. pdftotext is run first, for the page count and for
// whatever thin text layer the file does have, because the page count decides how
// much work there is and the text layer is what acceptance rule V09 measures the
// reading against. pdftoppm draws one page at a time. The reader is asked to write
// down what is on the picture, the nine rules in the vision package decide whether
// what came back is the page, and a page they refuse is drawn again at the next
// resolution on the ladder rather than published with a warning.
//
// Everything about the run is written down next to the readings, and the two
// things that matter about it, the model and the hash of the prompt, go into the
// front matter of every file. A page read by a model nobody can name under a
// prompt nobody kept is a page nobody can read again.
func extractVision(args []string) error {
	fs := flag.NewFlagSet("ax extract vision", flag.ContinueOnError)
	dry := fs.Bool("n", false, "read the pages, report what they hold, and write no content files")
	outline := fs.Bool("outline", false, "print every heading and block, and not just the counts")
	force := fs.Bool("force", false, "overwrite a file somebody has corrected by hand")
	lang := fs.String("lang", "en", "the language directory to write into")
	// No default, because a path that spends money on every page should make
	// somebody write down what it is spending it on.
	program := fs.String("reader", "", "the program that reads a page, run with the picture as its argument and the prompt on standard input")
	model := fs.String("model", "", "the model the reader is told to use, recorded in every file it produced")
	prompt := fs.String("prompt", "", "a file holding the prompt, for a run that is not using the built in one")
	ladder := fs.String("dpi", dots(vision.Ladder), "the resolutions to try, in order, and a page is only asked again at the next one if the rules refused it")
	budget := fs.Duration("timeout", vision.DefaultTimeout, "how long one page gets with the reader")
	paint := fs.Duration("paint-timeout", raster.Timeout, "how long one page gets with pdftoppm")
	pdftotextBinary := fs.String("pdftotext", pdftext.Binary, "the pdftotext to run, for the page count and the text layer")
	pdftoppmBinary := fs.String("pdftoppm", raster.Binary, "the pdftoppm to run, which draws the pages")
	// A page the rules refuse is usually a page nothing read. Sometimes it is a
	// blank page, or a page that is one full bleed photograph, and rule V01
	// cannot tell those from a failure. This is how a person says which it was,
	// after looking.
	anyway := fs.Bool("anyway", false, "keep a page the rules refused, for the blank page or the full page picture they cannot tell from a failure")
	// The cheap half of the path. Everything the rules ask is about text that is
	// already on the disk, so re-asking them costs nothing and no model is woken
	// up, which is what makes it safe to add a rule and run it over every paper
	// already read.
	recheck := fs.Bool("recheck", false, "put the rules to the readings already on disk, ask no model, and write no content files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	refs := fs.Args()
	if len(refs) == 0 {
		return errors.New("usage: ax extract vision -reader <program> -model <name> <id>v<n> [...]")
	}
	rungs, err := rungs(*ladder)
	if err != nil {
		return err
	}
	ids := make([]axid.ID, len(refs))
	for i, ref := range refs {
		id, err := axid.Parse(ref)
		if err != nil {
			return err
		}
		if id.Version < 1 {
			return fmt.Errorf("%s names no version, and a PDF is of one version", ref)
		}
		ids[i] = id
	}

	root := corpusRoot()
	plane := metadata.Plane{Root: root}
	manifest, err := fetch.Load(corpus.SourcesPath(root))
	if err != nil {
		return err
	}
	text := &pdftext.Reader{Binary: *pdftotextBinary}
	if err := text.Available(); err != nil {
		return err
	}
	painter := &raster.Painter{Binary: *pdftoppmBinary, Timeout: *paint}
	reader := &vision.Reader{Program: *program, Model: *model, Timeout: *budget}
	said := vision.DefaultPrompt
	if *prompt != "" {
		b, err := os.ReadFile(*prompt)
		if err != nil {
			return err
		}
		said = strings.TrimSpace(string(b))
		if said == "" {
			return fmt.Errorf("%s is empty, and an empty prompt is not a prompt", *prompt)
		}
	}
	reader.Prompt = said
	hash := vision.PromptSHA256(said)
	// A recheck reads what is on disk, so it needs neither the rasteriser nor the
	// reader, and demanding them would mean nobody could put a new rule to an old
	// paper without credentials for a service.
	if !*recheck {
		if err := painter.Available(); err != nil {
			return err
		}
		if err := reader.Available(); err != nil {
			return err
		}
		if *model == "" {
			return errors.New("no model was named, and a page read by a model the corpus cannot name is a page nobody can read again, so pass -model")
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	held := 0
	for i, id := range ids {
		p, made, err := readPages(ctx, pages{
			text: text, painter: painter, reader: reader, prompt: hash,
			root: root, manifest: manifest, ladder: rungs,
			anyway: *anyway, recheck: *recheck,
		}, id, refs[i])
		if err != nil {
			return err
		}
		reportVision(p, made, *outline)
		if len(made.missing) > 0 {
			held++
			// A recheck asked no model, so saying the pages could not be read would
			// be describing a run that never happened. What it found is that the
			// readings on disk no longer pass.
			if *recheck {
				fmt.Fprintf(os.Stderr, "%sv%d: %s no longer passes the rules, so the paper as it stands is short of those pages\n",
					id.Canonical, id.Version, printed(made.missing))
			} else {
				fmt.Fprintf(os.Stderr, "%sv%d: %s could not be read at %s, so the paper is short of those pages and is not being written\n",
					id.Canonical, id.Version, printed(made.missing), dots(rungs))
			}
			continue
		}
		if *dry || *recheck {
			continue
		}
		mark := func(f *extract.Front) {
			// The route the bytes came by is native, because the vision path reads
			// the PDF the native path reads. What read them is this path, and the
			// front matter has to say so or every reader of the corpus would be
			// told these paragraphs came off a text layer.
			f.Path = "vision"
			f.ExtractionModel = *model
			f.PromptSHA256 = hash
		}
		if err := writePaper(plane, id, p, made.entry, *lang, *force, mark); err != nil {
			return err
		}
	}
	if held > 0 {
		if *recheck {
			return fmt.Errorf("%d of %d papers have pages the rules now refuse, and a reading in the corpus that a rule objects to is something somebody has to look at", held, len(refs))
		}
		return fmt.Errorf("%d of %d papers have pages nothing could read, and a paper with a hole in it is not published", held, len(refs))
	}
	return nil
}

// pages is everything reading one paper's pages needs, which is enough things to
// be worth a name.
type pages struct {
	text     *pdftext.Reader
	painter  *raster.Painter
	reader   *vision.Reader
	prompt   string
	root     string
	manifest fetch.Manifest
	ladder   []int
	anyway   bool
	recheck  bool
}

// reading is what one paper's run did, for the report and for the decision about
// whether to write anything.
type reading struct {
	entry  fetch.Source
	record *vision.Record
	pages  int
	// read is pages this run read and asked is what the model was asked, which
	// are not the same number: a page the rules refused at three hundred dots is
	// one page and two asks, and it is the asks that were paid for.
	read    int
	asked   int
	reused  int
	missing []int
	took    time.Duration
}

// readPages reads every page of one paper and recovers the paper from the
// readings.
//
// The page loop is here rather than in the vision package because it is the
// command's business: it is where the record is kept, where the pictures are put,
// and where a page that failed the rules is given another rung of the ladder.
func readPages(ctx context.Context, in pages, id axid.ID, ref string) (*extract.Paper, reading, error) {
	out := reading{}
	entry, ok := in.manifest.Find(id.Canonical, id.Version, fetch.RouteNative)
	if !ok {
		return nil, out, fmt.Errorf("the manifest has no PDF of %sv%d, so run ax fetch native %sv%d first", id.Canonical, id.Version, id.Canonical, id.Version)
	}
	out.entry = entry
	pdf := filepath.Join(in.root, filepath.FromSlash(entry.Path))
	// pdftotext for the page count and the text layer, and its failure is fatal
	// rather than something to step over: a PDF poppler will not open is not a PDF
	// pdftoppm will draw either.
	doc, err := in.text.Read(ctx, pdf)
	if err != nil {
		return nil, out, err
	}
	out.pages = len(doc.Pages)
	if out.pages == 0 {
		return nil, out, fmt.Errorf("%s holds no pages, so there is nothing to look at", entry.Ref())
	}

	dir := corpus.VisionDir(in.root, id, id.Version)
	pics := corpus.PagesDir(in.root, id, id.Version)
	rec, err := vision.Load(dir)
	if err != nil {
		return nil, out, err
	}
	// A run under a different model or a different prompt is a different reading of
	// the paper, and keeping the old entries would leave the record claiming pages
	// were read by something that did not read them.
	if rec.Model != in.reader.Model || rec.Prompt != in.prompt {
		if len(rec.Pages) > 0 && !in.recheck {
			fmt.Fprintf(os.Stderr, "%s was last read by %s under prompt %s, and this run is %s under %s, so it is being read again\n",
				entry.Ref(), or(rec.Model, "nothing"), short(or(rec.Prompt, "nothing")), in.reader.Model, short(in.prompt))
			rec.Pages = nil
		}
	}
	if !in.recheck {
		rec.Paper, rec.Version = id.Canonical, id.Version
		rec.Model, rec.Prompt = in.reader.Model, in.prompt
		if v, err := in.painter.Version(ctx); err == nil {
			rec.Painter = v
		}
	}

	started := time.Now()
	readings := make([]string, out.pages)
	before := ""
	for page := 1; page <= out.pages; page++ {
		layer := ""
		if page-1 < len(doc.Pages) {
			layer = doc.Pages[page-1].Text
		}
		if in.recheck {
			kept, err := recheckPage(rec, dir, page, layer, before, in)
			if err != nil {
				return nil, out, err
			}
			readings[page-1] = kept
			before = kept
			continue
		}
		if kept, ok := rec.Done(dir, page, in.reader.Model, in.prompt); ok {
			out.reused++
			readings[page-1] = kept
			before = kept
			continue
		}
		kept, err := onePage(ctx, in, rec, pdf, dir, pics, page, layer, before, &out)
		if err != nil {
			return nil, out, err
		}
		readings[page-1] = kept
		before = kept
		// Written after every page, not at the end. This path is slow and paid
		// for, and a run interrupted on page thirty of forty has to leave thirty
		// pages behind that the next run does not pay for again.
		if err := rec.Save(dir); err != nil {
			return nil, out, err
		}
	}
	if in.recheck {
		if err := rec.Save(dir); err != nil {
			return nil, out, err
		}
	}
	out.took = time.Since(started)
	out.record = rec
	out.missing = rec.Missing(out.pages)
	return extract.Vision(readings, id.Canonical, id.Version), out, nil
}

// onePage climbs the ladder for one page and returns the reading that was
// accepted, or empty for a page nothing could read.
func onePage(ctx context.Context, in pages, rec *vision.Record, pdf, dir, pics string, page int, layer, before string, out *reading) (string, error) {
	e := vision.Entry{Page: page, Read: time.Now().UTC()}
	if held, ok := rec.Find(page); ok {
		// The resolutions this page has already been asked at, kept so that a
		// second run over a page that needed six hundred dots does not start at
		// three hundred again.
		e.Tried = held.Tried
	}
	for _, dpi := range in.ladder {
		picture, err := in.painter.Paint(ctx, pdf, page, dpi, pics)
		if err != nil {
			// A page poppler will not draw is not going to be drawn at a higher
			// resolution either, so the ladder stops here and the paper keeps its
			// hole.
			e.DPI, e.Refused = dpi, []string{"raster: " + err.Error()}
			rec.Put(e)
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return "", nil
		}
		out.asked++
		text, err := in.reader.Read(ctx, picture)
		if err != nil {
			// A reader that timed out or was refused is worth another rung, because
			// a rate limit is not a fact about the page. A reader that is not there
			// at all was caught before the loop started.
			e.DPI, e.Refused = dpi, []string{"reader: " + err.Error()}
			e.Tried = append(e.Tried, dpi)
			fmt.Fprintf(os.Stderr, "%v\n", err)
			continue
		}
		e.Tried = append(e.Tried, dpi)
		refused := vision.Ask(vision.Question{
			Page: page, Text: text, Layer: layer, Before: before, Prompt: in.reader.Prompt,
		})
		e.DPI = dpi
		if len(refused) == 0 || in.anyway {
			name, sum, err := vision.Write(dir, page, text)
			if err != nil {
				return "", err
			}
			e.File, e.SHA256, e.Chars = name, sum, len(text)
			e.Anyway = len(refused) > 0
			e.Refused = says(refused)
			rec.Put(e)
			out.read++
			for _, r := range refused {
				fmt.Fprintf(os.Stderr, "page %d was kept anyway: %s\n", page, r)
			}
			return text, nil
		}
		e.Refused = says(refused)
		for _, r := range refused {
			fmt.Fprintf(os.Stderr, "page %d at %d dots: %s\n", page, dpi, r)
		}
	}
	// Every rung refused, so the page is recorded as one nothing read. The entry
	// stays because the refusals are the only account of what was wrong, and a run
	// with no entry would ask again from the bottom of the ladder.
	e.File, e.SHA256 = "", ""
	rec.Put(e)
	return "", nil
}

// recheckPage puts the rules to a reading already on disk.
//
// It updates the record and asks nothing of any model, which is what makes adding
// a rule cheap: the rule is written, this is run over every paper already read, and
// what comes out is the list of pages the new rule objects to.
func recheckPage(rec *vision.Record, dir string, page int, layer, before string, in pages) (string, error) {
	e, ok := rec.Find(page)
	if !ok || e.File == "" {
		return "", nil
	}
	b, err := os.ReadFile(filepath.Join(dir, e.File))
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(b))
	if sum := vision.Hash(b); sum != e.SHA256 {
		fmt.Fprintf(os.Stderr, "page %d has been edited since it was read, so the record no longer speaks for it\n", page)
		e.SHA256 = sum
	}
	refused := vision.Ask(vision.Question{
		Page: page, Text: text, Layer: layer, Before: before, Prompt: in.reader.Prompt,
	})
	e.Refused = says(refused)
	e.Chars = len(text)
	if len(refused) > 0 && !e.Anyway {
		for _, r := range refused {
			fmt.Fprintf(os.Stderr, "page %d: %s\n", page, r)
		}
	}
	rec.Put(e)
	if !e.Accepted() {
		return "", nil
	}
	return text, nil
}

// says is the refusals as the record writes them.
func says(refused []vision.Refusal) []string {
	if len(refused) == 0 {
		return nil
	}
	out := make([]string, 0, len(refused))
	for _, r := range refused {
		out = append(out, r.String())
	}
	return out
}

// reportVision prints what came out, with the three things only this path can say
// about a paper.
func reportVision(p *extract.Paper, r reading, outline bool) {
	if p != nil {
		printPaper(p, outline)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "  pages\t%d, %d read now, %d already read\n", r.pages, r.read, r.reused)
	if r.asked > 0 {
		fmt.Fprintf(tw, "  asks\t%d\n", r.asked)
	}
	if len(r.missing) > 0 {
		fmt.Fprintf(tw, "  unread\t%s\n", printed(r.missing))
	}
	if r.record != nil && r.record.Painter != "" {
		fmt.Fprintf(tw, "  painter\t%s\n", r.record.Painter)
	}
	if r.took > 0 {
		fmt.Fprintf(tw, "  took\t%s\n", r.took.Round(time.Second))
	}
	tw.Flush()
}

// rungs reads a ladder off the command line.
func rungs(s string) ([]int, error) {
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("%q is not a resolution, and -dpi takes a list like 300,400,600", part)
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, errors.New("-dpi names no resolutions, and a page has to be drawn at something")
	}
	return out, nil
}

// dots is a ladder as it is written on the command line and in a message.
func dots(ladder []int) string {
	parts := make([]string, 0, len(ladder))
	for _, d := range ladder {
		parts = append(parts, strconv.Itoa(d))
	}
	return strings.Join(parts, ",")
}

// printed is a list of page numbers as a person would say it.
func printed(ns []int) string {
	parts := make([]string, 0, len(ns))
	for _, n := range ns {
		parts = append(parts, strconv.Itoa(n))
	}
	if len(parts) == 1 {
		return "page " + parts[0]
	}
	return "pages " + strings.Join(parts, ", ")
}

// short is a hash cut to the length people quote.
func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// or is the first of these that is not empty.
func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

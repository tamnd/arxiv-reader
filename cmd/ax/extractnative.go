package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/fetch"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/pdftext"
)

// extractNative reads a cached PDF's text layer and writes the paper into the
// content plane.
//
// The third path and the first one that guesses. The two above it read a document
// that says where its sections and its theorems are, and this one reads the
// characters that were printed on a page and works the rest out from the shape
// they were printed in. What comes out is a real paper with an unreliable table
// of contents, and the front matter says path: native so that nobody has to
// wonder which kind they are reading.
//
// This is where a paper arrives when arXiv never rendered it and its submission
// is a PDF rather than TeX, which is most of what was submitted before 2000 and a
// steady trickle since. It is also where a paper arrives when its conversion was
// too broken to use.
func extractNative(args []string) error {
	fs := flag.NewFlagSet("ax extract native", flag.ContinueOnError)
	dry := fs.Bool("n", false, "read the PDF, report what it holds, and write nothing")
	outline := fs.Bool("outline", false, "print every heading and block, and not just the counts")
	force := fs.Bool("force", false, "overwrite a file somebody has corrected by hand")
	lang := fs.String("lang", "en", "the language directory to write into")
	budget := fs.Duration("timeout", pdftext.Timeout, "how long one PDF gets before the read is stopped")
	// Poppler lays a page out differently between releases, so which pdftotext is
	// on the PATH changes what comes out, and a machine with more than one needs
	// a way to say which without anybody editing their PATH.
	binary := fs.String("pdftotext", pdftext.Binary, "the pdftotext to run, for a machine with more than one or with it somewhere unusual")
	// A PDF that is a scan belongs on the vision path and this command says so
	// rather than writing a paper out of a margin stamp. The flag is for the paper
	// that misses the threshold by a page and that somebody has looked at.
	anyway := fs.Bool("anyway", false, "extract a PDF whose text layer is too thin for this path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	refs := fs.Args()
	if len(refs) == 0 {
		return errors.New("usage: ax extract native [-n] <id>v<n> [...]")
	}
	// Every reference is read before any PDF is, so that a typo in the tenth of
	// ten papers is a refusal now rather than after nine extractions.
	ids := make([]axid.ID, len(refs))
	for i, ref := range refs {
		id, err := axid.Parse(ref)
		if err != nil {
			return err
		}
		ids[i] = id
	}

	root := corpusRoot()
	plane := metadata.Plane{Root: root}
	manifest, err := fetch.Load(corpus.SourcesPath(root))
	if err != nil {
		return err
	}
	reader := &pdftext.Reader{Binary: *binary, Timeout: *budget}
	if err := reader.Available(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if v, err := reader.Version(ctx); err == nil {
		fmt.Fprintln(os.Stderr, v)
	}

	vision := 0
	for i, id := range ids {
		p, doc, entry, err := readPDF(ctx, reader, root, manifest, id, refs[i], *anyway)
		// A PDF with no text layer is a fact about the paper and not a failure of
		// the command, the same way a submission that turned out to be a PDF is on
		// the path above this one, so a batch steps over it.
		var scan *scanned
		if errors.As(err, &scan) {
			vision++
			fmt.Fprintf(os.Stderr, "%v\n", err)
			continue
		}
		if err != nil {
			return err
		}
		reportNative(p, doc, *outline)
		if *dry {
			continue
		}
		if err := writePaper(plane, id, p, entry, *lang, *force); err != nil {
			return err
		}
	}
	if vision > 0 {
		fmt.Fprintf(os.Stderr, "%d of %d PDFs hold no text layer worth reading, so those papers are on the vision path\n", vision, len(refs))
	}
	return nil
}

// scanned is the refusal for a PDF this path cannot read.
//
// Its own type so a batch steps over one paper rather than stopping, and it
// carries the page counts rather than only the judgement, because a paper that
// missed the threshold by one page and a paper with nothing on any page both get
// the same answer from Born and want different things done about them.
type scanned struct {
	ref string
	why string
}

func (s *scanned) Error() string {
	return fmt.Sprintf("%s %s", s.ref, s.why)
}

// readPDF finds a cached PDF, reads its text layer and recovers the paper.
//
// The manifest and not the disk says where the file is, for the same reason it
// does on the render path: a file nobody recorded is a file nobody knows the
// licence of, and reading one because it happens to be on disk is how a corpus
// publishes something it was never given.
func readPDF(ctx context.Context, r *pdftext.Reader, root string, m fetch.Manifest, id axid.ID, ref string, anyway bool) (*extract.Paper, *pdftext.Document, fetch.Source, error) {
	if id.Version < 1 {
		return nil, nil, fetch.Source{}, fmt.Errorf("%s names no version, and a PDF is of one version", ref)
	}
	entry, ok := m.Find(id.Canonical, id.Version, fetch.RouteNative)
	if !ok {
		return nil, nil, entry, fmt.Errorf("the manifest has no PDF of %sv%d, so run ax fetch native %sv%d first", id.Canonical, id.Version, id.Canonical, id.Version)
	}
	doc, err := r.Read(ctx, filepath.Join(root, filepath.FromSlash(entry.Path)))
	if err != nil {
		return nil, nil, entry, err
	}
	if !doc.Born() && !anyway {
		return nil, doc, entry, &scanned{ref: entry.Ref(), why: doc.Why()}
	}
	pages := make([]string, 0, len(doc.Pages))
	for _, page := range doc.Pages {
		pages = append(pages, page.Text)
	}
	p := extract.Native(pages, id.Canonical, id.Version)
	// The margin stamp is the one statement inside a PDF about which version of a
	// paper it is, and a request for a version arXiv has no PDF of can land on a
	// different one. Extracting v4 into a corpus that decided v1 was the licence
	// it may publish under would be publishing something nobody was given, so this
	// stops rather than reports.
	if got := p.StampVersion(); got != 0 && got != id.Version {
		return nil, doc, entry, fmt.Errorf("the PDF cached for %s says it is of v%d, so either arXiv served a different version or the file has been swapped: %q", entry.Ref(), got, p.Stamp)
	}
	return p, doc, entry, nil
}

// reportNative prints what came out, with the three things only this path can
// say about a paper.
func reportNative(p *extract.Paper, doc *pdftext.Document, outline bool) {
	printPaper(p, outline)
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "  pages\t%s, %d of them typeset, read in %s\n", p.Pages, doc.Typeset(), doc.Took.Round(time.Millisecond))
	fmt.Fprintf(tw, "  characters\t%d\n", doc.Chars())
	tw.Flush()
	// Printed rather than recorded, because there is nowhere honest to record it
	// yet. The front matter says path: native and that is the field a reader acts
	// on, and the paper this fires on is one that path selection should send to
	// the vision path instead, which is the next item on M5's list.
	if p.Encoding != "" {
		fmt.Fprintf(os.Stderr, "%sv%d: %s\n", p.ID, p.Version, p.Encoding)
	}
}

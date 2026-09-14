package fetch

import (
	"bytes"
	"context"
	"fmt"

	"github.com/tamnd/arxiv-reader/corpus"
)

// NoPDF is the error a version arXiv serves no PDF for fails with.
//
// The rarest of the three and the end of the road. Every other absence has
// somewhere to fall through to: no rendering means the source path, no e-print
// means this one. A version with no PDF is a version that was withdrawn or
// removed, and there is nothing left to read. Its own type so a batch steps over
// it rather than stopping.
type NoPDF struct {
	Ref    string
	Status string
}

func (n *NoPDF) Error() string {
	return fmt.Sprintf("fetch: arXiv serves no PDF for %s (%s), and a version with no PDF is one that was withdrawn or removed, so there is nothing left for any path here to read", n.Ref, n.Status)
}

// NotBuilt is the error a PDF arXiv has not finished compiling fails with.
//
// arXiv answers a request for a PDF it is still building with a 200 and a page
// of HTML saying so, which is the one case in this package where the status code
// is not the answer. Left as its own type because it is the one failure here
// that fixes itself: the paper is fine, the compile is minutes away, and the
// answer is to fetch it again later rather than to look into anything.
type NotBuilt struct {
	Ref   string
	Bytes int
}

func (n *NotBuilt) Error() string {
	return fmt.Sprintf("fetch: arXiv answered the request for %s with %d bytes that are not a PDF, which is the page it serves while a submission is still being compiled, so nothing was written and this one is worth asking for again later", n.Ref, n.Bytes)
}

// PDF fetches arXiv's PDF of one version into the corpus.
//
// The same fetch as Render and EPrint in every respect but the surface and one
// extra check, and the check is there because this is the only surface that
// answers something other than what was asked for with a 200.
//
// One download serves both the paths that read a PDF. The native path runs
// pdftotext over these bytes and the vision path rasterises them, and neither
// asks arXiv for the file a second time.
func (f *Fetcher) PDF(ctx context.Context, root string, m *Manifest, o Order) (Result, error) {
	return f.artefact(ctx, root, m, o, surface{
		route:   RouteNative,
		noun:    "a PDF",
		base:    f.pdfBase(),
		path:    corpus.PDFPath,
		cap:     maxPDF,
		what:    "a PDF of a paper",
		missing: func(ref, status string) error { return &NoPDF{Ref: ref, Status: status} },
		check: func(ref string, body []byte) error {
			// The five bytes every PDF starts with, and the whole of the check.
			// Reading further would be parsing the file, which is pdftotext's
			// job and not worth a second implementation here to catch a case
			// that announces itself in the first line.
			if !bytes.HasPrefix(body, []byte("%PDF-")) {
				return &NotBuilt{Ref: ref, Bytes: len(body)}
			}
			return nil
		},
	})
}

func (f *Fetcher) pdfBase() string {
	if f.PDFBase != "" {
		return f.PDFBase
	}
	return PDFBase
}

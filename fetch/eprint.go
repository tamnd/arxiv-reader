package fetch

import (
	"context"
	"fmt"

	"github.com/tamnd/arxiv-reader/corpus"
)

// NoEPrint is the error a version with no e-print fails with.
//
// Rarer than a missing rendering and worse news. Every submission has source of
// some kind, so the usual reasons for this are a paper whose source the
// submitter asked arXiv to withhold and a version that was withdrawn, and
// neither of them has another path to fall through to. It is its own type so
// that a batch can step over it the way it steps over a paper with no
// rendering, rather than stopping on the first one.
type NoEPrint struct {
	Ref    string
	Status string
}

func (n *NoEPrint) Error() string {
	return fmt.Sprintf("fetch: arXiv serves no e-print for %s (%s), which is a submission whose source is withheld or a version that was withdrawn, and neither has a path through this tool", n.Ref, n.Status)
}

// EPrint fetches the files the submitter uploaded for one version.
//
// The same fetch as Render in every respect that is not the surface: the same
// licence gate before the request, the same skip for a file already on disk
// that hashes to what the manifest says, the same refusal when the bytes have
// moved, and the same entry written into the manifest in memory for the caller
// to save.
//
// What arrives is one gzip stream and nothing here looks inside it. Whether it
// holds a tar, a single TeX file or a PDF is a question for the source package,
// and asking it here would mean this function failing on a submission it
// downloaded perfectly well.
func (f *Fetcher) EPrint(ctx context.Context, root string, m *Manifest, o Order) (Result, error) {
	return f.artefact(ctx, root, m, o, surface{
		route: RouteSource,
		noun:  "an e-print",
		base:  f.sourceBase(),
		path:  corpus.EPrintPath,
		cap:   maxEPrint,
		what:  "a submission to arXiv",
		missing: func(ref, status string) error {
			return &NoEPrint{Ref: ref, Status: status}
		},
	})
}

func (f *Fetcher) sourceBase() string {
	if f.SourceBase != "" {
		return f.SourceBase
	}
	return EPrintBase
}

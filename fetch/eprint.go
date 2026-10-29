package fetch

import (
	"context"
	"fmt"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
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

// ReadGate decides whether a version's e-print may be read in memory, by way R2
// of AX2.5. It is Gate without the last refusal: the licence must be read from
// the abs page of the version, and any class may then be read, because nothing
// of the text is kept. AQ01 allows it.
func ReadGate(r metadata.Record, version int) error {
	v, ok := r.VersionAt(version)
	if !ok {
		return fmt.Errorf("fetch: the metadata plane has no v%d of %s, so either the reference is wrong or the record is stale", version, r.ID)
	}
	ref := fmt.Sprintf("%sv%d", r.ID, version)
	if v.Licence == "" {
		return fmt.Errorf("fetch: nobody has read a licence for %s yet, run ax licence resolve -write %s", ref, ref)
	}
	if v.LicenceFrom != metadata.SourceAbs {
		return fmt.Errorf("fetch: the licence for %s was read from %s, which states one licence for the paper and not one for the version, run ax licence resolve -write %s", ref, v.LicenceFrom, ref)
	}
	return nil
}

// EPrintInMemory fetches the e-print of one version and gives its bytes,
// without writing them anywhere. It is way R2 of AX2.5: a version that the
// corpus may not publish is read in memory by the process that extracts, and
// only its structure is kept. The entry for the manifest has the hash and the
// size, so that a later read is checked against it, and the path where the file
// would be, which never holds it.
func (f *Fetcher) EPrintInMemory(ctx context.Context, m *Manifest, o Order) ([]byte, Source, error) {
	id, err := axid.Parse(o.Record.ID)
	if err != nil {
		return nil, Source{}, fmt.Errorf("fetch: %w", err)
	}
	if err := ReadGate(o.Record, o.Version); err != nil {
		return nil, Source{}, err
	}
	v, _ := o.Record.VersionAt(o.Version)
	ref := fmt.Sprintf("%sv%d", id.Canonical, o.Version)
	entry := Source{
		ID:          id.Canonical,
		Version:     o.Version,
		Route:       RouteSource,
		URL:         f.sourceBase() + ref,
		Path:        corpus.EPrintPath("", id, o.Version),
		Licence:     v.Licence,
		LicenceFrom: v.LicenceFrom,
	}
	if f.Log != nil {
		f.Log(ref)
	}
	body, err := f.do(ctx, request{url: entry.URL, ref: ref, cap: maxEPrint, what: "a submission to arXiv", missing: func(ref, status string) error {
		return &NoEPrint{Ref: ref, Status: status}
	}})
	if err != nil {
		return nil, Source{}, err
	}
	entry.SHA256 = Digest(body)
	entry.Bytes = int64(len(body))
	entry.Fetched = time.Now().UTC().Truncate(time.Second)
	if held, had := m.Find(id.Canonical, o.Version, RouteSource); had && held.SHA256 != entry.SHA256 && !o.Accept {
		return nil, Source{}, &Changed{Was: held, Now: entry, Where: "at the source"}
	}
	return body, entry, nil
}

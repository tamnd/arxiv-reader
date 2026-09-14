// Package fetch downloads what a paper needs for the path it is on, and writes
// down where every byte came from.
//
// Three things make this more than a download. The first is the pace: these are
// requests to arxiv.org the website, which asks for fifteen seconds between
// them, so a fetch of a hundred papers is half an hour and the tool is built to
// be left running rather than watched. The second is the manifest, which
// records the URL, the hash, the size and the hour for every artefact, so that
// a source changing under a paper the corpus has already extracted arrives as a
// finding instead of as a silent overwrite. The third is the licence gate,
// which runs before the request rather than after it.
//
// The gate is the part worth arguing about. Nothing under work/ is published,
// so fetching a paper this corpus may never republish harms nobody. It is still
// refused by default, for two reasons. It spends fifteen seconds and one of
// arXiv's requests on work that cannot be used, and it puts the bytes on disk
// where the next command will happily extract them. The gate is what makes the
// licence census of M2 load bearing rather than decorative: the census counted,
// and this is the first command that acts on the count.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
)

// Route is the extraction path an artefact was fetched for.
//
// It is on the manifest entry because one version can be fetched more than
// once for more than one path, and the two files are different files with
// different hashes. A paper whose rendering was rejected and which fell through
// to the source path has both.
type Route string

// RouteRender is arXiv's own LaTeXML rendering, which is one HTML file and the
// only route this command can walk so far. The source, native and vision paths
// fetch a tarball and a PDF and they arrive with the milestones that read them.
const RouteRender Route = "render"

// ParseRoute reads a route name.
func ParseRoute(s string) (Route, error) {
	if Route(s) == RouteRender {
		return RouteRender, nil
	}
	return "", fmt.Errorf("fetch: %q is not a route that can be fetched yet, which is %s", s, RouteRender)
}

// HTMLBase is where arXiv serves its own rendering of a paper.
const HTMLBase = "https://arxiv.org/html/"

// Pace is the gap to leave between requests to the website.
//
// The same fifteen seconds as licence.Pace, because it is the same number off
// the same line of the same robots.txt and the abs page and the rendering are
// the same host. There is a test holding the two equal so that changing one and
// forgetting the other fails rather than halves the pace of whichever was
// forgotten.
const Pace = 15 * time.Second

// maxBody caps a rendering at sixteen megabytes.
//
// The largest of the three renderings measured in September 2026 was 730
// kilobytes, so this is twenty times the observed worst case. The cap is here
// so that a redirect to something that is not a rendering fails on its size
// rather than being read into memory in full.
const maxBody = 16 << 20

// Gate decides whether a version's bytes may be fetched into the corpus.
//
// Three refusals and they are in order of how little is known. Nobody has read
// a licence for this version. Somebody read one but from a surface that states
// one licence for the whole paper, which is a licence for the latest version
// wearing the wrong label. Somebody read the right one and it says no.
//
// The middle refusal is the one that catches real mistakes. Every bulk surface
// this project harvests carries a paper level licence, so a record straight
// from a harvest has a licence on every version and all of them are the latest
// version's. Taking that at face value is how a corpus republishes a v1 that
// was never offered under the licence its v5 carries.
func Gate(r metadata.Record, version int) error {
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
	if a := corpus.AccessFor(v.Licence); !a.MayPublishText() {
		return fmt.Errorf("fetch: %s is under %s, which is access class %s, and none of its content may be republished", ref, v.Licence, a)
	}
	return nil
}

// Changed is the error a source that no longer matches the manifest fails with.
//
// The bytes are not written and the entry is not updated. A source that moved
// under a paper the corpus has already read is the event the manifest exists to
// catch, and overwriting the file would be destroying the only evidence that it
// happened. What a caller does about it is look, and then either accept the new
// bytes or find out why arXiv served different ones.
type Changed struct {
	// Was is what the manifest holds.
	Was Source
	// Now is what is actually there, with the same identity and the new hash.
	Now Source
	// Where is "on disk" or "at the source".
	Where string
}

func (c *Changed) Error() string {
	// The advice differs because the two cases are different events. A source
	// that moved is arXiv's doing and accepting the new bytes is a reasonable
	// answer. A cached file that moved is this machine's doing, and accepting
	// it would mean recording a hash for bytes nobody fetched.
	next := "pass -accept to record the new bytes"
	if c.Where == "on disk" {
		next = "delete the file and fetch it again"
	}
	return fmt.Sprintf("fetch: %s changed %s: the manifest says %s at %d bytes and this is %s at %d bytes, nothing was written, %s",
		c.Was.Ref(), c.Where, Short(c.Was.SHA256), c.Was.Bytes, Short(c.Now.SHA256), c.Now.Bytes, next)
}

// NotRendered is the error a version with no HTML rendering fails with.
//
// Common and not alarming. arXiv began rendering submissions in December 2023,
// it only renders what has TeX source, and the rendering of an old paper is not
// backfilled. The answer is the source path, which is why this is its own type
// rather than a status code in a string.
type NotRendered struct {
	Ref    string
	Status string
}

func (n *NotRendered) Error() string {
	return fmt.Sprintf("fetch: arXiv has no HTML rendering of %s (%s), which is normal before December 2023 and for submissions with no TeX source, so this paper is on the source path", n.Ref, n.Status)
}

// Order is one thing to fetch.
type Order struct {
	// Record is the paper as the metadata plane holds it. Both the licence the
	// gate reads and the licence the manifest records come from here.
	Record metadata.Record
	// Version is the version to fetch, and it is never zero. A rendering
	// belongs to a version, and the latest one moves.
	Version int
	// Accept records new bytes for a source whose hash no longer matches the
	// manifest, instead of refusing and writing nothing.
	Accept bool
	// Anyway skips the licence gate, for somebody reading a paper this corpus
	// may never publish. It changes nothing else: the entry still records the
	// licence, so the manifest still says what was known at the time.
	Anyway bool
}

// Outcome is what a fetch did.
type Outcome string

const (
	// OutcomeFetched is bytes that arrived over the network and were written.
	OutcomeFetched Outcome = "fetched"
	// OutcomeCached is a file already on disk whose hash matched the manifest,
	// so nothing was requested and nothing was paced.
	OutcomeCached Outcome = "cached"
)

// Result is what one fetch did.
type Result struct {
	Entry   Source
	Outcome Outcome
}

// Fetcher reads artefacts off the website, one at a time and slowly.
type Fetcher struct {
	// Base defaults to HTMLBase and exists so a test can point at localhost.
	Base string
	// HTTP defaults to a client with a two minute timeout. A rendering is
	// several times the size of an abs page.
	HTTP *http.Client
	// UserAgent should name the project and a way to get hold of a person.
	// arXiv blocks anonymous readers of the website and they are right to.
	UserAgent string
	// Pace is the gap to leave between requests, and zero means the package
	// constant. Zero means the default rather than no wait, because the
	// dangerous value is the fast one and it is the one a caller should have to
	// write down.
	Pace time.Duration
	// Log, if set, is called with each reference before it is fetched. It is
	// not called for a cache hit, because nothing happens on a cache hit and a
	// line saying so would make a fast run look like a slow one.
	Log func(ref string)

	last time.Time
}

// Render fetches arXiv's rendering of one version into the corpus.
//
// It is idempotent and it is hash checked, in that order. A file already on
// disk whose hash matches the manifest is returned without a request, which is
// what makes re-running a batch cheap. A file on disk whose hash does not match
// fails without a request, because the thing that has to be explained is on the
// disk and fetching would only add a third version of the story.
//
// The manifest is updated in memory. Writing it is the caller's, so that a
// batch of fetches is one write rather than one per paper.
func (f *Fetcher) Render(ctx context.Context, root string, m *Manifest, o Order) (Result, error) {
	id, err := axid.Parse(o.Record.ID)
	if err != nil {
		return Result{}, fmt.Errorf("fetch: %w", err)
	}
	if o.Version < 1 {
		return Result{}, fmt.Errorf("fetch: %s names no version, and a rendering belongs to a version rather than to a paper", o.Record.ID)
	}
	if !o.Anyway {
		if err := Gate(o.Record, o.Version); err != nil {
			return Result{}, err
		}
	}
	v, ok := o.Record.VersionAt(o.Version)
	if !ok {
		return Result{}, fmt.Errorf("fetch: the metadata plane has no v%d of %s, so either the reference is wrong or the record is stale", o.Version, o.Record.ID)
	}

	ref := fmt.Sprintf("%sv%d", id.Canonical, o.Version)
	rel := corpus.RenderPath("", id, o.Version)
	file := filepath.Join(root, filepath.FromSlash(rel))
	entry := Source{
		ID:          id.Canonical,
		Version:     o.Version,
		Route:       RouteRender,
		URL:         f.base() + ref,
		Path:        rel,
		Licence:     v.Licence,
		LicenceFrom: v.LicenceFrom,
	}

	held, had := m.Find(id.Canonical, o.Version, RouteRender)
	if had {
		sum, n, err := digestFile(file)
		switch {
		case err == nil && sum == held.SHA256:
			return Result{Entry: held, Outcome: OutcomeCached}, nil
		case err == nil:
			now := held
			now.SHA256, now.Bytes = sum, n
			return Result{}, &Changed{Was: held, Now: now, Where: "on disk"}
		case !errors.Is(err, fs.ErrNotExist):
			return Result{}, err
		}
	}

	if f.Log != nil {
		f.Log(ref)
	}
	body, err := f.get(ctx, entry.URL, ref)
	if err != nil {
		return Result{}, err
	}
	entry.SHA256 = Digest(body)
	entry.Bytes = int64(len(body))
	entry.Fetched = time.Now().UTC().Truncate(time.Second)

	if had && entry.SHA256 != held.SHA256 && !o.Accept {
		return Result{}, &Changed{Was: held, Now: entry, Where: "at the source"}
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(file, body, 0o644); err != nil {
		return Result{}, err
	}
	m.Put(entry)
	return Result{Entry: entry, Outcome: OutcomeFetched}, nil
}

// Get reads one URL off the website at this fetcher's pace.
//
// It exists for figures. A figure is a second file on the same host as the
// rendering that named it, so it goes through the same fetcher and shares the
// same pace clock rather than getting its own. Two fetchers each waiting
// fifteen seconds is one request every seven and a half, which is not the pace
// that was agreed to.
//
// The ref is what a failure is reported against and what the log line names, so
// it should read like "2312.00752v2 selection.svg" and not like a URL.
func (f *Fetcher) Get(ctx context.Context, url, ref string) ([]byte, error) {
	if f.Log != nil {
		f.Log(ref)
	}
	return f.get(ctx, url, ref)
}

// URL is where this fetcher would look for one path under the rendering base.
//
// Exported so that a caller building a figure URL out of the path a rendering
// gave gets the same base a test pointed at, rather than hardcoding arxiv.org
// and quietly making itself untestable.
func (f *Fetcher) URL(rel string) string { return f.base() + rel }

func (f *Fetcher) base() string {
	if f.Base != "" {
		return f.Base
	}
	return HTMLBase
}

func (f *Fetcher) get(ctx context.Context, url, ref string) ([]byte, error) {
	pace := f.Pace
	if pace == 0 {
		pace = Pace
	}
	if !f.last.IsZero() {
		if wait := pace - time.Since(f.last); wait > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}
	}
	f.last = time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if f.UserAgent != "" {
		req.Header.Set("User-Agent", f.UserAgent)
	}
	client := f.HTTP
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, &NotRendered{Ref: ref, Status: resp.Status}
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("fetch: %s returned %s", url, resp.Status)
	case len(body) > maxBody:
		return nil, fmt.Errorf("fetch: %s is over %d bytes, which is not a rendering of a paper or a picture in one", url, maxBody)
	}
	return body, nil
}

package licence

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
)

// AbsBase is where a version's licence is stated.
//
// The abs page and nowhere else. Kaggle, the Hugging Face mirror and OAI-PMH
// all carry one licence per paper, which is the latest version's, so this page
// is the only surface that can answer the question the licence gate actually
// asks.
const AbsBase = "https://arxiv.org/abs/"

// Pace is the gap to leave between requests to the website.
//
// Fifteen seconds. This is the website and not the export API: arXiv asks bulk
// readers to use export.arxiv.org and to leave fifteen seconds between calls,
// and the abs page is being read here because no bulk surface carries the
// answer. That pace is why this command resolves a paper and not a corpus.
const Pace = 15 * time.Second

// Resolver reads the licence of one version off its abs page.
type Resolver struct {
	// Base defaults to AbsBase and exists so a test can point at localhost.
	Base string
	// HTTP defaults to a client with a one minute timeout. An abs page is a
	// hundred kilobytes, so the OAI reasoning for five minutes does not apply.
	HTTP *http.Client
	// UserAgent should name the project and a way to get hold of a person.
	// arXiv blocks anonymous readers of the website and they are right to.
	UserAgent string
	// Pace is the gap to leave between requests, and zero means the package
	// constant.
	//
	// Zero means the default rather than no wait, for the same reason it does
	// in the harvest: the dangerous value is the fast one, so it is the one a
	// caller has to write down.
	Pace time.Duration
	// Log, if set, is called with each reference before it is fetched.
	Log func(ref string)

	last time.Time
}

// Resolution is what one abs page said.
type Resolution struct {
	// ID is the canonical identifier, without the version.
	ID string
	// Version is the version that was read.
	Version int
	// Licence is what arXiv states for that version.
	Licence corpus.Licence
	// Withdrawn is true when arXiv says there is no licence for this version
	// because the version was withdrawn.
	//
	// The licence is unknown either way, which is record access either way, and
	// the flag is kept because a withdrawn version is a fact about the paper
	// and not a gap in the harvest.
	Withdrawn bool
	// URL is the licence deed the page linked to, empty when it linked none.
	URL string
}

// Ref is the versioned reference this resolution is of.
func (r Resolution) Ref() string { return fmt.Sprintf("%sv%d", r.ID, r.Version) }

// Resolve reads one version's licence.
//
// The reference has to name a version. Resolving a paper without one would read
// the latest, which is the number every bulk surface already has, so it would
// spend fifteen seconds to learn nothing.
func (r *Resolver) Resolve(ctx context.Context, ref string) (Resolution, error) {
	id, err := axid.Parse(ref)
	if err != nil {
		return Resolution{}, fmt.Errorf("licence: %w", err)
	}
	if id.Version == 0 {
		return Resolution{}, fmt.Errorf("licence: %s names no version, and the licence of the latest one is what every bulk surface already carries", ref)
	}
	if r.Log != nil {
		r.Log(id.Versioned())
	}
	body, err := r.get(ctx, r.base()+id.Versioned())
	if err != nil {
		return Resolution{}, err
	}
	l, withdrawn, url, err := ParseAbs(body)
	if err != nil {
		return Resolution{}, fmt.Errorf("licence: %s: %w", id.Versioned(), err)
	}
	return Resolution{ID: id.Canonical, Version: id.Version, Licence: l, Withdrawn: withdrawn, URL: url}, nil
}

// ResolveAll reads every version of a paper, oldest first.
//
// The count comes from the caller, because the metadata plane already knows it
// and asking arXiv would be another request for a number this project holds.
func (r *Resolver) ResolveAll(ctx context.Context, id string, versions int) ([]Resolution, error) {
	if versions < 1 {
		return nil, fmt.Errorf("licence: %s has %d versions to read", id, versions)
	}
	var out []Resolution
	for n := 1; n <= versions; n++ {
		got, err := r.Resolve(ctx, fmt.Sprintf("%sv%d", id, n))
		if err != nil {
			return out, err
		}
		out = append(out, got)
	}
	return out, nil
}

func (r *Resolver) base() string {
	if r.Base != "" {
		return r.Base
	}
	return AbsBase
}

func (r *Resolver) get(ctx context.Context, url string) (string, error) {
	pace := r.Pace
	if pace == 0 {
		pace = Pace
	}
	if !r.last.IsZero() {
		if wait := pace - time.Since(r.last); wait > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(wait):
			}
		}
	}
	r.last = time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	if r.UserAgent != "" {
		req.Header.Set("User-Agent", r.UserAgent)
	}
	client := r.HTTP
	if client == nil {
		client = &http.Client{Timeout: time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	// Capped, because a redirect to something that is not an abs page should
	// fail on the parse rather than be read into memory in full.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("licence: %s returned %s", url, resp.Status)
	}
	return string(body), nil
}

// licenceBlock is the div arXiv states the licence in. The class name is the
// load bearing part and it is the part most likely to change.
var licenceBlock = regexp.MustCompile(`class="abs-license"`)

// hrefIn finds the first link in a fragment.
var hrefIn = regexp.MustCompile(`href="([^"]*)"`)

// ParseAbs reads the licence out of an abs page.
//
// It returns an error rather than an empty licence when it cannot find the
// block it is looking for. That is the whole point of the function: a parser
// that reads a changed page as "no licence stated" would quietly mark every
// paper it touched as record access, and record access is the answer that
// stops work rather than the answer that starts it. A failure here should
// arrive as a failure.
func ParseAbs(page string) (l corpus.Licence, withdrawn bool, url string, err error) {
	loc := licenceBlock.FindStringIndex(page)
	if loc == nil {
		return "", false, "", fmt.Errorf("the page states no licence block, so either it is not an abs page or arXiv has changed the markup")
	}
	block := page[loc[1]:]
	// The block ends at the first closing div. For a withdrawn version that is
	// the nested div holding the sentence, and for every other version there is
	// no nested div at all, so the first one closes the block either way.
	if end := strings.Index(block, "</div>"); end >= 0 {
		block = block[:end]
	}

	if m := hrefIn.FindStringSubmatch(block); m != nil {
		l, err := corpus.LicenceFromURL(m[1])
		if err != nil {
			return "", false, m[1], err
		}
		return l, false, m[1], nil
	}
	// arXiv states this in as many words when a version has been withdrawn, and
	// it is the one case where a page with no licence link is not a broken read.
	if strings.Contains(block, "No license for this version") {
		return corpus.LicenceUnknown, true, "", nil
	}
	return "", false, "", fmt.Errorf("the licence block links to nothing and does not say why")
}

// Apply writes resolutions onto a record's versions.
//
// It returns the versions that changed, so a caller can say what the website
// disagreed with the snapshot about rather than just that something did. A
// resolution for a version the record does not have is an error: the plane is
// where the version count came from, so the two disagreeing means the record is
// stale and writing to it would be writing a guess.
func Apply(r metadata.Record, got []Resolution) (metadata.Record, []Resolution, error) {
	out := r
	out.Versions = append([]metadata.Version(nil), r.Versions...)
	var changed []Resolution
	for _, res := range got {
		if res.ID != r.ID {
			return r, nil, fmt.Errorf("licence: %s does not belong to %s", res.Ref(), r.ID)
		}
		i := -1
		for n := range out.Versions {
			if out.Versions[n].Version == res.Version {
				i = n
				break
			}
		}
		if i < 0 {
			return r, nil, fmt.Errorf("licence: %s has no v%d, so the record is older than the page", r.ID, res.Version)
		}
		if out.Versions[i].Licence != res.Licence {
			changed = append(changed, res)
		}
		out.Versions[i].Licence = res.Licence
		out.Versions[i].LicenceFrom = metadata.SourceAbs
	}
	return out, changed, nil
}

package selection

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Entry is one paper in the content plane, and why.
//
// The evidence fields are not decoration. A reason on its own is a word, and a word
// is what an argument about the corpus cannot be had over: cited-by-corpus with no
// count could be two papers, requested with no issue could be anybody, and
// category-canon with no rank could be the nine hundredth paper in its field. Each
// reason demands the evidence that makes it checkable and Check refuses an entry
// without it.
type Entry struct {
	ID      string `yaml:"id"`
	Version int    `yaml:"version"`
	Reason  Reason `yaml:"reason"`
	// Cited is how many papers inside the corpus are on the other end of the
	// closure reasons.
	Cited int `yaml:"cited,omitempty"`
	// Category and Rank are where category-canon put it, by primary category and
	// year.
	Category string `yaml:"category,omitempty"`
	Year     int    `yaml:"year,omitempty"`
	Rank     int    `yaml:"rank,omitempty"`
	// Issue is the issue a request was made in, and By is who made it.
	Issue int    `yaml:"issue,omitempty"`
	By    string `yaml:"by,omitempty"`
	// Collection is the reading list in collections.yaml this is a member of.
	Collection string `yaml:"collection,omitempty"`
	// Score orders the queue and decides nothing else. It is a weighted sum
	// computed elsewhere, and it is recorded rather than recomputed so that the
	// order a paper was read in can be explained later.
	Score float64 `yaml:"score,omitempty"`
	// Added is the date the entry was written, as YYYY-MM-DD.
	//
	// A date and not a timestamp, because nothing needs the minute and a timestamp
	// would put a diff on every row that was rewritten for another reason.
	Added string `yaml:"added"`
	// Status is how far down the pipeline this paper has got.
	Status Status `yaml:"status"`
	// Languages are the languages it exists in, English included.
	Languages []string `yaml:"languages,omitempty"`
}

// Ref is the versioned reference this entry is of.
func (e Entry) Ref() string { return fmt.Sprintf("%sv%d", e.ID, e.Version) }

// day is the date form every Added field takes.
var day = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// Check says whether an entry carries the evidence its reason needs.
//
// This is the whole value of the manifest. An entry that passes can be argued with,
// and an entry that cannot pass should not be in the corpus, because somebody wants
// that paper for a reason they have not written down.
func (e Entry) Check() error {
	if e.ID == "" {
		return errors.New("selection: an entry names no paper")
	}
	if e.Version < 1 {
		return fmt.Errorf("selection: %s has version %d, and the content plane holds one version of a paper", e.ID, e.Version)
	}
	if _, err := ParseReason(string(e.Reason)); err != nil {
		return err
	}
	if !Known(e.Status) {
		return fmt.Errorf("selection: %s is at status %q, and the statuses are %s", e.Ref(), e.Status, StatusNames())
	}
	if e.Added != "" && !day.MatchString(e.Added) {
		return fmt.Errorf("selection: %s was added %q, and a date is YYYY-MM-DD", e.Ref(), e.Added)
	}
	switch e.Reason {
	case CitedByCorpus, CitesCorpus:
		if e.Cited < Floor {
			return fmt.Errorf("selection: %s is %s with cited %d, and the reason is not true under %d", e.Ref(), e.Reason, e.Cited, Floor)
		}
	case CategoryCanon:
		if e.Category == "" || e.Rank < 1 {
			return fmt.Errorf("selection: %s is %s and says neither category nor rank, so nothing can check it", e.Ref(), e.Reason)
		}
	case Requested:
		// Both halves, because a request with no issue cannot be found again and a
		// request with no name on it is the corpus growing by itself.
		if e.Issue < 1 || e.By == "" {
			return fmt.Errorf("selection: %s is %s and needs both an issue and a name, so that somebody owns the request", e.Ref(), e.Reason)
		}
	case Collection:
		if e.Collection == "" {
			return fmt.Errorf("selection: %s is %s and names no reading list", e.Ref(), e.Reason)
		}
	}
	return nil
}

// Explain is the sentence behind the word, with this entry's own evidence in it.
func (e Entry) Explain() string {
	switch e.Reason {
	case CitedByCorpus:
		return fmt.Sprintf("cited by %d papers already in the content plane", e.Cited)
	case CitesCorpus:
		return fmt.Sprintf("cites %d papers already in the content plane", e.Cited)
	case CategoryCanon:
		if e.Year > 0 {
			return fmt.Sprintf("ranked %d by citation count in %s for %d", e.Rank, e.Category, e.Year)
		}
		return fmt.Sprintf("ranked %d by citation count in %s", e.Rank, e.Category)
	case Requested:
		return fmt.Sprintf("%s asked for it in issue %d", e.By, e.Issue)
	case Collection:
		return fmt.Sprintf("a member of the reading list %s", e.Collection)
	case Seed:
		if e.By != "" {
			return fmt.Sprintf("%s put it on the seed list", e.By)
		}
	}
	return e.Reason.Says()
}

// Manifest is manifests/selected.yaml.
//
// One file for the corpus rather than one per month, unlike the metadata plane and
// unlike the figures manifests. This is the selection and not the census: it holds
// the papers that were chosen, which is thousands and not three million, and the
// question it answers most often is what is in the content plane and why, which is
// a question about the whole corpus at once.
type Manifest struct {
	Selected []Entry `yaml:"selected"`
}

const header = `# Every paper in the content plane, and the reason it is there.
#
# Written by ax select. There are six reasons and no seventh, and each one has to
# carry the evidence that makes it checkable, so an entry added by hand that says
# nothing but a word will be refused the next time this file is read.
`

// Load reads the manifest.
//
// A file that is not there is an empty manifest and not an error, because a corpus
// with nothing selected yet is an ordinary state and not a broken one.
func Load(path string) (Manifest, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Manifest{}, nil
	}
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := yaml.Unmarshal(b, &m); err != nil {
		return Manifest{}, fmt.Errorf("selection: %s: %w", path, err)
	}
	for _, e := range m.Selected {
		if err := e.Check(); err != nil {
			return Manifest{}, fmt.Errorf("%w, in %s", err, path)
		}
	}
	return m, nil
}

// Save writes the manifest, sorted, creating the directory if it is missing.
//
// Every entry is checked on the way out as well as on the way in, because the
// caller that wrote a bad entry is the one that should be told about it.
func (m Manifest) Save(path string) error {
	out := append([]Entry(nil), m.Selected...)
	for _, e := range out {
		if err := e.Check(); err != nil {
			return err
		}
	}
	sortEntries(out)
	body, err := yaml.Marshal(Manifest{Selected: out})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(header), body...), 0o644)
}

// Find returns the entry for one paper, at whatever version was selected.
//
// By paper and not by paper and version, because the content plane holds one
// version of a paper: the version the licence gate decided may be published. Asking
// for a paper is asking about that one.
func (m Manifest) Find(id string) (Entry, bool) {
	for _, e := range m.Selected {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// Put adds an entry, replacing any entry for the same paper.
func (m *Manifest) Put(e Entry) {
	for i := range m.Selected {
		if m.Selected[i].ID == e.ID {
			m.Selected[i] = e
			return
		}
	}
	m.Selected = append(m.Selected, e)
	sortEntries(m.Selected)
}

// Drop removes a paper from the selection.
//
// This is not a takedown. A takedown deletes what was published and leaves a
// tombstone, and it lives in the licence gate. This is for a paper that was chosen
// and then should not have been, before anything was published.
func (m *Manifest) Drop(id string) bool {
	for i := range m.Selected {
		if m.Selected[i].ID == id {
			m.Selected = append(m.Selected[:i], m.Selected[i+1:]...)
			return true
		}
	}
	return false
}

// ByReason counts the entries under each of the six.
//
// Every reason is in the map, including the ones with nothing under them, because
// a reason that never fires is a fact about the selection and not an absence.
func (m Manifest) ByReason() map[Reason]int {
	out := make(map[Reason]int, len(Reasons))
	for _, r := range Reasons {
		out[r] = 0
	}
	for _, e := range m.Selected {
		out[e.Reason]++
	}
	return out
}

// ByStatus counts the entries at each status.
func (m Manifest) ByStatus() map[Status]int {
	out := make(map[Status]int, len(Statuses))
	for _, s := range Statuses {
		out[s] = 0
	}
	for _, e := range m.Selected {
		out[e.Status]++
	}
	return out
}

func sortEntries(e []Entry) {
	sort.SliceStable(e, func(i, j int) bool {
		if e[i].ID != e[j].ID {
			return e[i].ID < e[j].ID
		}
		return e[i].Version < e[j].Version
	})
}

// Today is the date an entry added now carries.
func Today() string { return time.Now().UTC().Format("2006-01-02") }

// Languages is a language list with no duplicates, in the order given.
func Languages(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, l := range in {
		l = strings.TrimSpace(l)
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	return out
}

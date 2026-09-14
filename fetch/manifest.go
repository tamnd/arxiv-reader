package fetch

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
)

// Source is one manifest entry: one artefact, fetched once, hashed.
//
// The licence is recorded here as well as on the metadata record because the
// two answer different questions. The record says what the licence is now. The
// manifest says what the licence was when these bytes were taken, and which
// surface said so. A paper whose licence was read off the abs page in March and
// relicensed in June is a paper whose extraction has to be looked at again, and
// nothing can notice that unless both readings survive.
type Source struct {
	ID      string `yaml:"id"`
	Version int    `yaml:"version"`
	Route   Route  `yaml:"route"`
	URL     string `yaml:"url"`
	// Path is where the bytes were written, relative to the corpus root.
	Path string `yaml:"path"`
	// SHA256 is the hex digest of exactly what arrived, before anything parsed
	// it.
	SHA256 string `yaml:"sha256"`
	Bytes  int64  `yaml:"bytes"`
	// Fetched is when the request was made, in UTC and to the second.
	Fetched     time.Time       `yaml:"fetched"`
	Licence     corpus.Licence  `yaml:"licence"`
	LicenceFrom metadata.Source `yaml:"licence_from"`
}

// Ref is the versioned reference this entry is of.
func (s Source) Ref() string { return fmt.Sprintf("%sv%d", s.ID, s.Version) }

// Manifest is manifests/sources.yaml.
//
// It is committed, and it is the only committed record of a fetch, because the
// bytes themselves are not. Everything about it is built for a git diff: one
// entry per artefact, sorted, and a timestamp to the second rather than to the
// nanosecond so that re-fetching an unchanged file produces no diff at all.
type Manifest struct {
	Sources []Source `yaml:"sources"`
}

// header is written above the entries so that somebody who opens the file knows
// what it is and who writes it.
const header = `# Every artefact this corpus fetched, where it came from, and what it hashed to.
#
# Written by ax fetch. Do not edit by hand: the hashes here are what ax checks a
# cached file against, and a hash edited to match a file is a hash that has
# stopped saying anything.
`

// Load reads the manifest.
//
// A file that is not there is an empty manifest and not an error, because the
// first fetch into a new corpus has nothing to read.
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
		return Manifest{}, fmt.Errorf("fetch: %s: %w", path, err)
	}
	for i, s := range m.Sources {
		if s.ID == "" {
			return Manifest{}, fmt.Errorf("fetch: %s: entry %d names no paper", path, i+1)
		}
		if s.Version < 1 {
			return Manifest{}, fmt.Errorf("fetch: %s: %s has version %d, and an artefact belongs to a version", path, s.ID, s.Version)
		}
		if s.SHA256 == "" {
			return Manifest{}, fmt.Errorf("fetch: %s: %s records no hash, so it records nothing worth having", path, s.Ref())
		}
	}
	return m, nil
}

// Save writes the manifest, sorted, creating the directory if it is missing.
func (m Manifest) Save(path string) error {
	out := append([]Source(nil), m.Sources...)
	sortSources(out)
	body, err := yaml.Marshal(Manifest{Sources: out})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(header), body...), 0o644)
}

// Find returns the entry for one artefact.
func (m Manifest) Find(id string, version int, route Route) (Source, bool) {
	for _, s := range m.Sources {
		if s.ID == id && s.Version == version && s.Route == route {
			return s, true
		}
	}
	return Source{}, false
}

// Put adds an entry, replacing any entry for the same artefact.
//
// Same artefact means the same paper, the same version and the same route. Two
// routes over one version are two entries, because they are two files with two
// hashes and either can change without the other.
func (m *Manifest) Put(s Source) {
	for i := range m.Sources {
		if m.Sources[i].ID == s.ID && m.Sources[i].Version == s.Version && m.Sources[i].Route == s.Route {
			m.Sources[i] = s
			return
		}
	}
	m.Sources = append(m.Sources, s)
	sortSources(m.Sources)
}

func sortSources(s []Source) {
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].ID != s[j].ID {
			return s[i].ID < s[j].ID
		}
		if s[i].Version != s[j].Version {
			return s[i].Version < s[j].Version
		}
		return s[i].Route < s[j].Route
	})
}

// State is what verifying one entry found.
type State string

const (
	// StateOK is a file on disk that hashes to what the manifest says.
	StateOK State = "ok"
	// StateMissing is an entry whose file is not there.
	//
	// This is the ordinary state of a fresh clone, because work/ is not
	// committed. It is a finding and not a failure, and what the caller does
	// about it is fetch the file again.
	StateMissing State = "missing"
	// StateChanged is a file on disk that hashes to something else.
	StateChanged State = "changed"
)

// Verification is one entry checked against the disk.
type Verification struct {
	Source Source
	State  State
	// Got is the digest of what is actually there, empty when nothing is.
	Got string
	// Bytes is the size of what is actually there.
	Bytes int64
}

// Verify checks every entry against the files in root.
//
// It returns one verification per entry, in manifest order, so that a caller
// can count the states rather than being handed only the bad ones. A corpus
// where every entry is missing and a corpus where every entry is fine are both
// reported as nothing changed if only the changes come back.
func (m Manifest) Verify(root string) ([]Verification, error) {
	out := make([]Verification, 0, len(m.Sources))
	for _, s := range m.Sources {
		v := Verification{Source: s}
		sum, n, err := digestFile(filepath.Join(root, filepath.FromSlash(s.Path)))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			v.State = StateMissing
		case err != nil:
			return nil, err
		case sum == s.SHA256:
			v.State, v.Got, v.Bytes = StateOK, sum, n
		default:
			v.State, v.Got, v.Bytes = StateChanged, sum, n
		}
		out = append(out, v)
	}
	return out, nil
}

// Count returns how many entries are in each state.
func Count(vs []Verification) map[State]int {
	out := map[State]int{StateOK: 0, StateMissing: 0, StateChanged: 0}
	for _, v := range vs {
		out[v.State]++
	}
	return out
}

// Digest is the hash the manifest records, which is of the bytes exactly as
// they arrived.
//
// Before any parse and before any normalisation. The point of the hash is to
// answer whether the source moved, and a hash taken after this project touched
// the bytes would also change when this project changed.
func Digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func digestFile(path string) (string, int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	return Digest(b), int64(len(b)), nil
}

// Short is the first twelve characters of a digest, for printing.
func Short(sum string) string {
	if len(sum) <= 12 {
		return sum
	}
	return strings.ToLower(sum[:12])
}

package figures

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/tamnd/arxiv-reader/corpus"
	"gopkg.in/yaml.v3"
)

// Figure is one picture the corpus has decided about.
//
// A withheld figure has an entry here too, and that is the point of the file. A
// picture that is not published leaves nothing on disk to look at, so without a
// record of the decision nobody can tell a figure that was refused from one that
// was never fetched, and the same download would be paid for on every run.
type Figure struct {
	Paper   string `yaml:"paper"`
	Version int    `yaml:"version"`
	// Number is what the paper prints, so "2" for Figure 2, and it is empty for
	// a panel the author did not number.
	Number string `yaml:"number,omitempty"`
	// Source is the path arXiv's rendering gave, relative to /html/, which is
	// what identifies a figure before anything has been decided about it.
	Source string `yaml:"source"`
	// File is where the bytes were committed, rooted at the corpus, and it is
	// empty for a figure that was withheld.
	File string `yaml:"file,omitempty"`
	// SHA256 is of the bytes that arrived, for a committed figure and for a
	// withheld one alike. It is what the cross paper match in rule F09 reads.
	SHA256 string `yaml:"sha256"`
	Bytes  int    `yaml:"bytes"`
	Format Format `yaml:"format"`
	Width  int    `yaml:"width"`
	Height int    `yaml:"height"`
	// PageFraction is how much of a page the picture covers, and PageFrom is
	// where the physical size was read from. PageFrom empty means the file
	// stated no resolution, so rule F06 had nothing to go on and the zero in
	// PageFraction is not a measurement.
	PageFraction float64   `yaml:"page_fraction,omitempty"`
	PageFrom     string    `yaml:"page_from,omitempty"`
	ThirdParty   Suspicion `yaml:"third_party"`
	// Rule is the numbered rule that withheld it, empty for a committed figure.
	Rule string `yaml:"rule,omitempty"`
	Why  string `yaml:"why,omitempty"`
	// Caption is kept because rule F07 says every committed figure has a
	// manifest entry with a caption, and because it is what a person reading
	// this file needs to know which picture an entry is about.
	Caption string         `yaml:"caption"`
	Licence corpus.Licence `yaml:"licence"`
	Fetched time.Time      `yaml:"fetched"`
}

// Ref is the versioned reference this figure belongs to.
func (f Figure) Ref() string { return fmt.Sprintf("%sv%d", f.Paper, f.Version) }

// Committed says the bytes are in the corpus.
func (f Figure) Committed() bool { return f.File != "" }

// Manifest is manifests/figures/<shard>.yaml.
type Manifest struct {
	Figures []Figure `yaml:"figures"`
}

const header = `# Every figure this corpus looked at, what it measured, and what was decided.
#
# Written by ax figures. Do not edit by hand. An entry with no file is a picture
# that was withheld, and the caption, the number and the tag are published
# anyway, so the entry is the only record that the decision was made.
`

// Load reads one shard's figure manifest.
//
// A file that is not there is an empty manifest and not an error, because the
// first paper of a month has nothing to read.
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
		return Manifest{}, fmt.Errorf("figures: %s: %w", path, err)
	}
	for i, f := range m.Figures {
		switch {
		case f.Paper == "":
			return Manifest{}, fmt.Errorf("figures: %s: entry %d names no paper", path, i+1)
		case f.Version < 1:
			return Manifest{}, fmt.Errorf("figures: %s: %s has version %d, and a figure belongs to a version", path, f.Paper, f.Version)
		case f.Source == "":
			return Manifest{}, fmt.Errorf("figures: %s: %s has an entry with no source, so nothing can say which picture it is", path, f.Ref())
		case f.SHA256 == "":
			return Manifest{}, fmt.Errorf("figures: %s: %s records no hash for %s, so it records nothing worth having", path, f.Ref(), f.Source)
		case f.Committed() && f.Caption == "" && f.Number != "":
			// Rule F07. A numbered figure with no caption in the manifest is a
			// picture nobody can identify without opening the paper.
			return Manifest{}, fmt.Errorf("figures: %s: %s committed %s with no caption, which rule F07 forbids", path, f.Ref(), f.Source)
		}
	}
	return m, nil
}

// Save writes the manifest, sorted, creating the directory if it is missing.
//
// Sorted and with the time truncated to the second, because this file is
// committed and everything about it is built for a git diff: a re-run that
// decides the same things has to produce no diff at all.
func (m Manifest) Save(path string) error {
	out := append([]Figure(nil), m.Figures...)
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch {
		case a.Paper != b.Paper:
			return a.Paper < b.Paper
		case a.Version != b.Version:
			return a.Version < b.Version
		}
		return a.Source < b.Source
	})
	body, err := yaml.Marshal(Manifest{Figures: out})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(header), body...), 0o644)
}

// Find returns what was decided about one picture.
func (m Manifest) Find(paper string, version int, source string) (Figure, bool) {
	for _, f := range m.Figures {
		if f.Paper == paper && f.Version == version && f.Source == source {
			return f, true
		}
	}
	return Figure{}, false
}

// Put adds a figure or replaces the entry for the same picture.
func (m *Manifest) Put(f Figure) {
	for i, held := range m.Figures {
		if held.Paper == f.Paper && held.Version == f.Version && held.Source == f.Source {
			m.Figures[i] = f
			return
		}
	}
	m.Figures = append(m.Figures, f)
}

// Forget drops every entry for one version, which is what a recheck does before
// it decides again.
func (m *Manifest) Forget(paper string, version int) {
	kept := m.Figures[:0]
	for _, f := range m.Figures {
		if f.Paper != paper || f.Version != version {
			kept = append(kept, f)
		}
	}
	m.Figures = kept
}

// Index is the hash to owner map the cross paper half of rule F09 reads.
//
// Built from committed figures only. A withheld picture's bytes are not in the
// corpus, so it has nothing to accuse anybody of, and treating it as the owner
// of a hash would let one refused figure withhold the same picture from every
// paper that legitimately has it.
func (m Manifest) Index() map[string]Owner {
	out := map[string]Owner{}
	for _, f := range m.Figures {
		if !f.Committed() {
			continue
		}
		if _, seen := out[f.SHA256]; !seen {
			out[f.SHA256] = Owner{Paper: f.Paper, Licence: string(f.Licence)}
		}
	}
	return out
}

// Digest is the hash a manifest entry records.
func Digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

package vision

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

	"gopkg.in/yaml.v3"
)

// Entry is what happened to one page.
//
// Kept for the pages that were refused as well as the ones that were read, which
// is the whole reason this file exists. A page that failed at every resolution
// leaves no Markdown behind, so without an entry nobody can tell it from a page
// that was never asked about, and the next run would pay for it again.
type Entry struct {
	Page int `yaml:"page"`
	// DPI is the resolution the accepted reading came off, and for a refused
	// page it is the last one tried.
	DPI int `yaml:"dpi"`
	// File is the reading, named relative to this record, and it is empty for a
	// page nothing could read.
	File string `yaml:"file,omitempty"`
	// SHA256 is of the reading's bytes. It is what says a reading on disk is
	// still the reading this record is about, so an edited file is noticed rather
	// than trusted.
	SHA256 string `yaml:"sha256,omitempty"`
	Chars  int    `yaml:"chars"`
	// Tried is every resolution this page was asked at, in order, so a page that
	// needed six hundred dots says so and the report can count them.
	Tried []int `yaml:"tried,omitempty"`
	// Refused is what the rules said about the last reading, empty for a page
	// that was accepted. The rule identifier is part of each line.
	Refused []string `yaml:"refused,omitempty"`
	// Anyway says a person accepted this page over the rules' objection, which is
	// what a genuinely blank page needs.
	Anyway bool      `yaml:"anyway,omitempty"`
	Read   time.Time `yaml:"read"`
}

// Accepted says this page has a reading the corpus may use.
func (e Entry) Accepted() bool { return e.File != "" && (len(e.Refused) == 0 || e.Anyway) }

// Record is what one submission's vision run did, at work/vision/<shard>/<id>v<n>/read.yaml.
//
// Not in manifests and not committed, unlike every other record this project
// writes. What it holds is not a fact about the paper, it is a fact about one
// reading of it, and it is large: a forty page paper is forty entries and forty
// Markdown files beside it. What gets committed is the content plane, which carries
// the model and the prompt hash in its front matter, so the corpus can still say
// what read a paper without carrying the working notes.
type Record struct {
	Paper   string `yaml:"paper"`
	Version int    `yaml:"version"`
	// Model is what was asked, and Prompt is the hash of what it was asked. A
	// page whose model or prompt hash differs from the run in progress is asked
	// again, because a reading produced by different instructions is a different
	// reading even when it looks the same.
	Model  string `yaml:"model"`
	Prompt string `yaml:"prompt_sha256"`
	// Painter is what pdftoppm said it was, because poppler renders a page
	// differently between releases and a page that reads differently after an
	// upgrade should have somewhere to point.
	Painter string  `yaml:"painter,omitempty"`
	Pages   []Entry `yaml:"pages"`
}

const header = `# What one vision run read, page by page, and what the rules said about it.
#
# Written by ax extract vision. Do not edit by hand. This is working state
# and not part of the corpus: the readings beside it are the model's output before
# anything was made of them, and an entry with no file is a page nothing could
# read at any resolution on the ladder.
`

// RecordName is what the record is called inside the vision directory.
const RecordName = "read.yaml"

// Load reads one submission's record.
//
// A file that is not there is an empty record and not an error, because the first
// run over a paper has nothing to read.
func Load(dir string) (*Record, error) {
	path := filepath.Join(dir, RecordName)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &Record{}, nil
	}
	if err != nil {
		return nil, err
	}
	var r Record
	if err := yaml.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("vision: %s: %w", path, err)
	}
	for i, e := range r.Pages {
		if e.Page < 1 {
			return nil, fmt.Errorf("vision: %s: entry %d is page %d, and pages are counted from one", path, i+1, e.Page)
		}
	}
	return &r, nil
}

// Save writes the record, sorted by page.
func (r *Record) Save(dir string) error {
	out := append([]Entry(nil), r.Pages...)
	sort.Slice(out, func(i, j int) bool { return out[i].Page < out[j].Page })
	for i := range out {
		out[i].Read = out[i].Read.UTC().Truncate(time.Second)
	}
	body, err := yaml.Marshal(&Record{
		Paper: r.Paper, Version: r.Version, Model: r.Model,
		Prompt: r.Prompt, Painter: r.Painter, Pages: out,
	})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, RecordName), append([]byte(header), body...), 0o644)
}

// Find is the entry for one page.
func (r *Record) Find(page int) (Entry, bool) {
	for _, e := range r.Pages {
		if e.Page == page {
			return e, true
		}
	}
	return Entry{}, false
}

// Put adds an entry or replaces the one for the same page.
func (r *Record) Put(e Entry) {
	for i, held := range r.Pages {
		if held.Page == e.Page {
			r.Pages[i] = e
			return
		}
	}
	r.Pages = append(r.Pages, e)
}

// PageName is what one page's reading is called.
func PageName(page int) string { return fmt.Sprintf("p%03d.md", page) }

// Done is the reading this page already has, if there is one worth keeping.
//
// The three things that have to hold, and all three are why this path can be run
// again over a paper it half finished without paying twice. The entry has to have
// been accepted. It has to have been read by the model this run is using, under the
// prompt this run is using, because a reading made under different instructions is
// not this run's reading. And the file beside it has to still hash to what the
// entry says, because a reading somebody edited by hand is a reading the record can
// no longer speak for.
func (r *Record) Done(dir string, page int, model, prompt string) (string, bool) {
	if r.Model != model || r.Prompt != prompt {
		return "", false
	}
	e, ok := r.Find(page)
	if !ok || !e.Accepted() {
		return "", false
	}
	b, err := os.ReadFile(filepath.Join(dir, e.File))
	if err != nil {
		return "", false
	}
	if Hash(b) != e.SHA256 {
		return "", false
	}
	return string(b), true
}

// Write puts one page's reading beside the record and returns its entry's file and
// hash.
func Write(dir string, page int, text string) (string, string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	name := PageName(page)
	b := []byte(text)
	if len(b) > 0 && b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
		return "", "", err
	}
	return name, Hash(b), nil
}

// Readings are the accepted pages in page order, with a gap for every page nothing
// could read.
//
// A gap and not a shortened list, because the extractor reads a slice whose index
// is the page number and a paper that lost page seven has to come out with the
// pages after it still numbered as the paper numbers them.
func (r *Record) Readings(dir string, pages int) []string {
	out := make([]string, pages)
	for _, e := range r.Pages {
		if !e.Accepted() || e.Page < 1 || e.Page > pages {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.File))
		if err != nil {
			continue
		}
		out[e.Page-1] = string(b)
	}
	return out
}

// Missing is the pages of this paper that have no accepted reading.
func (r *Record) Missing(pages int) []int {
	var out []int
	for page := 1; page <= pages; page++ {
		if e, ok := r.Find(page); !ok || !e.Accepted() {
			out = append(out, page)
		}
	}
	return out
}

// Hash is the hex sha256 of some bytes, which is how every hash in this project is
// written down.
func Hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

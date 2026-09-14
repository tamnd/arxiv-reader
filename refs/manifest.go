package refs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Manifest is manifests/refs/<shard>/<id>.yaml, one paper's bibliography.
//
// Keyed by version as well as by paper, because a paper adds references between
// versions constantly and a bibliography read from v1 does not describe v3.
type Manifest struct {
	Paper   string  `yaml:"paper"`
	Version int     `yaml:"version"`
	Entries []Entry `yaml:"entries"`
}

const header = `# The bibliography of one paper, parsed into fields.
#
# Written by ax refs. Do not edit by hand. The text of each entry is what gets
# published and the fields are a reading of it, so a field that is empty is a
# field the style did not make available and is not an error.
`

// Load reads one paper's references.
//
// A file that is not there is an empty manifest and not an error, because a
// paper nobody has run ax refs over has no references to read.
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
		return Manifest{}, fmt.Errorf("refs: %s: %w", path, err)
	}
	for i, e := range m.Entries {
		switch {
		case e.ID == "":
			return Manifest{}, fmt.Errorf("refs: %s: entry %d has no id, so nothing in the paper can point at it", path, i+1)
		case e.Text == "":
			return Manifest{}, fmt.Errorf("refs: %s: %s has no text, and the text is the part that gets published", path, e.ID)
		}
	}
	return m, nil
}

// Bytes is the manifest as it would be written.
//
// Separate from Save so that a run can tell whether anything changed without
// writing first. Entries keep the order the paper prints them in and are not
// sorted, because that order is the numbering in a numeric style and is
// information the file should not throw away.
func (m Manifest) Bytes() ([]byte, error) {
	body, err := yaml.Marshal(m)
	if err != nil {
		return nil, err
	}
	return append([]byte(header), body...), nil
}

// Save writes the manifest and says whether the bytes changed.
func (m Manifest) Save(path string) (bool, error) {
	body, err := m.Bytes()
	if err != nil {
		return false, err
	}
	if old, err := os.ReadFile(path); err == nil && string(old) == string(body) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, body, 0o644)
}

// Find returns the entry one anchor names.
func (m Manifest) Find(id string) (Entry, bool) {
	for _, e := range m.Entries {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

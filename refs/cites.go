package refs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Cites is manifests/cites/<shard>/<id>.yaml, every citation one paper makes.
//
// Beside the bibliography rather than inside it, because the two are counted
// differently and joined by the anchor. The refs manifest has one entry per work
// cited and this has one record per place in the paper that cites it.
//
// Nothing here says what the entry resolved to. That is in the refs manifest and
// it belongs in exactly one file, so the graph joins the two rather than reading
// a copy of one out of the other and having to wonder which is older.
type Cites struct {
	Paper   string `yaml:"paper"`
	Version int    `yaml:"version"`
	Cites   []Cite `yaml:"cites"`
}

const citesHeader = `# Every citation one paper makes, with the locator each of them carries.
#
# Written by ax refs cites. Do not edit by hand. One record per place in the
# paper that cites something, which is why a work cited nine times is nine lines
# here and one line in the bibliography beside it.
`

// LoadCites reads one paper's citations.
//
// A file that is not there is an empty manifest and not an error, the same as
// the bibliography beside it.
func LoadCites(path string) (Cites, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Cites{}, nil
	}
	if err != nil {
		return Cites{}, err
	}
	var c Cites
	if err := yaml.Unmarshal(b, &c); err != nil {
		return Cites{}, fmt.Errorf("refs: %s: %w", path, err)
	}
	for i, it := range c.Cites {
		switch {
		case it.Entry == "":
			return Cites{}, fmt.Errorf("refs: %s: citation %d names no entry, so it points at nothing", path, i+1)
		case it.Section == "":
			return Cites{}, fmt.Errorf("refs: %s: citation %d names no section, and where it sits is half of what it says", path, i+1)
		}
	}
	return c, nil
}

// Bytes is the manifest as it would be written.
func (c Cites) Bytes() ([]byte, error) {
	body, err := yaml.Marshal(c)
	if err != nil {
		return nil, err
	}
	return append([]byte(citesHeader), body...), nil
}

// Save writes the manifest and says whether the bytes changed.
func (c Cites) Save(path string) (bool, error) {
	body, err := c.Bytes()
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

// Attach keeps the citations whose anchor the bibliography carries, and names
// the anchors it does not.
//
// An anchor no entry carries is not a citation to record. It means the sections
// and the bibliography were read from different versions of the paper, because
// an anchor is only unique inside one version and a paper renumbers its
// references every time it adds one. Recording it would put an edge in the graph
// pointing at whatever that number happens to mean now.
func (m Manifest) Attach(cites []Cite) ([]Cite, []string) {
	var kept []Cite
	var orphan []string
	seen := map[string]bool{}
	for _, c := range cites {
		if _, ok := m.Find(c.Entry); ok {
			kept = append(kept, c)
			continue
		}
		if !seen[c.Entry] {
			seen[c.Entry] = true
			orphan = append(orphan, c.Entry)
		}
	}
	return kept, orphan
}

// Located is the citations that named a particular result, which are the ones a
// uses edge can be built on.
func (c Cites) Located() []Cite {
	var out []Cite
	for _, it := range c.Cites {
		if it.Kind != "" {
			out = append(out, it)
		}
	}
	return out
}

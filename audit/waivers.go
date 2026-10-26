package audit

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Waiver excuses one record from one rule, with the reason.
//
// A waiver is for a record that is wrong at the source and that the corpus must
// keep as the source gives it. An example is a version that arXiv dates before
// the version before it. The rule is right, and the record is right too.
type Waiver struct {
	ID     string `yaml:"id"`
	Rule   string `yaml:"rule"`
	Reason string `yaml:"reason"`
}

// Waivers is the waivers of a plane, by rule and then by id.
type Waivers map[string]map[string]string

// Reason is the reason a finding is waived, and false when it is not.
func (w Waivers) Reason(rule, id string) (string, bool) {
	r, ok := w[strings.ToUpper(rule)][id]
	return r, ok
}

// ReadWaivers reads a waiver file. A file that is not there is no waivers.
// Each entry must name an id, a rule and a reason, because a waiver with no
// reason is a finding that somebody hid.
func ReadWaivers(path string) (Waivers, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file struct {
		Waivers []Waiver `yaml:"waivers"`
	}
	if err := yaml.Unmarshal(b, &file); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	out := Waivers{}
	for i, w := range file.Waivers {
		if w.ID == "" || w.Rule == "" || strings.TrimSpace(w.Reason) == "" {
			return nil, fmt.Errorf("%s: waiver %d must name an id, a rule and a reason", path, i+1)
		}
		rule := strings.ToUpper(w.Rule)
		if out[rule] == nil {
			out[rule] = map[string]string{}
		}
		if _, ok := out[rule][w.ID]; ok {
			return nil, fmt.Errorf("%s: %s is waived from %s twice", path, w.ID, rule)
		}
		out[rule][w.ID] = w.Reason
	}
	return out, nil
}

package analysis

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"

	"github.com/tamnd/arxiv-reader/census"
	"gopkg.in/yaml.v3"
)

// WaiverReasons is the closed list of the reasons of a waiver (AX4.8). A
// reason can also be bug:<issue>, a known bug of the extractor.
var WaiverReasons = []string{"layout", "empty", "not-math", "duplicate-main", "author-error"}

var bugReason = regexp.MustCompile(`^bug:[1-9][0-9]*$`)

// KnownWaiverReason tells if a reason is in the closed list.
func KnownWaiverReason(r string) bool {
	if bugReason.MatchString(r) {
		return true
	}
	for _, k := range WaiverReasons {
		if r == k {
			return true
		}
	}
	return false
}

// Waivers is manifests/waivers.yaml: the waivers that a person wrote, one for
// each occurrence, and the waiver rules that a person approved.
type Waivers struct {
	byVersion map[string][]census.Waiver
	rules     []census.Waiver
}

type waiverFile struct {
	Waivers []struct {
		Paper      string `yaml:"paper"`
		Occurrence string `yaml:"occurrence"`
		Reason     string `yaml:"reason"`
		By         string `yaml:"by"`
	} `yaml:"waivers"`
	Rules []struct {
		Name     string `yaml:"name"`
		Env      string `yaml:"env"`
		Reason   string `yaml:"reason"`
		Approved string `yaml:"approved"`
	} `yaml:"rules"`
}

// ReadWaivers reads a waiver file. A file that is not there is no waivers.
// Each waiver must name a version as <id>v<n>, an occurrence, a reason from
// the closed list and the person. Each rule must name itself, an environment,
// a reason and the person who approved it.
func ReadWaivers(path string) (*Waivers, error) {
	w := &Waivers{byVersion: map[string][]census.Waiver{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return w, nil
	}
	if err != nil {
		return nil, err
	}
	var f waiverFile
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	seen := map[string]bool{}
	for i, x := range f.Waivers {
		if x.Paper == "" || x.Occurrence == "" || strings.TrimSpace(x.By) == "" {
			return nil, fmt.Errorf("%s: waiver %d must name a paper, an occurrence and the person", path, i+1)
		}
		if !versionRef.MatchString(x.Paper) {
			return nil, fmt.Errorf("%s: waiver %d names %q, which is not <id>v<n>", path, i+1, x.Paper)
		}
		if !KnownWaiverReason(x.Reason) {
			return nil, fmt.Errorf("%s: waiver %d has the reason %q, which is not in the closed list", path, i+1, x.Reason)
		}
		// An occurrence can be given as env-3 or as the whole id <id>v<n>#env-3.
		occ := x.Occurrence
		if !strings.Contains(occ, "#") {
			occ = x.Paper + "#" + occ
		}
		if seen[occ] {
			return nil, fmt.Errorf("%s: %s is waived twice", path, occ)
		}
		seen[occ] = true
		w.byVersion[x.Paper] = append(w.byVersion[x.Paper], census.Waiver{Occurrence: occ, Reason: x.Reason, By: x.By})
	}
	envs := map[string]bool{}
	for i, x := range f.Rules {
		if x.Name == "" || x.Env == "" || strings.TrimSpace(x.Approved) == "" {
			return nil, fmt.Errorf("%s: rule %d must name itself, an environment and the person who approved it", path, i+1)
		}
		if !KnownWaiverReason(x.Reason) {
			return nil, fmt.Errorf("%s: rule %s has the reason %q, which is not in the closed list", path, x.Name, x.Reason)
		}
		if envs[x.Env] {
			return nil, fmt.Errorf("%s: two rules waive the environment %s", path, x.Env)
		}
		envs[x.Env] = true
		w.rules = append(w.rules, census.Waiver{Env: x.Env, Reason: x.Reason, By: "rule:" + x.Name})
	}
	return w, nil
}

var versionRef = regexp.MustCompile(`^.+v[1-9][0-9]*$`)

// For is the waivers of one version: its own and the rules.
func (w *Waivers) For(paper string, version int) []census.Waiver {
	if w == nil {
		return nil
	}
	own := w.byVersion[fmt.Sprintf("%sv%d", paper, version)]
	out := make([]census.Waiver, 0, len(own)+len(w.rules))
	out = append(out, own...)
	return append(out, w.rules...)
}

// Len is the count of the waivers and of the rules.
func (w *Waivers) Len() (waivers, rules int) {
	if w == nil {
		return 0, 0
	}
	for _, x := range w.byVersion {
		waivers += len(x)
	}
	return waivers, len(w.rules)
}

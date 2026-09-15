// Package policy reads manifests/selection.yaml, which is the file this corpus
// is run by rather than the file it is described by.
//
// The other manifests record decisions that have already been made: selected.yaml
// says which papers are in and why, sources.yaml says what was downloaded, and
// baselines.yaml says what the corpus turned out to look like. This one is the
// other direction. It holds the numbers and the lists that change what happens
// next, and it is versioned for exactly that reason, because a change to it is a
// change to what the next run does and that should be reviewable.
//
// Today it holds one section, which is the audit's expectations per extraction
// path. The selection weights in 2166-04 and the extraction timeout in 2166-05
// belong in the same file and arrive with the commands that read them.
package policy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tamnd/arxiv-reader/selection"
)

// Manifest is the whole file.
type Manifest struct {
	Audit Audit `yaml:"audit"`
}

// Audit is what the numbered rules are expected to be able to answer.
//
// The four extraction paths do not produce the same thing. A paper read off a
// printed page has had its mathematics flattened into prose by pdftotext, so
// every rule that reads a TeX span has nothing to read, and the rule that says
// a paper has no figure files is stating the definition of that path rather
// than finding anything.
//
// Reporting those as pass is how a corpus acquires a rule everybody believes is
// working. Reporting them as not run is not much better, because it makes a
// correctly configured paper look like an unfinished one. So the path is an
// input to the audit, this section says which rules each path cannot answer,
// and the audit marks the rest not applicable.
type Audit struct {
	// Skip is, per path, the rules that path is not expected to satisfy.
	//
	// Written as what is skipped rather than as what is expected, which is the
	// important direction. A rule added next year is expected on every path
	// until somebody says otherwise, so the failure mode of forgetting to edit
	// this file is a rule that runs where it should not rather than a rule that
	// silently stops running. The first is noise somebody will complain about
	// and the second is a hole nobody finds.
	Skip map[selection.Path][]string `yaml:"skip"`
}

// Expects says whether a paper on this path is expected to satisfy a rule.
//
// A path nobody has recorded expects everything, which is the same direction as
// above: a paper whose front matter says nothing about how it was read is not a
// paper to stop checking.
func (a Audit) Expects(p selection.Path, rule string) bool {
	for _, skip := range a.Skip[p] {
		if strings.EqualFold(skip, rule) {
			return false
		}
	}
	return true
}

// Skipped is the rules one path does not answer, sorted.
func (a Audit) Skipped(p selection.Path) []string {
	out := append([]string(nil), a.Skip[p]...)
	sort.Strings(out)
	return out
}

// Default is the policy a corpus with no file of its own is run by.
//
// It is the list 2166-05 states, and nothing beyond it. Two kinds of rule are
// deliberately not here whatever a path produces. No rule in the S group is,
// because a licence rule marked not applicable is a licence breach with a label
// on it. Nor is any figure rule about bytes that are committed, because those
// ask whether something is on disk that should not be, and a path that commits
// no figures answers that with a pass rather than with a shrug.
func Default() Manifest {
	return Manifest{Audit: Audit{Skip: map[selection.Path][]string{
		// A PDF's text layer holds what a formula was printed as and not the
		// formula, so this path flattens every formula by construction and says
		// so in path: native. M05 and M13 stay, because the first reads the
		// replacement character that a file read with the wrong encoding brings
		// with it and the second reads the file's own Markdown, and this path
		// writes both of those like any other.
		selection.PathNative: {
			"M01", "M02", "M03", "M07", "M08", "M09",
			"M10", "M11", "M12", "M14",
			// A figure's picture is in the PDF as a drawing and not as a file
			// and a table's cells come back as text in columns, so this path
			// records the float, its number and its caption and nothing else.
			// F10 asks for the picture behind a number the paper printed, and
			// F11 and F12 ask for a table kept twice.
			"F10", "F11", "F12",
		},
	}}}
}

// Load reads the policy, falling back to the default.
//
// A corpus with no file of its own is run by the default, and so is one whose
// file says nothing about the audit. The second is the case worth being
// deliberate about: somebody adding the selection weights to this file should
// not quietly turn the M group loose on every paper read off a printed page.
func Load(path string) (Manifest, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := yaml.Unmarshal(b, &m); err != nil {
		return Manifest{}, fmt.Errorf("policy: %s: %w", path, err)
	}
	if m.Audit.Skip == nil {
		m.Audit = Default().Audit
	}
	for p := range m.Audit.Skip {
		if p != "" && !selection.KnownPath(p) {
			return Manifest{}, fmt.Errorf("policy: %s: %q is not one of the four paths, which are %s", path, p, selection.PathNames())
		}
	}
	return m, nil
}

// Save writes the policy, creating the directory if it is missing.
func (m Manifest) Save(path string) error {
	body, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(header), body...), 0o644)
}

const header = `# What the next run is told to do, rather than what the last one did.
#
# Versioned on purpose. Everything in here changes what happens next, so a diff
# to this file is a decision somebody should read before it lands.
#
# audit.skip is, per extraction path, the rules that path is not expected to be
# able to answer. The audit marks those not applicable rather than passed, so
# that a decision to read a lot of papers off printed pages shows up as what it
# is, which is a decision to stop checking their mathematics.
`

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
	Audit     Audit     `yaml:"audit"`
	Selection Selection `yaml:"selection"`
}

// Selection is the weights the selection queue is ordered by.
//
// From 2166-04, which says the score is a weighted sum of the citation count
// from inside the corpus, the citation count from outside where one is known,
// the age of the paper, and a penalty for a paper whose extraction path is
// vision. Its only job is to order a queue that is already filtered by licence,
// and it decides nothing else.
//
// The weights are in this file rather than in the code because a change to them
// changes what gets read next, and that should arrive as a diff somebody can
// argue with rather than as a release note.
type Selection struct {
	// Inside weights a citation from a paper already in the content plane.
	//
	// The heaviest term by a long way, because a paper the corpus already cites
	// three times is a paper the corpus is already talking about, and that is a
	// better reason to read something than anything a stranger's ranking says.
	Inside float64 `yaml:"inside"`
	// Outside weights a citation from anywhere else, where a count is known.
	//
	// No count is known today. The Cornell snapshot does not carry one and
	// arXiv does not publish one, so this term is nought on every paper until
	// somebody joins a citation index onto the metadata plane.
	Outside float64 `yaml:"outside"`
	// Age weights a year of age, up to AgeCap.
	//
	// Positive, because a paper that has been out for a decade has had a decade
	// to be built on, and capped, because the difference between twenty and
	// thirty years is not four times the difference between two and four.
	Age    float64 `yaml:"age"`
	AgeCap int     `yaml:"age_cap"`
	// Vision is the penalty for a paper only the vision path can read.
	//
	// Negative. A paper nobody can read except by photographing its pages costs
	// the most of any paper in the corpus and produces the least, so it needs a
	// better reason than a paper arXiv already renders.
	Vision float64 `yaml:"vision"`
}

// Zero reports whether nobody has set any of the weights.
//
// Used to tell a file that says nothing about the selection from one that sets
// every weight to nought, which is a corpus somebody has deliberately made
// scoreless and is not the same thing.
func (s Selection) Zero() bool { return s == Selection{} }

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
	return Manifest{Selection: DefaultSelection(), Audit: Audit{Skip: map[selection.Path][]string{
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

// DefaultSelection is the weights a corpus with no file of its own is run by.
//
// A citation from inside the corpus is worth ten of the years this project
// could plausibly wait, which is the ordering 2166-04 asks for: the closure is
// what makes the corpus a connected thing rather than a list, so a paper the
// corpus already points at three times outranks anything chosen on age. The
// vision penalty is set so that a paper only a camera can read needs about two
// inside citations before it is worth the run.
func DefaultSelection() Selection {
	return Selection{Inside: 10, Outside: 0.01, Age: 1, AgeCap: 25, Vision: -20}
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
	if m.Selection.Zero() {
		m.Selection = DefaultSelection()
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
#
# selection holds the weights the queue is ordered by. The score decides nothing
# except what gets read next, and it is applied to a list the licence gate has
# already filtered, so no weight in here can put a paper in the corpus that the
# corpus may not publish.
`

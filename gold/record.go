package gold

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tamnd/arxiv-reader/objects"
)

// The states of a gold record.
const (
	// Proposed is a record whose marks a program proposed. It is not scored.
	Proposed = "proposed"
	// Marked is a record whose marks a person decided. It names the person.
	Marked = "marked"
)

// Record is the gold record of one version of one paper in one gold set.
type Record struct {
	Set        string `json:"set"`
	SetVersion string `json:"set_version"`
	Paper      string `json:"paper"`
	Version    int    `json:"version"`
	Group      string `json:"group,omitempty"`
	State      string `json:"state"`
	// ProposedBy is the program that proposed the marks, when one did.
	ProposedBy string `json:"proposed_by,omitempty"`
	// By is the person who decided the marks, and On the date.
	By    string `json:"by,omitempty"`
	On    string `json:"on,omitempty"`
	Marks []Mark `json:"marks"`
}

// Mark is one item of a paper, as a person marks it (AX11.2).
type Mark struct {
	ID string `json:"id"`
	// Kind is an object kind: statement, definition, proof, remark, example,
	// and the others of the object record. Subkind is the item kind inside
	// it, as theorem or lemma.
	Kind    string `json:"kind"`
	Subkind string `json:"subkind,omitempty"`
	Number  string `json:"number,omitempty"`
	Label   string `json:"label,omitempty"`
	// File, From and To are the place of the item in the expanded source,
	// as lines.
	File  string   `json:"file,omitempty"`
	From  int      `json:"from,omitempty"`
	To    int      `json:"to,omitempty"`
	Parts []string `json:"parts,omitempty"`
	// Proves is the id of the mark that a proof proves.
	Proves      string `json:"proves,omitempty"`
	ProofStatus string `json:"proof_status,omitempty"`
	// Prose is true for an item that has no environment and no manual head,
	// as a definition in a sentence.
	Prose bool `json:"prose,omitempty"`
}

// Check tells what is wrong with a record, or nil.
func (r Record) Check() error {
	var errs []error
	if r.Set == "" || r.SetVersion == "" || r.Paper == "" || r.Version < 1 {
		errs = append(errs, errors.New("the record must name the set, its version, the paper and the version of the paper"))
	}
	switch r.State {
	case Proposed:
	case Marked:
		if strings.TrimSpace(r.By) == "" {
			errs = append(errs, errors.New("a marked record must name the person who marked it"))
		}
	default:
		errs = append(errs, fmt.Errorf("the state %q is not proposed or marked", r.State))
	}
	ids := map[string]bool{}
	for _, m := range r.Marks {
		if m.ID == "" || m.Kind == "" {
			errs = append(errs, fmt.Errorf("each mark needs an id and a kind, and %+v has not", m))
			continue
		}
		if ids[m.ID] {
			errs = append(errs, fmt.Errorf("the mark id %s is used twice", m.ID))
		}
		ids[m.ID] = true
	}
	for _, m := range r.Marks {
		if m.Proves != "" && !ids[m.Proves] {
			errs = append(errs, fmt.Errorf("mark %s proves %s, and no mark has that id", m.ID, m.Proves))
		}
	}
	return errors.Join(errs...)
}

// Read reads a gold record.
func Read(path string) (Record, error) {
	var r Record
	b, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, fmt.Errorf("%s: %w", path, err)
	}
	if err := r.Check(); err != nil {
		return r, fmt.Errorf("%s: %w", path, err)
	}
	return r, nil
}

// Write writes a gold record, after the check.
func (r Record) Write(path string) error {
	if err := r.Check(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// Propose makes a proposed record from the object records of a version. A
// person then reads the paper, corrects the marks and marks the record.
func Propose(set, setVersion, group, by string, paper string, version int, recs []objects.Record) Record {
	r := Record{Set: set, SetVersion: setVersion, Paper: paper, Version: version, Group: group, State: Proposed, ProposedBy: by, Marks: []Mark{}}
	v := fmt.Sprint(version)
	for _, o := range Items(recs, v) {
		m := Mark{ID: o.Local, Kind: o.Kind, Subkind: o.Subkind, Number: o.Number, Label: o.Label, Proves: o.Proves, ProofStatus: o.ProofStatus}
		if o.Span != nil {
			m.File, m.From, m.To = o.Span.File, o.Span.From, o.Span.To
		}
		for _, p := range o.Parts {
			m.Parts = append(m.Parts, p.Marker)
		}
		r.Marks = append(r.Marks, m)
	}
	return r
}

// Items is the object records of a version that the census counts: the
// records of the source path with an item kind.
func Items(recs []objects.Record, version string) []objects.Record {
	var out []objects.Record
	for _, o := range recs {
		if o.Version == version && o.Path == "source" && o.Item != "" {
			out = append(out, o)
		}
	}
	return out
}

package extract

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// State is what happened to one file of the content plane.
type State string

const (
	// StateWritten means the file was created or its contents changed.
	StateWritten State = "written"
	// StateUnchanged means the bytes on disk were already exactly these.
	StateUnchanged State = "unchanged"
	// StateProtected means somebody had corrected the file by hand and it was
	// left alone.
	StateProtected State = "protected"
	// StateRemoved means the file is no longer part of the paper.
	StateRemoved State = "removed"
)

// Result is one file and what happened to it.
type Result struct {
	Name  string
	State State
	// Why is filled for a protected file and says what was found.
	Why string
}

// Write puts a paper's files in a directory.
//
// Three properties, and the second and third are the ones that matter.
//
// It is idempotent. A file whose bytes are already what this would write is not
// written, so git status after a re-run of a finished paper is empty, and that
// is the test that this stage is honest rather than a claim about it.
//
// It does not overwrite a correction. A file whose recorded hash no longer
// matches its body, or which is marked edited, has been worked on by a person,
// and a person's work outranks a re-run of a converter. Force is the way past
// that and it is deliberately not the default.
//
// It removes what is no longer part of the paper. A re-extraction that produces
// four sections where there were five has to take the fifth file away, because
// a content plane holding a section the paper no longer has is a corpus that
// publishes something arXiv does not.
func Write(dir string, files []File, force bool) ([]Result, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	keep := map[string]bool{}
	var out []Result
	for _, f := range files {
		keep[f.Name] = true
		r, err := writeOne(filepath.Join(dir, f.Name), f, force)
		if err != nil {
			return nil, err
		}
		r.Name = f.Name
		out = append(out, r)
	}
	stale, err := stale(dir, keep)
	if err != nil {
		return nil, err
	}
	for _, name := range stale {
		r := Result{Name: name, State: StateRemoved}
		if !force {
			if d, err := readDoc(filepath.Join(dir, name)); err == nil && (d.Corrected() || d.Front.Edited) {
				r.State = StateProtected
				r.Why = "it is no longer part of the paper and it has been corrected by hand"
				out = append(out, r)
				continue
			}
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func writeOne(path string, f File, force bool) (Result, error) {
	want, err := f.Doc.Bytes()
	if err != nil {
		return Result{}, err
	}
	if have, err := os.ReadFile(path); err == nil {
		if string(have) == string(want) {
			return Result{State: StateUnchanged}, nil
		}
		if !force {
			d, err := ParseDocument(have)
			switch {
			case err != nil:
				return Result{State: StateProtected, Why: "it is on disk and this cannot read it, so it is not this tool's to overwrite"}, nil
			case d.Corrected():
				return Result{State: StateProtected, Why: "its body no longer matches its content_sha256, so it has been corrected by hand"}, nil
			case d.Front.Edited:
				return Result{State: StateProtected, Why: "it is marked edited"}, nil
			}
		}
	}
	if err := os.WriteFile(path, want, 0o644); err != nil {
		return Result{}, err
	}
	return Result{State: StateWritten}, nil
}

// sectionFile is the shape of a name this package owns.
//
// Only files it wrote are ever removed. A person who put notes.md in a paper's
// directory did so on purpose and a converter is not entitled to an opinion
// about it.
var sectionFile = regexp.MustCompile(`^[0-9]{2}_[a-z0-9_]+\.md$`)

func stale(dir string, keep map[string]bool) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || keep[name] || !sectionFile.MatchString(name) {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}

func readDoc(path string) (Document, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Document{}, err
	}
	return ParseDocument(b)
}

// Read loads one content file.
func Read(path string) (Document, error) { return readDoc(path) }

// Check reads every content file in a directory and says which ones have been
// corrected since they were written.
//
// This is what ax split runs. The hash is the whole mechanism: a correction
// somebody made in an editor is visible to the pipeline without their having
// had to tell it, and the alternative, which is trusting that nobody edits the
// corpus, is a promise every corpus breaks in its first month.
func Check(dir string) ([]Result, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && sectionFile.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	var results []Result
	for _, name := range out {
		d, err := readDoc(filepath.Join(dir, name))
		if err != nil {
			results = append(results, Result{Name: name, State: StateProtected, Why: err.Error()})
			continue
		}
		switch {
		case d.Front.ContentSHA256 == "":
			results = append(results, Result{Name: name, State: StateProtected, Why: "it carries no content_sha256, so nothing can tell whether it has been corrected"})
		case d.Corrected():
			results = append(results, Result{Name: name, State: StateProtected, Why: fmt.Sprintf("its body hashes to %s and its front matter says %s", short(Hash(d.Body)), short(d.Front.ContentSHA256))})
		default:
			results = append(results, Result{Name: name, State: StateUnchanged})
		}
	}
	return results, nil
}

// Accept restamps a corrected file and marks it edited.
//
// After this the correction is the file, and the next extraction will refuse to
// overwrite it rather than refusing because the hash does not match. The
// difference matters: an unaccepted correction is protected because something
// looks wrong, and an accepted one is protected because somebody decided.
func Accept(dir, name string) (bool, error) {
	path := filepath.Join(dir, name)
	d, err := readDoc(path)
	if err != nil {
		return false, err
	}
	if !d.Corrected() && d.Front.Edited {
		return false, nil
	}
	d.Front.Edited = true
	b, err := d.Bytes()
	if err != nil {
		return false, err
	}
	if have, err := os.ReadFile(path); err == nil && string(have) == string(b) {
		return false, nil
	}
	return true, os.WriteFile(path, b, 0o644)
}

func short(sum string) string {
	if len(sum) > 12 {
		return sum[:12]
	}
	return sum
}

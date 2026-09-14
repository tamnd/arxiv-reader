package tags

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Entry is one line of a paper's register: a tag and the object it names.
//
// Gone and Note are the tombstone. An object that is in the register and no
// longer in the paper is not deleted, it is marked with the version it was last
// present in and a sentence saying what happened to it, and the reading app
// serves its tag with a page saying so. That is straight from the Stacks
// Project, which keeps the tag of a result that turned out to be wrong along
// with an explanation of its disappearance. A reference that resolves to an
// explanation is a working reference and a reference that 404s is a broken
// promise.
type Entry struct {
	Tag   Tag
	Local string
	Gone  string
	Note  string
}

// Register is one paper's tags, in the order they were handed out.
//
// Order is the file's own and it matters, because the runs file names the first
// and last tag of each assignment and everything between them is that
// assignment's work. Sorting this would lose that.
type Register struct {
	Entries []Entry
}

// LoadRegister reads a paper's register, and a paper nobody has tagged has an
// empty one rather than an error.
func LoadRegister(path string) (Register, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Register{}, nil
	}
	if err != nil {
		return Register{}, err
	}
	return ParseRegister(b)
}

// ParseRegister reads the comma separated form.
func ParseRegister(b []byte) (Register, error) {
	r := csv.NewReader(bytes.NewReader(b))
	r.Comment = '#'
	// A line is two fields or four, so the reader is told not to hold them to
	// one shape and this checks the shape itself with a message a person can
	// act on.
	r.FieldsPerRecord = -1
	var reg Register
	seenTag := map[Tag]bool{}
	seenLocal := map[string]bool{}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Register{}, fmt.Errorf("tags: %w", err)
		}
		if len(rec) < 2 || len(rec) > 4 {
			return Register{}, fmt.Errorf("tags: a register line has %d fields, and a line is tag,local or tag,local,gone:vN,note", len(rec))
		}
		e := Entry{Tag: Tag(rec[0]), Local: rec[1]}
		if len(rec) > 2 {
			e.Gone = strings.TrimPrefix(rec[2], "gone:")
		}
		if len(rec) > 3 {
			e.Note = rec[3]
		}
		if !e.Tag.Valid() {
			return Register{}, fmt.Errorf("tags: %q is not a tag, and a tag is four characters of %s", rec[0], Alphabet)
		}
		if e.Local == "" {
			return Register{}, fmt.Errorf("tags: %s names no object, and a tag with no local identifier is a tag nothing resolves", e.Tag)
		}
		if seenTag[e.Tag] {
			return Register{}, fmt.Errorf("tags: %s is in the register twice, so two objects in one paper answer to one name", e.Tag)
		}
		if seenLocal[e.Local] {
			return Register{}, fmt.Errorf("tags: %s is in the register twice, so one object in one paper has two names", e.Local)
		}
		seenTag[e.Tag], seenLocal[e.Local] = true, true
		reg.Entries = append(reg.Entries, e)
	}
	return reg, nil
}

// Bytes is the register as it goes on disk.
func (r Register) Bytes() []byte {
	var buf bytes.Buffer
	buf.WriteString(registerHeader)
	w := csv.NewWriter(&buf)
	for _, e := range r.Entries {
		rec := []string{string(e.Tag), e.Local}
		if e.Gone != "" || e.Note != "" {
			rec = append(rec, "gone:"+e.Gone, e.Note)
		}
		// A csv.Writer only fails when the writer under it fails, and a
		// bytes.Buffer does not.
		_ = w.Write(rec)
	}
	w.Flush()
	return buf.Bytes()
}

const registerHeader = `# The permanent name of every object in this paper, and the object it names.
#
# Written by ax tags assign. A tag is assigned once and never changes, so a line
# here is only ever added or tombstoned and never edited. Four fields on a line
# means the object is gone and the tag now resolves to an explanation.
`

// Save writes the register and says whether the bytes changed.
func (r Register) Save(path string) (bool, error) {
	return write(path, r.Bytes())
}

// ByLocal is the lookup the content plane needs, from an object to its tag.
//
// Tombstoned entries are in it. An object that came back after being removed is
// the same object and gets its tag back rather than a new one.
func (r Register) ByLocal() map[string]Tag {
	m := make(map[string]Tag, len(r.Entries))
	for _, e := range r.Entries {
		m[e.Local] = e.Tag
	}
	return m
}

// Taken is every tag the paper has used, tombstones included.
//
// A tombstoned tag is never handed out again. Reusing it would point every
// reference written against the old object at a new one, which is worse than the
// reference breaking, because a broken reference is visible and a silently
// redirected one is not.
func (r Register) Taken() []Tag {
	out := make([]Tag, 0, len(r.Entries))
	for _, e := range r.Entries {
		out = append(out, e.Tag)
	}
	return out
}

// Live is the entries that still name something in the paper.
func (r Register) Live() []Entry {
	var out []Entry
	for _, e := range r.Entries {
		if e.Gone == "" {
			out = append(out, e)
		}
	}
	return out
}

// write puts bytes at a path and reports whether they differ from what was
// there, so a caller can say unchanged rather than claiming work it did not do.
func write(path string, b []byte) (bool, error) {
	old, err := os.ReadFile(path)
	if err == nil && bytes.Equal(old, b) {
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, b, 0o644)
}

// ErrNoObjects is what a paper with nothing to tag comes back with.
//
// A paper in the record class has no content plane and therefore no objects, and
// so does a paper nobody has extracted yet. The two are different situations and
// the caller knows which one it is looking at, so this says only that there was
// nothing here.
var ErrNoObjects = errors.New("tags: the paper has no taggable objects")

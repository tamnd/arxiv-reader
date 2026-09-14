package tags

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"os"
)

// Run is one assignment: the first and the last tag it handed out.
//
// The register is written in the order tags were handed out, so a run names a
// contiguous stretch of it and the two ends are enough to say which stretch. It
// is what tells a correct edit apart from a tag somebody pasted in the wrong
// place, and it is carried over from the Stacks Project, which has needed it.
//
// An assignment that handed out nothing writes no run. Re-running an assignment
// on a paper that did not change is the ordinary case and it should leave no
// trace, otherwise the runs file grows by a line every time CI runs and stops
// meaning anything.
type Run struct {
	First Tag
	Last  Tag
}

// LoadRuns reads a paper's runs, oldest first.
func LoadRuns(path string) ([]Run, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseRuns(b)
}

// ParseRuns reads the comma separated form.
func ParseRuns(b []byte) ([]Run, error) {
	r := csv.NewReader(bytes.NewReader(b))
	r.Comment = '#'
	r.FieldsPerRecord = 2
	var out []Run
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tags: %w", err)
		}
		run := Run{First: Tag(rec[0]), Last: Tag(rec[1])}
		if !run.First.Valid() || !run.Last.Valid() {
			return nil, fmt.Errorf("tags: the run %s,%s names something that is not a tag", rec[0], rec[1])
		}
		out = append(out, run)
	}
	return out, nil
}

const runsHeader = `# Where one assignment stopped and the next began, oldest first.
#
# Written by ax tags assign. The register is in the order tags were handed out,
# so the two tags on a line are the ends of the stretch one run wrote.
`

// RunsBytes is the runs file as it goes on disk.
func RunsBytes(runs []Run) []byte {
	var buf bytes.Buffer
	buf.WriteString(runsHeader)
	w := csv.NewWriter(&buf)
	for _, run := range runs {
		_ = w.Write([]string{string(run.First), string(run.Last)})
	}
	w.Flush()
	return buf.Bytes()
}

// SaveRuns writes the runs and says whether the bytes changed.
func SaveRuns(path string, runs []Run) (bool, error) {
	return write(path, RunsBytes(runs))
}

// Package witness holds the counts that the outside indexes of AX8.5 give for
// each paper: TheoremSearch and TheoremGraph. A witness is not ground truth.
// The indexes split and merge items in their own ways. But when an index has
// more results for a paper than the census has items, the census probably
// missed an item (AX4.5).
//
// A witness record holds structure only: the count, the count of each kind,
// and the printed numbers. It holds no text of the paper, so it is the same
// for each licence class.
package witness

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The indexes.
const (
	TheoremSearch = "theoremsearch"
	TheoremGraph  = "theoremgraph"
)

// Indexes is each index that ax imports.
var Indexes = []string{TheoremSearch, TheoremGraph}

// Record is what one index gives for one paper.
type Record struct {
	Paper string `json:"paper"`
	// Version is the version that the index read, or 0 when it does not say.
	Version int `json:"version,omitempty"`
	// Count is the results of the paper in the index.
	Count int `json:"count"`
	// Kinds is the count of each kind, as the index names it, in lower case.
	Kinds map[string]int `json:"kinds"`
	// Numbers is the printed numbers, in the order of the index. A result
	// with no number is not in the list.
	Numbers []string `json:"numbers,omitempty"`
}

// Add counts one result.
func (r *Record) Add(kind, number string) {
	r.Count++
	if r.Kinds == nil {
		r.Kinds = map[string]int{}
	}
	if kind == "" {
		kind = "unknown"
	}
	r.Kinds[strings.ToLower(kind)]++
	if number != "" {
		r.Numbers = append(r.Numbers, number)
	}
}

// Path is the witness file of one index and one month.
func Path(root, index, shard string) string {
	return path.Join(root, "analysis", "witness", index, shard+".jsonl")
}

// Write writes the records of one month, sorted by paper.
func Write(file string, recs []Record) error {
	sort.Slice(recs, func(i, j int) bool { return recs[i].Paper < recs[j].Paper })
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	tmp := file + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, r := range recs {
		b, err := json.Marshal(r)
		if err != nil {
			f.Close()
			return err
		}
		w.Write(b)
		w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// Read reads the records of one month by paper. A file that is not there is
// no records.
func Read(file string) (map[string]Record, error) {
	b, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]Record{}
	for i, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r Record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", file, i+1, err)
		}
		out[r.Paper] = r
	}
	return out, nil
}

// Cache reads the witness files of a corpus a month at a time.
type Cache struct {
	Root  string
	files map[string]map[string]Record
}

// Get is the record of an index for a paper in a month, and false when the
// index does not have the paper.
func (c *Cache) Get(index, shard, paper string) (Record, bool, error) {
	if c.files == nil {
		c.files = map[string]map[string]Record{}
	}
	key := index + "/" + shard
	m, ok := c.files[key]
	if !ok {
		var err error
		if m, err = Read(Path(c.Root, index, shard)); err != nil {
			return Record{}, false, err
		}
		c.files[key] = m
	}
	r, ok := m[paper]
	return r, ok, nil
}

// Counts is the count of each index for a paper, for the census field
// outside. An index that does not have the paper is not in the map.
func (c *Cache) Counts(shard, paper string) (map[string]int, error) {
	var out map[string]int
	for _, index := range Indexes {
		r, ok, err := c.Get(index, shard, paper)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if out == nil {
			out = map[string]int{}
		}
		out[index] = r.Count
	}
	return out, nil
}

var (
	versioned = regexp.MustCompile(`^(.+?)v([1-9][0-9]*)$`)
	numberTok = regexp.MustCompile(`^(?:[0-9]+(?:\.[0-9]+)*[a-z]?|[A-Z][0-9]*(?:\.[0-9]+)*|[IVXLC]+)$`)
)

// SplitVersion splits an id with a version, as 2511.12345v1, and gives 0 for
// an id with no version.
func SplitVersion(ref string) (string, int) {
	m := versioned.FindStringSubmatch(ref)
	if m == nil {
		return ref, 0
	}
	n := 0
	for _, c := range m[2] {
		n = n*10 + int(c-'0')
	}
	return m[1], n
}

// SplitName splits the name of a result, as "Proposition 2.1 (Ruelle
// inequality)", into the kind and the number. A name with no number gives an
// empty number.
func SplitName(name string) (kind, number string) {
	name = strings.TrimSpace(name)
	if i := strings.IndexAny(name, "(["); i >= 0 {
		name = strings.TrimSpace(name[:i])
	}
	f := strings.Fields(name)
	if len(f) == 0 {
		return "", ""
	}
	kind = strings.ToLower(strings.Trim(f[0], ".:"))
	if len(f) > 1 {
		if n := strings.TrimRight(f[1], ".:,"); numberTok.MatchString(n) {
			number = n
		}
	}
	return kind, number
}

package metadata

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Plane is the metadata plane on disk, rooted at a corpus directory.
//
// One file per month, JSON Lines, one record per line, sorted. The shape is
// chosen for git and not for a database: a month is a few thousand lines, a
// re-harvest that learns nothing rewrites nothing, and a diff on a month is
// readable by a person.
type Plane struct {
	// Root is the corpus directory, so the thing ARXIV_CORPUS points at.
	Root string
}

// shardPattern is YYMM. Four digits, and the month has to be a real one,
// because a stray file called 2106.jsonl.bak or 9999.jsonl in the directory
// would otherwise be read as a month and counted in every report.
var shardPattern = regexp.MustCompile(`^[0-9]{2}(0[1-9]|1[0-2])$`)

// ValidShard reports whether s is a YYMM shard name.
func ValidShard(s string) bool { return shardPattern.MatchString(s) }

// Path is the file holding one month.
func (p Plane) Path(shard string) string {
	return filepath.Join(p.Root, "metadata", shard+".jsonl")
}

// Dir is the metadata plane's directory.
func (p Plane) Dir() string { return filepath.Join(p.Root, "metadata") }

// Shards lists the months that exist, oldest first.
//
// Ordered by the month rather than by the string, so that 9107 comes before
// 0704 the way the calendar does. arXiv started in August 1991 and the century
// rolls over in the middle of the corpus, which is a detail that will bite
// anything that sorts these as text.
func (p Plane) Shards() ([]string, error) {
	entries, err := os.ReadDir(p.Dir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var shards []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		shard := strings.TrimSuffix(e.Name(), ".jsonl")
		if !ValidShard(shard) {
			continue
		}
		shards = append(shards, shard)
	}
	sort.Slice(shards, func(i, j int) bool {
		return shardOrder(shards[i]) < shardOrder(shards[j])
	})
	return shards, nil
}

// shardOrder turns YYMM into something that sorts by the calendar.
//
// arXiv's first paper is from August 1991, so anything from 91 up is in the
// twentieth century and anything below it is in the twenty first. That will
// hold until 2091, by which point a two digit year in a file name is somebody
// else's problem and arXiv's ids have the same one.
func shardOrder(shard string) int {
	yy := int(shard[0]-'0')*10 + int(shard[1]-'0')
	mm := int(shard[2]-'0')*10 + int(shard[3]-'0')
	year := 2000 + yy
	if yy >= 91 {
		year = 1900 + yy
	}
	// Months counted from zero rather than from one, so that the remainder is
	// the month and December does not divide into the following year.
	return year*12 + mm - 1
}

// ShardMonth is the first day of the month a shard names.
//
// The century rule is shardOrder's and lives in one place on purpose, because
// it is the kind of rule that gets rewritten slightly differently every time it
// is needed and then disagrees with itself somewhere in the 1990s.
func ShardMonth(shard string) (time.Time, error) {
	if !ValidShard(shard) {
		return time.Time{}, fmt.Errorf("%q is not a YYMM shard", shard)
	}
	order := shardOrder(shard)
	return time.Date(order/12, time.Month(order%12+1), 1, 0, 0, 0, 0, time.UTC), nil
}

// Scan reads one month and calls fn for each record in file order.
//
// Streaming rather than returning a slice, because the whole plane is 3.17
// million records and the audit, the census and the graph build all want to
// walk it without holding it. A month on its own fits in memory comfortably and
// Read is there for when that is what you want.
func (p Plane) Scan(shard string, fn func(Record) error) error {
	f, err := os.Open(p.Path(shard))
	if err != nil {
		return err
	}
	defer f.Close()
	return scan(f, p.Path(shard), fn)
}

// ScanAll walks every month, oldest first.
func (p Plane) ScanAll(fn func(shard string, r Record) error) error {
	shards, err := p.Shards()
	if err != nil {
		return err
	}
	for _, shard := range shards {
		if err := p.Scan(shard, func(r Record) error { return fn(shard, r) }); err != nil {
			return err
		}
	}
	return nil
}

func scan(r io.Reader, where string, fn func(Record) error) error {
	sc := bufio.NewScanner(r)
	// An abstract can run to a few kilobytes and the default 64KB limit is a
	// silent truncation rather than an error, which is the worst way for a
	// parser to fail.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		b := bytes.TrimSpace(sc.Bytes())
		if len(b) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(b, &rec); err != nil {
			return fmt.Errorf("%s:%d: %w", where, line, err)
		}
		if err := fn(rec); err != nil {
			return fmt.Errorf("%s:%d: %w", where, line, err)
		}
	}
	return sc.Err()
}

// Read loads one month into memory.
func (p Plane) Read(shard string) ([]Record, error) {
	var recs []Record
	err := p.Scan(shard, func(r Record) error {
		recs = append(recs, r)
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return recs, err
}

// Count is how many records a month holds.
func (p Plane) Count(shard string) (int, error) {
	n := 0
	err := p.Scan(shard, func(Record) error { n++; return nil })
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	return n, err
}

// Write replaces one month with recs.
//
// It returns false when the file on disk already says exactly this, which is
// the common case for a re-harvest and is worth detecting. Rewriting an
// identical file would put a commit in the history that changes nothing, and a
// corpus whose log is mostly no-ops is a corpus nobody reads the log of.
func (p Plane) Write(shard string, recs []Record) (bool, error) {
	if !ValidShard(shard) {
		return false, fmt.Errorf("%q is not a YYMM shard", shard)
	}
	Sort(recs)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	// The plane holds LaTeX, and LaTeX is full of < and > and &. Escaping them
	// would triple the size of a fifth of the abstracts in the corpus to
	// protect against an HTML injection that cannot happen in a file nothing
	// serves.
	enc.SetEscapeHTML(false)
	for _, r := range recs {
		if err := enc.Encode(r); err != nil {
			return false, err
		}
	}
	return writeIfChanged(p.Path(shard), buf.Bytes())
}

// writeIfChanged writes b to path atomically, and reports whether it had to.
func writeIfChanged(path string, b []byte) (bool, error) {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, b) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	// Written to a temporary file and renamed, because a harvest that is
	// interrupted halfway through a month should leave the old month intact
	// rather than half of a new one.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return false, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return false, err
	}
	return true, nil
}

// Stats is what a merge did.
type Stats struct {
	Added     int
	Updated   int
	Unchanged int
	// Written is false when the merge produced the file that was already there.
	Written bool
}

func (s Stats) String() string {
	return fmt.Sprintf("%d added, %d updated, %d unchanged", s.Added, s.Updated, s.Unchanged)
}

// Add sums two sets of counts, for a run that covered several months.
func (s Stats) Add(o Stats) Stats {
	return Stats{
		Added:     s.Added + o.Added,
		Updated:   s.Updated + o.Updated,
		Unchanged: s.Unchanged + o.Unchanged,
		Written:   s.Written || o.Written,
	}
}

// Merger folds an incoming record onto the stored one. Fill and Replace are the
// two, and which one a harvest wants depends on whether its surface can see the
// whole paper.
type Merger func(stored, incoming Record) Record

// Merge folds recs into the month already on disk, under the Fill rule.
//
// Fill is the default because every arXiv surface is partial. Callers that know
// their records are the whole truth can pass Replace to MergeWith.
func (p Plane) Merge(shard string, recs []Record) (Stats, error) {
	return p.MergeWith(shard, recs, Fill)
}

// MergeWith is Merge with the fold named.
func (p Plane) MergeWith(shard string, recs []Record, fold Merger) (Stats, error) {
	if fold == nil {
		fold = Fill
	}
	existing, err := p.Read(shard)
	if err != nil {
		return Stats{}, err
	}
	byID := make(map[string]int, len(existing))
	for i, r := range existing {
		byID[r.ID] = i
	}

	var stats Stats
	out := append([]Record(nil), existing...)
	for _, incoming := range recs {
		incoming = incoming.Normalise()
		i, ok := byID[incoming.ID]
		if !ok {
			byID[incoming.ID] = len(out)
			out = append(out, incoming)
			stats.Added++
			continue
		}
		merged := fold(out[i], incoming).Normalise()
		if sameRecord(out[i], merged) {
			stats.Unchanged++
			continue
		}
		out[i] = merged
		stats.Updated++
	}

	written, err := p.Write(shard, out)
	if err != nil {
		return stats, err
	}
	stats.Written = written
	return stats, nil
}

// sameRecord compares two records by the bytes they would be written as.
//
// Everything but Harvested, which is the date of the last write and would make
// every re-harvest look like a change to every paper.
func sameRecord(a, b Record) bool {
	a.Harvested, b.Harvested = "", ""
	ja, err := json.Marshal(a)
	if err != nil {
		return false
	}
	jb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(ja, jb)
}

// Group sorts records into the months they belong in.
//
// A record whose id will not parse has no month, so it is returned separately
// rather than dropped. Losing a paper silently because its id was odd is how a
// count comes out three short and nobody can say which three.
func Group(recs []Record) (byShard map[string][]Record, unplaceable []Record) {
	byShard = make(map[string][]Record)
	for _, r := range recs {
		shard, err := r.Shard()
		if err != nil {
			unplaceable = append(unplaceable, r)
			continue
		}
		byShard[shard] = append(byShard[shard], r)
	}
	return byShard, unplaceable
}

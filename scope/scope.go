// Package scope decides which papers are in the math scope (AX1.2) and reads
// and writes the list of them, manifests/scope/math.txt (AX2.1).
//
// The scope is a view of the metadata plane and not a copy of it. The list
// holds the ids only, and it is rebuilt from the plane.
package scope

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Math is the reason that puts a paper in the math scope by its category.
const Math = "scope-math"

// oldArchives are the archives that arXiv merged into math, with their
// old-style ids.
var oldArchives = map[string]bool{"alg-geom": true, "dg-ga": true, "funct-an": true, "q-alg": true}

// InMath tells if a category puts a paper in the math scope: each math.*
// category, math-ph, and the four old archives.
func InMath(category string) bool {
	return strings.HasPrefix(category, "math.") || category == "math" || category == "math-ph" || oldArchives[category]
}

// Of tells if a paper is in the math scope, and if it is in scope only by a
// cross-list. The first category is the primary one.
func Of(categories []string) (in, crossOnly bool) {
	for i, c := range categories {
		if InMath(c) {
			return true, i > 0 && !InMath(categories[0])
		}
	}
	return false, false
}

// List is the ids in scope, with the counts for the first comment line.
type List struct {
	Reason string
	IDs    []string
	// Cross is how many of the ids are in scope only by a cross-list.
	Cross int
	// Date is the day that the list was built, as 2026-10-27.
	Date string
}

// Path is the list of one reason in a corpus.
func Path(root, reason string) string {
	return filepath.Join(root, "manifests", "scope", strings.TrimPrefix(reason, "scope-")+".txt")
}

// Write writes the list, sorted, with the counts in its first comment line.
func (l *List) Write(path string) error {
	sort.Strings(l.IDs)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s: %d papers, %d with a primary category in scope and %d by a cross-list only. Built from the metadata plane on %s.\n", l.Reason, len(l.IDs), len(l.IDs)-l.Cross, l.Cross, l.Date)
	for _, id := range l.IDs {
		b.WriteString(id)
		b.WriteByte('\n')
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Read reads the ids of a list. A line that starts with # is a comment.
func Read(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ids := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ids[line] = true
	}
	return ids, sc.Err()
}

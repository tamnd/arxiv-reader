package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/arxiv-reader/analysis"
	"github.com/tamnd/arxiv-reader/licence"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/scope"
)

// queued is one paper in the queue of the analysis.
type queued struct {
	ID    string
	Month string
	Class licence.Class
	Old   bool
}

// orderQueue sorts the queue in the order of AX2.6: open first and then
// share-alike, the newest month first inside a class, and the old-style ids
// last. The priority by citation of K12.3 needs the nodes of math2, so it is
// not here yet.
func orderQueue(q []queued) {
	rank := func(c licence.Class) int {
		if c == licence.ClassOpen {
			return 0
		}
		return 1
	}
	sort.SliceStable(q, func(i, j int) bool {
		a, b := q[i], q[j]
		if a.Old != b.Old {
			return !a.Old
		}
		if rank(a.Class) != rank(b.Class) {
			return rank(a.Class) < rank(b.Class)
		}
		if a.Month != b.Month {
			return a.Month > b.Month
		}
		return a.ID > b.ID
	})
}

// analysisQueue is the open and share-alike papers of a scope list, in the
// order of AX2.6.
func analysisQueue(root, list string) ([]string, error) {
	in, err := scope.Read(scope.Path(root, list))
	if err != nil {
		return nil, fmt.Errorf("%w, so run ax select scope first", err)
	}
	var q []queued
	err = metadata.Plane{Root: root}.ScanAll(func(shard string, r metadata.Record) error {
		if !in[r.ID] {
			return nil
		}
		latest, ok := r.Latest()
		if !ok {
			return nil
		}
		l := latest.Licence
		if latest.LicenceFrom != metadata.SourceAbs {
			l, _ = licence.Latest(r)
		}
		c := licence.ClassOf(l)
		if c != licence.ClassOpen && c != licence.ClassShareAlikeNC {
			return nil
		}
		q = append(q, queued{ID: r.ID, Month: analysis.MonthOf(shard), Class: c, Old: strings.Contains(r.ID, "/")})
		return nil
	})
	if err != nil {
		return nil, err
	}
	orderQueue(q)
	out := make([]string, len(q))
	for i, x := range q {
		out[i] = x.ID
	}
	return out, nil
}

// runAnalysis is ax run analysis, which is ax census batch -scope math: the
// run of AX-M3 that takes each open and share-alike paper in scope to
// extracted or stopped.
func runAnalysis(args []string) error {
	return censusBatch(append([]string{"-scope", scope.Math}, args...))
}

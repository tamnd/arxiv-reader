package graph

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Load reads one shard's edges.
//
// A file that is not there is no edges and not an error, the same as every other
// manifest in this corpus. A month nobody has built a graph for is a month with
// no claims in it, which is a fact rather than a fault.
func Load(path string) ([]Edge, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Edge
	for i, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e Edge
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("graph: %s:%d: %w", path, i+1, err)
		}
		out = append(out, e)
	}
	return out, nil
}

// Bytes is a shard as it would be written, with every edge checked first.
//
// The check is here rather than in the caller because this is the one door every
// edge goes through on its way to disk, and an edge that names a predicate
// nothing knows or points at a node kind the predicate does not run to is a
// silent fault: it joins to nothing later and reads as missing data.
func Bytes(edges []Edge) ([]byte, error) {
	var b strings.Builder
	for _, e := range edges {
		if err := e.Validate(); err != nil {
			return nil, fmt.Errorf("graph: %w", err)
		}
		line, err := json.Marshal(e)
		if err != nil {
			return nil, err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return []byte(b.String()), nil
}

// Save writes a shard and says whether the bytes changed.
func Save(path string, edges []Edge) (bool, error) {
	body, err := Bytes(edges)
	if err != nil {
		return false, err
	}
	if old, err := os.ReadFile(path); err == nil && string(old) == string(body) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, body, 0o644)
}

// Merge folds a rebuild of some papers into a shard that already holds others.
//
// A shard file is a month and a rebuild is one paper, so the stored edges of the
// papers being rebuilt are dropped and every other paper's are left exactly as
// they were. That is what makes rebuilding one paper cost one file rather than a
// month, and it is why the file is sharded by the subject's paper in the first
// place.
//
// The date is carried over from the stored claim, so an edge that was already
// there keeps the day it was first written. A build that stamped today on
// everything it saw would turn every rebuild into a diff on every line.
func Merge(stored, fresh []Edge, papers []string) []Edge {
	rebuilt := make(map[string]bool, len(papers))
	for _, p := range papers {
		rebuilt[p] = true
	}
	was := make(map[string]string, len(stored))
	out := make([]Edge, 0, len(stored)+len(fresh))
	for _, e := range stored {
		was[e.Key()] = e.At
		if rebuilt[PaperOf(e.S)] {
			continue
		}
		out = append(out, e)
	}
	for _, e := range fresh {
		if at, held := was[e.Key()]; held && at != "" {
			e.At = at
		}
		out = append(out, e)
	}
	Sort(out)
	return out
}

// Counts is how many edges there are per predicate and per confidence, which is
// what reports/graph.md is written out of.
func Counts(edges []Edge) (predicate, confidence map[string]int) {
	predicate, confidence = map[string]int{}, map[string]int{}
	for _, e := range edges {
		predicate[e.P]++
		confidence[e.Conf]++
	}
	return predicate, confidence
}

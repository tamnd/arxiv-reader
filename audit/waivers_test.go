package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/arxiv-reader/metadata"
)

// The case is from math/0002124, where arXiv dates v2 before v1. S18 finds
// two such records here and one of them has a waiver.
func TestAWaiverExcusesOneRecordFromOneRule(t *testing.T) {
	bad := good()
	bad.Versions[1].Created = on("2021-06-01")
	other := withID(good(), "2106.09686")
	other.Versions[1].Created = on("2021-06-01")

	plane := metadata.Plane{Root: t.TempDir()}
	if _, err := plane.Write("2106", []metadata.Record{bad, other}); err != nil {
		t.Fatal(err)
	}
	m := Meta{
		Plane:   plane,
		Now:     func() time.Time { return on("2026-09-14") },
		Waivers: Waivers{"S18": {"2106.09685": "arXiv's own metadata gives these dates"}},
	}
	shards, err := plane.Shards()
	if err != nil {
		t.Fatal(err)
	}
	report, err := m.Run(shards)
	if err != nil {
		t.Fatal(err)
	}
	for _, res := range report.Results {
		if res.Rule.ID != "S18" {
			continue
		}
		if res.Waived != 1 || res.Total != 1 {
			t.Fatalf("S18 waived %d and found %d, want 1 and 1", res.Waived, res.Total)
		}
		if res.Findings[0].ID != "2106.09686" {
			t.Errorf("S18 kept %s, want the record with no waiver", res.Findings[0].ID)
		}
	}
	if !strings.Contains(report.Markdown(), "## The waivers") {
		t.Error("the report does not say what was waived")
	}
}

func TestReadWaivers(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "waivers-meta.yaml")

	w, err := ReadWaivers(file)
	if err != nil || w != nil {
		t.Fatalf("a missing file gave %v, %v, want no waivers", w, err)
	}

	good := "waivers:\n  - id: math/0002124\n    rule: s18\n    reason: arXiv's own metadata gives these dates\n"
	if err := os.WriteFile(file, []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err = ReadWaivers(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := w.Reason("S18", "math/0002124"); !ok {
		t.Errorf("the waiver is not read: %v", w)
	}

	for _, bad := range []string{
		"waivers:\n  - id: math/0002124\n    rule: S18\n",
		"waivers:\n  - {id: a, rule: S18, reason: r}\n  - {id: a, rule: S18, reason: r}\n",
	} {
		if err := os.WriteFile(file, []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadWaivers(file); err == nil {
			t.Errorf("ReadWaivers took %q", bad)
		}
	}
}

package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/mdrec"
)

func cites(from, to, conf, day string) Edge {
	return Edge{S: Paper(from), P: Cites, O: Paper(to), Conf: conf, Via: "arxiv", Stage: StageRefs, At: day}
}

func TestAShardNobodyBuiltIsNoEdgesAndNotAFault(t *testing.T) {
	edges, err := Load(filepath.Join(t.TempDir(), "2501.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if edges != nil {
		t.Errorf("got %+v", edges)
	}
}

func TestAShardIsWrittenAndReadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph", "2501.jsonl")
	want := []Edge{cites("2501.00001", "2401.01234", Certain, "2026-10-14")}
	changed, err := Save(path, want)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("writing a file that was not there did not change it")
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// A shard is Markdown with the edges in its header, and reads back the same.
func TestAShardIsOneRecordPerEdge(t *testing.T) {
	body, err := Bytes([]Edge{
		cites("2501.00001", "2401.01234", Certain, "2026-10-14"),
		cites("2501.00001", "2303.00002", Medium, "2026-10-14"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(body), "---\nrecords:\n") {
		t.Errorf("the file does not open with a header of records:\n%s", body)
	}
	if recs, err := mdrec.Decode(body); err != nil || len(recs) != 2 {
		t.Errorf("the file holds %d records (%v), and it must hold two", len(recs), err)
	}
	if strings.Contains(string(body), `"locator"`) {
		t.Error("a citation that named no result still wrote a locator field")
	}
}

// Nothing invalid reaches the disk, because this is the one door.
func TestAnEdgeNobodyCanMakeSenseOfIsNotWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "2501.jsonl")
	bad := cites("2501.00001", "2401.01234", "low", "2026-10-14")
	if _, err := Save(path, []Edge{bad}); err == nil {
		t.Fatal("a low confidence edge was written")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("the file was written anyway")
	}
}

func TestAShardWithATornLineSaysWhichLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "2501.jsonl")
	if err := os.WriteFile(path, []byte("{\"s\":\"ax://paper/2501.00001\"}\nnot json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("a torn shard loaded")
	}
	if !strings.Contains(err.Error(), ":2:") {
		t.Errorf("%v does not name line 2", err)
	}
}

// A rebuild that found the same claims is not a commit.
func TestARebuildThatFoundTheSameThingChangesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "2501.jsonl")
	edges := []Edge{cites("2501.00001", "2401.01234", Certain, "2026-10-14")}
	if _, err := Save(path, edges); err != nil {
		t.Fatal(err)
	}
	changed, err := Save(path, edges)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("writing the same edges twice reported a change")
	}
}

// The reason the shard is keyed on the subject's paper: rebuilding one paper
// costs one file and leaves every other paper in the month alone.
func TestARebuildOfOnePaperLeavesTheOthersAlone(t *testing.T) {
	stored := []Edge{
		cites("2501.00001", "2401.01234", Certain, "2025-01-01"),
		cites("2501.00009", "2106.09685", Certain, "2025-01-01"),
	}
	fresh := []Edge{cites("2501.00001", "2303.00002", Medium, "2026-10-14")}
	out := Merge(stored, fresh, []string{"2501.00001"})
	if len(out) != 2 {
		t.Fatalf("%d edges, want the other paper's one and the fresh one: %+v", len(out), out)
	}
	find(t, out, Paper("2501.00009"), Cites, Paper("2106.09685"))
	find(t, out, Paper("2501.00001"), Cites, Paper("2303.00002"))
	for _, e := range out {
		if e.O == Paper("2401.01234") {
			t.Error("a claim the rebuild no longer makes is still stored")
		}
	}
}

// Otherwise every rebuild is a diff on every line, and the date stops meaning
// the day the claim was first made.
func TestAClaimThatWasAlreadyThereKeepsItsDay(t *testing.T) {
	stored := []Edge{cites("2501.00001", "2401.01234", Certain, "2025-01-01")}
	fresh := []Edge{
		cites("2501.00001", "2401.01234", Certain, "2026-10-14"),
		cites("2501.00001", "2303.00002", Medium, "2026-10-14"),
	}
	out := Merge(stored, fresh, []string{"2501.00001"})
	if e := find(t, out, Paper("2501.00001"), Cites, Paper("2401.01234")); e.At != "2025-01-01" {
		t.Errorf("an old claim is dated %q", e.At)
	}
	if e := find(t, out, Paper("2501.00001"), Cites, Paper("2303.00002")); e.At != "2026-10-14" {
		t.Errorf("a new claim is dated %q", e.At)
	}
}

// A claim about an object is a claim about its paper for the purpose of a
// rebuild, because the object is the paper's.
func TestARebuildDropsTheClaimsTheObjectsMade(t *testing.T) {
	stored := []Edge{{
		S: Object("2501.00001", "AAAA"), P: Cites, O: Paper("2401.01234"),
		Conf: Certain, Via: "arxiv", Stage: StageRefs, At: "2025-01-01",
	}}
	if out := Merge(stored, nil, []string{"2501.00001"}); len(out) != 0 {
		t.Errorf("got %+v", out)
	}
}

func TestCountsAreByPredicateAndByConfidence(t *testing.T) {
	predicate, confidence := Counts([]Edge{
		cites("2501.00001", "2401.01234", Certain, "2026-10-14"),
		cites("2501.00001", "2303.00002", Medium, "2026-10-14"),
		{S: Paper("2501.00001"), P: InCategory, O: Category("cs.LG"), Conf: Certain, Via: ViaMetadata, Stage: StageHarvest, At: "2026-10-14"},
	})
	if predicate[Cites] != 2 || predicate[InCategory] != 1 {
		t.Errorf("got %v", predicate)
	}
	if confidence[Certain] != 2 || confidence[Medium] != 1 {
		t.Errorf("got %v", confidence)
	}
}

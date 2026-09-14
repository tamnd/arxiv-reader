package metadata

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/corpus"
)

func plane(t *testing.T) Plane {
	t.Helper()
	return Plane{Root: t.TempDir()}
}

func rec(id string) Record {
	return Record{
		ID:         id,
		Title:      "A paper",
		Abstract:   "An abstract.",
		Authors:    []Author{{Surname: "Bourbaki"}},
		Categories: []string{"math.AG"},
		Versions:   []Version{{Version: 1, Created: day("2021-06-17")}},
		Source:     SourceOAI,
		Harvested:  "2026-09-14",
	}
}

func TestValidShard(t *testing.T) {
	for _, s := range []string{"9107", "0704", "2106", "9912"} {
		if !ValidShard(s) {
			t.Errorf("%q should be a shard", s)
		}
	}
	// A stray file in the directory must not be read as a month, or it lands
	// in every count and every report with no way to trace where it came from.
	for _, s := range []string{"2113", "2100", "210", "21066", "21a6", ""} {
		if ValidShard(s) {
			t.Errorf("%q should not be a shard", s)
		}
	}
}

// arXiv started in August 1991 and its months run through a century rollover,
// so shards have to be ordered by the calendar and not as text.
func TestShardsAreChronological(t *testing.T) {
	p := plane(t)
	for _, shard := range []string{"2106", "9107", "0704", "9912", "0001"} {
		if _, err := p.Write(shard, []Record{rec("2106.09685")}); err != nil {
			t.Fatal(err)
		}
	}
	// Two files that are not months, which must be ignored rather than counted.
	if err := os.WriteFile(filepath.Join(p.Dir(), "2106.jsonl.bak"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Dir(), "9999.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := p.Shards()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"9107", "9912", "0001", "0704", "2106"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("got %v, want %v", got, want)
	}
}

// December is the case worth writing down. The month is counted from zero so
// that the remainder is the month, and counting it from one instead puts every
// December in the January of the following year.
func TestShardMonth(t *testing.T) {
	for _, tc := range []struct{ shard, want string }{
		{"9108", "1991-08-01"},
		{"9112", "1991-12-01"},
		{"9912", "1999-12-01"},
		{"0001", "2000-01-01"},
		{"0704", "2007-04-01"},
		{"2106", "2021-06-01"},
		{"2112", "2021-12-01"},
	} {
		got, err := ShardMonth(tc.shard)
		if err != nil {
			t.Fatalf("%s: %v", tc.shard, err)
		}
		if got.Format("2006-01-02") != tc.want {
			t.Errorf("%s is %s, want %s", tc.shard, got.Format("2006-01-02"), tc.want)
		}
	}
	for _, bad := range []string{"2113", "2100", "210", "21066", "june"} {
		if _, err := ShardMonth(bad); err == nil {
			t.Errorf("%q was accepted as a month", bad)
		}
	}
}

// The order the shards sort in and the month they name have to come from the
// same rule, or the 1990s end up in one place in the file listing and another
// in the report.
func TestShardMonthAgreesWithTheShardOrder(t *testing.T) {
	shards := []string{"9107", "9112", "9912", "0001", "0704", "2106", "2112"}
	for i := 1; i < len(shards); i++ {
		before, err := ShardMonth(shards[i-1])
		if err != nil {
			t.Fatal(err)
		}
		after, err := ShardMonth(shards[i])
		if err != nil {
			t.Fatal(err)
		}
		if !before.Before(after) {
			t.Errorf("%s is not before %s", shards[i-1], shards[i])
		}
	}
}

func TestShardsOfAnEmptyCorpus(t *testing.T) {
	shards, err := Plane{Root: t.TempDir()}.Shards()
	if err != nil {
		t.Fatalf("an empty corpus is not an error: %v", err)
	}
	if len(shards) != 0 {
		t.Errorf("got %v", shards)
	}
}

func TestWriteThenRead(t *testing.T) {
	p := plane(t)
	in := []Record{rec("2106.09685"), rec("2106.00001")}
	written, err := p.Write("2106", in)
	if err != nil {
		t.Fatal(err)
	}
	if !written {
		t.Fatal("the first write wrote nothing")
	}
	out, err := p.Read("2106")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("read %d records", len(out))
	}
	// Written sorted, so the file is stable whatever order the harvest
	// produced.
	if out[0].ID != "2106.00001" || out[1].ID != "2106.09685" {
		t.Errorf("read back in the order %v", ids(out))
	}
	n, err := p.Count("2106")
	if err != nil || n != 2 {
		t.Errorf("Count = %d, %v", n, err)
	}
}

// A re-harvest that learns nothing must write nothing. A rewrite of an
// identical file is a commit that changes nothing, and a log that is mostly
// no-ops is a log nobody reads.
func TestWriteIsANoOpWhenNothingChanged(t *testing.T) {
	p := plane(t)
	in := []Record{rec("2106.09685")}
	if _, err := p.Write("2106", in); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(p.Path("2106"))
	if err != nil {
		t.Fatal(err)
	}
	written, err := p.Write("2106", in)
	if err != nil {
		t.Fatal(err)
	}
	if written {
		t.Error("the second write rewrote an identical file")
	}
	after, err := os.Stat(p.Path("2106"))
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("the file was touched")
	}
}

func TestWriteRejectsANonShard(t *testing.T) {
	p := plane(t)
	if _, err := p.Write("nope", nil); err == nil {
		t.Fatal("wrote a file that is not a month")
	}
}

// LaTeX is full of < and > and &, and encoding/json escapes all three by
// default. Escaping them would bloat a fifth of the abstracts in the corpus to
// guard against an HTML injection into a file nothing serves.
func TestTheFileHoldsLaTeXUnescaped(t *testing.T) {
	p := plane(t)
	r := rec("2106.09685")
	r.Abstract = `We show A <: B & C > 0.`
	if _, err := p.Write("2106", []Record{r}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p.Path("2106"))
	if err != nil {
		t.Fatal(err)
	}
	for _, escape := range []string{`\u003c`, `\u003e`, `\u0026`} {
		if strings.Contains(string(b), escape) {
			t.Errorf("the file escaped %s:\n%s", escape, b)
		}
	}
	if !strings.Contains(string(b), `A <: B & C > 0.`) {
		t.Errorf("the abstract did not survive:\n%s", b)
	}
}

func TestOneRecordPerLine(t *testing.T) {
	p := plane(t)
	if _, err := p.Write("2106", []Record{rec("2106.00001"), rec("2106.09685")}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p.Path("2106"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "\n"); n != 2 {
		t.Errorf("two records came out as %d lines", n)
	}
	if !strings.HasSuffix(string(b), "\n") {
		t.Error("the file does not end in a newline")
	}
}

func TestMergeCounts(t *testing.T) {
	p := plane(t)
	if _, err := p.Write("2106", []Record{rec("2106.00001"), rec("2106.09685")}); err != nil {
		t.Fatal(err)
	}

	changed := rec("2106.09685")
	changed.Title = "A better title"
	stats, err := p.Merge("2106", []Record{rec("2106.00001"), changed, rec("2106.11111")})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Added != 1 || stats.Updated != 1 || stats.Unchanged != 1 {
		t.Errorf("got %s", stats)
	}
	if !stats.Written {
		t.Error("a merge that added a paper wrote nothing")
	}

	out, err := p.Read("2106")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("the month holds %d records", len(out))
	}
}

// The harvest date is when we last wrote a record, not a fact about the paper.
// If it counted as a change then every re-harvest would rewrite every line in
// the corpus and the diff would never say anything.
func TestMergeIgnoresTheHarvestDate(t *testing.T) {
	p := plane(t)
	if _, err := p.Write("2106", []Record{rec("2106.09685")}); err != nil {
		t.Fatal(err)
	}
	later := rec("2106.09685")
	later.Harvested = "2027-01-01"
	stats, err := p.Merge("2106", []Record{later})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Unchanged != 1 {
		t.Errorf("got %s, want it unchanged", stats)
	}
	if stats.Written {
		t.Error("the file was rewritten for a new harvest date alone")
	}
}

// The Kaggle snapshot carries no per version licence, so re-running the
// bootstrap after the M2 census must not wipe out the one field the whole
// content plane depends on.
func TestMergeNeverUnresolvesALicence(t *testing.T) {
	p := plane(t)
	resolved := rec("2106.09685")
	resolved.Versions[0].Licence = corpus.LicenceCCBY
	resolved.Versions[0].LicenceFrom = SourceOAI
	if _, err := p.Write("2106", []Record{resolved}); err != nil {
		t.Fatal(err)
	}

	fromKaggle := rec("2106.09685")
	fromKaggle.Source = SourceKaggle
	if _, err := p.Merge("2106", []Record{fromKaggle}); err != nil {
		t.Fatal(err)
	}

	out, err := p.Read("2106")
	if err != nil {
		t.Fatal(err)
	}
	if got := out[0].Versions[0].Licence; got != corpus.LicenceCCBY {
		t.Errorf("the licence came back as %q, want cc-by", got)
	}
	if got := out[0].Versions[0].LicenceFrom; got != SourceOAI {
		t.Errorf("the authority came back as %q, want oai", got)
	}
	// The rest of the record is still allowed to move.
	if out[0].Source != SourceKaggle {
		t.Errorf("the record's own source did not update, got %q", out[0].Source)
	}
}

// A later, better authority is allowed to overwrite an earlier one. The rule is
// one way: resolved beats unresolved, never the reverse.
func TestMergeLetsABetterAuthorityWin(t *testing.T) {
	p := plane(t)
	guess := rec("2106.09685")
	guess.Versions[0].Licence = corpus.LicenceUnknown
	guess.Versions[0].LicenceFrom = SourceKaggle
	if _, err := p.Write("2106", []Record{guess}); err != nil {
		t.Fatal(err)
	}
	better := rec("2106.09685")
	better.Versions[0].Licence = corpus.LicenceCCBY
	better.Versions[0].LicenceFrom = SourceOAI
	if _, err := p.Merge("2106", []Record{better}); err != nil {
		t.Fatal(err)
	}
	out, err := p.Read("2106")
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Versions[0].Licence != corpus.LicenceCCBY || out[0].Versions[0].LicenceFrom != SourceOAI {
		t.Errorf("got %+v", out[0].Versions[0])
	}
}

func TestMergeIntoAMonthThatDoesNotExist(t *testing.T) {
	p := plane(t)
	stats, err := p.Merge("2106", []Record{rec("2106.09685")})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Added != 1 {
		t.Errorf("got %s", stats)
	}
}

func TestScanAll(t *testing.T) {
	p := plane(t)
	if _, err := p.Write("2106", []Record{rec("2106.09685")}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Write("9711", []Record{rec("hep-th/9711200")}); err != nil {
		t.Fatal(err)
	}
	var seen []string
	err := p.ScanAll(func(shard string, r Record) error {
		seen = append(seen, shard+" "+r.ID)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "9711 hep-th/9711200, 2106 2106.09685"
	if strings.Join(seen, ", ") != want {
		t.Errorf("got %q, want %q", strings.Join(seen, ", "), want)
	}
}

// A truncated file has to fail loudly. A parser that skips the line it cannot
// read produces a corpus that is short by an amount nobody can name.
func TestScanReportsTheLine(t *testing.T) {
	p := plane(t)
	if err := os.MkdirAll(p.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"id":"2106.00001"}` + "\n" + `{"id":"2106.0000` + "\n"
	if err := os.WriteFile(p.Path("2106"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	err := p.Scan("2106", func(Record) error { return nil })
	if err == nil {
		t.Fatal("a truncated line was read without complaint")
	}
	if !strings.Contains(err.Error(), "2106.jsonl:2") {
		t.Errorf("the error does not name the line: %v", err)
	}
}

func TestGroup(t *testing.T) {
	byShard, bad := Group([]Record{
		rec("2106.09685"),
		rec("2106.00001"),
		rec("hep-th/9711200"),
		rec("this is not an id"),
	})
	if len(byShard["2106"]) != 2 || len(byShard["9711"]) != 1 {
		t.Errorf("grouped into %v", byShard)
	}
	// An unplaceable record is handed back rather than dropped, so a count that
	// comes out three short can say which three.
	if len(bad) != 1 {
		t.Errorf("got %d unplaceable records, want 1", len(bad))
	}
}

// A month is written to a temporary file and renamed, so a harvest interrupted
// halfway through leaves the old month intact rather than half of a new one.
func TestWriteLeavesNoTemporaryFiles(t *testing.T) {
	p := plane(t)
	if _, err := p.Write("2106", []Record{rec("2106.09685")}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(p.Dir())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("left %s behind", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("the directory holds %d files", len(entries))
	}
}

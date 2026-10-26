package licence

import (
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
)

func TestScopeCounter(t *testing.T) {
	rec := func(id string, l corpus.Licence, cats ...string) metadata.Record {
		return metadata.Record{ID: id, Categories: cats, Versions: []metadata.Version{{Version: 1, Licence: l}}}
	}
	c := NewScopeCounter("scope-math")
	for _, r := range []struct {
		shard string
		rec   metadata.Record
	}{
		{"2401", rec("2401.00001", corpus.LicenceCCBY, "math.NT")},
		{"2401", rec("2401.00002", corpus.LicenceArXiv, "math.NT")},
		{"2401", rec("2401.00003", corpus.LicenceCCBYSA, "cs.LG", "math.OC")},
		{"2402", rec("2402.00001", corpus.LicenceCCBYNCND, "math-ph")},
		{"2402", rec("2402.00002", corpus.LicenceCCBY, "cs.LG")},
		{"2402", rec("2402.00003", "", "math.AG")},
	} {
		if _, err := c.Add(r.shard, r.rec); err != nil {
			t.Fatal(err)
		}
	}
	got := c.Census()
	if got.Total.Records != 5 || got.Cross != 1 {
		t.Fatalf("records %d cross %d", got.Total.Records, got.Cross)
	}
	want := map[Class]int{ClassOpen: 2, ClassVerbatim: 1, ClassRecord: 2}
	for _, cl := range Classes {
		if got.Total.Classes[cl] != want[cl] {
			t.Errorf("%s: %d, want %d", cl, got.Total.Classes[cl], want[cl])
		}
	}
	if last := got.Categories[len(got.Categories)-1]; last.Key != CrossOnly || last.Records != 1 {
		t.Errorf("last category row %+v", last)
	}
	if got.Categories[0].Key != "math.NT" || got.Categories[0].Records != 2 {
		t.Errorf("first category row %+v", got.Categories[0])
	}
	md := got.Markdown()
	for _, s := range []string{"| 2024 | 5 | 2 | 0 | 1 | 2 | 40.0% |", "| cross-list only | 1 |"} {
		if !strings.Contains(md, s) {
			t.Errorf("the report has no %q:\n%s", s, md)
		}
	}
}

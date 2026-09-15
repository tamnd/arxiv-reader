package baselines

import (
	"math"
	"path/filepath"
	"testing"
)

func TestMedianOdd(t *testing.T) {
	if got := median([]float64{5, 1, 3}); got != 3 {
		t.Fatalf("median of three is the middle one, got %v", got)
	}
}

func TestMedianEven(t *testing.T) {
	if got := median([]float64{1, 3, 5, 9}); got != 4 {
		t.Fatalf("median of four is the mean of the middle two, got %v", got)
	}
}

func TestMedianEmpty(t *testing.T) {
	if got := median(nil); got != 0 {
		t.Fatalf("median of nothing is nought, got %v", got)
	}
}

func TestMedianLeavesTheInputAlone(t *testing.T) {
	in := []float64{5, 1, 3}
	median(in)
	if in[0] != 5 {
		t.Fatalf("median sorted its argument, and the caller still wants it in paper order: %v", in)
	}
}

func TestSpreadIgnoresOneOutlier(t *testing.T) {
	// The reason the spread is a median absolute deviation and not a standard
	// deviation. One review paper with forty displays a page is the last value
	// here, and a standard deviation over this would be near seventeen.
	in := []float64{4, 5, 5, 6, 6, 40}
	if got := spread(in); got > 2 {
		t.Fatalf("one outlier moved the spread to %v, which is what a standard deviation would have done", got)
	}
}

func TestSpreadOfAgreement(t *testing.T) {
	if got := spread([]float64{3, 3, 3}); got != 0 {
		t.Fatalf("papers that all agree have no spread, got %v", got)
	}
}

func TestDistanceWithNoSpread(t *testing.T) {
	s := Stat{Median: 3}
	if got := s.Distance(3); got != 0 {
		t.Fatalf("the median itself is no distance away, got %v", got)
	}
	if got := s.Distance(4); !math.IsInf(got, 1) {
		t.Fatalf("a category that has never been anywhere else puts everything else infinitely far, got %v", got)
	}
}

func TestDistanceCountsSpreads(t *testing.T) {
	s := Stat{Median: 10, Spread: 2}
	if got := s.Distance(16); got != 3 {
		t.Fatalf("six is three spreads of two, got %v", got)
	}
	if !s.Far(16, 2.5) {
		t.Fatal("three spreads is further than two and a half")
	}
	if s.Far(16, 3) {
		t.Fatal("three spreads is not further than three")
	}
}

func TestValues(t *testing.T) {
	v := Values(12, 2*Page, 50, 20)
	if v[DisplaysPerPage] != 6 {
		t.Fatalf("twelve displays over two pages is six a page, got %v", v[DisplaysPerPage])
	}
	if v[ReferencesResolved] != 0.4 {
		t.Fatalf("twenty of fifty is two fifths, got %v", v[ReferencesResolved])
	}
}

func TestValuesLeavesOutWhatThePaperCannotSay(t *testing.T) {
	v := Values(0, 0, 0, 0)
	if _, ok := v[DisplaysPerPage]; ok {
		t.Fatal("a paper with no body was counted as having no displays a page, which is a different claim")
	}
	if _, ok := v[ReferencesResolved]; ok {
		t.Fatal("a paper with no bibliography was counted as resolving none of it")
	}
}

func TestBuild(t *testing.T) {
	var samples []Sample
	for i := 0; i < 5; i++ {
		samples = append(samples, Sample{
			ID:       "2501.0000" + string(rune('1'+i)),
			Category: "math.AG",
			Values:   map[Metric]float64{DisplaysPerPage: float64(i + 1)},
		})
	}
	m := Build(samples, "2026-09-15")
	if m.Papers != 5 || m.Built != "2026-09-15" || m.Floor != Floor {
		t.Fatalf("the manifest header is wrong: %+v", m)
	}
	if len(m.Categories) != 1 {
		t.Fatalf("five papers of one category are one row, got %d", len(m.Categories))
	}
	c := m.Categories[0]
	if c.Category != "math.AG" || c.Papers != 5 {
		t.Fatalf("the row is wrong: %+v", c)
	}
	if c.Stats[DisplaysPerPage].Median != 3 {
		t.Fatalf("the median of one to five is three, got %v", c.Stats[DisplaysPerPage].Median)
	}
	if _, ok := c.Stats[ReferencesResolved]; ok {
		t.Fatal("no paper said anything about references, so there is no baseline for it")
	}
}

func TestBuildCountsThePapersAMetricCameFrom(t *testing.T) {
	// The paper count of a category and the paper count of a metric are not the
	// same number, and the second is the one the floor is applied to.
	m := Build([]Sample{
		{ID: "a", Category: "cs.LG", Values: map[Metric]float64{DisplaysPerPage: 2, ReferencesResolved: 0.5}},
		{ID: "b", Category: "cs.LG", Values: map[Metric]float64{DisplaysPerPage: 4}},
	}, "2026-09-15")
	c := m.Categories[0]
	if c.Papers != 2 {
		t.Fatalf("both papers are cs.LG, got %d", c.Papers)
	}
	if got := c.Stats[ReferencesResolved].Papers; got != 1 {
		t.Fatalf("one of the two has a bibliography, got %d", got)
	}
}

func TestBuildSortsAndSkipsTheUncategorised(t *testing.T) {
	m := Build([]Sample{
		{ID: "a", Category: "math.NT"},
		{ID: "b", Category: "astro-ph.HE"},
		{ID: "c", Category: ""},
	}, "2026-09-15")
	if len(m.Categories) != 2 {
		t.Fatalf("the paper with no category is not a category, got %d rows", len(m.Categories))
	}
	if m.Categories[0].Category != "astro-ph.HE" {
		t.Fatalf("the rows are not in category order: %+v", m.Categories)
	}
}

func TestOfNeedsTheFloor(t *testing.T) {
	m := Manifest{Floor: 3, Categories: []Category{
		{Category: "math.AG", Papers: 2, Stats: map[Metric]Stat{DisplaysPerPage: {Median: 9, Papers: 2}}},
		{Category: "math.CO", Papers: 4, Stats: map[Metric]Stat{DisplaysPerPage: {Median: 7, Papers: 4}}},
	}}
	if _, ok := m.Of("math.AG", DisplaysPerPage); ok {
		t.Fatal("two papers are under a floor of three and are not a baseline")
	}
	s, ok := m.Of("math.CO", DisplaysPerPage)
	if !ok || s.Median != 7 {
		t.Fatalf("four papers are over the floor, got %v %v", s, ok)
	}
	if _, ok := m.Of("math.CO", ReferencesResolved); ok {
		t.Fatal("the category has no numbers for that metric")
	}
	if _, ok := m.Of("hep-th", DisplaysPerPage); ok {
		t.Fatal("a category the corpus has never read is not a baseline")
	}
}

func TestOfCountsTheMetricsOwnPapers(t *testing.T) {
	// The category is over the floor and the metric is not, which happens as
	// soon as one paper in a category has no bibliography.
	m := Manifest{Floor: 3, Categories: []Category{
		{Category: "cs.LG", Papers: 5, Stats: map[Metric]Stat{ReferencesResolved: {Median: 0.5, Papers: 2}}},
	}}
	if _, ok := m.Of("cs.LG", ReferencesResolved); ok {
		t.Fatal("two papers behind the metric are under the floor whatever the category count is")
	}
}

func TestOfFallsBackToTheFloorConstant(t *testing.T) {
	// A file written before Floor was recorded, or one somebody edited the
	// field out of, is judged by the rule this binary knows rather than by no
	// rule at all.
	m := Manifest{Categories: []Category{
		{Category: "math.AG", Papers: Floor - 1, Stats: map[Metric]Stat{DisplaysPerPage: {Median: 9, Papers: Floor - 1}}},
	}}
	if _, ok := m.Of("math.AG", DisplaysPerPage); ok {
		t.Fatal("a manifest with no floor in it is read with the built in one")
	}
}

func TestFindAndUsable(t *testing.T) {
	m := Manifest{Floor: 3, Categories: []Category{
		{Category: "math.AG", Papers: 2},
		{Category: "math.CO", Papers: 4},
	}}
	c, ok := m.Find("math.AG")
	if !ok || c.Papers != 2 {
		t.Fatalf("a category under the floor is still in the file, got %v %v", c, ok)
	}
	if _, ok := m.Find("hep-th"); ok {
		t.Fatal("hep-th is not in this manifest")
	}
	if got := m.Usable(); got != 1 {
		t.Fatalf("one of the two is over the floor, got %d", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifests", "baselines.yaml")
	want := Build([]Sample{
		{ID: "a", Category: "math.CO", Values: map[Metric]float64{DisplaysPerPage: 8, ReferencesResolved: 0.75}},
		{ID: "b", Category: "math.CO", Values: map[Metric]float64{DisplaysPerPage: 12, ReferencesResolved: 0.5}},
	}, "2026-09-15")
	if err := want.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Floor != want.Floor || got.Built != want.Built || got.Papers != want.Papers {
		t.Fatalf("the header did not survive: %+v", got)
	}
	if len(got.Categories) != 1 || got.Categories[0].Stats[DisplaysPerPage] != want.Categories[0].Stats[DisplaysPerPage] {
		t.Fatalf("the numbers did not survive: %+v", got.Categories)
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	m, err := Load(filepath.Join(t.TempDir(), "baselines.yaml"))
	if err != nil {
		t.Fatalf("a corpus nobody has built the baselines for yet is not an error: %v", err)
	}
	if len(m.Categories) != 0 {
		t.Fatalf("got categories out of a file that is not there: %+v", m)
	}
}

func TestRoundKeepsFourPlaces(t *testing.T) {
	if got := round(1.0 / 3.0); got != 0.3333 {
		t.Fatalf("got %v", got)
	}
}

package harvest

import (
	"strings"
	"testing"
	"time"
)

func month(s string) time.Time {
	t, err := time.Parse("2006-01", s)
	if err != nil {
		panic(err)
	}
	return t
}

func announced(shard, m string, subs, delta int) Published {
	return Published{Shard: shard, Month: month(m), Submissions: subs, Delta: delta}
}

// Three months arXiv counted: one harvested whole, one short, one never read.
func sample() []Published {
	return []Published{
		announced("2104", "2021-04", 100, 0),
		announced("2105", "2021-05", 200, -10),
		announced("2106", "2021-06", 300, 0),
	}
}

func TestCompareCountsBothSides(t *testing.T) {
	c := Compare(map[string]int{"2104": 100, "2105": 95}, sample())

	if len(c.Rows) != 3 {
		t.Fatalf("%d rows, want 3", len(c.Rows))
	}
	if c.Held() != 195 {
		t.Errorf("holds %d, want 195", c.Held())
	}
	// 100 plus 190 plus 300, with arXiv's own correction already taken off May.
	if c.Live() != 590 {
		t.Errorf("announced %d, want 590", c.Live())
	}
	if got := c.Rows[1].Short(); got != 95 {
		t.Errorf("May is short by %d, want 95", got)
	}
	if got := c.Rows[0].Coverage(); got != 1 {
		t.Errorf("April is at %v, want a whole month", got)
	}
}

// A month arXiv announced papers in and the corpus has none of is the one case
// that is a gap rather than an edge effect, so it is the one called out.
func TestAMonthWithNothingInItIsAGap(t *testing.T) {
	c := Compare(map[string]int{"2104": 100, "2105": 190}, sample())

	empty := c.Empty()
	if len(empty) != 1 {
		t.Fatalf("%d empty months, want 1", len(empty))
	}
	if empty[0].Shard != "2106" {
		t.Errorf("the gap is %s, want 2106", empty[0].Shard)
	}
	// A month that is short is not a month that is empty, and conflating them
	// is how a real gap gets lost in a list of rounding differences.
	if c.Rows[0].Empty() {
		t.Error("a month that was fully harvested reads as a gap")
	}
}

// A month arXiv has no row for holds records whose identifiers name a month
// that does not exist, and dropping the row would hide that.
func TestAMonthArXivDoesNotCountIsKept(t *testing.T) {
	c := Compare(map[string]int{"2104": 100, "9106": 3}, sample())

	odd := c.Unlisted()
	if len(odd) != 1 {
		t.Fatalf("%d unlisted months, want 1", len(odd))
	}
	if odd[0].Shard != "9106" {
		t.Errorf("the unlisted month is %s, want 9106, which is before arXiv started", odd[0].Shard)
	}
	if got := odd[0].Coverage(); got != -1 {
		t.Errorf("a month with no published count has a coverage of %v", got)
	}
	if !strings.Contains(c.Markdown(), "Months arXiv does not count") {
		t.Error("the report does not mention the month arXiv has no row for")
	}
}

// A shard that is not a month cannot be placed on a calendar, and the report
// drops it rather than inventing a date for it. The audit's S14 is what
// catches it, and this only has to not crash.
func TestAShardThatIsNotAMonthIsSkipped(t *testing.T) {
	c := Compare(map[string]int{"2104": 100, "notamonth": 5, "2113": 7}, sample())
	if len(c.Rows) != 3 {
		t.Fatalf("%d rows, want the 3 real months", len(c.Rows))
	}
}

func TestRowsAreInCalendarOrder(t *testing.T) {
	c := Compare(map[string]int{"9107": 1}, []Published{
		announced("2106", "2021-06", 300, 0),
		announced("9912", "1999-12", 5, 0),
		announced("0001", "2000-01", 6, 0),
	})
	var got []string
	for _, r := range c.Rows {
		got = append(got, r.Shard)
	}
	// The century rolls over in the middle of the corpus, so sorting these as
	// text puts the 1990s after the 2020s.
	if want := "9107 9912 0001 2106"; strings.Join(got, " ") != want {
		t.Errorf("the months came out %s, want %s", strings.Join(got, " "), want)
	}
}

func TestShortestIsWorstFirst(t *testing.T) {
	c := Compare(map[string]int{"2104": 90, "2105": 100, "2106": 299}, sample())
	short := c.Shortest(10)
	if len(short) != 3 {
		t.Fatalf("%d short months, want 3", len(short))
	}
	if short[0].Shard != "2105" || short[0].Short() != 90 {
		t.Errorf("the worst month is %s short by %d, want 2105 short by 90", short[0].Shard, short[0].Short())
	}
	if short[2].Shard != "2106" {
		t.Errorf("the least bad month is %s, want 2106, which is short by one", short[2].Shard)
	}
	if got := c.Shortest(2); len(got) != 2 {
		t.Errorf("asked for 2 and got %d", len(got))
	}
}

// A month with nothing in it is already reported as a gap, so listing it again
// at the top of the short list would push every real shortfall off the end.
func TestShortestLeavesTheEmptyMonthsToTheGapList(t *testing.T) {
	c := Compare(map[string]int{"2104": 100}, sample())
	for _, r := range c.Shortest(10) {
		if r.Held == 0 {
			t.Errorf("%s is empty and is in the short list as well", r.Shard)
		}
	}
}

func TestYearsRollUp(t *testing.T) {
	c := Compare(map[string]int{"2104": 100, "2105": 95}, sample())
	years := c.Years()
	if len(years) != 1 {
		t.Fatalf("%d years, want 1", len(years))
	}
	y := years[0]
	if y.Year != 2021 || y.Months != 3 || y.Harvested != 2 {
		t.Errorf("got %d with %d of %d months, want 2021 with 2 of 3", y.Year, y.Harvested, y.Months)
	}
	if y.Held != 195 || y.Live != 590 {
		t.Errorf("got %d of %d, want 195 of 590", y.Held, y.Live)
	}
}

func TestMarkdownShape(t *testing.T) {
	c := Compare(map[string]int{"2104": 100, "2105": 95}, sample())
	c.Generated = month("2026-09")
	c.Source = "the committed copy"
	got := c.Markdown()

	for _, want := range []string{
		"# The harvest",
		"195 records of the 590 records arXiv says it has announced, which is 33.1%",
		"Read on 1 September 2026 against the committed copy.",
		"1 month with nothing in them",
		"## The months furthest short",
		"## By year",
		"| 2021 | 2 of 3 | 195 | 590 | 33.1% |",
		"## By month",
		"| 2021-04 | 100 | 100 | 0 | 100.0% |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report does not have %q:\n%s", want, got)
		}
	}
}

// The difference column is held minus announced, signed either way, because a
// month holding more than arXiv counted is a surplus and not a negative gap.
func TestTheDifferenceColumnIsSignedBothWays(t *testing.T) {
	c := Compare(map[string]int{"2104": 120, "2105": 95}, sample())
	got := c.Markdown()
	if !strings.Contains(got, "| 2021-04 | 120 | 100 | +20 |") {
		t.Errorf("a surplus does not read as one:\n%s", got)
	}
	if !strings.Contains(got, "| 2021-05 | 95 | 190 | -95 |") {
		t.Errorf("a shortfall does not read as one:\n%s", got)
	}
}

func TestTextIsTheSummary(t *testing.T) {
	c := Compare(map[string]int{"2104": 100, "2105": 95}, sample())
	got := c.Text()
	for _, want := range []string{
		"195 records of 590 records announced, 33.1%",
		"2 months held, 3 months announced",
		"1 month with nothing in them, first 2106, last 2106",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the summary does not say %q:\n%s", want, got)
		}
	}
}

// A corpus that is complete should say so plainly rather than listing every
// month it did not fall short in.
func TestACompleteHarvestHasNothingToReport(t *testing.T) {
	c := Compare(map[string]int{"2104": 100, "2105": 190, "2106": 300}, sample())
	got := c.Markdown()
	if c.Fraction() != 1 {
		t.Errorf("a complete harvest is at %v", c.Fraction())
	}
	for _, unwanted := range []string{"with nothing in them", "furthest short", "does not count"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("a complete harvest reported %q:\n%s", unwanted, got)
		}
	}
}

// An empty corpus is every month short, and the point of the check is that it
// says so rather than dividing by zero or reporting a hundred percent of
// nothing.
func TestAnEmptyCorpusIsEveryMonthShort(t *testing.T) {
	c := Compare(nil, sample())
	if c.Held() != 0 || c.Fraction() != 0 {
		t.Errorf("an empty corpus holds %d at %v", c.Held(), c.Fraction())
	}
	if len(c.Empty()) != 3 {
		t.Errorf("%d months are empty, want all 3", len(c.Empty()))
	}
	if !strings.Contains(c.Text(), "0 records of 590 records announced, 0.0%") {
		t.Errorf("the summary reads wrong:\n%s", c.Text())
	}
}

func TestThousands(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{
		{0, "0"},
		{7, "7"},
		{999, "999"},
		{1000, "1,000"},
		{16248, "16,248"},
		{3163381, "3,163,381"},
		{-16244, "-16,244"},
		{-999, "-999"},
	} {
		if got := thousands(tc.n); got != tc.want {
			t.Errorf("thousands(%d) is %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestPercent(t *testing.T) {
	for _, tc := range []struct {
		f    float64
		want string
	}{
		{1, "100.0%"},
		{0.5, "50.0%"},
		{0, "0.0%"},
		{-1, "n/a"},
	} {
		if got := percent(tc.f); got != tc.want {
			t.Errorf("percent(%v) is %q, want %q", tc.f, got, tc.want)
		}
	}
}

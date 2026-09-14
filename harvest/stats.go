package harvest

import (
	"context"
	_ "embed"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// StatsURL is where arXiv publishes its own monthly submission counts.
//
// It is a CSV of month, submissions and historical_delta, it is about six
// kilobytes for the whole of arXiv's history, and it is the only number this
// project has that was not produced by this project. That is what makes it
// worth having: a harvest that is short by a month cannot tell on its own,
// because a month it never read looks exactly like a month with nothing in it.
const StatsURL = "https://arxiv.org/stats/get_monthly_submissions"

// statsURL is what FetchStats actually reads, so a test can point it at a
// server on localhost. It is the constant above everywhere else.
var statsURL = StatsURL

// publishedCSV is a copy of that file, committed so the report works offline.
//
// Embedded rather than fetched, because the report runs in CI and CI does not
// talk to arXiv. It is a few hundred bytes of drift per month and `ax harvest
// report -fetch` is how it gets refreshed.
//
//go:embed data/monthly-submissions.csv
var publishedCSV string

// Published is one month as arXiv counts it.
type Published struct {
	// Shard is the month in the corpus's own YYMM form.
	Shard string
	// Month is the first of that month.
	Month time.Time
	// Submissions is what arXiv announced that month.
	Submissions int
	// Delta is arXiv's own correction for papers that have since been removed
	// from the record. It is never positive and it is zero for everything after
	// September 2005.
	Delta int
}

// Live is the number that should still be there to harvest.
func (p Published) Live() int { return p.Submissions + p.Delta }

// PublishedStats is the committed copy of arXiv's counts.
func PublishedStats() ([]Published, error) {
	return ParseStats(strings.NewReader(publishedCSV))
}

// ParseStats reads arXiv's monthly submissions CSV.
func ParseStats(r io.Reader) ([]Published, error) {
	rows, err := csv.NewReader(r).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("the monthly submissions file did not parse: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("the monthly submissions file is empty")
	}
	// The header is checked rather than skipped on faith, because the day arXiv
	// adds a column in the middle is the day a report starts quietly counting
	// the wrong one.
	if got := strings.Join(rows[0], ","); got != "month,submissions,historical_delta" {
		return nil, fmt.Errorf("the monthly submissions file starts %q, want month,submissions,historical_delta", got)
	}

	out := make([]Published, 0, len(rows)-1)
	for i, row := range rows[1:] {
		line := i + 2
		if len(row) != 3 {
			return nil, fmt.Errorf("line %d has %d fields, want 3", line, len(row))
		}
		month, err := time.Parse("2006-01", row[0])
		if err != nil {
			return nil, fmt.Errorf("line %d: %q is not a month: %w", line, row[0], err)
		}
		subs, err := strconv.Atoi(row[1])
		if err != nil {
			return nil, fmt.Errorf("line %d: %q is not a count: %w", line, row[1], err)
		}
		delta, err := strconv.Atoi(row[2])
		if err != nil {
			return nil, fmt.Errorf("line %d: %q is not a count: %w", line, row[2], err)
		}
		out = append(out, Published{
			Shard:       month.Format("0601"),
			Month:       month,
			Submissions: subs,
			Delta:       delta,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Month.Before(out[j].Month) })
	return out, nil
}

// FetchStats reads the file from arXiv.
//
// One request for six kilobytes, so none of the pacing the rows API and
// OAI-PMH need applies here. It is still a request to arXiv, which is why it
// only happens when somebody asks for it.
func FetchStats(ctx context.Context, client *http.Client, agent string) ([]Published, string, error) {
	if client == nil {
		client = &http.Client{Timeout: time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, statsURL, nil)
	if err != nil {
		return nil, "", err
	}
	if agent != "" {
		req.Header.Set("User-Agent", agent)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%s said %s", statsURL, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, "", err
	}
	stats, err := ParseStats(strings.NewReader(string(body)))
	if err != nil {
		return nil, "", err
	}
	return stats, string(body), nil
}

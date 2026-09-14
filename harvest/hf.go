package harvest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tamnd/arxiv-reader/metadata"
)

// Mirror is the Hugging Face dataset this reads unless told otherwise.
//
// Several people mirror Cornell's snapshot and they do not all keep it up.
// This one was rebuilt the day it was picked and it carries the same columns as
// the Kaggle file under the same names, which is what makes one row parser
// serve both.
const Mirror = "librarian-bots/arxiv-metadata-snapshot"

// RowsEndpoint is the Hugging Face datasets server.
//
// It answers with JSON whatever the dataset is stored as, which matters here:
// the mirrors keep the snapshot as parquet, and reading parquet would mean a
// dependency and a column reader for a file this project reads once. The rows
// API hands back the same fields as JSON and needs no login at all.
const RowsEndpoint = "https://datasets-server.huggingface.co/rows"

// RowsPage is the largest page the rows API will serve. Ask for 200 and it
// answers with an error rather than with 200 rows.
const RowsPage = 100

// RowsPace is the gap left between requests.
//
// Hugging Face publishes no number the way arXiv does, so this was measured
// rather than quoted. A second a request died on a CloudFront 429 forty pages
// into a walk. Three seconds ran seventy pages without one, twice. So three,
// which is also what arXiv asks for, and there is something to be said for one
// pace in the whole tool.
//
// The limit is CloudFront's and not the application's: the 429 comes back as
// HTML with no Retry-After, and it cleared in about twenty seconds. That is a
// short window rather than a ban, which is why backoff is worth doing at all.
//
// It is also why this is the fallback and not the bootstrap. The page is a
// hundred rows and the split is about 3.17 million, so a full walk is 31646
// requests and a day. Read the file if you can get the file.
const RowsPace = 3 * time.Second

// Rows reads the snapshot through the Hugging Face datasets server.
//
// This is the third way in and the only one that needs neither a login nor a
// five gigabyte download. Kaggle wants an account, a mirror's parquet wants a
// parquet reader, and this wants neither, so it is what is left when the other
// two are not available.
type Rows struct {
	// Dataset defaults to Mirror, as owner/name.
	Dataset string
	// Config defaults to "default" and Split to "train", which is what every
	// mirror of this snapshot uses.
	Config, Split string
	// Endpoint defaults to the package constant.
	Endpoint string
	// HTTP defaults to a client with a two minute timeout. A page is about
	// 190 KB and the server builds it from parquet on demand, so the first
	// request against a cold dataset is slower than the rest.
	HTTP *http.Client
	// UserAgent should name the project and a way to get hold of a person.
	UserAgent string
	// Pace is the gap to leave between requests, and zero means RowsPace.
	//
	// Zero means the default rather than no wait, for the same reason it does
	// on OAI: the dangerous value is the fast one, so it is the one that has to
	// be asked for.
	Pace time.Duration
	// Now defaults to time.Now.
	Now func() time.Time
	// Log, if set, is called once per page with the running count and the
	// number of rows the server says the split holds.
	Log func(page, records, total int)

	last time.Time
}

// RowsQuery is a walk over the split.
type RowsQuery struct {
	// Offset is the row to start at.
	//
	// A resume point and nothing else. The rows come back in the order the
	// mirror wrote its parquet, which is neither identifier order nor date
	// order: row zero of the mirror picked here is 0909.0774. So an offset
	// cannot be used to ask for recent papers, only to pick up a walk that
	// stopped.
	Offset int
	// Limit stops after this many rows, rounded up to the end of a page. Zero
	// means every row in the split.
	Limit int
}

// List walks the split and calls fn once per record.
//
// The rows carry the same field names as the Kaggle file, so they go through
// the same row parser and come out as the same records, tagged hf rather than
// kaggle. Everything the snapshot reader says about this data holds here too:
// one licence for the whole paper, no MSC class, authors already split.
func (r *Rows) List(ctx context.Context, q RowsQuery, fn func(metadata.Record) error) (SnapshotStats, error) {
	var stats SnapshotStats
	if q.Offset < 0 {
		return stats, fmt.Errorf("harvest: offset %d is before the start of the split", q.Offset)
	}

	snap := Snapshot{Source: metadata.SourceHF, Now: r.Now}
	offset := q.Offset
	page := 0
	total := 0

	for {
		length := RowsPage
		if q.Limit > 0 && q.Limit-stats.Read < length {
			length = q.Limit - stats.Read
		}
		if length < 1 {
			return stats, nil
		}

		body, err := r.get(ctx, offset, length)
		if err != nil {
			return stats, err
		}
		var resp rowsResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return stats, fmt.Errorf("harvest: page at offset %d is not the JSON we expected: %w", offset, err)
		}
		if resp.Error != "" {
			return stats, fmt.Errorf("harvest: hugging face answered: %s", resp.Error)
		}
		page++
		total = resp.Total
		// An empty page is the end of the split. The server does not say "no
		// more", it just stops having rows, so this is the only end condition
		// other than the limit.
		if len(resp.Rows) == 0 {
			return stats, nil
		}

		for _, row := range resp.Rows {
			// A truncated cell is the one thing here that could put wrong data
			// in the plane quietly. The server shortens large values and lists
			// which ones it shortened, so a long abstract could arrive cut off
			// and look like a complete abstract. Skipped and counted rather
			// than written.
			if len(row.Truncated) > 0 {
				stats.Skipped++
				continue
			}
			rec, err := snap.record(row.Row)
			if err != nil {
				return stats, fmt.Errorf("harvest: row %d: %w", row.Index, err)
			}
			if _, err := rec.Shard(); err != nil {
				stats.Skipped++
				continue
			}
			if err := fn(rec); err != nil {
				return stats, err
			}
			stats.Read++
		}

		offset += len(resp.Rows)
		if r.Log != nil {
			r.Log(page, stats.Read, total)
		}
		if q.Limit > 0 && stats.Read+stats.Skipped >= q.Limit {
			return stats, nil
		}
		if total > 0 && offset >= total {
			return stats, nil
		}
	}
}

// Total asks how many rows the split holds.
//
// One row is requested rather than none, because the count comes back in the
// envelope around the rows and there is no cheaper way to ask for it.
func (r *Rows) Total(ctx context.Context) (int, error) {
	body, err := r.get(ctx, 0, 1)
	if err != nil {
		return 0, err
	}
	var resp rowsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("harvest: %w", err)
	}
	if resp.Error != "" {
		return 0, fmt.Errorf("harvest: hugging face answered: %s", resp.Error)
	}
	return resp.Total, nil
}

// Page fetches one page of rows as the server sent it.
//
// This is here because the datasets server holds more than one dataset worth
// reading and only one of them parses into a metadata record. The Common Pile's
// arXiv collection has different columns and is read for a different reason, so
// it gets the JSON and parses it itself, but it goes through the same pace, the
// same backoff and the same set of statuses that mean wait rather than stop.
// There is one rate limit and it belongs to the server, not to the caller.
func (r *Rows) Page(ctx context.Context, offset, length int) ([]byte, error) {
	if offset < 0 {
		return nil, fmt.Errorf("harvest: offset %d is before the start of the split", offset)
	}
	if length < 1 || length > RowsPage {
		return nil, fmt.Errorf("harvest: %d rows is not a page the server will serve, which is 1 to %d", length, RowsPage)
	}
	return r.get(ctx, offset, length)
}

func (r *Rows) get(ctx context.Context, offset, length int) ([]byte, error) {
	endpoint := r.Endpoint
	if endpoint == "" {
		endpoint = RowsEndpoint
	}
	dataset := r.Dataset
	if dataset == "" {
		dataset = Mirror
	}
	config := r.Config
	if config == "" {
		config = "default"
	}
	split := r.Split
	if split == "" {
		split = "train"
	}
	pace := r.Pace
	if pace == 0 {
		pace = RowsPace
	}
	client := r.HTTP
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}

	v := url.Values{}
	v.Set("dataset", dataset)
	v.Set("config", config)
	v.Set("split", split)
	v.Set("offset", strconv.Itoa(offset))
	v.Set("length", strconv.Itoa(length))

	const tries = 5
	for try := 1; ; try++ {
		if err := r.wait(ctx, pace); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+v.Encode(), nil)
		if err != nil {
			return nil, err
		}
		if r.UserAgent != "" {
			req.Header.Set("User-Agent", r.UserAgent)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("harvest: %w", err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()

		if temporary(resp.StatusCode) {
			if try == tries {
				return nil, fmt.Errorf("harvest: hugging face answered %s %d times in a row, so it is down and not busy", resp.Status, tries)
			}
			time.Sleep(backoff(resp.Header.Get("Retry-After"), pace, try))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			// The body carries the reason and it is usually the useful half,
			// so a bad dataset name says so rather than saying 404.
			var e rowsResponse
			if json.Unmarshal(body, &e) == nil && e.Error != "" {
				return nil, fmt.Errorf("harvest: hugging face answered %s: %s", resp.Status, strings.TrimSpace(e.Error))
			}
			return nil, fmt.Errorf("harvest: hugging face answered %s", resp.Status)
		}
		if readErr != nil {
			return nil, fmt.Errorf("harvest: reading the page: %w", readErr)
		}
		return body, nil
	}
}

// backoff is how long to wait before try number try+1.
//
// A server that says Retry-After is answered literally, because it knows when
// it will be ready and this does not, and doubling a number somebody sent on
// purpose turns a five minute wait into forty.
//
// A server that says nothing gets a doubling wait instead. Repeating at the
// ordinary pace is the wrong thing after a 502: something between here and the
// dataset is restarting, and asking again a second later is asking during the
// restart. Five tries at a doubling second spans about a quarter of a minute.
func backoff(header string, pace time.Duration, try int) time.Duration {
	if header != "" {
		return retryAfter(header, pace)
	}
	return pace << (try - 1)
}

// temporary reports whether a status means wait rather than stop.
//
// 429 is what Hugging Face uses for too many requests and 503 turns up while
// the server is building a dataset's parquet index. Those two were obvious.
//
// 502 and 504 were not, and they are the ones that actually stopped a harvest:
// the datasets server sits behind a proxy, the proxy answers for it when it is
// slow or restarting, and a 5000 row walk hit one about forty pages in. A walk
// that gives up on a gateway hiccup is a walk nobody can leave running, which
// is the only way a walk of this length gets used at all.
func temporary(status int) bool {
	switch status {
	case http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

func (r *Rows) wait(ctx context.Context, pace time.Duration) error {
	if !r.last.IsZero() {
		if gap := pace - time.Since(r.last); gap > 0 {
			t := time.NewTimer(gap)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.last = time.Now()
	return nil
}

// rowsResponse is one page of the rows API.
type rowsResponse struct {
	Rows []struct {
		Index int         `json:"row_idx"`
		Row   snapshotRow `json:"row"`
		// Truncated names the cells the server shortened to keep the page
		// small. Empty for almost every row and never ignored.
		Truncated []string `json:"truncated_cells"`
	} `json:"rows"`
	// Total is the whole split and not this page, so it is the same number on
	// every page and it is what progress is measured against.
	Total int `json:"num_rows_total"`
	// Error is set instead of rows when the request was wrong, so a dataset
	// name with a typo in it comes back here.
	Error string `json:"error"`
}

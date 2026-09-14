// Package harvest reads arXiv's metadata off the surfaces that publish it and
// turns it into metadata.Records.
//
// Nothing here writes to disk. A harvester hands records to the metadata plane
// and the plane decides what happens to them, which is what makes it possible
// to test a harvest against a captured response with no corpus in sight.
package harvest

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
)

// Endpoint is arXiv's OAI-PMH endpoint.
//
// Note the host. arxiv.org/oai2 is the old address and it still answers, but it
// answers from behind the same rate limiter as the website, and a harvest that
// uses it will be blocked within the hour.
const Endpoint = "https://oaipmh.arxiv.org/oai"

// Formats are the two metadata formats this project reads, and it reads both
// because neither one is enough.
//
// arXivRaw has the version history, the sizes, the submitter and the licence.
// It gives the author list as one comma separated string, and splitting that
// back into names is a guess that gets "Balazs, C." and "Nguyen Van A" wrong in
// opposite directions.
//
// arXiv has the authors already split into keyname and forenames, which is the
// one thing arXivRaw cannot do. It has no version history, its created date is
// the date of the metadata rather than of the paper, and it substitutes Unicode
// for TeX in the title and the abstract. On math/9602216 the arXivRaw title
// reads "$ \kappa $ measurable" and the arXiv title reads "$ κ$ measurable",
// which is not a title you can put through LaTeX.
//
// So the raw format is the record and the arXiv format is an authors pass over
// it. FormatArXiv parses into a record with no title and no abstract on
// purpose, so that a merge can never let the mangled text win.
const (
	FormatRaw   = "arXivRaw"
	FormatArXiv = "arXiv"
)

// Pace is how long to wait between requests.
//
// arXiv asks for one request every three seconds in its terms of use for the
// OAI interface. This is not a rate limit to be discovered by hitting it, it is
// a number they published, and a harvest of three million records is a thing
// worth being allowed to finish.
const Pace = 3 * time.Second

// OAI reads records out of arXiv's OAI-PMH endpoint.
type OAI struct {
	// Endpoint defaults to the package constant.
	Endpoint string
	// HTTP defaults to a client with a one minute timeout. A full ListRecords
	// page is about a megabyte and arXiv is not always quick about it.
	HTTP *http.Client
	// UserAgent should name the project and a way to get hold of a person.
	// arXiv blocks anonymous bulk readers and they are right to.
	UserAgent string
	// Pace is the gap to leave between requests, and zero means the package
	// constant.
	//
	// Zero means the default rather than no wait on purpose. The dangerous
	// value is the fast one, so it is the one that has to be asked for: a
	// caller who forgets this field gets the polite behaviour and a caller who
	// wants to go faster has to write down a number and think about it.
	Pace time.Duration
	// Now defaults to time.Now, and exists so the harvested date in a test does
	// not change when the day does.
	Now func() time.Time
	// Log, if set, is called once per page with the running count.
	Log func(page, records int, token string)

	last time.Time
}

// Query is one ListRecords request.
type Query struct {
	// Format is FormatRaw or FormatArXiv.
	Format string
	// From and Until bound the datestamp, as YYYY-MM-DD, and either may be
	// empty.
	//
	// The datestamp is when arXiv last touched the record and not when the
	// paper was submitted. A range of one day in 2024 comes back holding a
	// paper from 1996 whose metadata was corrected that morning. That is the
	// right behaviour for keeping a corpus current and the wrong thing to
	// reason about if you wanted a day's submissions.
	From, Until string
	// Set restricts to one OAI set, so "math" or "cs". Empty means everything.
	Set string
	// Limit stops after this many records, rounded up to the end of a page.
	// Zero means no limit.
	Limit int
}

func (q Query) values() url.Values {
	v := url.Values{}
	v.Set("verb", "ListRecords")
	v.Set("metadataPrefix", q.Format)
	if q.From != "" {
		v.Set("from", q.From)
	}
	if q.Until != "" {
		v.Set("until", q.Until)
	}
	if q.Set != "" {
		v.Set("set", q.Set)
	}
	return v
}

// Validate reports what is wrong with a query, or nil.
func (q Query) Validate() error {
	if q.Format != FormatRaw && q.Format != FormatArXiv {
		return fmt.Errorf("harvest: %q is not a metadata format arXiv serves, want %s or %s", q.Format, FormatRaw, FormatArXiv)
	}
	for _, d := range []struct{ name, value string }{{"from", q.From}, {"until", q.Until}} {
		if d.value == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", d.value); err != nil {
			return fmt.Errorf("harvest: %s %q is not a YYYY-MM-DD date", d.name, d.value)
		}
	}
	if q.From != "" && q.Until != "" && q.From > q.Until {
		return fmt.Errorf("harvest: from %s is after until %s", q.From, q.Until)
	}
	return nil
}

// List walks every page of a ListRecords query and calls fn once per record.
//
// fn is called as the pages arrive rather than at the end, because a full
// harvest is three million records and holding them all costs more memory than
// the machine has.
func (o *OAI) List(ctx context.Context, q Query, fn func(metadata.Record) error) error {
	if err := q.Validate(); err != nil {
		return err
	}
	var (
		token string
		page  int
		count int
	)
	for {
		v := q.values()
		if token != "" {
			// A resumption token replaces every other argument. Sending the
			// original from and until alongside it is an error arXiv answers
			// with badArgument rather than with the next page.
			v = url.Values{"verb": {"ListRecords"}, "resumptionToken": {token}}
		}
		body, err := o.get(ctx, v)
		if err != nil {
			return err
		}
		var resp listResponse
		if err := xml.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("harvest: page %d is not the XML we expected: %w", page+1, err)
		}
		if resp.Error.Code != "" {
			// noRecordsMatch is arXiv saying the range is empty, which is a
			// normal answer for a catch up on a quiet day and not a failure.
			if resp.Error.Code == "noRecordsMatch" {
				return nil
			}
			return fmt.Errorf("harvest: arXiv answered %s: %s", resp.Error.Code, strings.TrimSpace(resp.Error.Message))
		}
		page++

		for _, rec := range resp.Records {
			if rec.Header.Status == "deleted" {
				// A deleted record has a header and no metadata. arXiv uses it
				// for papers withdrawn before announcement, and there is
				// nothing to write.
				continue
			}
			r, err := o.record(rec, q.Format)
			if err != nil {
				return err
			}
			if err := fn(r); err != nil {
				return err
			}
			count++
		}
		token = strings.TrimSpace(resp.Token)
		if o.Log != nil {
			o.Log(page, count, token)
		}
		// An empty token is the end. arXiv sends the element with no content on
		// the last page rather than leaving it out, so the test is on the
		// content and not on whether the element was there.
		if token == "" {
			return nil
		}
		if q.Limit > 0 && count >= q.Limit {
			return nil
		}
	}
}

// Records collects a whole query into memory. For a bounded range only.
func (o *OAI) Records(ctx context.Context, q Query) ([]metadata.Record, error) {
	var out []metadata.Record
	err := o.List(ctx, q, func(r metadata.Record) error {
		out = append(out, r)
		return nil
	})
	return out, err
}

// get makes one request, keeping to the pace and honouring a 503.
func (o *OAI) get(ctx context.Context, v url.Values) ([]byte, error) {
	endpoint := o.Endpoint
	if endpoint == "" {
		endpoint = Endpoint
	}
	pace := o.Pace
	if pace == 0 {
		pace = Pace
	}
	client := o.HTTP
	if client == nil {
		client = &http.Client{Timeout: time.Minute}
	}

	// Up to five tries. Beyond that the endpoint is down rather than busy, and
	// a harvest that retries forever is a harvest nobody notices has stopped.
	const tries = 5
	for try := 1; ; try++ {
		if err := o.wait(ctx, pace); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+v.Encode(), nil)
		if err != nil {
			return nil, err
		}
		if o.UserAgent != "" {
			req.Header.Set("User-Agent", o.UserAgent)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("harvest: %w", err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusServiceUnavailable {
			if try == tries {
				return nil, fmt.Errorf("harvest: arXiv asked us to wait %d times in a row, so it is down and not busy", tries)
			}
			// arXiv answers a busy endpoint with 503 and a Retry-After in
			// seconds. It means it, and the number is sometimes minutes.
			time.Sleep(retryAfter(resp.Header.Get("Retry-After"), pace))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("harvest: arXiv answered %s", resp.Status)
		}
		if readErr != nil {
			return nil, fmt.Errorf("harvest: reading the page: %w", readErr)
		}
		return body, nil
	}
}

// wait sleeps until the pace allows another request.
func (o *OAI) wait(ctx context.Context, pace time.Duration) error {
	if !o.last.IsZero() {
		if gap := pace - time.Since(o.last); gap > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(gap):
			}
		}
	}
	o.last = time.Now()
	return ctx.Err()
}

// retryAfter reads the header, in seconds, and falls back to the pace.
func retryAfter(header string, pace time.Duration) time.Duration {
	if n, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && n > 0 {
		// Capped at five minutes so a bad header cannot hang a harvest for a
		// day. arXiv's own values are tens of seconds.
		if d := time.Duration(n) * time.Second; d < 5*time.Minute {
			return d
		}
		return 5 * time.Minute
	}
	return pace
}

func (o *OAI) today() string {
	now := time.Now
	if o.Now != nil {
		now = o.Now
	}
	return now().UTC().Format("2006-01-02")
}

// record turns one OAI record into a metadata.Record.
func (o *OAI) record(rec oaiRecord, format string) (metadata.Record, error) {
	switch format {
	case FormatRaw:
		return o.fromRaw(rec)
	case FormatArXiv:
		return o.fromArXiv(rec)
	}
	return metadata.Record{}, fmt.Errorf("harvest: no parser for format %q", format)
}

func (o *OAI) fromRaw(rec oaiRecord) (metadata.Record, error) {
	raw := rec.Metadata.Raw
	licence, err := corpus.LicenceFromURL(raw.Licence)
	if err != nil {
		return metadata.Record{}, fmt.Errorf("harvest: %s: %w", raw.ID, err)
	}

	versions := make([]metadata.Version, 0, len(raw.Versions))
	for _, v := range raw.Versions {
		n, err := versionNumber(v.Version)
		if err != nil {
			return metadata.Record{}, fmt.Errorf("harvest: %s: %w", raw.ID, err)
		}
		created, err := parseDate(v.Date)
		if err != nil {
			return metadata.Record{}, fmt.Errorf("harvest: %s v%d: %w", raw.ID, n, err)
		}
		versions = append(versions, metadata.Version{
			Version: n,
			Created: created,
			// Every version gets the same licence, because that is all arXiv
			// says. The OAI record carries one licence element for the whole
			// paper and its version elements carry no licence attribute, which
			// the spec for this project got wrong: it assumed a per version
			// licence was available here. It is only on the abs page, at a
			// fifteen second pace, which is what the M2 census is for.
			Licence:     licence,
			LicenceFrom: metadata.SourceOAI,
		})
	}

	return metadata.Record{
		ID:         raw.ID,
		Title:      raw.Title,
		Abstract:   raw.Abstract,
		Categories: strings.Fields(raw.Categories),
		Versions:   versions,
		DOI:        raw.DOI,
		JournalRef: raw.JournalRef,
		ReportNo:   raw.ReportNo,
		Comments:   raw.Comments,
		MSCClass:   raw.MSCClass,
		ACMClass:   raw.ACMClass,
		Source:     metadata.SourceOAI,
		Harvested:  o.today(),
	}.Normalise(), nil
}

// fromArXiv reads the authors pass.
//
// Title and abstract are deliberately left empty. This format substitutes
// Unicode characters for TeX macros in both, and a record with an empty title
// merges cleanly onto one that has a good title while a record with a mangled
// title would quietly replace it.
func (o *OAI) fromArXiv(rec oaiRecord) (metadata.Record, error) {
	meta := rec.Metadata.ArXiv
	authors := make([]metadata.Author, 0, len(meta.Authors))
	for _, a := range meta.Authors {
		authors = append(authors, metadata.Author{
			Surname:  a.Keyname,
			Forename: a.Forenames,
			Suffix:   a.Suffix,
		})
	}
	return metadata.Record{
		ID:         meta.ID,
		Authors:    authors,
		Categories: strings.Fields(meta.Categories),
		DOI:        meta.DOI,
		JournalRef: meta.JournalRef,
		ReportNo:   meta.ReportNo,
		Comments:   meta.Comments,
		MSCClass:   meta.MSCClass,
		ACMClass:   meta.ACMClass,
		Source:     metadata.SourceOAI,
		Harvested:  o.today(),
	}.Normalise(), nil
}

// versionNumber reads the 3 out of "v3".
func versionNumber(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(s), "v"))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%q is not a version number", s)
	}
	return n, nil
}

// dateLayouts are the shapes arXiv's version dates come in.
//
// The first is what almost every record uses. The others turn up in the older
// half of the corpus, where the day of the week, the seconds and the timezone
// are each optional and not in a way that correlates with anything.
var dateLayouts = []string{
	"Mon, 2 Jan 2006 15:04:05 MST",
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04 MST",
	"2 Jan 2006 15:04:05 MST",
	"Mon, 2 Jan 2006",
	"2006-01-02",
}

func parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("%q is not a date in any shape arXiv sends", s)
}

// The XML below is only as much of OAI-PMH as this project reads. The full
// schema has sets, identify and metadata format listings, and none of it says
// anything about a paper.

type listResponse struct {
	XMLName xml.Name    `xml:"OAI-PMH"`
	Error   oaiError    `xml:"error"`
	Records []oaiRecord `xml:"ListRecords>record"`
	Token   string      `xml:"ListRecords>resumptionToken"`
}

type oaiError struct {
	Code    string `xml:"code,attr"`
	Message string `xml:",chardata"`
}

type oaiRecord struct {
	Header struct {
		Identifier string `xml:"identifier"`
		Datestamp  string `xml:"datestamp"`
		Status     string `xml:"status,attr"`
	} `xml:"header"`
	Metadata struct {
		Raw   rawMetadata   `xml:"arXivRaw"`
		ArXiv arxivMetadata `xml:"arXiv"`
	} `xml:"metadata"`
}

type rawMetadata struct {
	ID         string `xml:"id"`
	Submitter  string `xml:"submitter"`
	Title      string `xml:"title"`
	Authors    string `xml:"authors"`
	Categories string `xml:"categories"`
	Comments   string `xml:"comments"`
	ReportNo   string `xml:"report-no"`
	JournalRef string `xml:"journal-ref"`
	DOI        string `xml:"doi"`
	MSCClass   string `xml:"msc-class"`
	ACMClass   string `xml:"acm-class"`
	Licence    string `xml:"license"`
	Abstract   string `xml:"abstract"`
	Versions   []struct {
		Version string `xml:"version,attr"`
		Date    string `xml:"date"`
		Size    string `xml:"size"`
	} `xml:"version"`
}

type arxivMetadata struct {
	ID         string `xml:"id"`
	Created    string `xml:"created"`
	Updated    string `xml:"updated"`
	Title      string `xml:"title"`
	Categories string `xml:"categories"`
	Comments   string `xml:"comments"`
	ReportNo   string `xml:"report-no"`
	JournalRef string `xml:"journal-ref"`
	DOI        string `xml:"doi"`
	MSCClass   string `xml:"msc-class"`
	ACMClass   string `xml:"acm-class"`
	Licence    string `xml:"license"`
	Abstract   string `xml:"abstract"`
	Authors    []struct {
		Keyname   string `xml:"keyname"`
		Forenames string `xml:"forenames"`
		Suffix    string `xml:"suffix"`
	} `xml:"authors>author"`
}

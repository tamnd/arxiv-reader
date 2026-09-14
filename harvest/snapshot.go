package harvest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
)

// Snapshot reads the Cornell metadata snapshot, one JSON object per line.
//
// This is the bootstrap. OAI-PMH will serve the whole archive, but at three
// seconds a page and about a thousand records a page that is most of a week of
// continuous requests. The snapshot is the same 3.17 million records in one
// file, and it is republished often enough that the catch up afterwards is days
// rather than decades.
//
// Cornell puts it on Kaggle and several people mirror it to Hugging Face. The
// bytes are the same either way, so one reader serves both and the caller says
// which one it was. That matters later: audit rule S10 fails a content plane
// paper whose licence came from here, because this file has one licence for the
// whole paper and the content plane needs the licence of the version it
// extracted.
type Snapshot struct {
	// Source is SourceKaggle or SourceHF, and says where the bytes came from.
	Source metadata.Source
	// Now defaults to time.Now.
	Now func() time.Time
	// Log, if set, is called every LogEvery records.
	Log func(read, skipped int)
	// LogEvery defaults to 100000.
	LogEvery int
}

// SnapshotStats is what a pass over the file did.
type SnapshotStats struct {
	// Read is the number of lines that became records.
	Read int
	// Skipped is the number of lines that could not, each one reported through
	// the callback's error or counted here when the id is the problem.
	Skipped int
}

func (s SnapshotStats) String() string {
	if s.Skipped == 0 {
		return fmt.Sprintf("%d records", s.Read)
	}
	return fmt.Sprintf("%d records, %d lines skipped", s.Read, s.Skipped)
}

// Read streams r and calls fn once per record.
//
// Streamed and never loaded. The file is about 5 GB of JSON and holding the
// parsed form of it costs several times that, which is more than most machines
// have and all of it for no reason: nothing in this project needs two records
// at the same time.
func (s Snapshot) Read(r io.Reader, fn func(metadata.Record) error) (SnapshotStats, error) {
	var stats SnapshotStats
	every := s.LogEvery
	if every == 0 {
		every = 100000
	}

	sc := bufio.NewScanner(r)
	// A megabyte a line. bufio.Scanner defaults to 64 KB and truncates past it
	// without saying anything, which would turn a long abstract into a JSON
	// parse error a hundred lines into a five gigabyte file.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	line := 0
	for sc.Scan() {
		line++
		b := sc.Bytes()
		if len(strings.TrimSpace(string(b))) == 0 {
			continue
		}
		var row snapshotRow
		if err := json.Unmarshal(b, &row); err != nil {
			return stats, fmt.Errorf("harvest: line %d: %w", line, err)
		}
		rec, err := s.record(row)
		if err != nil {
			return stats, fmt.Errorf("harvest: line %d: %w", line, err)
		}
		if _, err := rec.Shard(); err != nil {
			// An id this tool cannot place is a record with nowhere to go.
			// Counted rather than fatal: one bad line should not throw away the
			// three million good ones behind it.
			stats.Skipped++
			continue
		}
		if err := fn(rec); err != nil {
			return stats, err
		}
		stats.Read++
		if s.Log != nil && stats.Read%every == 0 {
			s.Log(stats.Read, stats.Skipped)
		}
	}
	if err := sc.Err(); err != nil {
		return stats, fmt.Errorf("harvest: line %d: %w", line+1, err)
	}
	return stats, nil
}

func (s Snapshot) today() string {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	return now().UTC().Format("2006-01-02")
}

func (s Snapshot) record(row snapshotRow) (metadata.Record, error) {
	// A null licence is the ordinary case for the older half of the file and
	// not a gap in it. arXiv only started offering a choice in 2004, so
	// solv-int/9806004 has no licence element anywhere, and the submitter
	// agreed to arXiv's own terms. LicenceFromURL reads an empty string as that
	// default rather than as unknown, which is the whole reason it exists.
	licence, err := corpus.LicenceFromURL(row.Licence)
	if err != nil {
		return metadata.Record{}, fmt.Errorf("%s: %w", row.ID, err)
	}

	versions := make([]metadata.Version, 0, len(row.Versions))
	for _, v := range row.Versions {
		n, err := versionNumber(v.Version)
		if err != nil {
			return metadata.Record{}, fmt.Errorf("%s: %w", row.ID, err)
		}
		created, err := parseDate(v.Created)
		if err != nil {
			return metadata.Record{}, fmt.Errorf("%s v%d: %w", row.ID, n, err)
		}
		versions = append(versions, metadata.Version{
			Version: n,
			Created: created,
			// The file states one licence for the paper and this is it, on
			// every version. It is almost certainly the latest version's
			// licence, and an author who moved from the default to CC BY on v3
			// looks here like a CC BY paper all the way down. LicenceFrom says
			// kaggle so that the audit can refuse to publish on the strength of
			// it, and the M2 census replaces these with per version values read
			// off the abs page.
			Licence:     licence,
			LicenceFrom: s.Source,
		})
	}

	return metadata.Record{
		ID:         row.ID,
		Title:      row.Title,
		Abstract:   row.Abstract,
		Authors:    parsedAuthors(row.AuthorsParsed),
		Categories: strings.Fields(row.Categories),
		Versions:   versions,
		DOI:        row.DOI,
		JournalRef: row.JournalRef,
		ReportNo:   row.ReportNo,
		Comments:   row.Comments,
		Source:     s.Source,
		Harvested:  s.today(),
	}.Normalise(), nil
}

// parsedAuthors reads authors_parsed, which is a list of triples.
//
// The triple is surname, forenames, suffix, in that order, and the plain
// authors string next to it is ignored. Splitting "Q. P. Liu and Manuel Manas"
// back into two people is a guess, and Cornell already did the work.
func parsedAuthors(parsed [][]string) []metadata.Author {
	out := make([]metadata.Author, 0, len(parsed))
	for _, a := range parsed {
		var author metadata.Author
		if len(a) > 0 {
			author.Surname = a[0]
		}
		if len(a) > 1 {
			author.Forename = a[1]
		}
		if len(a) > 2 {
			author.Suffix = a[2]
		}
		out = append(out, author)
	}
	return out
}

// snapshotRow is a line of the file.
//
// The hyphenated names are Cornell's and they are what is in the file. Several
// fields are null rather than absent for most of the corpus, which encoding/json
// reads into the zero string, and that is the behaviour wanted here.
type snapshotRow struct {
	ID         string `json:"id"`
	Submitter  string `json:"submitter"`
	Authors    string `json:"authors"`
	Title      string `json:"title"`
	Comments   string `json:"comments"`
	JournalRef string `json:"journal-ref"`
	DOI        string `json:"doi"`
	ReportNo   string `json:"report-no"`
	Categories string `json:"categories"`
	Licence    string `json:"license"`
	Abstract   string `json:"abstract"`
	// UpdateDate is read and not used. It is when arXiv last touched the record
	// and the versions already say when the paper changed, so nothing here has
	// a use for it. It is a plain date in the Kaggle file and a timestamp in
	// the Hugging Face rows API, which is the only field where the two surfaces
	// disagree about a shape.
	UpdateDate string `json:"update_date"`
	// There is no msc_class and no acm_class in this file, which OAI-PMH does
	// carry. It is a small gap and the catch up closes it for anything recent,
	// but a paper from 1998 that nobody has touched since will have no MSC
	// class in the plane until something asks arXiv for it directly.
	AuthorsParsed [][]string `json:"authors_parsed"`
	Versions      []struct {
		Version string `json:"version"`
		Created string `json:"created"`
	} `json:"versions"`
}

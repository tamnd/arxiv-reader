// Package metadata is the metadata plane: one record per paper, every paper on
// arXiv, sharded by the month the id encodes.
//
// Nothing in this package asks the licence gate anything, and that is the point
// of having two planes rather than one. arXiv publishes its metadata under CC0,
// so this plane can cover all 3.17 million papers while the content plane
// covers the fraction whose own licence permits republishing.
//
// Nothing here talks to the network either. The harvesters produce Records and
// this package decides what a Record is, what a well formed one looks like, and
// how a month of them is written to and read from disk.
package metadata

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
)

// Source is the surface a record or a licence came from.
//
// It is kept because the surfaces disagree about what they can tell us, and the
// disagreement matters later. Every bulk surface has one licence field per
// paper, which is the latest version's, and only the abs page has one per
// version. A corpus that forgets which of the two it read is a corpus that
// cannot answer the only question the licence gate asks.
type Source string

const (
	// SourceKaggle is the Cornell snapshot on Kaggle, which is the bootstrap:
	// one download, every paper, an afternoon.
	SourceKaggle Source = "kaggle"
	// SourceHF is the same data on Hugging Face, which is the fallback when
	// Kaggle is unreachable or behind a login it will not give up.
	SourceHF Source = "hf"
	// SourceOAI is arXiv's own OAI-PMH endpoint, which is the catch up and
	// thereafter.
	//
	// It was written here that this was the one surface carrying a per version
	// licence. That was wrong and it was checked on 2026-09-14: arXivRaw
	// returns exactly one license element, a sibling of title, and its version
	// elements carry only a date and a size. It is a paper level licence like
	// every other bulk surface, and it is the latest version's.
	SourceOAI Source = "oai"
	// SourceAbs is the abs page on the website, which is the only surface that
	// states a licence for a version rather than for a paper.
	//
	// It is read one page at a time at arXiv's pace for the website, so it is
	// what runs when a paper is selected and never over the corpus.
	SourceAbs Source = "abs"
)

// Sources is every surface a whole record can be read from, in the order a
// harvest would use them.
//
// The abs page is not among them. It states a licence for one version and
// nothing else about the paper, so a record claiming to have been harvested
// from it is a record claiming something that cannot have happened.
var Sources = []Source{SourceKaggle, SourceHF, SourceOAI}

// Authorities is every surface that can be named as the authority for a
// licence, which is the three above and the abs page.
var Authorities = []Source{SourceKaggle, SourceHF, SourceOAI, SourceAbs}

// Valid reports whether s is a surface a whole record is read from.
func (s Source) Valid() bool {
	for _, known := range Sources {
		if s == known {
			return true
		}
	}
	return false
}

// ValidAuthority reports whether s can be named as the authority for a licence.
func (s Source) ValidAuthority() bool {
	for _, known := range Authorities {
		if s == known {
			return true
		}
	}
	return false
}

// Author is one name, split the way arXiv splits it.
//
// Not a single string. Reference resolution in M3 matches on surnames, and
// splitting "Balázs, C." back out of a joined string is a guess that gets
// Spanish and Vietnamese names wrong in opposite directions.
type Author struct {
	Surname  string `json:"surname"`
	Forename string `json:"forename,omitempty"`
	Suffix   string `json:"suffix,omitempty"`
}

// String writes the name the way a bibliography would.
func (a Author) String() string {
	parts := make([]string, 0, 3)
	if a.Forename != "" {
		parts = append(parts, a.Forename)
	}
	if a.Surname != "" {
		parts = append(parts, a.Surname)
	}
	if a.Suffix != "" {
		parts = append(parts, a.Suffix)
	}
	return strings.Join(parts, " ")
}

// Version is one version of a paper.
//
// A licence belongs here and not on the Record, because it belongs to a
// version. A v1 under CC BY and a v2 under arXiv's default licence are two
// different decisions, and a corpus that resolves to the latest and assumes is
// a corpus that will republish something it may not.
type Version struct {
	// Version is the number, so 1 for v1. Never the string.
	Version int `json:"version"`
	// Created is when that version was announced, in UTC.
	Created time.Time `json:"created"`
	// Licence is empty until the census in M2 fills it in.
	//
	// Empty is not the same as corpus.LicenceUnknown. Empty means nobody has
	// looked yet. Unknown means somebody looked and arXiv did not say, which is
	// treated as the default licence and is therefore a decision that has been
	// made. Conflating the two is how a corpus ends up publishing on the
	// strength of a field nobody ever filled in.
	Licence corpus.Licence `json:"licence,omitempty"`
	// LicenceFrom is the surface the licence was read from, empty when Licence
	// is. Audit rule S10 fails a content plane paper whose licence came from
	// the Kaggle snapshot, because that snapshot has one licence for the whole
	// paper and this project needs one for the version it extracted.
	LicenceFrom Source `json:"licence_from,omitempty"`
}

// Record is one paper in the metadata plane.
//
// Field order here is the field order on the wire, because encoding/json writes
// struct fields in declaration order and this plane is committed to git.
// Reordering the struct would rewrite three million lines and produce a diff
// nobody can read for a change that means nothing.
type Record struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Abstract   string    `json:"abstract"`
	Authors    []Author  `json:"authors"`
	Categories []string  `json:"categories"`
	Versions   []Version `json:"versions"`
	DOI        string    `json:"doi,omitempty"`
	JournalRef string    `json:"journal_ref,omitempty"`
	ReportNo   string    `json:"report_no,omitempty"`
	Comments   string    `json:"comments,omitempty"`
	MSCClass   string    `json:"msc_class,omitempty"`
	ACMClass   string    `json:"acm_class,omitempty"`
	// Source is the surface this record was last written from.
	Source Source `json:"source"`
	// Harvested is the date it was last written, as YYYY-MM-DD.
	//
	// A date and not a timestamp. The second a harvest ran is not information,
	// and a timestamp would make every re-harvest a diff on every line.
	Harvested string `json:"harvested"`
}

// Primary is the category a paper was submitted to, which is the first one.
//
// arXiv's category order is significant and this is why the Categories field is
// never sorted. A moderator can cross list a paper into five more archives
// years later and the primary stays where it was.
func (r Record) Primary() string {
	if len(r.Categories) == 0 {
		return ""
	}
	return r.Categories[0]
}

// Archive is the primary category's archive, so cs from cs.CL and math from
// math.FA. Old style archives that carry a hyphen, like cond-mat, come back
// whole.
func (r Record) Archive() string {
	primary := r.Primary()
	if i := strings.IndexByte(primary, '.'); i >= 0 {
		return primary[:i]
	}
	return primary
}

// Latest is the highest numbered version, and false when there are none.
func (r Record) Latest() (Version, bool) {
	if len(r.Versions) == 0 {
		return Version{}, false
	}
	return r.Versions[len(r.Versions)-1], true
}

// VersionAt returns version n, counting from 1.
func (r Record) VersionAt(n int) (Version, bool) {
	for _, v := range r.Versions {
		if v.Version == n {
			return v, true
		}
	}
	return Version{}, false
}

// Shard is the month file this record belongs in.
func (r Record) Shard() (string, error) {
	id, err := axid.Parse(r.ID)
	if err != nil {
		return "", err
	}
	return corpus.Shard(id), nil
}

// SortKey orders records the way the plane files are written.
//
// It comes from axid rather than from the id string, because sorting
// "hep-th/9711200" and "2106.09685" as text puts the whole of the 1990s after
// the whole of the 2020s.
func (r Record) SortKey() string {
	id, err := axid.Parse(r.ID)
	if err != nil {
		return r.ID
	}
	return id.SortKey()
}

// Normalise cleans a record up into the one form the plane stores.
//
// Every harvester calls this, which is the only reason two surfaces can write
// the same paper and produce the same line. arXiv wraps titles and abstracts at
// about eighty columns with leading spaces on the continuation lines, and three
// surfaces wrap them in three different places.
func (r Record) Normalise() Record {
	r.ID = strings.TrimSpace(r.ID)
	if id, err := axid.Parse(r.ID); err == nil {
		r.ID = id.Canonical
	}
	r.Title = unwrap(r.Title)
	r.Abstract = unwrap(r.Abstract)
	r.DOI = strings.TrimSpace(r.DOI)
	r.JournalRef = unwrap(r.JournalRef)
	r.ReportNo = unwrap(r.ReportNo)
	r.Comments = unwrap(r.Comments)
	r.MSCClass = unwrap(r.MSCClass)
	r.ACMClass = unwrap(r.ACMClass)

	authors := make([]Author, 0, len(r.Authors))
	for _, a := range r.Authors {
		a.Surname = unwrap(a.Surname)
		a.Forename = unwrap(a.Forename)
		a.Suffix = unwrap(a.Suffix)
		if a.Surname == "" && a.Forename == "" {
			continue
		}
		authors = append(authors, a)
	}
	r.Authors = authors

	// Deduplicated but never sorted, because the first one is the primary.
	seen := make(map[string]bool, len(r.Categories))
	cats := make([]string, 0, len(r.Categories))
	for _, c := range r.Categories {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		cats = append(cats, c)
	}
	r.Categories = cats

	versions := append([]Version(nil), r.Versions...)
	for i := range versions {
		versions[i].Created = versions[i].Created.UTC()
	}
	sortVersions(versions)
	r.Versions = versions
	return r
}

// sortVersions puts a version list in the order arXiv announced them.
func sortVersions(versions []Version) {
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Version < versions[j].Version
	})
}

// unwrap turns arXiv's hard wrapped text back into one line.
//
// Any run of whitespace that contains a newline becomes a single space, and
// every other run is left alone. Collapsing all whitespace would eat the
// alignment inside the handful of abstracts that contain a small table, and
// leaving newlines in would put a line break in the middle of a sentence in
// every title in the corpus.
func unwrap(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if !unicode.IsSpace(rune(s[i])) {
			b.WriteByte(s[i])
			i++
			continue
		}
		j, newline := i, false
		for j < len(s) && unicode.IsSpace(rune(s[j])) {
			if s[j] == '\n' || s[j] == '\r' {
				newline = true
			}
			j++
		}
		if newline {
			b.WriteByte(' ')
		} else {
			b.WriteString(s[i:j])
		}
		i = j
	}
	return strings.TrimSpace(b.String())
}

// Validate reports what is wrong with a record, or nil.
//
// This is the cheap structural check that runs on the way in. The audit in
// M1 runs the same check over a committed plane, plus the ones that need to see
// more than one record at a time.
func (r Record) Validate() error {
	if _, err := axid.Parse(r.ID); err != nil {
		return fmt.Errorf("id %q: %w", r.ID, err)
	}
	if strings.TrimSpace(r.Title) == "" {
		return fmt.Errorf("%s: no title", r.ID)
	}
	if len(r.Categories) == 0 {
		return fmt.Errorf("%s: no category, so it cannot be placed in the tree", r.ID)
	}
	if len(r.Versions) == 0 {
		return fmt.Errorf("%s: no versions, and every announced paper has at least v1", r.ID)
	}
	for i, v := range r.Versions {
		// Numbered from one with no gaps. arXiv has no v0 and skips no
		// numbers, so a gap means the harvest dropped a version, and a dropped
		// version is a licence we never read.
		if v.Version != i+1 {
			return fmt.Errorf("%s: versions are %s, which is not a run from v1", r.ID, versionRun(r.Versions))
		}
		if v.Created.IsZero() {
			return fmt.Errorf("%s: v%d has no creation date", r.ID, v.Version)
		}
		if v.Licence == "" && v.LicenceFrom != "" {
			return fmt.Errorf("%s: v%d records where a licence came from and not what it was", r.ID, v.Version)
		}
		if v.Licence != "" && v.LicenceFrom == "" {
			return fmt.Errorf("%s: v%d has a licence and no authority for it", r.ID, v.Version)
		}
		if v.LicenceFrom != "" && !v.LicenceFrom.ValidAuthority() {
			return fmt.Errorf("%s: v%d names %q as the authority for its licence, which is not a surface we read", r.ID, v.Version, v.LicenceFrom)
		}
		if v.Licence != "" {
			if _, err := corpus.ParseLicence(string(v.Licence)); err != nil {
				return fmt.Errorf("%s: v%d: %w", r.ID, v.Version, err)
			}
		}
	}
	if !r.Source.Valid() {
		return fmt.Errorf("%s: source %q is not a surface we read", r.ID, r.Source)
	}
	if _, err := time.Parse("2006-01-02", r.Harvested); err != nil {
		return fmt.Errorf("%s: harvested %q is not a YYYY-MM-DD date", r.ID, r.Harvested)
	}
	return nil
}

func versionRun(versions []Version) string {
	out := make([]string, len(versions))
	for i, v := range versions {
		out[i] = fmt.Sprintf("v%d", v.Version)
	}
	return strings.Join(out, " ")
}

// Sort orders records the way a plane file is written.
func Sort(recs []Record) {
	sort.SliceStable(recs, func(i, j int) bool {
		return recs[i].SortKey() < recs[j].SortKey()
	})
}

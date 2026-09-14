package licence

import (
	"strings"
	"testing"
	"time"

	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
)

// paper is one record with the licence its latest version carries.
//
// The licence goes on every version, because that is what the harvest writes
// when a bulk surface hands it one licence for the whole paper, and the census
// has to be counted over exactly what is on disk rather than over a tidier
// record nobody has.
func paper(id string, categories []string, versions int, l corpus.Licence, from metadata.Source) metadata.Record {
	r := metadata.Record{
		ID:         id,
		Title:      "A paper",
		Abstract:   "An abstract.",
		Authors:    []metadata.Author{{Surname: "Noether", Forename: "Emmy"}},
		Categories: categories,
		Source:     from,
		Harvested:  "2026-09-14",
	}
	for i := 1; i <= versions; i++ {
		r.Versions = append(r.Versions, metadata.Version{
			Version:     i,
			Created:     time.Date(2021, 6, i, 0, 0, 0, 0, time.UTC),
			Licence:     l,
			LicenceFrom: from,
		})
	}
	return r
}

func kaggle(id string, versions int, l corpus.Licence, categories ...string) metadata.Record {
	return paper(id, categories, versions, l, metadata.SourceKaggle)
}

// count runs the census over records filed in one month.
func count(t *testing.T, shard string, recs ...metadata.Record) Census {
	t.Helper()
	c := NewCounter()
	for _, r := range recs {
		if err := c.Add(shard, r); err != nil {
			t.Fatal(err)
		}
	}
	return c.Census()
}

func row(t *testing.T, c Census, l corpus.Licence) Row {
	t.Helper()
	for _, r := range c.Licences {
		if r.Licence == l {
			return r
		}
	}
	t.Fatalf("the census has no row for %q", l)
	return Row{}
}

func TestCountsByLicence(t *testing.T) {
	c := count(t, "2106",
		kaggle("2106.00001", 1, corpus.LicenceCCBY, "cs.CL"),
		kaggle("2106.00002", 1, corpus.LicenceCCBY, "cs.CL"),
		kaggle("2106.00003", 1, corpus.LicenceArXiv, "math.FA"),
	)
	if c.Records != 3 {
		t.Errorf("counted %d records, want 3", c.Records)
	}
	if c.Months != 1 {
		t.Errorf("counted %d months, want 1", c.Months)
	}
	if got := row(t, c, corpus.LicenceCCBY); got.Records != 2 {
		t.Errorf("cc-by has %d, want 2", got.Records)
	}
	if got := row(t, c, corpus.LicenceArXiv); got.Records != 1 {
		t.Errorf("arxiv-1.0 has %d, want 1", got.Records)
	}
	if c.Translatable() != 2 {
		t.Errorf("%d translatable, want the two cc-by", c.Translatable())
	}
	if c.Republishable() != 2 {
		t.Errorf("%d republishable, want the two cc-by", c.Republishable())
	}
}

// Every licence gets a row whether or not the corpus holds any, because a
// corpus with no CC0 in it is a fact and a missing row reads as an oversight.
func TestEveryLicenceGetsARow(t *testing.T) {
	c := count(t, "2106", kaggle("2106.00001", 1, corpus.LicenceCCBY, "cs.CL"))
	if len(c.Licences) != len(corpus.Licences)+1 {
		t.Fatalf("%d rows, want one per licence plus the unresolved one", len(c.Licences))
	}
	if got := row(t, c, corpus.LicenceCC0); got.Records != 0 {
		t.Errorf("cc0 has %d records in a corpus holding none", got.Records)
	}
	if last := c.Licences[len(c.Licences)-1]; last.Label() != "not recorded" {
		t.Errorf("the last row is %q, want the unresolved one", last.Label())
	}
}

// A paper nobody has looked at is not a paper resolved to unknown, and the
// census keeps the two apart for the same reason the audit has four states.
func TestAPaperWithNoLicenceIsNotUnknown(t *testing.T) {
	c := count(t, "2106",
		kaggle("2106.00001", 1, corpus.LicenceUnknown, "cs.CL"),
		paper("2106.00002", []string{"cs.CL"}, 1, "", ""),
	)
	if got := row(t, c, corpus.LicenceUnknown); got.Records != 1 {
		t.Errorf("unknown has %d, want the one paper that was looked at", got.Records)
	}
	if got := row(t, c, ""); got.Records != 1 {
		t.Errorf("not recorded has %d, want the one paper nobody looked at", got.Records)
	}
	// Both are record access, so neither is translatable, and that is the
	// point: keeping them apart costs nothing and telling them apart is what
	// says whether a harvest still has work to do.
	if c.Translatable() != 0 {
		t.Errorf("%d translatable, want none", c.Translatable())
	}
	if len(c.Authorities) != 1 {
		t.Errorf("%d authorities, want only the one that said something", len(c.Authorities))
	}
}

// A record with no versions at all has no latest version to read a licence off,
// and it is counted rather than dropped.
func TestARecordWithNoVersionsIsCounted(t *testing.T) {
	c := count(t, "2106", paper("2106.00001", []string{"cs.CL"}, 0, "", ""))
	if c.Records != 1 {
		t.Errorf("counted %d, want 1", c.Records)
	}
	if got := row(t, c, ""); got.Records != 1 {
		t.Errorf("not recorded has %d, want 1", got.Records)
	}
	if c.MultiVersion != 0 {
		t.Errorf("%d multi-version, want none", c.MultiVersion)
	}
}

func TestAccessClasses(t *testing.T) {
	c := count(t, "2106",
		kaggle("2106.00001", 1, corpus.LicenceCC0, "cs.CL"),
		kaggle("2106.00002", 1, corpus.LicenceCCBYSA, "cs.CL"),
		kaggle("2106.00003", 1, corpus.LicenceCCBYNCND, "cs.CL"),
		kaggle("2106.00004", 1, corpus.LicenceArXiv, "cs.CL"),
	)
	want := map[corpus.Access]int{
		corpus.AccessOpen:       1,
		corpus.AccessShareAlike: 1,
		corpus.AccessVerbatim:   1,
		corpus.AccessRecord:     1,
	}
	for _, r := range c.Access {
		if r.Records != want[r.Access] {
			t.Errorf("%s has %d, want %d", r.Access, r.Records, want[r.Access])
		}
	}
	// The no-derivatives licence is the one that separates the two totals, and
	// a census that reports the same number twice has lost the distinction the
	// whole gate is built on.
	if c.Republishable() != 3 {
		t.Errorf("%d republishable, want the three that are not arxiv-1.0", c.Republishable())
	}
	if c.Translatable() != 2 {
		t.Errorf("%d translatable, want the two that permit derivatives", c.Translatable())
	}
}

func TestVersionDistribution(t *testing.T) {
	c := count(t, "2106",
		kaggle("2106.00001", 1, corpus.LicenceCCBY, "cs.CL"),
		kaggle("2106.00002", 2, corpus.LicenceCCBY, "cs.CL"),
		kaggle("2106.00003", 2, corpus.LicenceCCBY, "cs.CL"),
		kaggle("2106.00004", 9, corpus.LicenceCCBY, "cs.CL"),
	)
	if c.MultiVersion != 3 {
		t.Errorf("%d multi-version, want 3", c.MultiVersion)
	}
	want := map[int]int{1: 1, 2: 2, 3: 0, 4: 0, VersionCap: 1}
	for _, v := range c.Versions {
		if v.Records != want[v.Versions] {
			t.Errorf("%s versions has %d, want %d", v.Label(), v.Records, want[v.Versions])
		}
	}
	if last := c.Versions[len(c.Versions)-1]; last.Label() != "5 or more" {
		t.Errorf("the last bar is %q", last.Label())
	}
}

// The archive is the primary category's, so a paper cross listed into four
// archives counts once and in the one its author chose.
func TestByArchive(t *testing.T) {
	c := count(t, "2106",
		kaggle("2106.00001", 1, corpus.LicenceCCBY, "cs.CL", "math.FA", "stat.ML"),
		kaggle("2106.00002", 1, corpus.LicenceArXiv, "cs.LG"),
		kaggle("2106.00003", 1, corpus.LicenceCCBY, "math.FA"),
		kaggle("2106.00004", 1, corpus.LicenceCCBY, "cond-mat.str-el"),
	)
	got := map[string]ArchiveRow{}
	for _, a := range c.Archives {
		got[a.Archive] = a
	}
	if got["cs"].Records != 2 {
		t.Errorf("cs has %d, want 2", got["cs"].Records)
	}
	if got["cs"].Translatable != 1 {
		t.Errorf("cs has %d translatable, want 1", got["cs"].Translatable)
	}
	if got["math"].Records != 1 {
		t.Errorf("math has %d, want 1 rather than the cross listing as well", got["math"].Records)
	}
	// An old style archive carries a hyphen and does not split on the dot the
	// way cs.CL does.
	if got["cond-mat"].Records != 1 {
		t.Errorf("cond-mat has %d, want 1", got["cond-mat"].Records)
	}
	// Largest first, so the table opens with the archive the corpus is mostly
	// made of rather than with whichever one sorts first.
	if c.Archives[0].Archive != "cs" {
		t.Errorf("the table opens with %q, want cs", c.Archives[0].Archive)
	}
}

// The year is the shard's, so the year the paper was announced. A paper held in
// moderation over a new year is announced in the year its identifier names and
// not the year it was submitted, and the plane files it the same way.
func TestByYearFollowsTheShardAndNotTheVersionDate(t *testing.T) {
	c := NewCounter()
	rec := kaggle("2201.00001", 1, corpus.LicenceCCBY, "cs.CL")
	rec.Versions[0].Created = time.Date(2021, 12, 30, 0, 0, 0, 0, time.UTC)
	if err := c.Add("2201", rec); err != nil {
		t.Fatal(err)
	}
	if err := c.Add("9108", kaggle("hep-th/9108001", 1, corpus.LicenceArXiv, "hep-th")); err != nil {
		t.Fatal(err)
	}
	got := c.Census()
	if len(got.Years) != 2 {
		t.Fatalf("%d years, want 2", len(got.Years))
	}
	// Oldest first, and 1991 comes before 2022 rather than after it the way a
	// text sort of the shard names would have it.
	if got.Years[0].Year != 1991 || got.Years[1].Year != 2022 {
		t.Errorf("the years are %d and %d, want 1991 then 2022", got.Years[0].Year, got.Years[1].Year)
	}
	if got.Years[1].Translatable != 1 {
		t.Errorf("2022 has %d translatable, want 1", got.Years[1].Translatable)
	}
	if got.Years[0].Translatable != 0 {
		t.Errorf("1991 has %d translatable, want none", got.Years[0].Translatable)
	}
}

func TestAddRefusesAShardThatIsNotOne(t *testing.T) {
	c := NewCounter()
	err := c.Add("2113", kaggle("2106.00001", 1, corpus.LicenceCCBY, "cs.CL"))
	if err == nil {
		t.Fatal("month thirteen was accepted")
	}
	if !strings.Contains(err.Error(), "2113") {
		t.Errorf("said %q, which does not name the shard", err)
	}
}

func TestShares(t *testing.T) {
	var recs []metadata.Record
	for i := 0; i < 4; i++ {
		l := corpus.LicenceArXiv
		if i == 0 {
			l = corpus.LicenceCCBY
		}
		recs = append(recs, kaggle("2106.0000"+string(rune('1'+i)), 1, l, "cs.CL"))
	}
	c := count(t, "2106", recs...)
	if got := row(t, c, corpus.LicenceCCBY); got.Share != 0.25 {
		t.Errorf("cc-by is %v of the corpus, want a quarter", got.Share)
	}
	if got := c.Years[0].Share(); got != 0.25 {
		t.Errorf("2021 is %v translatable, want a quarter", got)
	}
}

func TestMarkdownSaysItIsAnUpperBoundBeforeAnythingElse(t *testing.T) {
	c := count(t, "2106",
		kaggle("2106.00001", 1, corpus.LicenceCCBY, "cs.CL"),
		kaggle("2106.00002", 3, corpus.LicenceArXiv, "cs.CL"),
	)
	c.Generated = time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	md := c.Markdown()

	// The headline is the number people will quote, so the words that qualify
	// it have to be in the same sentence and not in a footnote.
	if !strings.Contains(md, "Of 2 papers counted, 1 may be translated and 1 may be republished in English") {
		t.Errorf("the headline reads wrong:\n%s", md)
	}
	if !strings.Contains(md, "upper bounds") {
		t.Error("the headline does not say the numbers are upper bounds")
	}
	if !strings.Contains(md, "## Why this is an upper bound") {
		t.Error("the report does not explain itself")
	}
	// The measurement is the argument, so the report carries the numbers rather
	// than asserting the conclusion.
	for _, want := range []string{"forty multi-version papers", "five whose v1 licence differs", "ax licence resolve"} {
		if !strings.Contains(md, want) {
			t.Errorf("the explanation does not mention %q", want)
		}
	}
	if !strings.Contains(md, "1 paper counted here carries more than one version, which is 50.0%") {
		t.Errorf("the multi-version share is wrong:\n%s", md)
	}
	for _, heading := range []string{"## By licence", "## By access class", "## Who said so", "## Versions", "## By year", "## By archive"} {
		if !strings.Contains(md, heading) {
			t.Errorf("the report has no %q section", heading)
		}
	}
	if !strings.Contains(md, "| cc-by | 1 | 50.0% | open | yes | yes |") {
		t.Errorf("the cc-by row reads wrong:\n%s", md)
	}
	if !strings.Contains(md, "| arxiv-1.0 | 1 | 50.0% | record | no | no |") {
		t.Errorf("the arxiv-1.0 row reads wrong:\n%s", md)
	}
	if !strings.Contains(md, "| kaggle | 2 |") {
		t.Error("the authority table does not name kaggle")
	}
	if !strings.Contains(md, "Counted on 14 September 2026 over 1 month.") {
		t.Errorf("the date line reads wrong:\n%s", md)
	}
}

// An empty plane has counted nothing, and a headline that reads 0 of 0 papers
// with a percentage beside it is the sentence that lets a report look like it
// found an answer when it had nothing to look at.
func TestAnEmptyCensusSaysSo(t *testing.T) {
	c := count(t, "2106")
	if c.Records != 0 {
		t.Fatalf("counted %d over nothing", c.Records)
	}
	md := c.Markdown()
	if !strings.Contains(md, "Nothing counted, because the metadata plane is empty.") {
		t.Errorf("the headline reads wrong:\n%s", md)
	}
	if strings.Contains(md, "may be translated") {
		t.Error("an empty plane reported a translatable count")
	}
	if strings.Contains(md, "## Who said so") {
		t.Error("an empty plane named an authority")
	}
	// Every share is nothing over nothing, and none of them may print as zero.
	if strings.Contains(md, "0.0%") {
		t.Errorf("an empty plane printed a percentage:\n%s", md)
	}
}

func TestTextIsTheShortForm(t *testing.T) {
	c := count(t, "2106",
		kaggle("2106.00001", 1, corpus.LicenceCCBY, "cs.CL"),
		kaggle("2106.00002", 1, corpus.LicenceArXiv, "cs.CL"),
	)
	got := c.Text()
	if !strings.Contains(got, "Of 2 papers counted, 1 may be translated and 1 may be republished in English") {
		t.Errorf("the summary reads wrong:\n%s", got)
	}
	// The short form leaves out the licences the corpus holds none of, because
	// five empty lines in a terminal is noise where in a committed table it is
	// a fact.
	if strings.Contains(got, "cc0") {
		t.Errorf("the short form printed a licence with nothing in it:\n%s", got)
	}
	if strings.Count(got, "\n") != 3 {
		t.Errorf("the short form is %d lines:\n%s", strings.Count(got, "\n"), got)
	}
}

func TestTakeReadsThePlane(t *testing.T) {
	plane := metadata.Plane{Root: t.TempDir()}
	for _, shard := range []string{"2106", "2201"} {
		if _, err := plane.Write(shard, []metadata.Record{
			kaggle(shard+".00001", 1, corpus.LicenceCCBY, "cs.CL"),
			kaggle(shard+".00002", 2, corpus.LicenceArXiv, "math.FA"),
		}); err != nil {
			t.Fatal(err)
		}
	}
	c, err := Take(plane, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Records != 4 {
		t.Errorf("read %d records, want 4", c.Records)
	}
	if c.Months != 2 {
		t.Errorf("read %d months, want 2", c.Months)
	}
	if c.Translatable() != 2 {
		t.Errorf("%d translatable, want 2", c.Translatable())
	}

	// Naming a month counts that month and no other.
	one, err := Take(plane, []string{"2201"})
	if err != nil {
		t.Fatal(err)
	}
	if one.Records != 2 {
		t.Errorf("one month read %d records, want 2", one.Records)
	}
	if len(one.Years) != 1 || one.Years[0].Year != 2022 {
		t.Errorf("one month covered %v", one.Years)
	}
}

func TestTakeOverAnEmptyPlane(t *testing.T) {
	c, err := Take(metadata.Plane{Root: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Records != 0 || c.Months != 0 {
		t.Errorf("an empty plane counted %d records over %d months", c.Records, c.Months)
	}
}

func TestLatest(t *testing.T) {
	// The latest version's licence is the one that counts, and it is the one
	// the bulk surfaces carry.
	r := kaggle("2106.00001", 3, corpus.LicenceCCBY, "cs.CL")
	r.Versions[0].Licence = corpus.LicenceArXiv
	l, from := Latest(r)
	if l != corpus.LicenceCCBY {
		t.Errorf("read %q, want the latest version's cc-by and not v1's", l)
	}
	if from != metadata.SourceKaggle {
		t.Errorf("read the authority %q", from)
	}

	if l, from := Latest(paper("2106.00002", []string{"cs.CL"}, 0, "", "")); l != "" || from != "" {
		t.Errorf("a record with no versions read %q from %q", l, from)
	}
}

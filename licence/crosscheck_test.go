package licence

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/harvest"
	"github.com/tamnd/arxiv-reader/metadata"
)

// The five labels the Common Pile was seen to use, copied off
// common-pile/arxiv_papers on 2026-09-14 over six offsets of the collection.
// Four carry a deed and one does not, and one of the four is a 3.0 deed rather
// than a 4.0, which is why the mapping reads the prefix and not the whole URL.
const (
	labelBY   = "Creative Commons - Attribution - https://creativecommons.org/licenses/by/4.0/"
	labelBY3  = "Creative Commons - Attribution - https://creativecommons.org/licenses/by/3.0/"
	labelBYSA = "Creative Commons - Attribution Share-Alike - https://creativecommons.org/licenses/by-sa/4.0/"
	labelCC0  = "Creative Commons Zero - Public Domain - https://creativecommons.org/publicdomain/zero/1.0/"
	labelPD   = "Public Domain"
)

func TestPileLicenceReadsEveryLabelSeenInTheWild(t *testing.T) {
	for _, c := range []struct {
		label string
		want  corpus.Licence
	}{
		{labelBY, corpus.LicenceCCBY},
		{labelBY3, corpus.LicenceCCBY},
		{labelBYSA, corpus.LicenceCCBYSA},
		{labelCC0, corpus.LicenceCC0},
		{labelPD, corpus.LicenceCC0},
	} {
		got, err := PileLicence(c.label)
		if err != nil {
			t.Fatalf("%q: %v", c.label, err)
		}
		if got != c.want {
			t.Errorf("%q read as %q, want %q", c.label, got, c.want)
		}
	}
}

func TestPileLicenceRefusesALabelItDoesNotKnow(t *testing.T) {
	for _, label := range []string{
		"",
		"   ",
		"MIT",
		"Creative Commons - Attribution - https://example.com/nothing",
	} {
		if l, err := PileLicence(label); err == nil {
			t.Errorf("%q read as %q, want an error", label, l)
		}
	}
}

// A label this package cannot map has to fail rather than come back unknown.
// Unknown is record access, record access disagrees with every open licence,
// and one unmapped label would then fill the report with disagreements that are
// this package failing to read.
func TestAnUnmappedLabelIsNotUnknown(t *testing.T) {
	l, err := PileLicence("Some Licence We Have Never Seen")
	if err == nil {
		t.Fatal("want an error")
	}
	if l == corpus.LicenceUnknown {
		t.Fatalf("an unmapped label came back as %q, which would disagree with everything", l)
	}
}

// theirs is one of their rows as the test server serves it.
type theirs struct {
	id        string
	label     string
	truncated bool
}

// pileServe stands in for the datasets server, serving their row shape and
// honouring offset and length the way the real one does.
func pileServe(t *testing.T, rows []theirs) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		offset, _ := strconv.Atoi(q.Get("offset"))
		length, _ := strconv.Atoi(q.Get("length"))
		if offset > len(rows) {
			offset = len(rows)
		}
		end := offset + length
		if end > len(rows) {
			end = len(rows)
		}
		var out struct {
			Rows []map[string]any `json:"rows"`
			// The real server states the whole collection on every page.
			Total int `json:"num_rows_total"`
		}
		out.Total = len(rows)
		for i, r := range rows[offset:end] {
			cell := map[string]any{
				"id":     r.id,
				"text":   "The whole paper arrives whether or not it is asked for.",
				"source": "arxiv",
				"metadata": map[string]any{
					"license": r.label,
					"url":     "https://arxiv.org/abs/" + r.id,
				},
			}
			cut := []string{}
			if r.truncated {
				cut = append(cut, "text")
			}
			out.Rows = append(out.Rows, map[string]any{
				"row_idx":         offset + i,
				"row":             cell,
				"truncated_cells": cut,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(out); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// reader points a Reader at the test server with no pace, because there is no
// server here to be kind to.
func reader(t *testing.T, rows []theirs) *Reader {
	t.Helper()
	return &Reader{Rows: &harvest.Rows{
		Endpoint: pileServe(t, rows),
		Dataset:  Pile,
		Pace:     1,
	}}
}

func walk(t *testing.T, rows []theirs, offset, limit int) ([]Claim, Reading) {
	t.Helper()
	var claims []Claim
	reading, err := reader(t, rows).Walk(context.Background(), offset, limit, func(c Claim) error {
		claims = append(claims, c)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return claims, reading
}

func TestWalkReadsClaims(t *testing.T) {
	claims, reading := walk(t, []theirs{
		{id: "0704.3395", label: labelPD},
		{id: "0705.1329", label: labelBYSA},
		{id: "2106.09685", label: labelBY},
	}, 0, 3)

	if len(claims) != 3 {
		t.Fatalf("read %d claims, want 3", len(claims))
	}
	if claims[0].ID != "0704.3395" || claims[0].Licence != corpus.LicenceCC0 {
		t.Errorf("first claim is %+v", claims[0])
	}
	if claims[0].Label != labelPD {
		t.Errorf("the claim did not keep what they wrote, it kept %q", claims[0].Label)
	}
	if claims[2].Access() != corpus.AccessOpen {
		t.Errorf("cc-by read as %s access", claims[2].Access())
	}
	if reading.Rows != 3 || reading.Total != 3 {
		t.Errorf("reading is %+v, want 3 of 3", reading)
	}
}

// Their rows are bigger than a page, so a walk longer than PileRows is the
// ordinary case and the paging is the part most likely to be wrong.
func TestWalkPages(t *testing.T) {
	var rows []theirs
	for i := 0; i < PileRows*2+3; i++ {
		rows = append(rows, theirs{id: fmt.Sprintf("2106.%05d", i), label: labelBY})
	}
	claims, reading := walk(t, rows, 0, len(rows))
	if len(claims) != len(rows) {
		t.Fatalf("read %d claims over %d rows", len(claims), len(rows))
	}
	if reading.Rows != len(rows) {
		t.Errorf("read %d rows, want %d", reading.Rows, len(rows))
	}
	if claims[len(claims)-1].ID != rows[len(rows)-1].id {
		t.Errorf("the last claim is %s, want %s", claims[len(claims)-1].ID, rows[len(rows)-1].id)
	}
}

func TestWalkStopsAtTheLimit(t *testing.T) {
	var rows []theirs
	for i := 0; i < 40; i++ {
		rows = append(rows, theirs{id: fmt.Sprintf("2106.%05d", i), label: labelBY})
	}
	claims, reading := walk(t, rows, 0, 12)
	if len(claims) != 12 || reading.Rows != 12 {
		t.Fatalf("read %d claims over %d rows, want 12", len(claims), reading.Rows)
	}
}

func TestWalkStartsAtTheOffset(t *testing.T) {
	rows := []theirs{
		{id: "0704.3395", label: labelPD},
		{id: "0705.1329", label: labelBYSA},
		{id: "2106.09685", label: labelBY},
	}
	claims, _ := walk(t, rows, 2, 5)
	if len(claims) != 1 || claims[0].ID != "2106.09685" {
		t.Fatalf("read %+v, want the last row alone", claims)
	}
}

// The end of the collection is a short page, and the walk should not spend a
// request to be told the next one is empty.
func TestWalkStopsAtTheEndOfTheCollection(t *testing.T) {
	rows := []theirs{{id: "0704.3395", label: labelPD}}
	claims, reading := walk(t, rows, 0, 1000)
	if len(claims) != 1 {
		t.Fatalf("read %d claims, want 1", len(claims))
	}
	if reading.Rows != 1 {
		t.Errorf("read %d rows, want 1", reading.Rows)
	}
}

// A shortened cell is the one thing here that could put a wrong licence in the
// report quietly, so a shortened row is skipped and counted.
func TestWalkSkipsATruncatedRow(t *testing.T) {
	claims, reading := walk(t, []theirs{
		{id: "0704.3395", label: labelPD, truncated: true},
		{id: "0705.1329", label: labelBYSA},
	}, 0, 2)
	if len(claims) != 1 || claims[0].ID != "0705.1329" {
		t.Fatalf("read %+v, want the second row alone", claims)
	}
	if reading.Truncated != 1 {
		t.Errorf("counted %d truncated rows, want 1", reading.Truncated)
	}
}

// A label nobody has mapped stops that row and not the walk, and it is written
// down so the report can say the reading was short rather than say the corpora
// agreed.
func TestWalkRecordsALabelItCannotRead(t *testing.T) {
	claims, reading := walk(t, []theirs{
		{id: "0704.3395", label: "Some Licence We Have Never Seen"},
		{id: "0705.1329", label: labelBYSA},
	}, 0, 2)
	if len(claims) != 1 {
		t.Fatalf("read %d claims, want 1", len(claims))
	}
	if len(reading.Unreadable) != 1 {
		t.Fatalf("wrote down %d unreadable rows, want 1", len(reading.Unreadable))
	}
	if reading.Unreadable[0].ID != "0704.3395" || reading.Unreadable[0].Label == "" {
		t.Errorf("the unreadable row is %+v, and it should quote them", reading.Unreadable[0])
	}
}

func TestWalkRefusesTheArgumentsItShouldRefuse(t *testing.T) {
	r := reader(t, []theirs{{id: "0704.3395", label: labelPD}})
	nothing := func(Claim) error { return nil }
	if _, err := r.Walk(context.Background(), -1, 10, nothing); err == nil {
		t.Error("a negative offset was accepted")
	}
	if _, err := r.Walk(context.Background(), 0, 0, nothing); err == nil {
		t.Error("a walk with no limit was accepted, and the whole collection is about six hours")
	}
}

// plane writes records into a temporary corpus, one month at a time.
func plane(t *testing.T, shard string, recs ...metadata.Record) metadata.Plane {
	t.Helper()
	p := metadata.Plane{Root: t.TempDir()}
	if _, err := p.Write(shard, recs); err != nil {
		t.Fatal(err)
	}
	return p
}

func claim(id string, label string) Claim {
	l, err := PileLicence(label)
	if err != nil {
		panic(err)
	}
	return Claim{ID: id, Label: label, Licence: l}
}

func TestCompareAgreesWhenBothSidesReadTheSameLicence(t *testing.T) {
	p := plane(t, "2106", kaggle("2106.00001", 1, corpus.LicenceCCBY, "cs.CL"))
	got, err := Compare(p, []Claim{claim("2106.00001", labelBY)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Held != 1 || got.Agreed != 1 || len(got.Disagreements) != 0 {
		t.Fatalf("crosscheck is %+v, want one agreement", got)
	}
}

func TestCompareReportsADisagreement(t *testing.T) {
	p := plane(t, "2106", kaggle("2106.00001", 1, corpus.LicenceArXiv, "cs.CL"))
	got, err := Compare(p, []Claim{claim("2106.00001", labelBY)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Disagreements) != 1 {
		t.Fatalf("found %d disagreements, want 1", len(got.Disagreements))
	}
	d := got.Disagreements[0]
	if d.Ours != corpus.LicenceArXiv || d.Theirs != corpus.LicenceCCBY {
		t.Errorf("the disagreement is %+v", d)
	}
	if d.From != metadata.SourceKaggle {
		t.Errorf("the disagreement did not say who told us, it said %q", d.From)
	}
	if d.Label != labelBY {
		t.Errorf("the disagreement did not quote them, it quoted %q", d.Label)
	}
}

// The disagreement that costs something is the one where they may republish the
// English and we may not, because they already have.
func TestWiderIsTheDirectionThatCostsSomething(t *testing.T) {
	p := plane(t, "2106",
		kaggle("2106.00001", 1, corpus.LicenceArXiv, "cs.CL"),
		kaggle("2106.00002", 1, corpus.LicenceCCBY, "cs.CL"),
	)
	got, err := Compare(p, []Claim{
		// They say share alike, we say attribution, and both permit the text.
		claim("2106.00002", labelBYSA),
		// They say attribution, we say the default arXiv licence, which permits
		// nothing but a record.
		claim("2106.00001", labelBY),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Disagreements) != 2 {
		t.Fatalf("found %d disagreements, want 2", len(got.Disagreements))
	}
	if got.Wider() != 1 {
		t.Fatalf("counted %d that cost something, want 1", got.Wider())
	}
	// The one that costs something is printed first, because a report nobody
	// scrolls to the end of is a report that hid its conclusion.
	if got.Disagreements[0].ID != "2106.00001" {
		t.Errorf("the first disagreement is %s, want the one that costs something", got.Disagreements[0].ID)
	}
}

// A paper they hold and we do not is a gap in the harvest, and calling it a
// disagreement would blame the licence for a missing record.
func TestAPaperWeDoNotHoldIsAbsentAndNotADisagreement(t *testing.T) {
	p := plane(t, "2106", kaggle("2106.00001", 1, corpus.LicenceCCBY, "cs.CL"))
	got, err := Compare(p, []Claim{claim("2106.00002", labelBY)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Held != 0 || len(got.Disagreements) != 0 {
		t.Fatalf("crosscheck is %+v, want nothing compared", got)
	}
	if len(got.Absent) != 1 || got.Absent[0] != "2106.00002" {
		t.Fatalf("absent is %v, want the one paper", got.Absent)
	}
}

// A month the harvest has not reached yet is not an error. Every claim in it is
// absent, which is the same answer as a month that is there and short.
func TestAMonthThePlaneDoesNotHoldIsAbsentAndNotAnError(t *testing.T) {
	p := plane(t, "2106", kaggle("2106.00001", 1, corpus.LicenceCCBY, "cs.CL"))
	got, err := Compare(p, []Claim{claim("0704.3395", labelPD)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Absent) != 1 || got.Absent[0] != "0704.3395" {
		t.Fatalf("absent is %v, want the paper from the month we do not hold", got.Absent)
	}
}

// A record with no licence at all is a paper nobody has looked at, which is not
// a disagreement and is not an agreement either.
func TestAPaperWeHoldWithNoLicenceIsUnresolved(t *testing.T) {
	p := plane(t, "2106", kaggle("2106.00001", 1, "", "cs.CL"))
	got, err := Compare(p, []Claim{claim("2106.00001", labelBY)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Held != 1 || got.Agreed != 0 || len(got.Disagreements) != 0 {
		t.Fatalf("crosscheck is %+v, want nothing to compare", got)
	}
	if len(got.Unresolved) != 1 {
		t.Fatalf("unresolved is %v, want the one paper", got.Unresolved)
	}
}

func TestCompareRefusesAnIdentifierThatIsNotOne(t *testing.T) {
	p := plane(t, "2106", kaggle("2106.00001", 1, corpus.LicenceCCBY, "cs.CL"))
	if _, err := Compare(p, []Claim{{ID: "not-an-arxiv-id", Licence: corpus.LicenceCCBY}}); err == nil {
		t.Fatal("an identifier that is not one was accepted")
	}
}

func TestCompareOverNoClaims(t *testing.T) {
	p := plane(t, "2106", kaggle("2106.00001", 1, corpus.LicenceCCBY, "cs.CL"))
	got, err := Compare(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Claims != 0 || got.Held != 0 {
		t.Fatalf("crosscheck is %+v, want nothing", got)
	}
}

// The report has to say what it is comparing before it says what it found,
// because two readings of the same field agreeing is weak evidence and a reader
// who takes the agreement as proof has been misled by the report.
func TestMarkdownSaysWhatItComparesBeforeItSaysWhatItFound(t *testing.T) {
	p := plane(t, "2106", kaggle("2106.00001", 1, corpus.LicenceArXiv, "cs.CL"))
	got, err := Compare(p, []Claim{claim("2106.00001", labelBY)})
	if err != nil {
		t.Fatal(err)
	}
	md := got.Markdown()
	what := strings.Index(md, "## What this compares")
	count := strings.Index(md, "## The count")
	if what < 0 || count < 0 || what > count {
		t.Fatalf("the report explains itself at %d and counts at %d", what, count)
	}
	for _, want := range []string{
		"carry no version",
		"weak evidence",
		"identifier order",
		"2106.00001",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the report does not mention %q", want)
		}
	}
}

func TestAnEmptyCrosscheckSaysSo(t *testing.T) {
	var c Crosscheck
	md := c.Markdown()
	if !strings.Contains(md, "Nothing compared") {
		t.Fatalf("a crosscheck over nothing says:\n%s", md)
	}
	// Nothing over nothing is not zero percent and it is not agreement either.
	if strings.Contains(md, "all agree") {
		t.Error("a crosscheck over nothing claimed agreement")
	}
}

// Holding none of what they hold is a different answer from agreeing with them
// about all of it, and the headline is where that difference gets lost.
func TestHoldingNoneOfWhatTheyHoldIsNotAgreement(t *testing.T) {
	p := plane(t, "2106", kaggle("2106.00001", 1, corpus.LicenceCCBY, "cs.CL"))
	got, err := Compare(p, []Claim{claim("2106.00002", labelBY)})
	if err != nil {
		t.Fatal(err)
	}
	head := got.headline()
	if !strings.Contains(head, "Nothing compared") {
		t.Fatalf("the headline is %q", head)
	}
	if !strings.Contains(got.Markdown(), "Nothing to disagree about") {
		t.Error("the disagreements section did not say there was nothing to compare")
	}
}

func TestCrosscheckTextIsTheShortForm(t *testing.T) {
	p := plane(t, "2106", kaggle("2106.00001", 1, corpus.LicenceArXiv, "cs.CL"))
	got, err := Compare(p, []Claim{claim("2106.00001", labelBY)})
	if err != nil {
		t.Fatal(err)
	}
	txt := got.Text()
	if len(txt) >= len(got.Markdown()) {
		t.Error("the short form is not shorter")
	}
	if !strings.Contains(txt, "disagreed") {
		t.Errorf("the short form is:\n%s", txt)
	}
}

func TestCheckWalksAndCompares(t *testing.T) {
	p := plane(t, "2106",
		kaggle("2106.00001", 1, corpus.LicenceArXiv, "cs.CL"),
		kaggle("2106.00002", 1, corpus.LicenceCCBY, "cs.CL"),
	)
	r := reader(t, []theirs{
		{id: "2106.00001", label: labelBY},
		{id: "2106.00002", label: labelBY},
		{id: "2106.00003", label: labelBY},
	})
	got, err := Check(context.Background(), r, p, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got.Claims != 3 || got.Held != 2 || got.Agreed != 1 {
		t.Fatalf("crosscheck is %+v", got)
	}
	if got.Wider() != 1 {
		t.Errorf("counted %d that cost something, want 1", got.Wider())
	}
	if len(got.Absent) != 1 || got.Absent[0] != "2106.00003" {
		t.Errorf("absent is %v", got.Absent)
	}
	if got.Reading.Dataset != Pile {
		t.Errorf("the reading names %q", got.Reading.Dataset)
	}
}

// The rows endpoint is the same one the metadata mirror is read through, so the
// dataset has to make it into the query or this would silently crosscheck the
// corpus against itself.
func TestTheWalkAsksForTheirCollection(t *testing.T) {
	var asked url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		asked = req.URL.Query()
		fmt.Fprint(w, `{"rows":[],"num_rows_total":0}`)
	}))
	defer srv.Close()

	r := &Reader{Rows: &harvest.Rows{Endpoint: srv.URL, Dataset: Pile, Pace: 1}}
	if _, err := r.Walk(context.Background(), 0, 10, func(Claim) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if asked.Get("dataset") != Pile {
		t.Errorf("the walk asked for %q, want %q", asked.Get("dataset"), Pile)
	}
	if asked.Get("length") != strconv.Itoa(PileRows) {
		t.Errorf("the walk asked for %q rows, want %d", asked.Get("length"), PileRows)
	}
}

package graph

import (
	"strings"
	"testing"
)

// edge is a valid claim, which each test then breaks in one way.
func edge() Edge {
	return Edge{
		S: Paper("2405.11111"), P: Cites, O: Paper("2106.09685"),
		Conf: Certain, Via: "arxiv", Stage: StageRefs, At: "2026-10-14",
	}
}

func TestAWholeClaimValidates(t *testing.T) {
	if err := edge().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestEveryPredicateIsInTheTable(t *testing.T) {
	if len(Predicates) != 7 {
		t.Fatalf("the table has %d rows and 08-graph.md section 3 has nine over seven names", len(Predicates))
	}
	for _, name := range Names() {
		if _, ok := Lookup(name); !ok {
			t.Errorf("%s is named and not in the table", name)
		}
	}
	if _, ok := Lookup("depends-on"); ok {
		t.Error("a predicate nobody defined resolved")
	}
}

// The failure this catches is silent. An edge pointing at the wrong kind of node
// joins to nothing later, and missing joins read as missing data.
func TestAnEndOfTheWrongKindIsRefused(t *testing.T) {
	for _, tc := range []struct {
		what string
		e    Edge
		says string
	}{
		{"a paper that uses something", Edge{S: Paper("2405.11111"), P: Uses, O: Object("2106.09685", "0A3F")}, "runs from object"},
		{"a citation of an author", Edge{S: Paper("2405.11111"), P: Cites, O: Name("Ada First")}, "runs to paper"},
		{"a paper that is a version of one", Edge{S: Paper("2405.11111"), P: VersionOf, O: Paper("2405.11111")}, "runs from version"},
		{"an object in a category", Edge{S: Object("2405.11111", "0C2M"), P: InCategory, O: Category("cs.CL")}, "runs from paper"},
	} {
		e := tc.e
		e.Conf, e.Via, e.Stage, e.At = Certain, "metadata", StageHarvest, "2026-10-14"
		err := e.Validate()
		if err == nil {
			t.Fatalf("%s validated", tc.what)
		}
		if !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%s says %q, want it to mention %q", tc.what, err, tc.says)
		}
	}
}

func TestAClaimNobodyMadeIsRefused(t *testing.T) {
	for _, tc := range []struct {
		what string
		fix  func(*Edge)
	}{
		{"no predicate", func(e *Edge) { e.P = "" }},
		{"a predicate nobody defined", func(e *Edge) { e.P = "depends-on" }},
		{"a subject that is not a uri", func(e *Edge) { e.S = "2405.11111" }},
		{"an object that is not a uri", func(e *Edge) { e.O = "arxiv.org/abs/2106.09685" }},
		{"no confidence", func(e *Edge) { e.Conf = "" }},
		{"a confidence of low", func(e *Edge) { e.Conf = "low" }},
		{"nothing about how it was found", func(e *Edge) { e.Via = "" }},
		{"no stage", func(e *Edge) { e.Stage = "" }},
		{"no date", func(e *Edge) { e.At = "" }},
		{"a date that is a timestamp", func(e *Edge) { e.At = "2026-10-14T09:00:00Z" }},
	} {
		e := edge()
		tc.fix(&e)
		if err := e.Validate(); err == nil {
			t.Errorf("an edge with %s validated", tc.what)
		}
	}
}

// There is no low, and the whole point of saying so is that an edge that would
// be low is not stored at all.
func TestThereIsNoLow(t *testing.T) {
	if len(Confidences) != 3 {
		t.Fatalf("there are %d confidences", len(Confidences))
	}
	for _, c := range Confidences {
		if c == "low" {
			t.Error("low is a confidence")
		}
	}
}

func TestTheStrongerConfidenceWins(t *testing.T) {
	for _, tc := range [][3]string{
		{Certain, Medium, Certain},
		{Medium, Certain, Certain},
		{High, Medium, High},
		{High, High, High},
		{Medium, "nonsense", Medium},
	} {
		if got := Strongest(tc[0], tc[1]); got != tc[2] {
			t.Errorf("Strongest(%q, %q) is %q, want %q", tc[0], tc[1], got, tc[2])
		}
	}
}

// A claim written twice is one claim, and the day it was written is not part of
// what it says.
func TestTheDateIsNotPartOfAClaimsIdentity(t *testing.T) {
	a, b := edge(), edge()
	b.At = "2019-01-01"
	b.Conf = Medium
	if a.Key() != b.Key() {
		t.Error("the same claim on two days is two claims")
	}
	c := edge()
	c.Locator = "theorem 2"
	if a.Key() == c.Key() {
		t.Error("a citation that named a result and one that did not are one claim")
	}
}

func TestEdgesSortByTheTableAndNotByTheAlphabet(t *testing.T) {
	edges := []Edge{
		{S: Paper("2405.11111"), P: VersionOf, O: Paper("2405.11111")},
		{S: Paper("2405.11111"), P: Cites, O: Paper("2106.09685")},
		{S: Paper("2405.11111"), P: AuthoredBy, O: Name("Ada First")},
		{S: Paper("1901.00001"), P: Cites, O: Paper("2106.09685")},
	}
	Sort(edges)
	want := []string{Paper("1901.00001"), Paper("2405.11111"), Paper("2405.11111"), Paper("2405.11111")}
	order := []string{Cites, Cites, AuthoredBy, VersionOf}
	for i, e := range edges {
		if e.S != want[i] || e.P != order[i] {
			t.Fatalf("edge %d is %s %s, want %s %s", i, e.S, e.P, want[i], order[i])
		}
	}
}

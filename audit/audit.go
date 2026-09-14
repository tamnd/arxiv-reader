// Package audit runs the numbered rules over a corpus and says what it found.
//
// Each rule is a sentence a person can argue with, each says what it found and
// where, and a hard rule with a finding fails the build. The rules and their
// numbers are in 2166-10, and a rule keeps its number for the life of the
// project, because a number that moves is a number nobody can cite.
//
// This package knows nothing about where the corpus lives beyond what the
// metadata plane hands it, so a rule can be tested against records in memory
// with no corpus on disk.
package audit

import (
	"fmt"
	"sort"
	"strings"
)

// State is what a rule came back with.
//
// Four and not two. The distinction between "looked and found nothing" and
// "had nothing to look at" is the one a corpus loses first, and losing it is
// how a project acquires a rule everybody believes is working.
type State string

const (
	// Pass means the rule looked and found nothing.
	Pass State = "pass"
	// Fail means the rule found something.
	Fail State = "fail"
	// NotRun means the rule had nothing to look at, so an empty corpus or a
	// plane that has not been built yet.
	NotRun State = "not run"
	// NotApplicable means the thing the rule checks cannot exist here. A
	// content plane rule asked about the metadata plane is this, and so is
	// every rule in the M group for a paper extracted on the native path.
	NotApplicable State = "n/a"
)

// Group is the letter a rule belongs to.
type Group string

const (
	// GroupSources is S, the group that makes a public repository defensible.
	GroupSources Group = "S"
	// GroupStructure is T, the group that says a content file is a content
	// file.
	GroupStructure Group = "T"
	// GroupTags is G, the group that reads a paper's register against the
	// bodies the tags in it were written into.
	GroupTags Group = "G"
	// GroupObjects is X, the group that reads the object model.
	GroupObjects Group = "X"
)

// Name is what the scoreboard calls the group.
func (g Group) Name() string {
	switch g {
	case GroupSources:
		return "Sources"
	case GroupStructure:
		return "Structure"
	case GroupTags:
		return "Tags"
	case GroupObjects:
		return "Objects"
	}
	return string(g)
}

// Rule is one numbered sentence.
type Rule struct {
	// ID is the letter and the number, so "S14".
	ID string
	// Group is the letter on its own.
	Group Group
	// Hard says whether a finding fails the build.
	//
	// Every rule in the S group is hard, because a soft licence rule is a
	// licence breach with a warning next to it.
	Hard bool
	// Says is the rule, as a sentence. It is printed in the report and it is
	// the thing somebody disagreeing with a finding argues with.
	Says string
	// Why is the reasoning, for the rules where the sentence alone does not
	// carry it. Empty for the ones where it does.
	Why string
}

// Finding is one thing a rule found.
type Finding struct {
	// Rule is the rule's ID.
	Rule string
	// File is the finding's file, relative to the corpus root.
	//
	// Empty on the metadata plane, where the file is the month's JSONL and
	// Shard already names it. The content plane has a file per section and no
	// such convention, so it says the path.
	File string
	// Shard is the month, and Line is the one based line in the file. Line is
	// zero for a finding that is not about a particular line.
	Shard string
	Line  int
	// ID is the paper, empty when the finding is about the file itself.
	ID string
	// What happened, as a phrase that reads after the paper's id.
	What string
}

// Where is the finding's place, in the form an editor will jump to.
func (f Finding) Where() string {
	file := f.File
	if file == "" {
		if f.Shard == "" {
			return ""
		}
		file = fmt.Sprintf("metadata/%s.jsonl", f.Shard)
	}
	if f.Line == 0 {
		return file
	}
	return fmt.Sprintf("%s:%d", file, f.Line)
}

func (f Finding) String() string {
	parts := make([]string, 0, 3)
	if where := f.Where(); where != "" {
		parts = append(parts, where)
	}
	if f.ID != "" {
		parts = append(parts, f.ID)
	}
	parts = append(parts, f.What)
	return strings.Join(parts, ": ")
}

// Result is one rule's outcome over one run.
type Result struct {
	Rule Rule
	// Checked is how many things the rule looked at, which is what separates a
	// pass from a rule that quietly had no work.
	Checked int
	// Findings is what it found, capped. Held is the number kept and Total is
	// the number there were.
	Findings []Finding
	Total    int
}

// State is the rule's state, worked out rather than stored, so it cannot
// disagree with the findings next to it.
func (r Result) State() State {
	switch {
	case r.Total > 0:
		return Fail
	case r.Checked == 0:
		return NotRun
	default:
		return Pass
	}
}

// Dropped is the number of findings the cap threw away.
//
// A rule with four thousand findings is a rule that is wrong or a corpus that
// is not ready, and either way that is one line rather than four thousand.
func (r Result) Dropped() int { return r.Total - len(r.Findings) }

// Report is a whole run.
type Report struct {
	// Plane is which half was audited, "meta" or "content".
	Plane string
	// Records and Shards are what it read.
	Records, Shards int
	// Unit and Scope are the words for what those two count.
	//
	// The metadata plane counts records over months and the content plane
	// counts files over papers. Empty means the metadata plane's words, which
	// are the ones the report was written with.
	Unit, Scope string
	// Results is one per rule, in rule order.
	Results []Result
}

func (r Report) unit() string {
	if r.Unit == "" {
		return "record"
	}
	return r.Unit
}

func (r Report) scope() string {
	if r.Scope == "" {
		return "month"
	}
	return r.Scope
}

// Failed reports whether a hard rule found something, which is the build's
// answer and the process's exit code.
func (r Report) Failed() bool {
	for _, res := range r.Results {
		if res.Rule.Hard && res.Total > 0 {
			return true
		}
	}
	return false
}

// Findings is the total across every rule.
func (r Report) Findings() int {
	n := 0
	for _, res := range r.Results {
		n += res.Total
	}
	return n
}

// byGroup rolls the results up for the scoreboard, in the order the groups
// first appear.
func (r Report) byGroup() []groupTally {
	index := map[Group]int{}
	var out []groupTally
	for _, res := range r.Results {
		g := res.Rule.Group
		i, ok := index[g]
		if !ok {
			index[g] = len(out)
			out = append(out, groupTally{Group: g})
			i = len(out) - 1
		}
		t := &out[i]
		t.Rules++
		t.Findings += res.Total
		switch res.State() {
		case Pass:
			t.Pass++
		case Fail:
			t.Fail++
		case NotRun:
			t.NotRun++
		case NotApplicable:
			t.NA++
		}
	}
	return out
}

type groupTally struct {
	Group                     Group
	Rules, Pass, Fail, NotRun int
	NA, Findings              int
}

// sortFindings puts findings in the order somebody would read them, which is
// the order the file is in.
func sortFindings(f []Finding) {
	sort.SliceStable(f, func(i, j int) bool {
		if f[i].File != f[j].File {
			return f[i].File < f[j].File
		}
		if f[i].Shard != f[j].Shard {
			return f[i].Shard < f[j].Shard
		}
		return f[i].Line < f[j].Line
	})
}

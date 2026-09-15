package selection

import (
	"fmt"
	"strings"
)

// Status is how far down the pipeline one selected paper has got.
//
// It is a ladder and not a set of flags, because the stages are strictly ordered:
// nothing is extracted before it is fetched and nothing is translated before it is
// tagged. Recording the rung rather than a flag per stage means a paper cannot claim
// to be translated and not extracted, which is a state the corpus would have no way
// to make sense of.
type Status string

const (
	// StatusSelected is chosen and nothing else done.
	StatusSelected Status = "selected"
	// StatusFetched is the bytes on disk and in sources.yaml.
	StatusFetched Status = "fetched"
	// StatusExtracted is Markdown in the content plane.
	StatusExtracted Status = "extracted"
	// StatusTagged is the permanent identifiers assigned.
	StatusTagged Status = "tagged"
	// StatusTranslated is at least one language beyond English.
	StatusTranslated Status = "translated"
	// StatusPublished is in the built outputs.
	StatusPublished Status = "published"
)

// Statuses are the six rungs, in order.
var Statuses = []Status{StatusSelected, StatusFetched, StatusExtracted, StatusTagged, StatusTranslated, StatusPublished}

// Known says whether this is one of the rungs.
func Known(s Status) bool {
	for _, k := range Statuses {
		if k == s {
			return true
		}
	}
	return false
}

// Rung is how far along a status is, counted from one, and zero for a status that
// is not one of them.
func Rung(s Status) int {
	for i, k := range Statuses {
		if k == s {
			return i + 1
		}
	}
	return 0
}

// ParseStatus reads a status off the command line.
func ParseStatus(s string) (Status, error) {
	if Known(Status(s)) {
		return Status(s), nil
	}
	return "", fmt.Errorf("selection: %q is not a status, which are %s", s, StatusNames())
}

// StatusNames is the ladder, for a message.
func StatusNames() string {
	out := make([]string, 0, len(Statuses))
	for _, s := range Statuses {
		out = append(out, string(s))
	}
	return strings.Join(out, ", ")
}

// Reached says whether a paper at this status has got at least as far as want.
//
// A paper that is published has been extracted, and a caller asking for everything
// extracted means everything extracted or further on. Asking the other way round,
// which stage is next, is what Next is for.
func Reached(at, want Status) bool {
	return Rung(at) >= Rung(want) && Rung(want) > 0
}

// Next is the rung after this one, and published is its own next, because there is
// nothing after publishing and returning the empty status would make every caller
// handle a case that never happens.
func Next(s Status) Status {
	r := Rung(s)
	if r == 0 || r == len(Statuses) {
		return StatusPublished
	}
	return Statuses[r]
}

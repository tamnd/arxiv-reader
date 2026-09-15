package selection

import (
	"strings"
	"testing"
)

// Six reasons and no seventh. The number is the design: a selection with a seventh
// reason in it is a selection somebody has started adding papers to for reasons
// nobody wrote down.
func TestThereAreSixReasonsAndEachOneSaysSomething(t *testing.T) {
	if len(Reasons) != 6 {
		t.Errorf("there are %d reasons and the selection was designed around six", len(Reasons))
	}
	seen := map[Reason]bool{}
	for _, r := range Reasons {
		if r.Says() == "" {
			t.Errorf("%s says nothing, and a reason nobody can read is a reason nobody can argue with", r)
		}
		if seen[r] {
			t.Errorf("%s is in the list twice", r)
		}
		seen[r] = true
	}
}

func TestAReasonComesBackFromItsName(t *testing.T) {
	for _, r := range Reasons {
		got, err := ParseReason(string(r))
		if err != nil {
			t.Fatalf("%s: %v", r, err)
		}
		if got != r {
			t.Errorf("%s came back as %s", r, got)
		}
	}
}

// And the refusal names the six, because somebody who typed the wrong word wants to
// be told which words there are.
func TestAReasonNobodyAgreedOnIsRefused(t *testing.T) {
	_, err := ParseReason("because-i-like-it")
	if err == nil {
		t.Fatal("a reason that is not one of the six was accepted")
	}
	for _, r := range Reasons {
		if !strings.Contains(err.Error(), string(r)) {
			t.Errorf("the refusal does not mention %s: %v", r, err)
		}
	}
}

// The two closure reasons say the same number, and it is the floor rather than the
// threshold anybody runs, so the sentence has to carry it.
func TestTheClosureReasonsSayTheirFloor(t *testing.T) {
	for _, r := range []Reason{CitedByCorpus, CitesCorpus} {
		if !strings.Contains(r.Says(), "3") {
			t.Errorf("%s says %q and the floor is %d", r, r.Says(), Floor)
		}
	}
}

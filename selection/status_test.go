package selection

import "testing"

func TestTheStatusesAreALadderInOrder(t *testing.T) {
	want := []Status{StatusSelected, StatusFetched, StatusExtracted, StatusTagged, StatusTranslated, StatusPublished}
	if len(Statuses) != len(want) {
		t.Fatalf("there are %d statuses and the pipeline has %d stages", len(Statuses), len(want))
	}
	for i, s := range want {
		if Statuses[i] != s {
			t.Errorf("rung %d is %s and should be %s", i+1, Statuses[i], s)
		}
		if Rung(s) != i+1 {
			t.Errorf("%s is at rung %d and should be at %d", s, Rung(s), i+1)
		}
	}
	if Rung("halfway") != 0 {
		t.Error("a status that is not a rung has a rung")
	}
}

// A paper that is published has been extracted. That is the whole reason the status
// is a rung and not a set of flags: asking for everything extracted has to include
// everything that got further.
func TestAPaperThatGotFurtherHasReachedTheStagesBehindIt(t *testing.T) {
	if !Reached(StatusPublished, StatusExtracted) {
		t.Error("a published paper is not counted as extracted")
	}
	if Reached(StatusFetched, StatusExtracted) {
		t.Error("a fetched paper is being counted as extracted")
	}
	if !Reached(StatusExtracted, StatusExtracted) {
		t.Error("a paper has not reached the stage it is at")
	}
	// A status nobody knows has reached nothing, rather than reaching everything,
	// which is what a zero rung would do if the comparison were the other way
	// round.
	if Reached(StatusPublished, "typeset") {
		t.Error("a paper reached a stage that does not exist")
	}
}

func TestNextIsTheRungAfterAndPublishingIsTheEnd(t *testing.T) {
	if got := Next(StatusSelected); got != StatusFetched {
		t.Errorf("after selected comes %s", got)
	}
	if got := Next(StatusPublished); got != StatusPublished {
		t.Errorf("after published comes %s, and there is nothing after publishing", got)
	}
}

func TestAStatusNobodyAgreedOnIsRefused(t *testing.T) {
	if _, err := ParseStatus("nearly"); err == nil {
		t.Error("a status that is not a rung was accepted")
	}
	for _, s := range Statuses {
		if got, err := ParseStatus(string(s)); err != nil || got != s {
			t.Errorf("%s came back as %s, %v", s, got, err)
		}
	}
}

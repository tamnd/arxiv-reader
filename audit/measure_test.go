package audit

import (
	"os"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-cli/pkg/axid"
)

// measured builds the fixture, measures it and returns the one paper's numbers.
func measured(t *testing.T, root string) Measurement {
	t.Helper()
	c := Content{Root: root}
	papers, err := c.Papers()
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.Measure(papers)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("the fixture is one paper, got %d", len(out))
	}
	return out[0]
}

func TestMeasure(t *testing.T) {
	root := paper(t, front(), section(1, "One", display(2)+prose(20)), section(2, "Two", display(1)+prose(20)), section(3, "Three", prose(20)))
	got := measured(t, root)
	if got.ID != fixture {
		t.Fatalf("got %s", got.ID)
	}
	if got.Displays != 3 {
		t.Fatalf("the fixture has three display blocks, got %d", got.Displays)
	}
	if got.Characters < 2000 {
		t.Fatalf("the bodies are longer than that, got %d", got.Characters)
	}
	if got.Entries != 1 || got.Resolved != 1 {
		t.Fatalf("the fixture cites one paper and it resolves, got %d of %d", got.Resolved, got.Entries)
	}
}

func TestMeasureCountsEveryFile(t *testing.T) {
	// Displays are counted over the whole paper and not over one file, because
	// a paper split into four sections and the same paper split into twelve are
	// the same paper and have to measure the same.
	one := measured(t, paper(t, front(), section(1, "One", display(4)+prose(30)), section(2, "Two", prose(1)), section(3, "Three", prose(1))))
	many := measured(t, paper(t, front(), section(1, "One", display(2)+prose(15)), section(2, "Two", display(2)+prose(15)), section(3, "Three", prose(1))))
	if one.Displays != many.Displays {
		t.Fatalf("the same four displays counted as %d and %d", one.Displays, many.Displays)
	}
}

func TestMeasureWithoutABibliography(t *testing.T) {
	// A paper nobody has run ax refs over has no resolution rate, and the
	// baselines leave it out of that metric rather than counting it as nought.
	root := paper(t, front(), section(1, "One", prose(20)), section(2, "Two", prose(20)), section(3, "Three", prose(20)))
	if err := os.Remove(refsPath(root)); err != nil {
		t.Fatal(err)
	}
	got := measured(t, root)
	if got.Entries != 0 || got.Resolved != 0 {
		t.Fatalf("there is no bibliography to count, got %d of %d", got.Resolved, got.Entries)
	}
	if got.Characters == 0 {
		t.Fatal("the rest of the paper is still there and is still measured")
	}
}

func TestMeasureCountsTheUnresolved(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(20)), section(2, "Two", prose(20)), section(3, "Three", prose(20)))
	entry := oneEntry()
	second := oneEntry()
	second.ID, second.Label, second.ArXiv, second.Resolved, second.Via = "bib.bibx2", "[2]", "", "", ""
	bibbed(t, root, entry, second)
	got := measured(t, root)
	if got.Entries != 2 || got.Resolved != 1 {
		t.Fatalf("one of the two resolves, got %d of %d", got.Resolved, got.Entries)
	}
}

func TestMeasureReportsNothingAboutAPaperThatDoesNotParse(t *testing.T) {
	// A file with no front matter is S05's finding and not this one's. Measuring
	// is a description of the corpus and the corpus has broken files in it, so
	// the paper is measured on the files that do parse rather than refused.
	root := paper(t, front(), section(1, "One", display(2)+prose(20)), section(2, "Two", prose(20)), section(3, "Three", prose(20)))
	path := root + "/content/en/2501/2501.00001/03_three.md"
	if err := os.WriteFile(path, []byte("no front matter here at all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := measured(t, root)
	if got.Displays != 2 {
		t.Fatalf("the two displays are in a file that parses, got %d", got.Displays)
	}
}

func TestMeasureTakesThePapersItIsGiven(t *testing.T) {
	root := paper(t, front(), section(1, "One", prose(20)), section(2, "Two", prose(20)), section(3, "Three", prose(20)))
	out, err := (Content{Root: root}).Measure([]axid.ID{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("no papers were asked about, got %d", len(out))
	}
}

// display is n display blocks, written the way the extractors write them.
func display(n int) string {
	return strings.Repeat("$$\nx = y\n$$\n\n", n)
}

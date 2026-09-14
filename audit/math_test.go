package audit

import (
	"strings"
	"testing"
)

// maths builds a one section paper whose body is the mathematics under test.
//
// The prose on the end is there so the section clears T08's floor, which keeps
// every test in this file about the rule it names.
func maths(t *testing.T, body string) string {
	t.Helper()
	return paper(t, front(), section(1, "One", body+"\n\n"+prose(8)))
}

func TestTheSplitterFindsInlineAndDisplayMath(t *testing.T) {
	body := "Let $x$ be at most $y + 1$.\n\n$$\na = 1\nb = 2\n$$\n\n```\necho $HOME\n```\n\nAnd $z$ ends it.\n"
	found := spans(classify(body))
	if len(found) != 4 {
		t.Fatalf("found %d spans: %v", len(found), found)
	}
	want := []span{
		{line: 1, col: 5, tex: "x", closed: true},
		{line: 1, col: 20, tex: "y + 1", closed: true},
		{line: 3, col: 1, tex: "a = 1\nb = 2", display: true, closed: true},
		{line: 12, col: 5, tex: "z", closed: true},
	}
	for i, w := range want {
		if found[i] != w {
			t.Errorf("span %d is %+v, want %+v", i, found[i], w)
		}
	}
}

func TestADisplayBlockCountsItsOwnLines(t *testing.T) {
	s := span{line: 10, tex: "a = 1\nb = 2\nc = 3", display: true}
	for off, want := range map[int]int{0: 11, 6: 12, 12: 13} {
		if got := s.at(off); got != want {
			t.Errorf("offset %d is on line %d, want %d", off, got, want)
		}
	}
	inline := span{line: 10, tex: "a = 1"}
	if got := inline.at(3); got != 10 {
		t.Errorf("an inline span is on line %d, want 10", got)
	}
}

func TestM01ReportsASpanNothingCloses(t *testing.T) {
	t.Run("inline", func(t *testing.T) {
		f := fires(t, maths(t, "The cost is $n + 1 and the rest of it follows."), "M01")
		if !strings.Contains(f.What, "column 13") {
			t.Errorf("says %q", f.What)
		}
	})
	t.Run("display", func(t *testing.T) {
		f := fires(t, maths(t, "It follows that\n\n$$\na = 1"), "M01")
		if !strings.Contains(f.What, "display block") {
			t.Errorf("says %q", f.What)
		}
	})
}

func TestM02ReportsAMacroOnlyThePreambleDefines(t *testing.T) {
	f := fires(t, maths(t, `Every point of $\R$ is a limit.`), "M02")
	if !strings.Contains(f.What, `the reals as \R`) {
		t.Errorf("says %q", f.What)
	}
}

func TestM02ReportsOneSetWrittenTwoWays(t *testing.T) {
	f := fires(t, maths(t, `On $\mathbb{R}$ and again on $\mathbf{R}$ the same thing holds.`), "M02")
	if !strings.Contains(f.What, `\mathbf{R} here and as \mathbb{R} elsewhere`) {
		t.Errorf("says %q", f.What)
	}
	if f.Line != 1 {
		t.Errorf("reported line %d, want 1", f.Line)
	}
}

func TestM02LeavesALetterOnlyWrittenOneWay(t *testing.T) {
	root := maths(t, `Vectors $\mathbf{x}$ and $\mathbf{W}$ act on $\mathbb{R}$ here.`)
	if got := audited(t, root)["M02"]; got.Total != 0 {
		t.Errorf("M02 found %v", got.Findings)
	}
}

func TestM03ReportsTeXLeftInTheProse(t *testing.T) {
	f := fires(t, maths(t, `The bound is \frac{1}{2} of the whole.`), "M03")
	if !strings.Contains(f.What, `\frac`) {
		t.Errorf("says %q", f.What)
	}
}

func TestM03LeavesMarkdownsOwnEscapes(t *testing.T) {
	root := maths(t, `The result of Ma and Kaplan \[[24](#bib.bib24)\] is the same.`)
	if got := audited(t, root)["M03"]; got.Total != 0 {
		t.Errorf("M03 found %v", got.Findings)
	}
}

func TestM05ReportsAMarkerForSomethingNothingCouldRead(t *testing.T) {
	f := fires(t, maths(t, "The constant is [illegible] in the printed copy."), "M05")
	if !strings.Contains(f.What, "[illegible]") {
		t.Errorf("says %q", f.What)
	}
}

func TestM07ReportsABracketThatCrossesIntoTheMathematics(t *testing.T) {
	f := fires(t, maths(t, "We know (see $x)$ that the sum converges here."), "M07")
	if !strings.Contains(f.What, "opens ( in the prose") {
		t.Errorf("says %q", f.What)
	}
}

func TestM07LeavesBracketsThatStayOnOneSide(t *testing.T) {
	root := maths(t, "From the recurrent view, the $(\\bm{A},\\bm{B})$ transitions in ([2](#eq-2)) hold.")
	if got := audited(t, root)["M07"]; got.Total != 0 {
		t.Errorf("M07 found %v", got.Findings)
	}
}

func TestM08ReportsAMatrixWithNoEnvironmentLeft(t *testing.T) {
	t.Run("a row break", func(t *testing.T) {
		f := fires(t, maths(t, "It follows that\n\n$$\na = 1 \\\\\nb = 2\n$$"), "M08")
		if f.Line != 4 {
			t.Errorf("reported line %d, want 4", f.Line)
		}
	})
	t.Run("stacked by hand", func(t *testing.T) {
		f := fires(t, maths(t, `The pair ${a \atop b}$ appears throughout.`), "M08")
		if !strings.Contains(f.What, `\atop`) {
			t.Errorf("says %q", f.What)
		}
	})
}

func TestM08LeavesAnEnvironmentThatIsStillThere(t *testing.T) {
	root := maths(t, "It follows that\n\n$$\n\\begin{aligned}\na &= 1 \\\\\nb &= 2\n\\end{aligned}\n$$")
	if got := audited(t, root)["M08"]; got.Total != 0 {
		t.Errorf("M08 found %v", got.Findings)
	}
}

func TestM09ReportsABaseWithTwoOfOneScript(t *testing.T) {
	f := fires(t, maths(t, `The term $x^{a}^{b}$ appears in every row of the table.`), "M09")
	if !strings.Contains(f.What, "two superscripts") {
		t.Errorf("says %q", f.What)
	}
}

func TestM09LeavesOneScriptOfEachKind(t *testing.T) {
	root := maths(t, `The term $x^{a}_{b}$ appears in every row of the table.`)
	if got := audited(t, root)["M09"]; got.Total != 0 {
		t.Errorf("M09 found %v", got.Findings)
	}
}

func TestM10ReportsANegationSeparatedFromItsRelation(t *testing.T) {
	t.Run("a loose not", func(t *testing.T) {
		f := fires(t, maths(t, `The claim is that $a \not b$ holds for every pair.`), "M10")
		if !strings.Contains(f.What, `\not`) {
			t.Errorf("says %q", f.What)
		}
	})
	t.Run("a stranded stroke", func(t *testing.T) {
		f := fires(t, maths(t, "The two are =\u0338 for any choice of the constants."), "M10")
		if !strings.Contains(f.What, "combining negation stroke") {
			t.Errorf("says %q", f.What)
		}
	})
}

func TestM10LeavesANegationStillAttached(t *testing.T) {
	root := maths(t, `The claim is that $a \not\in B$ holds for every pair.`)
	if got := audited(t, root)["M10"]; got.Total != 0 {
		t.Errorf("M10 found %v", got.Findings)
	}
}

func TestM11ReportsTheOtherDelimiters(t *testing.T) {
	t.Run("round", func(t *testing.T) {
		f := fires(t, maths(t, `The value \(x\) is fixed for the rest of the section.`), "M11")
		if !strings.Contains(f.What, `\(`) {
			t.Errorf("says %q", f.What)
		}
	})
	t.Run("square", func(t *testing.T) {
		f := fires(t, maths(t, "It follows that\n\n\\[\na = 1\n\\]"), "M11")
		if f.Line != 3 {
			t.Errorf("reported line %d, want 3", f.Line)
		}
	})
}

func TestM11LeavesABracketMarkdownHadToEscape(t *testing.T) {
	root := maths(t, `Compare the bound of \[3,3,2,1\] with the one above.`)
	if got := audited(t, root)["M11"]; got.Total != 0 {
		t.Errorf("M11 found %v", got.Findings)
	}
}

func TestM12ReportsAFormulaLooseInsideItsDollars(t *testing.T) {
	f := fires(t, maths(t, "The value $ x + 1 $ is the one that matters here."), "M12")
	if !strings.Contains(f.What, "$x + 1$") {
		t.Errorf("says %q", f.What)
	}
}

func TestM13ReportsAFenceWithAnOddNumberOfDollars(t *testing.T) {
	f := fires(t, maths(t, "Run it with\n\n```\necho $HOME\n```"), "M13")
	if !strings.Contains(f.What, "1 dollar sign") {
		t.Errorf("says %q", f.What)
	}
	if f.Line != 3 {
		t.Errorf("reported line %d, want 3", f.Line)
	}
}

func TestM13LeavesAFenceWhoseDollarsPairInside(t *testing.T) {
	root := maths(t, "Run it with\n\n```\necho $HOME $PATH\n```")
	if got := audited(t, root)["M13"]; got.Total != 0 {
		t.Errorf("M13 found %v", got.Findings)
	}
}

func TestM14ReportsAPaperWhoseFormulasWereFlattened(t *testing.T) {
	f := fires(t, maths(t, "The step α is at most β whenever γ is small enough."), "M14")
	if !strings.Contains(f.What, "not one math span") {
		t.Errorf("says %q", f.What)
	}
	if f.Line != 0 {
		t.Errorf("reported line %d, and this is about a paper", f.Line)
	}
}

func TestM14LeavesAPaperThatKeptOneSpan(t *testing.T) {
	root := maths(t, "The step α is at most β whenever $\\gamma$ is small enough.")
	if got := audited(t, root)["M14"]; got.Total != 0 {
		t.Errorf("M14 found %v", got.Findings)
	}
}

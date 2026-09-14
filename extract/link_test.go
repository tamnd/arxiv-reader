package extract

import "testing"

func TestCrossReferencesAreRewrittenToLocalIdentifiers(t *testing.T) {
	anchors := map[string]string{
		"S3.SS1": "s3-1",
		"S3.F2":  "fig-2",
		"S2.E3":  "eq-3a",
		"Thm1":   "thm-1",
	}
	cases := map[string]string{
		"see [Section 3.1](#S3.SS1)":       "see [Section 3.1](#s3-1)",
		"([Figure 2](#S3.F2))":             "([Figure 2](#fig-2))",
		"both [1](#S3.SS1) and [2](#Thm1)": "both [1](#s3-1) and [2](#thm-1)",
		// LaTeXML hangs a sub-anchor off the object's own id, so S2.E3.1 is
		// subequation 3a of equation 3, and landing the reader on the equation
		// is right.
		"([3a](#S2.E3.1))": "([3a](#eq-3a))",
		// A bibliography entry is left exactly as it was, because ax refs
		// resolves those into the metadata plane in its own pass.
		"([Nobody, 1999](#bib.bibx1))": "([Nobody, 1999](#bib.bibx1))",
		// An anchor nothing knows about is left alone rather than rewritten
		// into a guess.
		"[what](#S9.E9)": "[what](#S9.E9)",
		"no links here":  "no links here",
	}
	for in, want := range cases {
		if got := link(in, anchors); got != want {
			t.Errorf("got %q\nwant %q", got, want)
		}
	}
}

// Dropping the E out of S2.E2 would turn a reference to equation 2 into a
// reference to section 2, which is a link that works, goes somewhere real and
// is wrong. That is worse than a link left alone.
func TestOnlyANumericTailIsDropped(t *testing.T) {
	anchors := map[string]string{"S2": "s2", "S2.E3": "eq-3a"}
	if got := link("[2](#S2.E2)", anchors); got != "[2](#S2.E2)" {
		t.Fatalf("got %q, want the reference left alone", got)
	}
	if got := link("[3a](#S2.E3.1)", anchors); got != "[3a](#eq-3a)" {
		t.Fatalf("got %q, want [3a](#eq-3a)", got)
	}
}

func TestLinkWithNoAnchorsIsTheSameString(t *testing.T) {
	const in = "see [Section 3.1](#S3.SS1)"
	if got := link(in, nil); got != in {
		t.Fatalf("got %q", got)
	}
}

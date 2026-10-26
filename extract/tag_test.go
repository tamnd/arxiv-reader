package extract

import "testing"

func TestTagNumber(t *testing.T) {
	for in, want := range map[string]string{
		"Theorem 3.1":     "3.1",
		"3.1 Assumption":  "3.1",
		"Theorem A.":      "A",
		"Lemma 2.4:":      "2.4",
		"(1.2)":           "1.2",
		"Main Theorem":    "Theorem",
		"":                "",
		"Proposition 5 ":  "5",
		"2 Lemma.":        "2",
		"Corollary III.2": "III.2",
	} {
		if got := tagNumber(in); got != want {
			t.Errorf("tagNumber(%q) = %q, want %q", in, got, want)
		}
	}
}

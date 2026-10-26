package fingerprint

import (
	"strings"
	"testing"
)

type macros map[string]string

func (m macros) Expand(s string) string {
	for k, v := range m {
		s = strings.ReplaceAll(s, k, v)
	}
	return s
}

func TestNormalizeExpandsMacrosAndRemovesLabels(t *testing.T) {
	a := Normalize(`Let $x\in\R$.\label{lem:a} By \cref{thm:b},  $x^2\ge 0$.`, macros{`\R`: `\mathbb{R}`})
	b := Normalize(`Let $x\in\mathbb{R}$. By \ref{other}, $x^2\ge 0$.`, nil)
	if a != b {
		t.Fatalf("got\n%q\n%q", a, b)
	}
	if Hash(a) != Hash(b) {
		t.Fatal("the hashes differ")
	}
}

func TestMinHashFindsNearDuplicates(t *testing.T) {
	a := Normalize(`Let $G$ be a finite group and $p$ a prime dividing $|G|$. Then $G$ has an element of order $p$.`, nil)
	b := Normalize(`Let $G$ be a finite group and let $p$ be a prime dividing $|G|$. Then $G$ has an element of order $p$.`, nil)
	c := Normalize(`Every bounded monotone sequence of real numbers converges.`, nil)
	if s := Similarity(MinHash(a), MinHash(b)); s < 0.6 {
		t.Errorf("near duplicates: similarity %.2f", s)
	}
	if s := Similarity(MinHash(a), MinHash(c)); s > 0.2 {
		t.Errorf("different texts: similarity %.2f", s)
	}
	if len(MinHash(a)) != 344 {
		t.Errorf("the MinHash has %d characters, not 344", len(MinHash(a)))
	}
}

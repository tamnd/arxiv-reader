package cues

import (
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/tex"
	"github.com/tamnd/arxiv-reader/theorems"
)

func defaultCues(t *testing.T) *Cues {
	t.Helper()
	b, _ := theorems.Manifest("prose.yaml")
	c, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestFind(t *testing.T) {
	src := `\documentclass{amsart}
\newtheorem{thm}{Theorem}
\begin{document}
\section{Introduction}
We say that a module $M$ is \emph{tame} if every finitely generated submodule is free.
This is e.g. the case for $\mathbb{Z}$. It is not known whether every such space is metrizable.

\begin{thm}
We say that this sentence is in a theorem, so it is not prose.
\end{thm}
\begin{equation}
x = y. \text{We conjecture nothing here.}
\end{equation}
Define the rank of $M$ to be the dimension of $M \otimes \mathbb{Q}$.
A ring is \emph{reduced} if it has no nilpotent elements.
\section{Bounds}
We conjecture that the bound is sharp.
\end{document}
`
	d, err := tex.ReadFile("main.tex", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var items []tex.Region
	for _, n := range d.Tree.Nodes {
		if n.Name == "thm" {
			items = append(items, tex.Region{From: n.From, To: n.To})
		}
	}
	got := defaultCues(t).Find(d, items, "")
	var lines []string
	for _, c := range got {
		lines = append(lines, c.Kind+" "+c.Cue+" "+c.Section+" "+strings.Join(strings.Fields(string(d.Text[c.From:c.To])), " "))
	}
	want := []string{
		"definition we say that sec-1 We say that a module $M$ is \\emph{tame} if every finitely generated submodule is free.",
		"problem it is not known whether sec-1 It is not known whether every such space is metrizable.",
		"definition define ... to be sec-1 Define the rank of $M$ to be the dimension of $M \\otimes \\mathbb{Q}$.",
		"definition emphasis sec-1 A ring is \\emph{reduced} if it has no nilpotent elements.",
		"conjecture we conjecture sec-2 We conjecture that the bound is sharp.",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if len(got) > 0 && (got[0].Span.File != "main.tex" || got[0].Span.From != 5) {
		t.Errorf("the span of the first candidate is %+v, want main.tex line 5", got[0].Span)
	}
}

func TestWords(t *testing.T) {
	got := strings.Join(words([]byte(`is \emph{called} \(X\) and $Y$~Hahn--Banach's`)), " ")
	if want := "is called $ and $ hahn--banach's"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

package cues

import (
	"fmt"
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

const findSource = `\documentclass{amsart}
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

// theoremItems reads the source and gives the extents of its theorems.
func theoremItems(t *testing.T) (*tex.Document, []tex.Region) {
	t.Helper()
	d, err := tex.ReadFile("main.tex", []byte(findSource))
	if err != nil {
		t.Fatal(err)
	}
	var items []tex.Region
	for _, n := range d.Tree.Nodes {
		if n.Name == "thm" {
			items = append(items, tex.Region{From: n.From, To: n.To})
		}
	}
	return d, items
}

func TestFind(t *testing.T) {
	d, items := theoremItems(t)
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

func TestSections(t *testing.T) {
	d, items := theoremItems(t)
	secs := defaultCues(t).Sections(d, items, "", 1)
	var lines []string
	for _, s := range secs {
		for _, x := range s.Sentences {
			var marks []string
			for _, m := range x.Cues {
				marks = append(marks, m.Kind)
			}
			lines = append(lines, fmt.Sprintf("%s %d %s:%d [%s] %s", s.Section, x.N, x.File, x.Line, strings.Join(marks, " "), x.Text))
		}
	}
	want := []string{
		"sec-1 1 main.tex:5 [definition] We say that a module $M$ is \\emph{tame} if every finitely generated submodule is free.",
		"sec-1 2 main.tex:6 [] This is e.g. the case for $\\mathbb{Z}$.",
		"sec-1 3 main.tex:6 [problem] It is not known whether every such space is metrizable.",
		"sec-1 4 main.tex:14 [definition] Define the rank of $M$ to be the dimension of $M \\otimes \\mathbb{Q}$.",
		"sec-1 5 main.tex:15 [definition] A ring is \\emph{reduced} if it has no nilpotent elements.",
		"sec-2 6 main.tex:17 [conjecture] We conjecture that the bound is sharp.",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

package theorems

import (
	"testing"

	"github.com/tamnd/arxiv-reader/tex"
)

func TestAHeadCommandOfThePaper(t *testing.T) {
	src := []byte(`\documentclass{article}
\newcounter{Mycounter}[section]
\newcounter{theorem}[section]
\renewcommand{\thetheorem}{{Theorem \thesection.\arabic{theorem}}}
\newcommand{\theorem}{%
     \setcounter{theorem}{\value{Mycounter}}
     \refstepcounter{theorem}
     \stepcounter{Mycounter}
     {\bf \thetheorem:\ }}
\newcommand{\Thm}{\textbf{Theorem}}
\begin{document}
\section{One}

% a rule
\theorem \label{t:a} Each graph is finite.

By \Thm{} the tree is a graph.

\Thm{} is used here as a word.
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	hs := FindHeads(d, NewResolver().Titles, nil)
	if len(hs) != 1 {
		t.Fatalf("the heads are %+v, and there must be one", hs)
	}
	if h := hs[0]; h.Title != "Theorem" || h.Item != "statement/theorem" || h.Number != "" {
		t.Errorf("the head is %+v, and it must be an unnumbered Theorem", h)
	}
}

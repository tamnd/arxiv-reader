package census

import (
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/tex"
	"github.com/tamnd/arxiv-reader/theorems"
)

// TestAHeadCommandGetsTheNumberOfItsCounter reads heads that commands of the
// paper write, with one counter that they all share, as in alg-geom/9712012v2.
func TestAHeadCommandGetsTheNumberOfItsCounter(t *testing.T) {
	src := []byte(`\documentclass{article}
\newcounter{Mycounter}[section]
\newcounter{lemma}[section]
\renewcommand{\thelemma}{{Lemma \thesection.\arabic{lemma}}}
\newcommand{\lemma}{\setcounter{lemma}{\value{Mycounter}}\refstepcounter{lemma}\stepcounter{Mycounter}{\bf \thelemma:\ }}
\newcounter{theorem}[section]
\renewcommand{\thetheorem}{{Theorem \thesection.\arabic{theorem}}}
\newcommand{\theorem}{\setcounter{theorem}{\value{Mycounter}}\refstepcounter{theorem}\stepcounter{Mycounter}{\bf \thetheorem:\ }}
\begin{document}
\section{One}

\lemma Each tree is a graph.

\theorem Each graph is finite.

\section{Two}

\theorem Each graph is a tree.
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	r := theorems.NewResolver()
	c := Run(Input{Paper: "2601.00001", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles})
	var got []string
	for _, o := range c.Occurrences {
		if o.Type == Head {
			got = append(got, o.Title+" "+o.Number)
		}
	}
	want := []string{"Lemma 1.1", "Theorem 1.2", "Theorem 2.1"}
	if len(got) != len(want) {
		t.Fatalf("the heads are %q, and they must be %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("head %d is %q, and it must be %q", i, got[i], want[i])
		}
	}
}

// TestASectionEnvironmentStepsTheSection reads \begin{section}{Title}, as in
// math/0406514v2.
func TestASectionEnvironmentStepsTheSection(t *testing.T) {
	src := []byte(`\documentclass{article}
\newtheorem{lem}{Lemma}[section]
\begin{document}
\begin{section}{One}
\begin{lem} Each tree is a graph. \end{lem}
\end{section}
\begin{section}{Two}
\begin{lem} Each graph is finite. \end{lem}
\end{section}
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	r := theorems.NewResolver()
	c := Run(Input{Paper: "2601.00001", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles})
	var got []string
	for _, o := range c.Occurrences {
		if o.Type == Env {
			got = append(got, o.Number)
		}
	}
	if len(got) != 2 || got[0] != "1.1" || got[1] != "2.1" {
		t.Fatalf("the numbers are %q, and they must be 1.1 and 2.1", got)
	}
}

// TestAFormatInTheCodeOfAMacroIsNotThePapers reads \def\thethm{#1} in the
// begin code of an environment, as in 2404.06445v3.
func TestAFormatInTheCodeOfAMacroIsNotThePapers(t *testing.T) {
	src := []byte(`\documentclass{article}
\newtheorem{thm}{Theorem}[section]
\newenvironment{named}[1]{\def\thethm{#1}\begin{thm}}{\end{thm}\addtocounter{thm}{-1}}
\begin{document}
\section{One}
\begin{thm} Each tree is a graph. \end{thm}
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	r := theorems.NewResolver()
	c := Run(Input{Paper: "2601.00001", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles})
	for _, o := range c.Occurrences {
		if o.Type == Env && o.Number != "1.1" {
			t.Errorf("the number is %q, and it must be 1.1", o.Number)
		}
	}
}

// TestATitleThatStartsAChapter reads \setcounter{chapter}{5} in the preamble
// and a \maketitle that steps the chapter, as svmult does in 2311.01874v3.
func TestATitleThatStartsAChapter(t *testing.T) {
	src := []byte(`\documentclass{article}
\setcounter{chapter}{5}
\makeatletter
\renewcommand\maketitle{\par\refstepcounter{chapter}\setcounter{section}{0}}
\renewcommand\thesection{\thechapter.\@arabic\c@section}
\makeatother
\newtheorem{thm}{Theorem}[section]
\begin{document}
\maketitle
\section{One}
\begin{thm} Each tree is a graph. \end{thm}
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	r := theorems.NewResolver()
	c := Run(Input{Paper: "2601.00001", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles})
	n := 0
	for _, o := range c.Occurrences {
		if o.Type == Env {
			n++
			if o.Model != "6.1.1" {
				t.Errorf("the model gives %q, and it must give 6.1.1", o.Model)
			}
		}
	}
	if n != 1 {
		t.Fatalf("%d theorems, want 1", n)
	}
}

// TestACommandThatWritesASection steps the section at each \sec, as in
// math/9902147v1.
func TestACommandThatWritesASection(t *testing.T) {
	src := []byte(`\documentclass{amsart}
\newtheorem{thm}{Theorem}[section]
\renewcommand{\sec}[2]{\section{#2}\label{s:#1}}
\begin{document}
\sec{one}{One}
\begin{thm} Each tree is a graph. \end{thm}
\sec{two}{Two}
\begin{thm} Each path is a tree. \end{thm}
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	r := theorems.NewResolver()
	c := Run(Input{Paper: "2601.00001", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles})
	var got []string
	for _, o := range c.Occurrences {
		if o.Type == Env {
			got = append(got, o.Model)
		}
	}
	if strings.Join(got, " ") != "1.1 2.1" {
		t.Errorf("the model gives %v, and it must give 1.1 2.1", got)
	}
}

// TestAFormatInADefinitionWaitsForTheCall reads no format from the code of
// \bppendix, which the paper does not call, as in svmult.cls.
func TestAFormatInADefinitionWaitsForTheCall(t *testing.T) {
	src := []byte(`\documentclass{article}
\newcommand\bppendix{\par\renewcommand\thesection{\Alph{section}}}
\newtheorem{thm}{Theorem}[section]
\begin{document}
\section{One}
\begin{thm} Each tree is a graph. \end{thm}
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	r := theorems.NewResolver()
	c := Run(Input{Paper: "2601.00001", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles})
	for _, o := range c.Occurrences {
		if o.Type == Env && o.Model != "1.1" {
			t.Errorf("the model gives %q, and it must give 1.1", o.Model)
		}
	}
}

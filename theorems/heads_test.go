package theorems

import (
	"strings"
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

// A paper with CRLF line ends has blank lines of "\r\n\r\n", and each
// paragraph of them starts a head.
func TestHeadsAfterCRLFBlankLines(t *testing.T) {
	src := []byte("\\documentclass{article}\r\n\\begin{document}\r\nWe show two results:\\\\\r\n\r\n\\noindent{\\bf Theorem C.} Each tree is a graph.\\\\\r\n\r\n\\noindent{\\bf Corollary E.}\r\n\r\nEach path is a graph.\r\n\\end{document}\r\n")
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	hs := FindHeads(d, NewResolver().Titles, nil)
	if len(hs) != 2 || hs[0].Number != "C" || hs[1].Item != "statement/corollary" {
		t.Fatalf("the heads are %+v, and there must be Theorem C and Corollary E", hs)
	}
}

// TestHeadsOfAFileInPlainTeX reads a file in Plain TeX that sets its numbers
// by hand as commands and writes its proof head with a command. Seen in the
// gold pilot (set X).
func TestHeadsOfAFileInPlainTeX(t *testing.T) {
	src := []byte(`\magnification=1200
\def\Proof{\medbreak\noindent{\bf Proof.}\enspace}
\def \ONElem{1.6}
\def \TWOthm{2.1}

{\bf Lemma \ONElem.}\enspace\slant Each tree is a graph.\endslant

\Proof Clear.

{\bf Theorem \TWOthm.}\enspace By Lemma \ONElem, each path is a graph.
\bye
`)
	d, err := tex.ReadFile("paper.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, h := range FindHeads(d, NewResolver().Titles, nil) {
		got = append(got, h.Item+" "+h.Number)
	}
	if want := "statement/lemma 1.6|proof |statement/theorem 2.1"; strings.Join(got, "|") != want {
		t.Errorf("the heads are %q, and they must be %q", got, want)
	}
}

// TestAProofHeadInsideAStatement reads a paper that writes the proof of a
// lemma inside the lemma environment. Seen in the gold pilot (set X).
func TestAProofHeadInsideAStatement(t *testing.T) {
	src := []byte(`\documentclass{article}
\newtheorem{lemma}{Lemma}
\begin{document}
\begin{lemma}\label{l:a}
Each tree is a graph.\\
\normalfont
%
{\bf Proof.}
%
A tree is a connected graph.
\hfill {\bf End of proof.}
\end{lemma}
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	from := strings.Index(string(d.Text), `\begin{lemma}`)
	to := strings.Index(string(d.Text), `\end{lemma}`) + len(`\end{lemma}`)
	hs := FindHeads(d, NewResolver().Titles, []tex.Region{{From: from, To: to, Statement: true}})
	if len(hs) != 1 || !hs[0].Proof {
		t.Fatalf("the heads are %+v, and there must be one proof", hs)
	}
	if hs := FindHeads(d, NewResolver().Titles, []tex.Region{{From: from, To: to}}); len(hs) != 0 {
		t.Errorf("the heads in a region that is not a statement are %+v, and there must be none", hs)
	}
}

// TestAHeadThatCitesItsSource reads heads that cite a source after the
// number, one with a misspelt title, and heads numbered with letters and
// digits or with a word. Seen in the gold pilot (set X).
// Seen in the gold pilot (set X): a paper checks the assumptions of a cited
// work one by one, each under a head that cites it.
func TestAHeadThatRestatesACitedItem(t *testing.T) {
	src := []byte(`\documentclass{article}
\begin{document}
\section{Assumptions}
\paragraph{\cite[Assumption 2.4]{AB13}:} For $z<1$ the map is continuous.

\paragraph{\cite[Definition 2.5]{AB13} Symmetry of the model.}{\it A model is symmetric.}

{\bf \cite[Assumption 4.1]{AB13}.} The symmetries hold.

\paragraph{\cite[Section 3]{AB13}} explains the rest.
\end{document}
`)
	d, err := tex.ReadFile("paper.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, h := range FindHeads(d, NewResolver().Titles, nil) {
		got = append(got, h.Item+" "+h.Number+" "+h.Name)
	}
	if want := "context/hypothesis  [AB13, Assumption 2.4]|definition  [AB13, Definition 2.5]|context/hypothesis  [AB13, Assumption 4.1]"; strings.Join(got, "|") != want {
		t.Errorf("the heads are %q, and they must be %q", got, want)
	}
}

// Seen in the gold pilot (set X): the subsections "Part (a): ..." divide a
// paper; they are not cases.
func TestASectionThatNamesAPartIsNotAnItem(t *testing.T) {
	src := []byte(`\documentclass{article}
\begin{document}
\subsection{Part (a): The expansion}
We expand.

\subsection{Lemma 3.1}
Each tree is a path.

\paragraph{Case 2:} the tree is empty.
\end{document}
`)
	d, err := tex.ReadFile("paper.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, h := range FindHeads(d, NewResolver().Titles, nil) {
		got = append(got, h.Item+" "+h.Number)
	}
	if want := "statement/lemma 3.1|other/case 2"; strings.Join(got, "|") != want {
		t.Errorf("the heads are %q, and they must be %q", got, want)
	}
}

func TestAHeadThatCitesItsSource(t *testing.T) {
	src := []byte(`\documentclass{article}
\begin{document}
\textbf{Definition 2.1.3.$^{[22]}$} A tree is a connected graph.

\textbf{Defnition 2.1.5.$^{[10,25]}$} A path is a tree.

\textbf{Lemma 2.1.8.$^{[5,8,20]}$} Each path is a graph.

\textbf{Lemma 2.1.9 holds for every tree.} So it does.

\textbf{Theorem BI1.} (See \cite{x}) Each tree is a path.

\textbf{Lemma A2.} Each graph is a graph.

{\bf Challenge 2'} (A prize) Prove it.

{\bf Theorem One.} Each path is a tree.
\end{document}
`)
	d, err := tex.ReadFile("paper.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, h := range FindHeads(d, NewResolver().Titles, nil) {
		got = append(got, h.Item+" "+h.Number)
	}
	if want := "definition 2.1.3|definition 2.1.5|statement/lemma 2.1.8|statement/theorem BI1|statement/lemma A2|problem 2'|statement/theorem One"; strings.Join(got, "|") != want {
		t.Errorf("the heads are %q, and they must be %q", got, want)
	}
}

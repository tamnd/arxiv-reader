package census

import (
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/source"
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

// TestAFormatThatCountsFromZero reads a format with a macro of one argument
// that prints a counter less one. Seen in the gold pilot (set X).
func TestAFormatThatCountsFromZero(t *testing.T) {
	src := []byte(`\documentclass{article}
\def\zerobased#1{\the\numexpr(\arabic{#1})-1\relax}
\newtheorem{thm}{Theorem}[section]
\renewcommand\thesection{\zerobased{section}}
\renewcommand\thethm{\thesection.\zerobased{thm}}
\begin{document}
\section{Zero}
\begin{thm}A.\end{thm}
\begin{thm}B.\end{thm}
\section{One}
\begin{thm}C.\end{thm}
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	r := theorems.NewResolver()
	c := Run(Input{Paper: "test", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles})
	var got []string
	for _, o := range c.Occurrences {
		if o.Type == Env {
			got = append(got, o.Number)
		}
	}
	if want := "0.0 0.1 1.0"; strings.Join(got, " ") != want {
		t.Errorf("the numbers are %q, and they must be %q", got, want)
	}
}

// TestAClassWhoseTheoremsAreSubsections reads a class that makes each theorem
// a subsection, so that the theorems and the subsections share one counter.
// Seen in the gold pilot (set X).
func TestAClassWhoseTheoremsAreSubsections(t *testing.T) {
	cls := []byte(`\LoadClass{article}
\def\state#1{\subsection{\sc#1}\begingroup\em}
\def\endstate{\endgroup\par}
\def\newtheorem#1{\@ifnextchar[{\@newthm{#1}}{\@newthm{#1}[]}}
\def\@newthm#1[#2]#3{\@ifnextchar[{\@@newthm{#1}[#2]{#3}}{\@@newthm{#1}[#2]{#3}[ ]}}
\def\@@newthm#1[#2]#3[#4]{\expandafter\def\csname #1\endcsname{%
\state{#3.}}\expandafter\let\csname end#1\endcsname\endstate}
`)
	main := []byte(`\documentclass{statecls}
% \usepackage{amsthm}
\newtheorem{theorem}{Theorem}[section]
\newtheorem{lemma}{Lemma}[section]
\begin{document}
\section{One}
\section{Two}
\subsection{Setting}
\begin{lemma}A.\end{lemma}
\begin{theorem}B.\end{theorem}
\subsection{More}
\begin{lemma}C.\end{lemma}
\end{document}
`)
	ds, err := tex.Read(&source.Bundle{Tarred: true, Files: []source.File{{Name: "main.tex", Data: main}, {Name: "statecls.cls", Data: cls}}})
	if err != nil {
		t.Fatal(err)
	}
	d := ds[0]
	r := theorems.NewResolver()
	c := Run(Input{Paper: "test", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles})
	var got []string
	for _, o := range c.Occurrences {
		if o.Type == Env {
			got = append(got, o.Number)
		}
	}
	if want := "2.2 2.3 2.5"; strings.Join(got, " ") != want {
		t.Errorf("the numbers are %q, and they must be %q", got, want)
	}
}

// TestTheECompanionStartsTheNumbersAgain reads the \ECSwitch of a class in the
// manner of INFORMS, after which the theorems numbered through are EC.1, EC.2.
// Seen in the gold pilot (set X).
func TestTheECompanionStartsTheNumbersAgain(t *testing.T) {
	cls := []byte(`\LoadClass{article}
\def\TheoremsNumberedThrough{%
\newtheorem{theorem}{Theorem}
\newtheorem{lemma}{Lemma}
\def\ECHowTheorems{\setcounter{theorem}{0}\def\thetheorem{EC.\oldthetheorem}}}
\def\ECSwitch{\clearpage\ECHowTheorems\setcounter{section}{0}}
`)
	main := []byte(`\documentclass{ecclass}
\TheoremsNumberedThrough
\begin{document}
\section{One}
\begin{theorem}A.\end{theorem}
\begin{lemma}B.\end{lemma}
\begin{theorem}C.\end{theorem}
\ECSwitch
\section{Proofs}
\begin{lemma}D.\end{lemma}
\begin{theorem}E.\end{theorem}
\end{document}
`)
	ds, err := tex.Read(&source.Bundle{Tarred: true, Files: []source.File{{Name: "main.tex", Data: main}, {Name: "ecclass.cls", Data: cls}}})
	if err != nil {
		t.Fatal(err)
	}
	d := ds[0]
	r := theorems.NewResolver()
	c := Run(Input{Paper: "test", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles})
	var got []string
	for _, o := range c.Occurrences {
		if o.Type == Env {
			got = append(got, o.Number)
		}
	}
	if want := "1 1 2 EC.1 EC.1"; strings.Join(got, " ") != want {
		t.Errorf("the numbers are %q, and they must be %q", got, want)
	}
}

// TestAnUnnumberedEnvironmentAfterAnEmptyParagraph gives an environment with
// no number of its own the number of the \paragraph{} right before it, as a
// paper that numbers its paragraphs writes. Seen in the gold pilot (set X).
func TestAnUnnumberedEnvironmentAfterAnEmptyParagraph(t *testing.T) {
	src := []byte(`\documentclass{article}
\newenvironment{Proposition}{\smallskip\noindent{\sc Proposition.}}{\bigskip}
\renewcommand{\theparagraph}{\arabic{section}.\arabic{subsection}.\arabic{paragraph}}
\newcommand{\np}{\paragraph}
\begin{document}
\section{One}
\np{}\begin{Proposition}\label{p:a}A.\end{Proposition}
\np{} Some text.
\paragraph{}\begin{Proposition}B.\end{Proposition}
\subsection{Two}
\begin{Proposition}C.\end{Proposition}
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	r := theorems.NewResolver()
	c := Run(Input{Paper: "test", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles})
	var got []string
	for _, o := range c.Occurrences {
		if o.Type == Env {
			got = append(got, o.Number)
		}
	}
	if want := "1.0.1 1.0.3 "; strings.Join(got, " ") != want {
		t.Errorf("the numbers are %q, and they must be %q", got, want)
	}
}

// TestTheAppendixOverridesTheFormatOfTheSection reads \appendix after a
// preamble that sets \thesection, which \appendix sets to letters again. Seen
// in the gold pilot (set X).
func TestTheAppendixOverridesTheFormatOfTheSection(t *testing.T) {
	src := []byte(`\documentclass{article}
\renewcommand*\thesection{\arabic{section}}
\newtheorem{theorem}{Theorem}[section]
\begin{document}
\section{One}
\begin{theorem}A.\end{theorem}
\appendix
\section{Extra}
\begin{theorem}B.\end{theorem}
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	r := theorems.NewResolver()
	c := Run(Input{Paper: "test", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles})
	var got []string
	for _, o := range c.Occurrences {
		if o.Type == Env {
			got = append(got, o.Number)
		}
	}
	if want := "1.1 A.1"; strings.Join(got, " ") != want {
		t.Errorf("the numbers are %q, and they must be %q", got, want)
	}
}

// TestACounterWithADigitInItsName numbers \newtheorem{thm0}{Theorem}[section],
// whose counter has a digit in its name. Seen in the gold pilot (set X).
func TestACounterWithADigitInItsName(t *testing.T) {
	src := []byte(`\documentclass{article}
\newtheorem{thm0}{Theorem}[section]
\begin{document}
\section{One}
\begin{thm0}A.\end{thm0}
\begin{thm0}B.\end{thm0}
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	r := theorems.NewResolver()
	c := Run(Input{Paper: "test", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles})
	var got []string
	for _, o := range c.Occurrences {
		if o.Type == Env {
			got = append(got, o.Number)
		}
	}
	if want := "1.1 1.2"; strings.Join(got, " ") != want {
		t.Errorf("the numbers are %q, and they must be %q", got, want)
	}
}

// TestAResetDoesNotNumberWithin reads \@addtoreset, which starts a counter
// again at each section but leaves the section out of its number, and the
// format a class writes right after it, which puts the section in. Seen in the
// gold pilot (set X).
func TestAResetDoesNotNumberWithin(t *testing.T) {
	for _, c := range []struct{ code, want string }{
		{`\@addtoreset{theorem}{section}`, "1 2 1"},
		{`\@addtoreset{theorem}{section}\def\thetheorem{\thesection.\arabic{theorem}}`, "1.1 1.2 2.1"},
	} {
		src := []byte(`\documentclass{article}
\newtheorem{theorem}{Theorem}
\makeatletter` + c.code + `\makeatother
\begin{document}
\section{One}
\begin{theorem}A.\end{theorem}
\begin{theorem}B.\end{theorem}
\section{Two}
\begin{theorem}C.\end{theorem}
\end{document}
`)
		d, err := tex.ReadFile("main.tex", src)
		if err != nil {
			t.Fatal(err)
		}
		r := theorems.NewResolver()
		cs := Run(Input{Paper: "test", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles})
		var got []string
		for _, o := range cs.Occurrences {
			if o.Type == Env {
				got = append(got, o.Number)
			}
		}
		if strings.Join(got, " ") != c.want {
			t.Errorf("%s: the numbers are %q, and they must be %q", c.code, got, c.want)
		}
	}
}

// TestAnIEEEClassNumbersWithinInArabic numbers a theorem within a section in
// a class of the IEEE, whose sections are II, III, as 2.1, as the aux of a
// paper shows. Seen in the gold pilot (set X).
func TestAnIEEEClassNumbersWithinInArabic(t *testing.T) {
	src := []byte(`\documentclass[conference]{IEEEtran}
\renewcommand\thesection{\Roman{section}}
\newtheorem{theorem}{Theorem}[section]
\begin{document}
\section{One}
\section{Two}
\begin{theorem}A.\end{theorem}
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	r := theorems.NewResolver()
	c := Run(Input{Paper: "test", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles})
	var got []string
	for _, o := range c.Occurrences {
		if o.Type == Env {
			got = append(got, o.Number)
		}
	}
	if want := "2.1"; strings.Join(got, " ") != want {
		t.Errorf("the numbers are %q, and they must be %q", got, want)
	}
}

// TestAnEnvironmentWrittenAsTwoCommands counts \definition ... \enddefinition
// as an environment, numbered with the others. Seen in the gold pilot (set X).
func TestAnEnvironmentWrittenAsTwoCommands(t *testing.T) {
	src := []byte(`\documentclass{article}
\newtheorem{definition}{Definition}
\newtheorem{lemma}[definition]{Lemma}
\begin{document}
\definition\label{d:a} A tree is a graph.
\enddefinition
\begin{lemma}B.\end{lemma}
\definition C.\enddefinition

\definition
D, which nothing closes.

\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	r := theorems.NewResolver()
	c := Run(Input{Paper: "test", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles})
	var got []string
	for _, o := range c.Occurrences {
		if o.Type == Env {
			got = append(got, o.Env+" "+o.Number)
		}
	}
	if want := "definition 1|lemma 2|definition 3|definition 4"; strings.Join(got, "|") != want {
		t.Errorf("the occurrences are %q, and they must be %q", got, want)
	}
}

// TestANumberWithinAfterAFormat reads a class that sets \thetheorem with a
// prefix command and then numbers the theorems within the section, which
// writes the format again. Seen in the gold pilot (set X).
func TestANumberWithinAfterAFormat(t *testing.T) {
	cls := []byte(`\LoadClass{article}
\def\prelabel{}
\newtheorem{theorem}{Theorem}
\newtheorem{proposition}[theorem]{Proposition}
\renewcommand{\thetheorem}{\prelabel\arabic{theorem}}
\renewcommand\thesection{\prelabel\arabic{section}}
\numberwithin{theorem}{section}
`)
	main := []byte(`\documentclass{prelabelcls}
\begin{document}
\section{One}
\section{Two}
\begin{proposition}A.\end{proposition}
\begin{theorem}B.\end{theorem}
\end{document}
`)
	ds, err := tex.Read(&source.Bundle{Tarred: true, Files: []source.File{{Name: "main.tex", Data: main}, {Name: "prelabelcls.cls", Data: cls}}})
	if err != nil {
		t.Fatal(err)
	}
	d := ds[0]
	r := theorems.NewResolver()
	c := Run(Input{Paper: "test", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles})
	var got []string
	for _, o := range c.Occurrences {
		if o.Type == Env {
			got = append(got, o.Number)
		}
	}
	if want := "2.1 2.2"; strings.Join(got, " ") != want {
		t.Errorf("the numbers are %q, and they must be %q", got, want)
	}
}

// TestATitleWithItsOwnNumber numbers \newtheorem*{theoA}{Theorem A} as A, and
// strips the dot that a format prints after a number. Seen in the gold pilot
// (set X).
func TestATitleWithItsOwnNumber(t *testing.T) {
	src := []byte(`\documentclass{amsart}
\newtheorem*{theoA}{Theorem A}
\newtheorem*{conjB}{Conjecture 2'}
\newtheorem*{plain}{Theorem}
\newtheorem{definition}{Definition}
\renewcommand{\thedefinition}{S.\arabic{definition}.}
\begin{document}
\begin{theoA}A.\end{theoA}
\begin{conjB}B.\end{conjB}
\begin{plain}C.\end{plain}
\begin{definition}D.\end{definition}
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	r := theorems.NewResolver()
	c := Run(Input{Paper: "test", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles})
	var got []string
	for _, o := range c.Occurrences {
		if o.Type == Env {
			got = append(got, o.Number)
		}
	}
	if want := "A|2'||S.1"; strings.Join(got, "|") != want {
		t.Errorf("the numbers are %q, and they must be %q", got, want)
	}
}

// TestAStyleThatWritesTheNoteForTheNumber numbers a theorem whose style
// writes the note and not the number with the number of the theorem its note
// refers to. Seen in the gold pilot (set X).
func TestAStyleThatWritesTheNoteForTheNumber(t *testing.T) {
	src := []byte(`\documentclass{article}
\usepackage{amsthm}
\newtheorem{thm}{Theorem}[section]
\newtheoremstyle{noteonly}{}{}{\itshape}{}{\bfseries}{.}{ }{\thmname{#1}\thmnote{ #3}}
\theoremstyle{noteonly}
\newtheorem{again}{Theorem}
\begin{document}
\section{One}
\begin{thm}\label{t:main}A.\end{thm}
\begin{thm}B.\end{thm}
\section{Two}
\begin{again}[\ref{t:main}]A again.\end{again}
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	r := theorems.NewResolver()
	c := Run(Input{Paper: "test", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles})
	var got []string
	for _, o := range c.Occurrences {
		if o.Type == Env {
			got = append(got, o.Number)
		}
	}
	if want := "1.1 1.2 1.1"; strings.Join(got, " ") != want {
		t.Errorf("the numbers are %q, and they must be %q", got, want)
	}
}

// TestACommandThatWritesAnEnvironment reads a paper that writes its theorems
// with commands whose bodies are whole environments, and its proofs with a
// command that writes the head before its argument. Seen in the gold pilot
// (set X).
func TestACommandThatWritesAnEnvironment(t *testing.T) {
	src := []byte(`\documentclass{article}
\usepackage{amsthm}
\newtheorem{theorem}{Theorem}[section]
\newtheorem{lemma}[theorem]{Lemma}
\newtheorem{claim}{Claim}[theorem]
\newcommandtwoopt{\theo}[3][][]{
  \begin{theorem}[#1]\label{#2}
    #3
  \end{theorem}}
\newcommand{\lemm}[2]{\begin{lemma}\label{l:#1} #2$\hfill\dashv$\end{lemma}}
\newcommand{\clai}[2][]{\begin{claim}[#1]#2\end{claim}}
\renewcommand{\proof}[1]{\textsc{Proof.} #1$\qed$\\}
\begin{document}
\section{One}
\theo[Main][t:main]{A holds.}
\proof{By \clai{B holds.} and so on.}
\lemm{b}{C holds.}
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	r := theorems.NewResolver()
	c := Run(Input{Paper: "test", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles})
	var got []string
	for _, o := range c.Occurrences {
		switch o.Type {
		case Env:
			got = append(got, o.Env+" "+o.Number+" "+o.Opt+" "+strings.Join(o.Labels, ","))
		case Proof:
			got = append(got, "proof")
		}
	}
	if want := "theorem 1.1 Main t:main|claim 1.1.1  |lemma 1.2  l:b|proof"; strings.Join(got, "|") != want {
		t.Errorf("the occurrences are %q, and they must be %q", got, want)
	}
}

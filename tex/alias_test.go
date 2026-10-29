package tex_test

import (
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/tex"
)

func TestStructureAliases(t *testing.T) {
	src := `\documentclass{amsart}
\newtheorem{lem}{Lemma}
\newtheorem{theorem}{Theorem}
\def\<{\begin}
\def\>{\end}
\newcommand{\bthm}{\begin{theorem}}
\newcommand{\ethm}{\end{theorem}}
\begin{document}
\<{lem} One. \>{lem}
\ifdefined\x
\bthm Two.\ethm
\fi
Three $a \> b$ is a medium space, not an end.
\begin{lem} Four. \end{lem}
\end{document}
`
	d, err := tex.ReadFile("main.tex", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range d.Tree.Nodes {
		if n.Kind != "env" {
			continue
		}
		got = append(got, n.Name+" "+strings.Join(strings.Fields(string(d.Text[n.Inner:n.Close])), " "))
		if p := d.Pos(n.From); n.Name == "theorem" && p.Line != 11 {
			t.Errorf("the theorem is on line %d, want 11", p.Line)
		}
	}
	want := []string{"lem One.", "theorem Two.", "lem Four."}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
	if p := d.Pos(strings.Index(string(d.Text), "Four")); p.Line != 14 {
		t.Errorf("Four is on line %d, want 14", p.Line)
	}
	if !strings.Contains(string(d.Text), `$a \> b$`) {
		t.Errorf("the medium space changed: %s", d.Text)
	}
	if len(d.Conditional) > 0 && !strings.Contains(string(d.Text[d.Conditional[0].From:d.Conditional[0].To]), `\begin{theorem}`) {
		t.Errorf("the conditional region did not move with the text")
	}
	if !strings.HasSuffix(strings.TrimSpace(string(d.Text[:d.End+len(`\end{document}`)])), `\end{document}`) {
		t.Errorf("the end of the body did not move with the text")
	}
}

func TestAnAliasWithTextAroundTheEnd(t *testing.T) {
	src := `\documentclass{amsart}
\usepackage{amsthm}
\def\qed{QED}
\def\prf{\begin{proof}}
\def\eprf{\qed \end{proof}}
\def\two{\begin{proof}\end{proof}}
\begin{document}
\prf One.\eprf
\prf Two.\eprf
\end{document}
`
	d, err := tex.ReadFile("main.tex", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range d.Tree.Nodes {
		if n.Kind == "env" {
			got = append(got, n.Name+" "+strings.Join(strings.Fields(string(d.Text[n.Inner:n.Close])), " "))
		}
	}
	want := []string{"proof One.\\qed", "proof Two.\\qed"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAnAliasThatGivesItsArgumentsToTheEnvironment(t *testing.T) {
	src := `\documentclass{amsart}
\newtheorem{Lem}{Lemma}
\newenvironment{lemma}[1]{\begin{Lem}\label{l:#1}}{\end{Lem}}
\newcommand{\lem}[1]{\begin{lemma}{#1}}
\newcommand{\elem}{\end{lemma}}
\newcommand{\swap}[2]{\begin{lemma}{#2}{#1}}
\newcommand{\opt}[1][x]{\begin{lemma}{#1}}
\begin{document}
\lem{a} One. \elem
\swap{b}{c} Two. \end{lemma}
\end{document}
`
	d, err := tex.ReadFile("main.tex", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range d.Tree.Nodes {
		if n.Kind == "env" {
			got = append(got, n.Name+" "+strings.Join(strings.Fields(string(d.Text[n.Inner:n.Close])), " "))
		}
	}
	want := []string{"lemma {a} One."}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTheArgumentOfACommandThatPrintsNothing(t *testing.T) {
	src := `\documentclass{amsart}
\newtheorem{lemma}{Lemma}
\newcommand{\off}[1]{}
\newcommand{\two}[2]{}
\newcommand{\keep}[1]{#1}
\newcommand{\twice}[1]{}
\renewcommand{\twice}[1]{#1}
\begin{document}
\begin{lemma} One. \end{lemma}
\off{Text. \begin{lemma} Two. \end{lemma}}
\two{a}
{\begin{lemma} Three. \end{lemma}}
\keep{\begin{lemma} Four. \end{lemma}}
\twice{\begin{lemma} Five. \end{lemma}}
\end{document}
`
	d, err := tex.ReadFile("main.tex", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range d.Tree.Nodes {
		if n.Kind == "env" && n.Name == "lemma" {
			got = append(got, strings.Join(strings.Fields(string(d.Text[n.Inner:n.Close])), " "))
		}
	}
	want := []string{"One.", "Four.", "Five."}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
	if strings.Count(string(d.Text), "\n") != strings.Count(src, "\n") {
		t.Errorf("the text has %d lines, and the source has %d", strings.Count(string(d.Text), "\n"), strings.Count(src, "\n"))
	}
}

func TestAnAliasOfMoreThanOneLineKeepsTheLines(t *testing.T) {
	src := `\documentclass{amsart}
\newtheorem{lem}{Lemma}
\newtheorem{rem}{Remark}
\newcommand{\er}{\relax
  \end{rem}}
\def\]{\relax
  \ifmmode \egroup \end{trivlist}
  \else \@badmath \fi}
\begin{document}
\begin{rem} A tree is a graph. \er
\begin{rem} A path is a tree. \er
Each $x$ is \[ x = x \]
\begin{lem} One. \end{lem}
\end{document}
`
	d, err := tex.ReadFile("main.tex", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	rems, lems := 0, 0
	for _, n := range d.Tree.Nodes {
		switch {
		case n.Kind == "env" && n.Name == "rem":
			rems++
		case n.Kind == "env" && n.Name == "lem":
			lems++
			if p := d.Pos(n.From); p.Line != 13 {
				t.Errorf("the lemma is on line %d, want 13", p.Line)
			}
		case n.Kind == "env" && n.Name == "trivlist":
			t.Errorf("\\] is read as the end of a trivlist")
		}
	}
	if rems != 2 || lems != 1 {
		t.Errorf("%d remarks and %d lemmas, want 2 and 1", rems, lems)
	}
}

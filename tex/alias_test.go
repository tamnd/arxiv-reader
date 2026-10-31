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

// A class can redefine \] to close a trivlist of its own, over several lines
// and under \ifmmode. That is no alias: written in for each \], it would add
// lines and an \end the paper does not have. Seen in the gold pilot (set E).
func TestADisplayEndOfAClassIsNoAlias(t *testing.T) {
	src := `\documentclass{amsart}
\usepackage{amsthm}
\def\]{\relax
  \ifmmode \egroup
  \end{trivlist}
  \else \fi}
\begin{document}
\[ a \]
\[ b \]
\begin{proof} One. \end{proof}
\end{document}
`
	d, err := tex.ReadFile("main.tex", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(d.Text[d.Body:]), "trivlist") {
		t.Errorf("\\] was written in as an alias")
	}
	if p := d.Pos(strings.Index(string(d.Text), "One")); p.Line != 10 {
		t.Errorf("One is on line %d, want 10", p.Line)
	}
}

func TestWrappers(t *testing.T) {
	src := `\documentclass{amsart}
\newtheorem{lemma}{Lemma}
\newcommandtwoopt{\lemm}[3][][]{
  \begin{lemma}[#1]\label{#2}
    #3
  \end{lemma}}
\newcommand{\eq}[1]{\begin{align*} #1 \end{align*}}
\begin{document}
\lemm[Small][one]{
One.}
\eq{a=b}
\lemm{Two.}
\end{document}
`
	d, err := tex.ReadFile("main.tex", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	lines := map[string]int{}
	for _, n := range d.Tree.Nodes {
		if n.Kind != "env" {
			continue
		}
		in := strings.Join(strings.Fields(string(d.Text[n.Inner:n.Close])), " ")
		got = append(got, n.Name+" "+in)
		lines[n.Name+" "+in] = d.Pos(n.From).Line
	}
	want := []string{`lemma \label{one} One.`, `lemma Two.`}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
	if lines[want[0]] != 9 || lines[want[1]] != 12 {
		t.Errorf("the lemmas are on lines %v, want 9 and 12", lines)
	}
	if !strings.Contains(string(d.Text), `\eq{a=b}`) {
		t.Errorf("the wrapper of a formula is unwrapped")
	}
}

package theorems

import (
	"bytes"
	"testing"

	"github.com/tamnd/arxiv-reader/source"
	"github.com/tamnd/arxiv-reader/tex"
)

func TestTitledProof(t *testing.T) {
	src := []byte(`\documentclass{article}
\newenvironment{proof}[1][Proof]{\noindent{\bf #1.}}{\hfill$\Box$}
\newenvironment{thm}[1]{\noindent{\bf #1.}\it}{}
\begin{document}
\begin{proof} The case is clear. \end{proof}
\begin{proof}[Proof of Lemma 2] We use induction. \end{proof}
\begin{thm}{Lemma} Each graph is finite. \end{thm}
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	r := NewResolver()
	s := r.Resolve(d)
	p := s.Get("proof")
	if p == nil || !p.Proof || !p.TitleArg || !p.TitleOpt {
		t.Fatalf("proof is %+v, and it must be a titled proof with an optional title", p)
	}
	cases := []struct {
		env, after, title, item string
	}{
		{"proof", `\begin{proof}`, "Proof", "proof"},
		{"proof", `\begin{proof}`, "Proof of Lemma 2", "proof"},
		{"thm", `\begin{thm}`, "Lemma", "statement/lemma"},
	}
	from := 0
	for _, c := range cases {
		i := bytes.Index(d.Text[from:], []byte(c.after))
		if i < 0 {
			t.Fatalf("%s is not in the text", c.after)
		}
		inner := from + i + len(c.after)
		from = inner
		title, item, _ := s.Get(c.env).TitleOf(d.Text, inner, r.Titles)
		if title != c.title || item != c.item {
			t.Errorf("%s: the title is %q and the item is %q, and they must be %q and %q", c.after, title, item, c.title, c.item)
		}
	}
}

func TestAClassThatDeclaresWithItsOwnMacro(t *testing.T) {
	cls := []byte(`\newtheorem{theorem}{Theorem}
\newcommand{\newsiamthm}[2]{\newtheorem{#1}[theorem]{#2}}
\newsiamthm{lemma}{Lemma}
\newsiamthm{proposition}{Proposition}
`)
	main := []byte(`\documentclass{siamart220329}
\begin{document}
\begin{proposition} Each graph is finite. \end{proposition}
\begin{lemma} Each tree is a graph. \end{lemma}
\end{document}
`)
	ds, err := tex.Read(&source.Bundle{Tarred: true, Files: []source.File{{Name: "main.tex", Data: main}, {Name: "siamart220329.cls", Data: cls}}})
	if err != nil || len(ds) == 0 {
		t.Fatalf("the read gave %d documents and the error %v", len(ds), err)
	}
	s := NewResolver().Resolve(ds[0])
	for _, env := range []string{"theorem", "lemma", "proposition"} {
		d := s.Get(env)
		if d == nil || d.Proof {
			t.Errorf("%s is %+v, and it must be a theorem", env, d)
			continue
		}
		if env != "theorem" && d.Counter != "theorem" {
			t.Errorf("%s uses the counter %q, and it must use theorem", env, d.Counter)
		}
	}
}

func TestTheSvprocClassDefinesItsEnvironments(t *testing.T) {
	d, err := tex.ReadFile("main.tex", []byte(`\documentclass{svproc}
\begin{document}
\begin{definition} A graph is a set with a relation. \end{definition}
\begin{example} The empty graph. \end{example}
\begin{proof} Clear. \end{proof}
\end{document}
`))
	if err != nil {
		t.Fatal(err)
	}
	s := NewResolver().Resolve(d)
	for _, env := range []string{"definition", "example", "lemma", "remark"} {
		if e := s.Get(env); e == nil || e.Proof || e.From != "class:svproc" {
			t.Errorf("%s is %+v, and it must come from the class svproc", env, e)
		}
	}
	if p := s.Get("proof"); p == nil || !p.Proof {
		t.Errorf("proof is %+v, and it must be a proof", p)
	}
}

func TestALocalClassThatLoadsAClassOfTheTable(t *testing.T) {
	cls := []byte(`\NeedsTeXFormat{LaTeX2e}
\ProvidesClass{l4dc2026}
\LoadClass[pmlr]{jmlr}
`)
	main := []byte(`\documentclass{l4dc2026}
\begin{document}
\begin{lemma} Each tree is a graph. \end{lemma}
\end{document}
`)
	ds, err := tex.Read(&source.Bundle{Tarred: true, Files: []source.File{{Name: "main.tex", Data: main}, {Name: "l4dc2026.cls", Data: cls}}})
	if err != nil || len(ds) == 0 {
		t.Fatalf("the read gave %d documents and the error %v", len(ds), err)
	}
	s := NewResolver().Resolve(ds[0])
	for _, env := range []string{"theorem", "lemma", "proposition"} {
		if e := s.Get(env); e == nil || e.Proof || e.From != "class:jmlr" {
			t.Errorf("%s is %+v, and it must come from the class jmlr", env, e)
		}
	}
	if e := s.Get("lemma"); e != nil && e.Counter != "theorem" {
		t.Errorf("lemma uses the counter %q, and it must use theorem", e.Counter)
	}
}

func TestTheOptionStandardOfNtheorem(t *testing.T) {
	read := func(opts string) *Set {
		t.Helper()
		d, err := tex.ReadFile("main.tex", []byte(`\documentclass{scrartcl}
\usepackage`+opts+`{ntheorem}
\begin{document}
\begin{remark} A remark. \end{remark}
\end{document}
`))
		if err != nil {
			t.Fatal(err)
		}
		return NewResolver().Resolve(d)
	}
	s := read("[standard,thmmarks]")
	for _, env := range []string{"theorem", "Remark", "remark", "definition"} {
		if e := s.Get(env); e == nil || e.Proof || e.From != "package:ntheorem[standard]" {
			t.Errorf("%s is %+v, and it must come from ntheorem with the option standard", env, e)
		}
	}
	if p := s.Get("proof"); p == nil || !p.Proof {
		t.Errorf("proof is %+v, and it must be a proof", p)
	}
	if e := read("[thmmarks]").Get("remark"); e != nil {
		t.Errorf("remark is %+v, and ntheorem without the option standard does not declare it", e)
	}
}

// TestAWrapperThatCallsTheCommandOfTheEnvironment reads the wrapper of 2503.22636v1, which opens its theorem with \innercustomthm and not with \begin.
func TestAWrapperThatCallsTheCommandOfTheEnvironment(t *testing.T) {
	d, err := tex.ReadFile("main.tex", []byte(`\documentclass{amsart}
\newtheorem{innercustomthm}{Theorem}
\newenvironment{customthm}[1]
  {\renewcommand\theinnercustomthm{#1}\innercustomthm}
  {\endinnercustomthm}
\begin{document}
\begin{customthm}{A} Each fan is Ehrhart. \end{customthm}
\end{document}
`))
	if err != nil {
		t.Fatal(err)
	}
	w := NewResolver().Resolve(d).Get("customthm")
	if w == nil || w.Inner != "innercustomthm" || w.From != "wrapper" || w.Proof {
		t.Fatalf("customthm is %+v, and it must be a wrapper of innercustomthm", w)
	}
}

func TestTitledWithAFixedWord(t *testing.T) {
	src := []byte(`\documentclass{article}
\newcounter{item}
\newenvironment{theo}[1]{\refstepcounter{item}\par{\bf \theitem.\hspace{1em}Theorem#1.}\sl}{\par}
\begin{document}
\begin{theo}{\label{t:a}} Each graph is finite. \end{theo}
\begin{theo}{ (Euler)} Each tree is a graph. \end{theo}
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	r := NewResolver()
	s := r.Resolve(d)
	th := s.Get("theo")
	if th == nil || !th.TitleArg || th.Title != "Theorem" || th.Item != "statement/theorem" || !th.Numbered {
		t.Fatalf("theo is %+v, and it must be a numbered theorem whose argument follows the title", th)
	}
	from := 0
	for range 2 {
		i := bytes.Index(d.Text[from:], []byte(`\begin{theo}`))
		inner := from + i + len(`\begin{theo}`)
		from = inner
		title, item, after := th.TitleOf(d.Text, inner, r.Titles)
		if title != "Theorem" || item != "statement/theorem" || after != inner {
			t.Errorf("the use at %d gives %q, %q and %d, and it must give Theorem, statement/theorem and %d", inner, title, item, after, inner)
		}
	}
}

func TestAWrapperThatWritesTheNumberBeforeTheTitle(t *testing.T) {
	src := []byte(`\documentclass{article}
\newcounter{item}
\newenvironment{lem}{\refstepcounter{item}\par\indent {\bf \theitem.\hspace{1em}Lemma.} \sl}{\par}
\begin{document}
\begin{lem} Each graph is finite. \end{lem}
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	l := NewResolver().Resolve(d).Get("lem")
	if l == nil || l.Title != "Lemma" || l.Item != "statement/lemma" || !l.Numbered {
		t.Fatalf("lem is %+v, and it must be a numbered lemma", l)
	}
}

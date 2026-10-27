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

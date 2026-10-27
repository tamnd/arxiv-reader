package theorems

import (
	"bytes"
	"testing"

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

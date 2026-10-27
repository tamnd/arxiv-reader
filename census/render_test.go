package census

import (
	"testing"

	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/tex"
	"github.com/tamnd/arxiv-reader/theorems"
)

func TestRenderCountsByInnerName(t *testing.T) {
	src := []byte(`\documentclass{article}
\usepackage{amsthm}
\newtheorem{theorem}{Theorem}
\newenvironment{Theorem}{\begin{theorem}}{\end{theorem}}
\newtheorem{lemma}{Lemma}
\begin{document}
\begin{Theorem} A. \end{Theorem}
\begin{Theorem} B. \end{Theorem}
\begin{lemma} C. \end{lemma}
\begin{lemma} D. \end{lemma}
\begin{lemma} E. \end{lemma}
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	r := theorems.NewResolver()
	in := Input{Paper: "2601.00001", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles}
	in.Render = &Render{Theorems: []RenderItem{{Env: "theorem", Number: "1"}, {Env: "theorem", Number: "2"}, {Env: "lemma", Number: "1"}}}
	c := Run(in)
	rc := c.renderCounts()
	if rc.SourceTheorems != 5 || rc.Theorems != 3 || rc.Agree {
		t.Fatalf("the counts are %+v, and the source must have 5 and the render 3", rc)
	}
	if len(rc.Envs) != 1 || rc.Envs[0] != (EnvCount{Env: "lemma", Source: 3, Render: 1}) {
		t.Fatalf("the envs are %+v, and only lemma must differ", rc.Envs)
	}
}

// TestARenderedProofTheoremIsAProof reads a proof that a class declares with \newtheorem, which LaTeXML renders as a theorem, as in math/0009023v3.
func TestARenderedProofTheoremIsAProof(t *testing.T) {
	p := &extract.Paper{Sections: []extract.Section{{Blocks: []extract.Block{
		{Kind: extract.KindTheorem, Env: "theorem", Tag: "1.1"},
		{Kind: extract.KindTheorem, Env: "proof"},
		{Kind: extract.KindProof},
	}}}}
	r := FromRender(p)
	if len(r.Theorems) != 1 || r.Proofs != 2 {
		t.Fatalf("the render has %d theorems and %d proofs, want 1 and 2", len(r.Theorems), r.Proofs)
	}
}

// TestTheProofCommandCountsAsARenderedProof reads proofs that start with the command \proof of amsthm, as in 2507.13548v1. LaTeXML renders them as proofs, and it renders a head "Proof." as text.
func TestTheProofCommandCountsAsARenderedProof(t *testing.T) {
	src := []byte(`\documentclass{article}
\usepackage{amsthm}
\newtheorem{lemma}{Lemma}
\begin{document}
\begin{lemma} A. \end{lemma}

\proof
The case is clear. \qed

\begin{lemma} B. \end{lemma}

\proof
We use induction. \qed

\begin{lemma} C. \end{lemma}

\noindent Proof. It is trivial. \qed
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	r := theorems.NewResolver()
	in := Input{Paper: "2601.00001", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles}
	in.Render = &Render{Theorems: []RenderItem{{Env: "lemma", Number: "1"}, {Env: "lemma", Number: "2"}, {Env: "lemma", Number: "3"}}, Proofs: 2}
	rc := Run(in).renderCounts()
	if rc.SourceProofs != 2 || !rc.Agree {
		t.Fatalf("the counts are %+v, and the source must have the 2 proofs of the command", rc)
	}
}

package census

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/source"
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

// TestAWrapperWithATextHeadIsNotInTheRender reads the proof of 2405.09294v1, which a style file defines with \newenvironment and a bold head. LaTeXML renders it as a paragraph that starts with the head, so the render has no proof.
func TestAWrapperWithATextHeadIsNotInTheRender(t *testing.T) {
	src := []byte(`\documentclass{article}
\newtheorem{lemma}{Lemma}
\newenvironment{proof}{\vskip1ex\noindent{\bf Proof:}\hskip.5em}{\hfill$\Box$}
\begin{document}
\begin{lemma} A. \end{lemma}
\begin{proof} The case is clear. \end{proof}
\begin{lemma} B. \end{lemma}
\begin{proof} We use induction. \end{proof}
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	r := theorems.NewResolver()
	in := Input{Paper: "2601.00001", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles}
	if p := in.Set.Get("proof"); p == nil || !p.Proof || p.From != "wrapper" {
		t.Fatalf("proof is %+v, and it must be a wrapper", p)
	}
	in.Render = &Render{Theorems: []RenderItem{{Env: "lemma", Number: "1"}, {Env: "lemma", Number: "2"}}}
	rc := Run(in).renderCounts()
	if rc.SourceProofs != 0 || rc.SourceTheorems != 2 || !rc.Agree {
		t.Fatalf("the counts are %+v, and the proofs of the wrapper must not count", rc)
	}
}

// TestAWrapperOfALocalClassIsInTheRender reads the proof of 2306.00590v2, which the local class sigma.cls defines again. LaTeXML does not read the class, so it renders the proofs with its binding of amsthm.
func TestAWrapperOfALocalClassIsInTheRender(t *testing.T) {
	cls := []byte(`\RequirePackage{amsthm}
\renewenvironment{proof}[1][\proofname]{\par\noindent{\bf #1.}}{\qed}
`)
	main := []byte(`\documentclass{sigma}
\newtheorem{lemma}{Lemma}
\begin{document}
\begin{lemma} A. \end{lemma}
\begin{proof} The case is clear. \end{proof}
\end{document}
`)
	ds, err := tex.Read(&source.Bundle{Tarred: true, Files: []source.File{{Name: "main.tex", Data: main}, {Name: "sigma.cls", Data: cls}}})
	if err != nil || len(ds) == 0 {
		t.Fatalf("the read gave %d documents and the error %v", len(ds), err)
	}
	r := theorems.NewResolver()
	in := Input{Paper: "2601.00001", Version: 1, Doc: ds[0], Set: r.Resolve(ds[0]), Titles: r.Titles}
	in.Render = &Render{Theorems: []RenderItem{{Env: "lemma", Number: "1"}}, Proofs: 1}
	if rc := Run(in).renderCounts(); rc.SourceProofs != 1 || !rc.Agree {
		t.Fatalf("the counts are %+v, and the proof of the class must count", rc)
	}
}

// TestAProofThatThePaperDefinesAgainIsNotInTheRender reads the proofs of 2602.07485v1 and 2606.23841v1. The class or amsthm declares proof, and the paper defines it again with a head of text, so the render has no proof.
func TestAProofThatThePaperDefinesAgainIsNotInTheRender(t *testing.T) {
	for _, tc := range []struct {
		name, preamble string
		proofs         int
	}{
		{"def", `\documentclass{amsart}
\def\proof{{\it Proof.}\ }
\def\endproof{\hfill $\Box$\par\vskip3mm}`, 0},
		{"renewenvironment", `\documentclass{article}
\usepackage{amsthm}
\renewenvironment{proof}[1]{\par\noindent{\itshape #1}~}{\par\medskip}`, 0},
		{"let", `\documentclass{amsart}
\let\oldproof\proof
\def\proof{\oldproof\small}
\def\endproof{\qed}`, 2},
		{"class", `\documentclass{amsart}`, 2},
	} {
		src := []byte(tc.preamble + `
\newtheorem{lemma}{Lemma}
\begin{document}
\begin{lemma} A. \end{lemma}
\begin{proof} The case is clear. \end{proof}
\begin{lemma} B. \end{lemma}
\begin{proof} We use induction. \end{proof}
\end{document}
`)
		d, err := tex.ReadFile("main.tex", src)
		if err != nil {
			t.Fatal(err)
		}
		r := theorems.NewResolver()
		in := Input{Paper: "2601.00001", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles}
		in.Render = &Render{Theorems: []RenderItem{{Env: "lemma", Number: "1"}, {Env: "lemma", Number: "2"}}, Proofs: tc.proofs}
		rc := Run(in).renderCounts()
		if rc.SourceProofs != tc.proofs || rc.SourceTheorems != 2 || !rc.Agree {
			t.Errorf("%s: the counts are %+v, want %d source proofs", tc.name, rc, tc.proofs)
		}
	}
}

// TestAProofOfALocalStyleIsInTheRender reads the proof of 2508.01642v1, which the local file jr.sty defines with the code of amsthm. The render has the proofs.
func TestAProofOfALocalStyleIsInTheRender(t *testing.T) {
	sty := []byte(`\newenvironment{proof}[1][Proof]{\par\normalfont\trivlist\item[\hskip\labelsep\itshape #1.]}{\endtrivlist}
`)
	main := []byte(`\documentclass{amsart}
\usepackage{jr}
\newtheorem{lemma}{Lemma}
\begin{document}
\begin{lemma} A. \end{lemma}
\begin{proof} The case is clear. \end{proof}
\end{document}
`)
	ds, err := tex.Read(&source.Bundle{Tarred: true, Files: []source.File{{Name: "main.tex", Data: main}, {Name: "jr.sty", Data: sty}}})
	if err != nil || len(ds) == 0 {
		t.Fatal(err)
	}
	r := theorems.NewResolver()
	in := Input{Paper: "2508.01642", Version: 1, Doc: ds[0], Set: r.Resolve(ds[0]), Titles: r.Titles}
	in.Render = &Render{Theorems: []RenderItem{{Env: "lemma", Number: "1"}}, Proofs: 1}
	if rc := Run(in).renderCounts(); rc.SourceProofs != 1 || !rc.Agree {
		t.Fatalf("the counts are %+v, and the proof of jr.sty must count", rc)
	}
}

// TestAnEnvironmentThatTheRenderDoesNotKnow reads 2604.07243v3, whose theorems the local class jgcc.cls declares. LaTeXML does not read the class, so the render prints {lem} as an error and has no theorem.
func TestAnEnvironmentThatTheRenderDoesNotKnow(t *testing.T) {
	r := FromRender(&extract.Paper{Faults: []extract.Fault{{Where: "S1", Text: "{lem}"}, {Where: "S1", Text: `\address`}, {Where: "S2", Text: "{lem}"}}})
	if len(r.Undefined) != 1 || !r.Undefined["lem"] {
		t.Fatalf("the undefined environments are %v, want lem", r.Undefined)
	}
	src := []byte(`\documentclass{amsart}
\newtheorem{lem}{Lemma}
\newtheorem{thm}{Theorem}
\begin{document}
\begin{lem} A. \end{lem}
\begin{lem} B. \end{lem}
\begin{thm} C. \end{thm}
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	res := theorems.NewResolver()
	in := Input{Paper: "2604.07243", Version: 3, Doc: d, Set: res.Resolve(d), Titles: res.Titles}
	r.Theorems = []RenderItem{{Env: "thm", Number: "1"}}
	in.Render = r
	if rc := Run(in).renderCounts(); rc.SourceTheorems != 1 || !rc.Agree {
		t.Fatalf("the counts are %+v, and the lemmas must not count", rc)
	}
}

// TestATheoremOfTcolorboxIsNotInTheRender reads 2508.01642v1. LaTeXML draws each box of \newtcbtheorem as a picture, so the render has no theorem for it.
func TestATheoremOfTcolorboxIsNotInTheRender(t *testing.T) {
	src := []byte(`\documentclass{article}
\usepackage{tcolorbox}
\tcbuselibrary{theorems}
\newtheorem{lemma}{Lemma}
\newtcbtheorem{Technical}{Technical Discussion}{}{Technical}
\begin{document}
\begin{Technical}{Bayes}{bayes} A. \end{Technical}
\begin{Technical}{Price}{price} B. \end{Technical}
\begin{lemma} C. \end{lemma}
\end{document}
`)
	d, err := tex.ReadFile("main.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	r := theorems.NewResolver()
	in := Input{Paper: "2508.01642", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles}
	if t2 := in.Set.Get("Technical"); t2 == nil || t2.Form != "newtcbtheorem" {
		t.Fatalf("Technical is %+v", t2)
	}
	in.Render = &Render{Theorems: []RenderItem{{Env: "lemma", Number: "1"}}}
	if rc := Run(in).renderCounts(); rc.SourceTheorems != 1 || !rc.Agree {
		t.Fatalf("the counts are %+v, and the boxes must not count", rc)
	}
}

// TestARenderThatStopsEarlyIsNotAWitness reads a paper like math/0003117v2,
// whose render stops in section 1 and has 2 of the 278 theorems of the source.
// The render is cut short, so it gives no D08 and no numbers. A render with
// the same few theorems and the full text is a D08.
func TestARenderThatStopsEarlyIsNotAWitness(t *testing.T) {
	var b strings.Builder
	b.WriteString("\\documentclass{article}\n\\newtheorem{lemma}{Lemma}\n\\begin{document}\n")
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&b, "\\section{Part %d}\n%s\n\\begin{lemma} Each step holds. \\end{lemma}\n", i, strings.Repeat("The text of the part goes on. ", 20))
	}
	b.WriteString("\\end{document}\n")
	d, err := tex.ReadFile("main.tex", []byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	r := theorems.NewResolver()
	in := Input{Paper: "2601.00001", Version: 1, Doc: d, Set: r.Resolve(d), Titles: r.Titles}
	in.Render = &Render{Theorems: []RenderItem{{Env: "lemma", Number: "1"}, {Env: "lemma", Number: "2"}}, Text: 600}
	c := Run(in)
	rc := c.renderCounts()
	if !rc.Truncated || rc.Agree || hasD08(c.Findings) {
		t.Fatalf("the counts are %+v and the findings %v, and the render is cut short", rc, c.Findings)
	}
	in.Render.Text = 12000
	c = Run(in)
	if rc := c.renderCounts(); rc.Truncated || !hasD08(c.Findings) {
		t.Fatalf("the counts are %+v and the findings %v, and a render with the full text is a witness", rc, c.Findings)
	}
}

func hasD08(fs []Finding) bool {
	for _, f := range fs {
		if f.Rule == "D08" {
			return true
		}
	}
	return false
}

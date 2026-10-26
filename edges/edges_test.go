package edges_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/analysis"
	"github.com/tamnd/arxiv-reader/edges"
	"github.com/tamnd/arxiv-reader/source"
)

const preamble = `\documentclass{article}
\usepackage{amsthm}
\newtheorem{theorem}{Theorem}
\newtheorem{lemma}{Lemma}
\newtheorem{proposition}{Proposition}
\newtheorem{definition}{Definition}
\newtheorem{remark}{Remark}
\newtheorem{assumption}{Assumption}
\begin{document}
`

func run(t *testing.T, body string) *analysis.Result {
	t.Helper()
	b := &source.Bundle{Files: []source.File{{Name: "main.tex", Data: []byte(preamble + body + "\n\\end{document}\n")}}}
	res, err := analysis.Analyze(b, analysis.Paper{ID: "test", Version: 1, Extractor: "test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// line is one edge as "from predicate to method confidence", with the part
// and the flags when the edge has them.
func line(e edges.Edge) string {
	s := fmt.Sprintf("%s %s %s %s %s", strings.TrimPrefix(e.From, "test#"), e.Predicate, strings.TrimPrefix(e.To, "test#"), e.Method, e.Confidence)
	if e.Part != "" {
		s += " part=" + e.Part
	}
	for _, f := range e.Flags {
		s += " " + f
	}
	return s
}

func lines(res *analysis.Result) []string {
	var out []string
	for _, e := range res.Edges {
		out = append(out, line(e))
	}
	return out
}

func want(t *testing.T, res *analysis.Result, wants ...string) {
	t.Helper()
	got := lines(res)
	for _, w := range wants {
		if !slices.Contains(got, w) {
			t.Errorf("want edge %q, got:\n%s", w, strings.Join(got, "\n"))
		}
	}
}

func not(t *testing.T, res *analysis.Result, nots ...string) {
	t.Helper()
	got := lines(res)
	for _, n := range nots {
		if slices.Contains(got, n) {
			t.Errorf("want no edge %q", n)
		}
	}
}

func TestARefInAProofIsAUse(t *testing.T) {
	res := run(t, `
\begin{lemma}\label{l1} A. \end{lemma}
\begin{theorem}\label{t1} B. \end{theorem}
\begin{proof} By Lemma~\ref{l1}, B holds. \end{proof}
\begin{remark} See Theorem \ref{t1} for more. \end{remark}
`)
	want(t, res,
		"proof-1 proves theorem-1 attach certain",
		"proof-1 uses lemma-1 ref certain",
		"remark-1 refers-to theorem-1 ref certain",
	)
	not(t, res, "proof-1 uses theorem-1 ref certain")
}

func TestATextReferenceResolvesByTheNumber(t *testing.T) {
	res := run(t, `
\begin{lemma} A. \end{lemma}
\begin{lemma} B. \end{lemma}
\begin{lemma} C. \end{lemma}
\begin{proposition} D. \end{proposition}
\begin{theorem} E. \end{theorem}
\begin{proof} Using Lemmas 1--3 and Proposition 1(b), E follows. \end{proof}
`)
	want(t, res,
		"proof-1 uses lemma-1 text high",
		"proof-1 uses lemma-2 text high",
		"proof-1 uses lemma-3 text high",
		"proof-1 uses proposition-1 text high part=(b)",
	)
}

func TestAWrongTitleIsMedium(t *testing.T) {
	res := run(t, `
\begin{lemma} A. \end{lemma}
\begin{lemma} B. \end{lemma}
\begin{theorem} C. \end{theorem}
\begin{proof} By Proposition 2, C holds. \end{proof}
`)
	want(t, res, "proof-1 uses lemma-2 text medium title-mismatch")
}

func TestACitationWithALocatorIsACitedResult(t *testing.T) {
	res := run(t, `
\begin{theorem} A. \end{theorem}
\begin{proof} This follows from \cite[Theorem 4.1]{Ser} and \cite{Bou}. Theorem 2 of \cite{Weil} gives the rest. \end{proof}
`)
	want(t, res,
		"proof-1 cites-result bib:Ser cite high part=Theorem 4.1",
		"proof-1 uses bib:Bou cite medium",
		"proof-1 cites-result bib:Weil cite high part=Theorem 2",
	)
	not(t, res, "proof-1 uses theorem-2 text high")
}

func TestAReferenceByPosition(t *testing.T) {
	res := run(t, `
\begin{lemma} A. \end{lemma}
\begin{remark} R. \end{remark}
\begin{theorem} B. \end{theorem}
\begin{proof} By the previous lemma, B holds. \end{proof}
`)
	want(t, res, "proof-1 uses lemma-1 position medium")
}

func TestATermGoesToItsDefinition(t *testing.T) {
	res := run(t, `
\begin{definition} A space is \emph{tame} if it is small. \end{definition}
\begin{theorem} Each tame space is compact. \end{theorem}
`)
	want(t, res, "theorem-1 uses definition-1 term medium")
}

func TestAStandingAssumptionHoldsItsSection(t *testing.T) {
	res := run(t, `
\section{One}
\begin{assumption} Throughout this section, $X$ is smooth. \end{assumption}
\begin{lemma} A. \end{lemma}
\section{Two}
\begin{lemma} B. \end{lemma}
`)
	want(t, res, "lemma-1 uses assumption-1 scope high")
	not(t, res, "lemma-2 uses assumption-1 scope high")
}

func TestAForwardReferenceIsNotAUse(t *testing.T) {
	res := run(t, `
\begin{lemma} A. \end{lemma}
\begin{proof} We use Theorem~\ref{t} below only as a remark. \end{proof}
\begin{theorem}\label{t} B. \end{theorem}
`)
	want(t, res, "proof-1 refers-to theorem-1 ref certain")
}

func TestACycleIsFlagged(t *testing.T) {
	res := run(t, `
\begin{lemma}\label{a} A. \end{lemma}
\begin{proof} By Lemma \ref{b}. \end{proof}
\begin{lemma}\label{b} B. \end{lemma}
\begin{proof} By Lemma \ref{a}. \end{proof}
`)
	if len(res.Cycles) != 1 || !slices.Equal(res.Cycles[0], []string{"lemma-1", "lemma-2"}) {
		t.Fatalf("cycles %v", res.Cycles)
	}
	want(t, res, "proof-1 uses lemma-2 ref certain cycle", "proof-2 uses lemma-1 ref certain cycle")
}

func TestAnEquationLabelIsAPart(t *testing.T) {
	res := run(t, `
\begin{lemma} We have \begin{equation}\label{eq:k} x = y. \end{equation} \end{lemma}
\begin{theorem} B. \end{theorem}
\begin{proof} By \eqref{eq:k}, B holds. \end{proof}
`)
	want(t, res, "proof-1 uses lemma-1 ref certain part=eq:k")
}

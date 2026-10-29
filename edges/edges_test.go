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
	return runWith(t, "", body)
}

// runWith runs a paper whose preamble also has the definitions defs.
func runWith(t *testing.T, defs, body string) *analysis.Result {
	t.Helper()
	pre := strings.Replace(preamble, "\\begin{document}", defs+"\n\\begin{document}", 1)
	b := &source.Bundle{Files: []source.File{{Name: "main.tex", Data: []byte(pre + body + "\n\\end{document}\n")}}}
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
	want(t, res, "proof-1 refers-to lemma-2 text medium title-mismatch")
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

// A paper of 1999 (math/9902147) opens its lemmas through a wrapper whose
// begin code sets the label, and refers to them through a command that adds
// the prefix of the label.
func TestAWrapperLabelAndAReferenceCommand(t *testing.T) {
	res := runWith(t, `
\newenvironment{lem}[1]{\begin{lemma}\label{l:#1}}{\end{lemma}}
\newcommand{\lm}[1]{\begin{lem}{#1}}
\newcommand{\elm}{\end{lem}}
\newcommand{\refl}[1]{Lemma~\ref{l:#1}}
\newcommand{\refls}[2]{Lemmas~\ref{l:#1} and~\ref{l:#2}}
`, `
\lm{first} A. \elm
\lm{second} B. \elm
\begin{theorem} C. \end{theorem}
\begin{proof} By \refls{first}{second}, and again by \refl{first}, C holds. \end{proof}
`)
	var labels []string
	for _, o := range res.Objects {
		if o.Path == "source" && o.Kind == "statement" && o.Subkind == "lemma" {
			labels = append(labels, o.Label)
		}
	}
	if !slices.Equal(labels, []string{"l:first", "l:second"}) {
		t.Fatalf("lemma labels %v", labels)
	}
	want(t, res, "proof-1 uses lem-1 ref certain", "proof-1 uses lem-2 ref certain")
}

// An equation of the argument of a proof, cited in a later proof, is not an
// edge: a proof is not a result that another item uses. Seen in the gold
// pilot (set E).
func TestAnEquationOfAProofIsNotAUse(t *testing.T) {
	res := run(t, `
\begin{lemma} A. \end{lemma}
\begin{proof} We have \begin{equation}\label{eq:p} x = y. \end{equation} \end{proof}
\begin{theorem} B. \end{theorem}
\begin{proof} By \eqref{eq:p}, B holds. \end{proof}
`)
	not(t, res, "proof-2 uses proof-1 ref certain part=eq:p", "proof-2 uses proof-1 ref certain")
	for _, e := range res.Edges {
		if strings.HasSuffix(e.From, "proof-2") && strings.HasSuffix(e.To, "proof-1") {
			t.Errorf("want no edge from proof-2 to proof-1, got %s", line(e))
		}
	}
}

// In a proof a term is a mention of its definition, and a use only when the
// sentence appeals to the definition. Seen in the gold pilot (set E).
func TestATermInAProofIsAUseByDefinitionOnly(t *testing.T) {
	res := run(t, `
\begin{definition} A space is \emph{tame} if it is small. \end{definition}
\begin{theorem} Each small space is compact. \end{theorem}
\begin{proof} Let $X$ be a tame space. It is compact. \end{proof}
\begin{lemma} A union of two small spaces is small. \end{lemma}
\begin{proof} By definition, tame spaces are small, so the union is small. \end{proof}
`)
	want(t, res, "proof-1 refers-to definition-1 term medium", "proof-2 uses definition-1 term medium")
}

// A reference to the result of another work is a citation, in each of its
// forms, and the head of a case is not a reference to it. A case and a
// result named by "the following" are not used. Seen in the gold pilot (set
// E).
func TestTextReferencesThatAreNotUses(t *testing.T) {
	res := run(t, `
\begin{lemma} A. \end{lemma}
\begin{theorem} B. \end{theorem}
\begin{proof} By \cite{k}, Lemma 1, and Theorem 1 and Lemma 1 of \cite{m}, B holds.
We first prove the following lemma.
\end{proof}
\begin{lemma} C. \end{lemma}
\begin{proof} D. \end{proof}
`)
	not(t, res, "proof-1 uses lemma-1 text high", "proof-1 uses theorem-1 text high", "proof-1 uses lemma-2 position medium")
}

// A proof with no mark at its end ends with the environment that holds it, so
// the text of the next part is not its argument. Seen in the gold pilot (set
// E).
func TestAProofEndsWithItsEnvironment(t *testing.T) {
	res := run(t, `
\def\proof{{\noindent\it Proof}\ }
\begin{lemma}\label{a} A. \end{lemma}
\begin{subsection}{One}
\begin{theorem} B. \end{theorem}
\proof  B is clear.
\end{subsection}
\begin{subsection}{Two}
By Lemma \ref{a}, the next part starts.
\end{subsection}
`)
	for _, e := range res.Edges {
		if strings.Contains(e.To, "lemma-1") {
			t.Errorf("want no edge to lemma-1, got %s", line(e))
		}
	}
}

// The head of a proof names what it proves, and "this proves", "to complete
// the proof of", "cf. Prop." and a result of a chapter of another work are not
// uses. Seen in the gold pilot (set E).
func TestProofNamesThatAreNotUses(t *testing.T) {
	res := run(t, `
\begin{lemma}\label{a} A. \end{lemma}
\begin{proposition}\label{b} B. \end{proposition}
\begin{proposition}\label{c} C. \end{proposition}
\begin{proof}[Proof of Propositions~\ref{b} and \ref{c}]
The sets are finite (cf. Prop. 2). This also proves Proposition~\ref{c}.
Proposition 2.1 of Chapter 2 of \cite{k} gives the rest.
\end{proof}
\begin{theorem}\label{d} D. \end{theorem}
\begin{proof}[Proof that Lemma \ref{a} implies Theorem \ref{d}]
To complete the proof of Theorem \ref{d}, note that it is clear.
\end{proof}
`)
	want(t, res, "proof-2 uses lemma-1 ref certain")
	for _, e := range res.Edges {
		if e.Predicate == "uses" && !strings.HasSuffix(e.To, "lemma-1") {
			t.Errorf("want no uses edge but to lemma-1, got %s", line(e))
		}
	}
}

// In a proof "(cf. Proposition 1)" after a step is its reason, and "by the lemma
// below" uses the lemma. Seen in the gold pilot (set E).
func TestACfAndAForwardReasonAreUses(t *testing.T) {
	res := run(t, `
\begin{proposition} A. \end{proposition}
\begin{theorem} B. \end{theorem}
\begin{proof} The group is zero (cf. Proposition 1). By the lemma below, B holds. \end{proof}
\begin{lemma}\label{l} C. \end{lemma}
`)
	want(t, res, "proof-1 uses proposition-1 text high", "proof-1 uses lemma-1 position medium")
}

// A manual proof that ends with a written "Q.E.D." ends there, and the text
// after it is not in the proof. Seen in the gold pilot (set E).
func TestAProofEndsWithAWrittenQED(t *testing.T) {
	res := run(t, `
\def\proof{{\noindent\it Proof}\ }
\begin{lemma}\label{a} A. \end{lemma}
\begin{theorem} B. \end{theorem}
\proof  B is clear. Q.E.D.
\vspace{2mm}
By Lemma \ref{a}, the next part starts.
`)
	for _, e := range res.Edges {
		if strings.Contains(e.To, "lemma-1") {
			t.Errorf("want no edge to lemma-1, got %s", line(e))
		}
	}
}

// "Lemma 4.1" when the paper has only a Proposition 4.1, a result named by
// the authors of another work, a result of a supplement and "[22] Theorem C"
// are not uses. Seen in the gold pilot (set E).
func TestResultsOfOtherWorksAreNotUses(t *testing.T) {
	res := run(t, `
\begin{proposition}\label{a} A. \end{proposition}
\begin{theorem}\label{b} B. \end{theorem}
\begin{corollary}\label{c} C. \end{corollary}
\begin{proof}
By Lemma 1, the set is finite. Theorem 1 of Bucher-Connel-Lafont \cite{k}
gives the rest, and Theorem 1 in the supplementary material shows more.
By [22] Theorem 1, it is small.
\end{proof}
`)
	for _, e := range res.Edges {
		if e.Predicate == "uses" && !strings.Contains(e.To, "bib:") {
			t.Errorf("want no uses edge, got %s", line(e))
		}
	}
}

// "Lemma 1 and Theorem 2 are proven" and "we finish to prove Theorem 2"
// name what the proof proves. Seen in the gold pilot (set E).
func TestWhatIsProvenIsNotUsed(t *testing.T) {
	res := run(t, `
\begin{lemma}\label{a} A. \end{lemma}
\begin{theorem}\label{b} B. \end{theorem}
\begin{corollary}\label{c} C. \end{corollary}
\begin{proof}
It is clear. Thus Lemma \ref{a} and
Theorem \ref{b} are proven. So we finish to prove Theorem \ref{b}.
\end{proof}
`)
	for _, e := range res.Edges {
		if e.Predicate == "uses" {
			t.Errorf("want no uses edge, got %s", line(e))
		}
	}
}

// "Is given by the following theorem", "the following theorem gives" and
// "the following theorem of de Jong (\cite{k})" do not use the next theorem;
// "by the following lemma" does. Seen in the gold pilot (set E).
func TestWhatTheFollowingTheoremGives(t *testing.T) {
	res := run(t, `
\begin{lemma}\label{a} A. \end{lemma}
\begin{proof}
The rate is given by the following theorem. The following theorem gives
a bound, and we use the following theorem of de Jong (\cite{k}).
By the following lemma, it is small.
\end{proof}
\begin{theorem}\label{b} B. \end{theorem}
\begin{lemma}\label{c} C. \end{lemma}
`)
	want(t, res, "proof-1 uses lemma-2 position medium")
	for _, e := range res.Edges {
		if e.Predicate == "uses" && strings.HasSuffix(e.To, "theorem-1") {
			t.Errorf("want no uses edge to theorem-1, got %s", line(e))
		}
	}
}

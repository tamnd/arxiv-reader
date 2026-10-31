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

// Seen in the gold pilot (set E): a number written with the title of another
// environment of the same counter is not read.
func TestAWrongTitleIsNotRead(t *testing.T) {
	res := runWith(t, `\newtheorem{prop}[lemma]{Proposition}`, `
\begin{lemma} A. \end{lemma}
\begin{lemma} B. \end{lemma}
\begin{prop} C. \end{prop}
\begin{theorem} D. \end{theorem}
\begin{proof} By Proposition 2, D holds. \end{proof}
`)
	for _, e := range lines(res) {
		if strings.Contains(e, " text ") {
			t.Errorf("a text edge of a wrong title: %s", e)
		}
	}
}

// Seen in the gold pilot (set E): a title whose environment does not share
// the counter of the item with the number is no wrong title of that item.
// "Assumption 2" is not Lemma 2, and "Proposition 2" is not Lemma 2 when the
// two count apart.
func TestAWrongTitleOfAnotherCounterIsNoEdge(t *testing.T) {
	res := run(t, `
\begin{lemma} A. \end{lemma}
\begin{lemma} B. \end{lemma}
\begin{proposition} C. \end{proposition}
\begin{theorem} D. \end{theorem}
\begin{proof} By Assumption 2 and Proposition 2, D holds. \end{proof}
`)
	for _, l := range lines(res) {
		if strings.HasPrefix(l, "proof-1 uses lemma-2") {
			t.Errorf("an edge %s", l)
		}
	}
}

// Seen in the gold pilot (set E): a chapter of a book stands between the
// number and the citation. The number is of the book, not of the paper.
func TestAResultOfAChapterOfABookIsNoTextEdge(t *testing.T) {
	res := run(t, `
\begin{lemma} A. \end{lemma}
\begin{theorem} B. \end{theorem}
\begin{proof} This follows from Lemma 1 of Chapter 3 of
\cite{Book}. By Lemma 1, part 2 in \cite{Other}, we end. \end{proof}
`)
	for _, l := range lines(res) {
		if strings.HasPrefix(l, "proof-1 uses lemma-1") {
			t.Errorf("an edge %s", l)
		}
	}
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

// In a proof, "the previous proposition" is not the one that the proof
// proves. Seen in the gold pilot (set E).
func TestAReferenceByPositionSkipsWhatTheProofProves(t *testing.T) {
	res := runWith(t, `\newtheorem{proposition}{Proposition}`, `
\begin{proposition} A. \end{proposition}
\begin{proof} A holds. \end{proof}
\begin{proposition} B. \end{proposition}
\begin{proof} By the previous proposition, B holds. \end{proof}
`)
	want(t, res, "proof-2 uses proposition-1 position medium")
	not(t, res, "proof-2 uses proposition-2 position medium")
}

// "The following lemma" in a proof is most often an announcement of what
// comes next, not a use of it. Seen in the gold pilot (set E).
func TestAReferenceForwardIsLow(t *testing.T) {
	res := run(t, `
\begin{theorem} B. \end{theorem}
\begin{proof} B follows from the following lemma. \end{proof}
\begin{lemma} A. \end{lemma}
`)
	want(t, res, "proof-1 uses lemma-1 position low")
}

func TestATermGoesToItsDefinition(t *testing.T) {
	res := run(t, `
\begin{definition} A space is \emph{tame} if it is small. \end{definition}
\begin{theorem} Each tame space is compact. \end{theorem}
`)
	want(t, res, "theorem-1 uses definition-1 term low")
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

// Seen in the gold pilot (set E): a proof that relies on an equation of the
// proof of an earlier lemma uses the lemma.
func TestAnEquationOfAnotherProofIsAUseOfWhatItProves(t *testing.T) {
	res := run(t, `\begin{lemma}\label{lem:a}
A statement.
\end{lemma}
\begin{proof}
We have
\begin{equation}\label{eq:step}
x = y.
\end{equation}
\end{proof}
\begin{theorem}
Another statement.
\end{theorem}
\begin{proof}
By (\ref{eq:step}) the claim holds.
\end{proof}`)
	want(t, res, "proof-2 uses lemma-1 ref certain part=eq:step")
	not(t, res, "proof-2 uses proof-1 ref certain part=eq:step")
}

// Seen in the gold pilot (set E): the proof of a claim inside a proof names
// the claim, which is what it proves, and not an edge.
func TestTheProofOfANestedClaimProvesTheClaim(t *testing.T) {
	res := runWith(t, `\newtheorem{claim}{Claim}`, `\begin{lemma}
A statement.
\end{lemma}
\begin{proof}
A step.
\begin{claim}\label{cl:one}
An inner statement.
\end{claim}
\begin{proof}[Proof of Claim~\ref{cl:one}]
The inner step.
\end{proof}
\end{proof}`)
	want(t, res, "proof-2 proves claim-1 attach certain")
	for _, l := range lines(res) {
		if strings.HasPrefix(l, "proof-2 uses") {
			t.Errorf("an edge %s", l)
		}
	}
}

// Seen in the gold pilot (set E): a proof head written by hand names its
// statement with \ref. The proof proves the statement, and the reference in
// the head is no use.
func TestAManualProofHeadWithARefProvesTheStatement(t *testing.T) {
	res := run(t, `\begin{theorem}\label{main}
A statement.
\end{theorem}
\begin{lemma}\label{aux}
A helper.
\end{lemma}

\noindent {\it Proof of Theorem~\ref{main}.} A step. \qed

Some text.`)
	want(t, res, "head-1 proves theorem-1 attach certain")
	for _, l := range lines(res) {
		if strings.HasPrefix(l, "head-1 uses theorem-1") {
			t.Errorf("an edge %s", l)
		}
	}
}

// Seen in the gold pilot (set E): a proof head written by hand ends with
// "Q.E.D." in plain text. The text after it is not a part of the proof.
func TestAPlainQEDEndsAManualProof(t *testing.T) {
	res := run(t, `\begin{theorem}\label{a}
A statement.
\end{theorem}
\begin{theorem}\label{b}
Another statement.
\end{theorem}

\noindent {\it Proof.} A step. Q.E.D.

By Theorem~\ref{a}, we obtain the following.`)
	want(t, res, "head-1 proves theorem-2 attach certain")
	not(t, res, "head-1 uses theorem-1 ref certain")
}

// Seen in the gold pilot (set E): a proof refers to its own cases. A case is
// a part of the proof, so the reference is no use.
func TestAProofThatNamesItsOwnCaseHasNoEdge(t *testing.T) {
	res := runWith(t, `\newtheorem{case}{Case}`, `\begin{theorem}\label{main}
A statement.
\end{theorem}
\begin{proof}
\begin{case}\label{c:one} A step. \end{case}

\begin{case}\label{c:two} Another step. \end{case}

By Theorem~\ref{main}, Cases \ref{c:one} and \ref{c:two} cover it.
\end{proof}`)
	for _, l := range lines(res) {
		if strings.HasPrefix(l, "proof-1 uses") {
			t.Errorf("an edge %s", l)
		}
	}
}

// Seen in the gold pilot (set E): a proof states a claim, proves it, and then
// names it. The claim is a part of the proof, so the reference is no use.
func TestAProofThatNamesItsOwnClaimHasNoEdge(t *testing.T) {
	res := runWith(t, `\newtheorem{claim}{Claim}`, `\begin{lemma}\label{lem:a}
A statement.
\end{lemma}
\begin{proof}
\begin{claim}\label{cl:one}
An inner statement.
\end{claim}
\begin{proof}
The inner step.
\end{proof}
By Claim~\ref{cl:one} and Lemma~\ref{lem:a} we are done.
\end{proof}`)
	not(t, res, "proof-1 uses claim-1 ref certain")
}

// Seen in the gold pilot (set E): a claim numbered in the proof of another
// item, as Claim 2 of \ref{b}, is not the claim with that number elsewhere.
func TestAClaimOfAnotherItemIsNoTextEdge(t *testing.T) {
	res := runWith(t, `\newtheorem{claim}{Claim}`, `
\begin{lemma}\label{a} A. \end{lemma}
\begin{claim} B. \end{claim}
\begin{claim} C. \end{claim}
\begin{theorem}\label{b} D. \end{theorem}
\begin{proof} By Claim 2 of \ref{a}, D holds. \end{proof}
`)
	want(t, res, "proof-1 uses lemma-1 ref certain")
	for _, l := range lines(res) {
		if strings.HasPrefix(l, "proof-1 uses claim-2") {
			t.Errorf("an edge %s", l)
		}
	}
}

// A result of another work is no text edge, whether the citation follows a
// list of results, comes before the title, or the work is named by its
// author. A result written after it of the paper itself still is. Seen in
// the gold pilot (set E).
func TestAResultOfAnotherWorkIsNoTextEdge(t *testing.T) {
	res := runWith(t, `\newtheorem{assumption}{Assumption}`, `
\begin{assumption} A. \end{assumption}
\begin{assumption} B. \end{assumption}
\begin{lemma} C. \end{lemma}
\begin{lemma} D. \end{lemma}
\begin{theorem} E. \end{theorem}
\begin{proof} We need Assumption 1 (a), Assumption 1 (b) and Assumption 2 in \cite{k}.
By (\cite{k}, Lemma 1) and by Lemma 2 of Smith (1990), and by Lemma 1 in the supplementary material, E holds.
Then Lemma 2 of Section 3 gives it again.
\end{proof}
`)
	for _, l := range lines(res) {
		if strings.HasPrefix(l, "proof-1 uses assumption") || strings.HasPrefix(l, "proof-1 uses lemma-1") {
			t.Errorf("an edge %s", l)
		}
	}
	want(t, res, "proof-1 uses lemma-2 text high")
}

// A sentence that closes a proof names what it proves, not a use. Seen in the
// gold pilot (set E).
func TestAClosingSentenceIsNoUse(t *testing.T) {
	res := run(t, `
\begin{lemma}\label{a} A. \end{lemma}
\begin{theorem}\label{b} B. \end{theorem}
\begin{theorem}\label{c} C. \end{theorem}
\begin{proof} By Lemma~\ref{a}, C holds. This also proves Theorem~\ref{b}. \end{proof}
`)
	want(t, res, "proof-1 uses lemma-1 ref certain")
	not(t, res, "proof-1 uses theorem-1 ref certain")
}

// The theoremref package writes \thlabel and \thref. Seen in the gold pilot
// (set E).
func TestAThrefIsAReference(t *testing.T) {
	res := run(t, `
\begin{lemma}\thlabel{a} A. \end{lemma}
\begin{theorem} B. \end{theorem}
\begin{proof} By \thref{a}, B holds. \end{proof}
`)
	want(t, res, "proof-1 uses lemma-1 ref certain")
}

// Seen in the gold pilot (set E): a proof head names two items that the proof
// proves together. The second is not a use, by a reference or by its number,
// in the head or in the body.
func TestAProofHeadOfTwoItemsIsNoUse(t *testing.T) {
	res := run(t, `
\begin{lemma}\label{a} A. \end{lemma}
\begin{proposition}\label{b} B. \end{proposition}
\begin{proposition}\label{c} C. \end{proposition}
\begin{proof}[Proof of Proposition~\ref{b} and Proposition~\ref{c}] By Lemma~\ref{a}, B and C hold. For Proposition~\ref{c}, it is enough to prove C. \end{proof}
\begin{proof}[Proof of Propositions 1 and 2] By Lemma 1, B and C hold. \end{proof}
`)
	want(t, res, "proof-1 uses lemma-1 ref certain", "proof-2 uses lemma-1 text high")
	for _, e := range lines(res) {
		if strings.Contains(e, "uses proposition") {
			t.Errorf("a use of a proposition of the head: %s", e)
		}
	}
}

// Seen in the gold pilot (set E): a sentence of a proof that says what a
// lemma can be replaced by, or that part of a conjecture is proved, is about
// the target and not a step of the argument.
func TestARemarkAboutATargetIsNoUse(t *testing.T) {
	res := run(t, `
\begin{lemma}\label{a} A. \end{lemma}
\begin{lemma}\label{b} B. \end{lemma}
\begin{proposition}\label{c} C. \end{proposition}
\begin{theorem} D. \end{theorem}
\begin{proof} In the general case, Lemma~\ref{a} can be replaced by Lemma~\ref{b}. The analog of Lemma~\ref{a} fails. Thus part (1) of Proposition~\ref{c} is proved. \end{proof}
`)
	want(t, res, "proof-1 refers-to lemma-1 ref certain", "proof-1 refers-to lemma-2 ref certain", "proof-1 refers-to proposition-1 ref certain")
	not(t, res, "proof-1 uses lemma-1 ref certain", "proof-1 uses lemma-2 ref certain", "proof-1 uses proposition-1 ref certain")
}

// Seen in the gold pilot (set E): a proof head can name what the proof uses
// as well as what it proves.
func TestAProofHeadNamesItsUses(t *testing.T) {
	res := run(t, `
\begin{lemma}\label{a} A. \end{lemma}
\begin{proposition}\label{b} B. \end{proposition}
\begin{theorem}\label{c} C. \end{theorem}
\begin{proof}[Proof of Theorem~\ref{c} given Lemma~\ref{a}] By Lemma~\ref{a}, C holds. \end{proof}
\begin{proof}[Proof that Proposition~\ref{b} implies Theorem~\ref{c}] First, Proposition~\ref{b} holds on a larger set. \end{proof}
`)
	want(t, res, "proof-1 uses lemma-1 ref certain", "proof-2 uses proposition-1 ref certain")
	not(t, res, "proof-1 uses theorem-1 ref certain", "proof-2 uses theorem-1 ref certain")
}

// Seen in the gold pilot (set E): in a proof right after its theorem, "the
// above theorem" is the theorem that the proof proves, not the one before it.
// "The previous theorem" is still the one before.
func TestTheAboveTheoremInItsProofIsNoEdge(t *testing.T) {
	res := run(t, `
\begin{theorem} A. \end{theorem}
\begin{theorem} B. \end{theorem}
\begin{proof} We check each case of the above theorem. \end{proof}
\begin{theorem} C. \end{theorem}
\begin{proof} By the previous theorem, C holds. \end{proof}
`)
	want(t, res, "proof-2 uses theorem-2 position medium")
	not(t, res, "proof-1 uses theorem-1 position medium")
}

// Seen in the gold pilot (set E): "as for" names the topic of a sentence and
// not a reason.
func TestAsForIsNoUse(t *testing.T) {
	res := run(t, `
\begin{theorem}\label{a} A. \end{theorem}
\begin{theorem} B. \end{theorem}
\begin{proof} As for the other parts of Theorem~\ref{a}, they are similar. \end{proof}
`)
	want(t, res, "proof-1 refers-to theorem-1 ref certain")
}

// Seen in the gold pilot (set E): later in a sentence, "as for" is "as in".
func TestAsForLaterIsAUse(t *testing.T) {
	res := run(t, `
\begin{theorem}\label{a} A. \end{theorem}
\begin{theorem} B. \end{theorem}
\begin{proof} The proof is exactly as for Theorem~\ref{a}. \end{proof}
`)
	want(t, res, "proof-1 uses theorem-1 ref certain")
}

// Seen in the gold pilot (set E): a proof that says it finishes the proof of
// a theorem proves that theorem too, so its other references to the theorem
// are not uses.
func TestAProofThatFinishesATheoremDoesNotUseIt(t *testing.T) {
	res := run(t, `
\begin{theorem}\label{a} A. \end{theorem}
\begin{lemma} B. \end{lemma}
\begin{proof} A step. This finishes the proof of part (a) of Theorem~\ref{a}. Part (b) follows from the argument of (a) of Theorem~\ref{a}. \end{proof}
`)
	want(t, res, "proof-1 refers-to theorem-1 ref certain")
	not(t, res, "proof-1 uses theorem-1 ref certain")
}

// Seen in the gold pilot (set E): a bold "Case 1." in a proof is an item, and
// a use in it is a use of the proof that holds it.
func TestAUseInACaseIsAUseOfItsProof(t *testing.T) {
	res := run(t, `
\begin{lemma}\label{l1} A. \end{lemma}
\begin{theorem} B. \end{theorem}
\begin{proof} We split in two.

\textbf{Case 1.} By Lemma~\ref{l1}, B holds here.

\textbf{Case 2.} As in Case 1, B holds.
\end{proof}
`)
	want(t, res, "proof-1 uses lemma-1 ref certain")
	for _, l := range lines(res) {
		if strings.HasPrefix(l, "head-") || strings.Contains(l, " head-") {
			t.Errorf("an edge from or to a case: %q", l)
		}
	}
}

// Seen in the gold pilot (set E): "Case 2" in a manual proof names a part
// of the argument, not a result.
func TestACaseNumberIsNoTextEdge(t *testing.T) {
	res := run(t, `
\begin{theorem} B. \end{theorem}
\noindent\textbf{Proof.} We split in two.

\textbf{Case 1.} Here B holds.

\textbf{Case 2.} As in Case 1, B holds. \qed
`)
	for _, l := range lines(res) {
		if strings.Contains(l, " text ") {
			t.Errorf("a text edge to a case: %q", l)
		}
	}
}

// Seen in the gold pilot (set E): "Claim 1" is written afresh in several
// arguments, so "by Claim 1" is not known to be the one outside a proof.
func TestAClaimNumberWrittenTwiceIsNoTextEdge(t *testing.T) {
	res := run(t, `
\begin{definition} A construction.

\textbf{Claim 1:} The points are apart.
\end{definition}
\begin{theorem} B. \end{theorem}
\begin{proof} We argue in steps.

\textbf{Claim 1:} The points are near.

By Claim 1, B holds.
\end{proof}
`)
	for _, l := range lines(res) {
		if strings.Contains(l, " text ") {
			t.Errorf("a text edge to a claim of a number written twice: %q", l)
		}
	}
}

// Seen in the gold pilot (set E): a proof written as \proof{...}, with a
// command whose one argument is the body, ends at the close of the argument,
// and the text after it is not in the proof.
func TestAProofCommandEndsAtItsArgument(t *testing.T) {
	res := runWith(t, `\renewcommand{\proof}[1]{\textsc{Proof.} #1$\qed$\\}`, `
\begin{lemma}\label{l1} A. \end{lemma}
\begin{theorem}\label{t1} B. \end{theorem}
\proof{
  By Lemma~\ref{l1}, B holds.
}

As for the converse, see Lemma~\ref{l1}.

\begin{theorem} C. \end{theorem}
`)
	want(t, res, "head-1 proves theorem-1 attach certain", "head-1 uses lemma-1 ref certain")
	for _, o := range res.Objects {
		if o.Local == "head-1" && (o.Span == nil || o.Span.To > o.Span.From+2) {
			t.Errorf("the proof spans %v", o.Span)
		}
	}
}

// Seen in the gold pilot (set E): an equation in a step of an algorithm is not
// an equation of the step, and one in a case of a proof is an equation of
// the proof, so of what the proof proves.
func TestAnEquationOfAStepIsNotOfTheStep(t *testing.T) {
	res := run(t, `
\begin{theorem}\label{t1} B. \end{theorem}
\begin{proof} We argue by cases.

\textbf{Case 1.} We have
\begin{equation}\label{e1} x = 1. \end{equation}

\textbf{Case 2.} Similar.
\end{proof}

\textit{Step 1:} Set
\begin{equation}\label{e2} y = 2. \end{equation}

\begin{lemma} C. \end{lemma}
\begin{proof} By \eqref{e1} and \eqref{e2}, C holds. \end{proof}
`)
	want(t, res, "proof-2 uses theorem-1 ref certain part=e1")
	for _, l := range lines(res) {
		if strings.Contains(l, " head-") {
			t.Errorf("an edge to a case or a step: %q", l)
		}
	}
}

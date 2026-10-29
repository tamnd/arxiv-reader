package theorems

import (
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/tex"
)

func TestALetterAccentNeedsASpaceOrABrace(t *testing.T) {
	for in, want := range map[string]string{
		`Gar\c{c}on`:   "garçon",
		`Gar\c con`:    "garçon",
		`Example \Hex`: "example",
		`Lemma \cor`:   "lemma",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) is %q, and it must be %q", in, got, want)
		}
	}
}

// Seen in the gold pilot (set X): a Plain TeX paper numbers its items with
// commands, and \Hex starts as the accent \H does.
func TestANumberCommandThatStartsAsAnAccent(t *testing.T) {
	src := []byte(`\documentclass{article}
\def \Hex{1.3}
\def \Hcor{2.11}
\begin{document}
We give an example.
\medbreak
\noindent
{\bf Example \Hex.}\enspace Let $B$ be a space.

{\bf Corollary \Hcor.}\enspace Each space is a space.
\end{document}
`)
	d, err := tex.ReadFile("paper.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, h := range FindHeads(d, NewResolver().Titles, nil) {
		got = append(got, h.Item+" "+h.Number)
	}
	if want := "example 1.3|statement/corollary 2.11"; strings.Join(got, "|") != want {
		t.Errorf("the heads are %q, and they must be %q", got, want)
	}
}

// Seen in the gold pilot (set X): a Plain TeX paper writes \Proof on the
// line after its lemma, and \Proof starts a paragraph itself.
func TestAProofCommandThatStartsAParagraph(t *testing.T) {
	src := []byte(`\documentclass{article}
\def\Proof{\medbreak\noindent{\bf Proof.}\enspace}
\def\qed{\hfill\hbox{Q}}
\begin{document}
{\bf Lemma 1.9.}\enspace\sl Each tree is a path.\rm
\Proof This is immediate.\qed
\medbreak
{\bf Lemma 1.10.}\enspace Each path is a tree.
\end{document}
`)
	d, err := tex.ReadFile("paper.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, h := range FindHeads(d, NewResolver().Titles, nil) {
		got = append(got, h.Item+" "+h.Number)
	}
	if want := "statement/lemma 1.9|proof |statement/lemma 1.10"; strings.Join(got, "|") != want {
		t.Errorf("the heads are %q, and they must be %q", got, want)
	}
}

// Seen in the gold pilot (set X): a head in a group of its own, and a claim
// in sans serif with its proof, each on a new line of the paragraph.
func TestAHeadInAGroupAndASansSerifClaim(t *testing.T) {
	src := []byte(`\documentclass{article}
\begin{document}
Text.

{{\emph{Remark 1:}}} In the high region the terms are small.

We have $x$ by Lemma 2.
\newline\textsf{Claim 1.}
For every $N$ the mesh is small.
\newline\textit{Proof of Claim 1.}
If not, then a set is large.
\end{document}
`)
	d, err := tex.ReadFile("paper.tex", src)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, h := range FindHeads(d, NewResolver().Titles, nil) {
		got = append(got, h.Item+" "+h.Number+" "+h.Form)
	}
	if want := "remark 1 italic|statement/claim 1 sans|proof  italic"; strings.Join(got, "|") != want {
		t.Errorf("the heads are %q, and they must be %q", got, want)
	}
}

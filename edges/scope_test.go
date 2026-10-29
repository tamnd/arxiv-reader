package edges_test

import (
	"testing"

	"github.com/tamnd/arxiv-reader/edges"
)

// A claim numbered again in each proof is named by "Claim 1" in the proof
// that holds it, and a proof that names its own claim does not use it. The
// proof of a claim proves the claim of its own proof. Seen in the gold pilot
// (set E).
func TestAClaimIsNamedInItsOwnProof(t *testing.T) {
	res := run(t, `
\newtheorem{claim}{Claim}
\begin{theorem} A. \end{theorem}
\begin{proof}
\begin{claim} B. \end{claim}
\begin{proof} C. \end{proof}
By Claim 1, A holds.
\end{proof}
\begin{theorem} D. \end{theorem}
\begin{proof}
\setcounter{claim}{0}
\begin{claim} E. \end{claim}
\begin{proof}[Proof of Claim 1] F. \end{proof}
By Claim 1, D holds.
\end{proof}
`)
	want(t, res, "proof-2 proves claim-1 attach certain", "proof-4 proves claim-2 attach certain")
	for _, e := range res.Edges {
		if e.Predicate != edges.Proves {
			t.Errorf("want no edge but proves, got %s", line(e))
		}
	}
}

// "By Claim 1" in a proof, with a claim 1 in another proof, names the claim
// in an item that holds the reference. Seen in the gold pilot (set E).
func TestAClaimNumberIsReadInItsScope(t *testing.T) {
	res := run(t, `
\newtheorem{claim}{Claim}
\begin{theorem} A. \end{theorem}
\begin{proof}
\begin{claim} B. \end{claim}
\begin{proof} C. \end{proof}
The argument goes on for a while here, and it goes on for a long while,
with many words that are only there to make the text long, so that the
reference that comes next is nearer to the claim of the next proof than to
the claim of its own proof, and the words go on and on and on and on and on.
\begin{remark} By Claim 1, A holds. \end{remark}
\end{proof}
\begin{theorem} D. \end{theorem}
\begin{proof}
\setcounter{claim}{0}
\begin{claim} E. \end{claim}
\begin{proof} F. \end{proof}
\end{proof}
`)
	want(t, res, "remark-1 uses claim-1 text high")
	not(t, res, "remark-1 uses claim-2 text high")
}

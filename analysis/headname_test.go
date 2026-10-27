package analysis

import "testing"

func TestSplitHead(t *testing.T) {
	for _, c := range []struct{ opt, name string }{
		{`Hahn--Banach`, `Hahn--Banach`},
		{`The Hahn--Banach theorem`, `The Hahn--Banach theorem`},
		{`Friedman \cite{F}`, `Friedman`},
		{`{\cite[7.0.4]{Sweedler}}`, ``},
		{`See \cite{limani2022abstract}`, ``},
		{`cf.~\cite{Curien_operad, Hyland_elts}`, ``},
		{`{see \cite[Definition 3.2]{NWY}}`, ``},
		{`Sketch of {\cite[Definition~2.18]{ST11}}`, `Sketch`},
		{`Theorem 1 in \cite{Banaji_2020}`, ``},
		{`\cite{Robbins:1971aa}, Lemma 2.3 and 2.4`, ``},
		{`Proof of Theorem~\ref{thm:main}`, ``},
		{`A`, ``},
		{`\app`, ``},
		{`Small characteristic`, `Small characteristic`},
		{`Zorn's lemma, see \cite{Z}`, `Zorn's lemma`},
		{`Main Theorem`, `Main Theorem`},
	} {
		if got, _ := splitHead(c.opt); got != c.name {
			t.Errorf("splitHead(%q) = %q, want %q", c.opt, got, c.name)
		}
	}
}

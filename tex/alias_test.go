package tex_test

import (
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/tex"
)

func TestStructureAliases(t *testing.T) {
	src := `\documentclass{amsart}
\newtheorem{lem}{Lemma}
\newtheorem{theorem}{Theorem}
\def\<{\begin}
\def\>{\end}
\newcommand{\bthm}{\begin{theorem}}
\newcommand{\ethm}{\end{theorem}}
\begin{document}
\<{lem} One. \>{lem}
\ifdefined\x
\bthm Two.\ethm
\fi
Three $a \> b$ is a medium space, not an end.
\begin{lem} Four. \end{lem}
\end{document}
`
	d, err := tex.ReadFile("main.tex", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range d.Tree.Nodes {
		if n.Kind != "env" {
			continue
		}
		got = append(got, n.Name+" "+strings.Join(strings.Fields(string(d.Text[n.Inner:n.Close])), " "))
		if p := d.Pos(n.From); n.Name == "theorem" && p.Line != 11 {
			t.Errorf("the theorem is on line %d, want 11", p.Line)
		}
	}
	want := []string{"lem One.", "theorem Two.", "lem Four."}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
	if p := d.Pos(strings.Index(string(d.Text), "Four")); p.Line != 14 {
		t.Errorf("Four is on line %d, want 14", p.Line)
	}
	if !strings.Contains(string(d.Text), `$a \> b$`) {
		t.Errorf("the medium space changed: %s", d.Text)
	}
	if len(d.Conditional) > 0 && !strings.Contains(string(d.Text[d.Conditional[0].From:d.Conditional[0].To]), `\begin{theorem}`) {
		t.Errorf("the conditional region did not move with the text")
	}
	if !strings.HasSuffix(strings.TrimSpace(string(d.Text[:d.End+len(`\end{document}`)])), `\end{document}`) {
		t.Errorf("the end of the body did not move with the text")
	}
}

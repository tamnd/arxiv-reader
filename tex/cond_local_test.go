package tex

import (
	"strings"
	"testing"
)

func TestDecideLocalReadsThePackageOptions(t *testing.T) {
	sty := `\newif\if@lemmaNoSection{\@lemmaNoSectionfalse}
\DeclareOption{lemma-no-section}{\@lemmaNoSectiontrue}
\newif\if@theoremSection{\@theoremSectionfalse}
\DeclareOption{theorem-section}{\@theoremSectiontrue}
\ProcessOptions
\if@lemmaNoSection
  \newtheorem{lemma}{Lemma}\else
  \newtheorem{lemma}{Lemma}[section]\fi
\if@theoremSection
  \newtheorem{theorem}{Theorem}[section]\else
  \newtheorem{theorem}{Theorem}\fi
`
	main := `\documentclass{article}
\usepackage[theorem-section]{mythm}
\begin{document}
\end{document}
`
	got := string(decideLocal([]byte(main), "mythm.sty", []byte(sty)))
	for _, want := range []string{`\newtheorem{lemma}{Lemma}[section]`, `\newtheorem{theorem}{Theorem}[section]`} {
		if !strings.Contains(got, want) {
			t.Errorf("the style has no %s:\n%s", want, got)
		}
	}
	for _, env := range []string{`\newtheorem{lemma}`, `\newtheorem{theorem}`} {
		if n := strings.Count(got, env); n != 1 {
			t.Errorf("the style declares %s %d times, want once", env, n)
		}
	}
}

// Seen in the gold pilot (set X): the LIPIcs class numbers its theorems
// within the section only under the option numberwithinsect, which sets a flag
// with \let and tests it with \ifx.
func TestDecideLocalReadsAnOptionFlag(t *testing.T) {
	cls := `\DeclareOption{numberwithinsect}{\let\numberwithinsect\relax}
\DeclareOption{cleveref}{\let\usecleveref\relax}
\ProcessOptions
\newtheorem{theorem}{Theorem}
\ifx\numberwithinsect\relax
  \@addtoreset{theorem}{section}
\fi
\ifx\usecleveref\relax
  \newtheorem{lemma}[theorem]{Lemma}
\else
  \newtheorem{lemma}{Lemma}
\fi
`
	for _, c := range []struct {
		opts        string
		reset       bool
		sharedLemma bool
	}{
		{"[a4paper,cleveref]", false, true},
		{"[numberwithinsect]", true, false},
	} {
		main := `\documentclass` + c.opts + `{myclass}
\begin{document}
\end{document}
`
		got := string(decideLocal([]byte(main), "myclass.cls", []byte(cls)))
		if strings.Contains(got, `\@addtoreset{theorem}{section}`) != c.reset {
			t.Errorf("%s: the reset is there: %v, want %v:\n%s", c.opts, !c.reset, c.reset, got)
		}
		if strings.Contains(got, `\newtheorem{lemma}[theorem]{Lemma}`) != c.sharedLemma {
			t.Errorf("%s: the lemma shares the theorem counter: %v, want %v:\n%s", c.opts, !c.sharedLemma, c.sharedLemma, got)
		}
	}
}

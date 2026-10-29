package tex_test

import (
	"errors"
	"testing"

	"github.com/tamnd/arxiv-reader/tex"
)

// TestAFileInPlainTeXHasABody reads a file in Plain TeX, whose text starts
// after the lines of its definitions and ends at \bye. Seen in the gold pilot
// (set X).
func TestAFileInPlainTeXHasABody(t *testing.T) {
	plain := "\\magnification=1200\n\\def\\R{{\\bf R}}\n\\def\\Proof{\\medbreak\\noindent\n  {\\bf Proof.}\\enspace}\n% numbers\n\\def \\ROOTlem{1.6}\n\n{\\bf Theorem 1.} Each $x$ is in $\\R$.\n\\bye\nAfter.\n"
	d, err := tex.ReadFile("paper.tex", []byte(plain))
	if err != nil {
		t.Fatalf("Plain TeX gave %v", err)
	}
	if got := string(d.Text[d.Body:d.End]); got != "{\\bf Theorem 1.} Each $x$ is in $\\R$.\n" {
		t.Errorf("the body is %q", got)
	}
	if m := d.Macros.Commands[`\ROOTlem`]; m == nil || m.Body != "1.6" {
		t.Errorf("the number macro is %+v", m)
	}
	if _, err := tex.ReadFile("paper.tex", []byte("\\magnification=1200\n\\def\\R{{\\bf R}}\n\\bye\n")); !errors.Is(err, tex.ErrPlainTeX) {
		t.Errorf("Plain TeX with no text gave %v, want %v", err, tex.ErrPlainTeX)
	}
	latex := "\\documentclass{article}\n% \\bye is a word of Plain TeX\nNo body.\n"
	if _, err := tex.ReadFile("paper.tex", []byte(latex)); !errors.Is(err, tex.ErrNoDocument) {
		t.Errorf("LaTeX with no body gave %v, want %v", err, tex.ErrNoDocument)
	}
}

// TestAnExcludeCommentAfterAPercentSign reads 2404.06445v3, whose preamble has the line % \excludecomment{proof}.
func TestAnExcludeCommentAfterAPercentSign(t *testing.T) {
	got := tex.Excluded([]byte("% \\excludecomment{proof}\n\\excludecomment{answer}\nThe rate is 5\\% \\excludecomment{hint}\n"))
	if len(got) != 2 || !got["answer"] || !got["hint"] || got["proof"] {
		t.Fatalf("got %v, want answer and hint", got)
	}
}

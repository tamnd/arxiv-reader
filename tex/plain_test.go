package tex_test

import (
	"errors"
	"testing"

	"github.com/tamnd/arxiv-reader/tex"
)

func TestAFileInPlainTeXSaysSo(t *testing.T) {
	plain := "\\magnification=1200\n\\def\\R{{\\bf R}}\n{\\bf Theorem 1.} Each $x$ is in $\\R$.\n\\bye\n"
	if _, err := tex.ReadFile("paper.tex", []byte(plain)); !errors.Is(err, tex.ErrPlainTeX) {
		t.Errorf("Plain TeX gave %v, want %v", err, tex.ErrPlainTeX)
	}
	latex := "\\documentclass{article}\n% \\bye is a word of Plain TeX\nNo body.\n"
	if _, err := tex.ReadFile("paper.tex", []byte(latex)); !errors.Is(err, tex.ErrNoDocument) {
		t.Errorf("LaTeX with no body gave %v, want %v", err, tex.ErrNoDocument)
	}
}

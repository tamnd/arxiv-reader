package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// submissionOf writes a directory of TeX files the way a submission arrives.
func submissionOf(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const paper = `\documentclass{article}
\usepackage{tikz}
\newcommand{\myblue}{blue!40}
\begin{document}
\begin{tikzpicture}\draw (0,0);\end{tikzpicture}
\input{sections/one}
\end{document}
`

const included = `A section that begins no document of its own.
\begin{tikzpicture}\draw (1,1);\end{tikzpicture}
`

// An author with fifty figures keeps them in fifty files, and a run that read
// the main document alone would find none of them.
func TestADrawingInAnIncludedFileIsFound(t *testing.T) {
	dir := submissionOf(t, map[string]string{"ms.tex": paper, "sections/one.tex": included})
	found, preamble, err := drawings(dir, "ms.tex")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("found %d drawings across two files", len(found))
	}
	// The main document first and then the rest by name, which is an order
	// rather than the order.
	if found[0].file != "ms.tex" || found[1].file != "sections/one.tex" {
		t.Errorf("the drawings came out of %s and %s", found[0].file, found[1].file)
	}
	// An included file has no preamble of its own, and the macros a drawing uses
	// were defined in the document that includes it.
	if !strings.Contains(preamble, `\newcommand{\myblue}`) {
		t.Errorf("the preamble came out %q", preamble)
	}
}

// A submission read out of a directory has nobody to tell it which file the
// paper starts from, so it is the one with a class and a body.
func TestTheMainDocumentIsFoundWithoutBeingNamed(t *testing.T) {
	dir := submissionOf(t, map[string]string{"ms.tex": paper, "sections/one.tex": included})
	_, preamble, err := drawings(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preamble, `\usepackage{tikz}`) {
		t.Errorf("the preamble came out %q, so the main document was not found", preamble)
	}
}

// A drawing is a few lines in the middle of a file and not a file, so the file
// and the line are what name it, and both are the same on a re-run over the same
// submission.
func TestADrawingIsNamedForWhereItIs(t *testing.T) {
	dir := submissionOf(t, map[string]string{"ms.tex": paper, "sections/one.tex": included})
	found, _, err := drawings(dir, "ms.tex")
	if err != nil {
		t.Fatal(err)
	}
	if got := found[0].source(); got != "tikz:ms.tex:5" {
		t.Errorf("the first drawing is called %q", got)
	}
	if got := found[1].source(); got != "tikz:sections/one.tex:2" {
		t.Errorf("the second drawing is called %q", got)
	}
	// The committed file goes in a directory with everything else the paper has,
	// so a drawing out of a subdirectory cannot be a path.
	for _, want := range []string{"tikz-ms-1.svg", "tikz-sections-one-1.svg"} {
		var got []string
		for _, d := range found {
			got = append(got, d.name())
		}
		if !strings.Contains(strings.Join(got, " "), want) {
			t.Errorf("the drawings are committed to %v and one of them should be %s", got, want)
		}
	}
}

// Two drawings in one file are two pictures, and naming them both for the file
// would commit one over the other.
func TestTwoDrawingsInOneFileAreTwoFiles(t *testing.T) {
	dir := submissionOf(t, map[string]string{"ms.tex": `\documentclass{article}
\begin{document}
\begin{tikzpicture}\draw (0,0);\end{tikzpicture}
\begin{tikzpicture}\draw (1,1);\end{tikzpicture}
\end{document}
`})
	found, _, err := drawings(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("found %d drawings in a file with two", len(found))
	}
	if found[0].name() == found[1].name() {
		t.Errorf("both drawings are committed to %s", found[0].name())
	}
}

// Most of arXiv draws nothing, and the two kinds of nothing are different facts.
// A paper that loads TikZ and draws nothing with it is the one worth looking at.
func TestAPaperThatDrawsNothingSaysWhichKindOfNothing(t *testing.T) {
	plain := submissionOf(t, map[string]string{"ms.tex": "\\documentclass{article}\n\\usepackage{graphicx}\n\\begin{document}\n\\end{document}\n"})
	if got := nothingToDraw(plain); !strings.Contains(got, "draws none of its own figures") {
		t.Errorf("a paper that only includes images says %q", got)
	}
	loaded := submissionOf(t, map[string]string{"ms.tex": "\\documentclass{article}\n\\usepackage{tikz}\n\\begin{document}\n\\tikz \\draw (0,0);\n\\end{document}\n"})
	if got := nothingToDraw(loaded); !strings.Contains(got, "no tikzpicture environment") {
		t.Errorf("a paper that loads TikZ and draws with the shorthand says %q", got)
	}
}

// A submission holds the author's bibliography, their style files and their
// makefile, and none of those is a document to read drawings out of.
func TestOnlyTeXFilesAreRead(t *testing.T) {
	dir := submissionOf(t, map[string]string{
		"ms.tex":    paper,
		"refs.bib":  `@article{x, note = {\begin{tikzpicture}\draw (0,0);\end{tikzpicture}}}`,
		"my.sty":    `\begin{tikzpicture}\draw (0,0);\end{tikzpicture}`,
		"fig/a.TeX": included,
	})
	found, _, err := drawings(dir, "ms.tex")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("found %d drawings, and the two TeX files hold one each", len(found))
	}
	for _, d := range found {
		if strings.HasSuffix(d.file, ".bib") || strings.HasSuffix(d.file, ".sty") {
			t.Errorf("a drawing was read out of %s", d.file)
		}
	}
}

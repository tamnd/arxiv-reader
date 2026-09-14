package tikz

import (
	"strings"
	"testing"
)

// paper is a submission with two drawings in it, one in a figure and one not,
// and a third that the author commented out.
const paper = `\documentclass[11pt]{article}
\usepackage{amsmath}
\usepackage{tikz}
\usetikzlibrary{arrows.meta}
\newcommand{\myblue}{blue!40}

\begin{document}
\section{One}

Some prose about the diagram.

\begin{figure}[t]
\centering
\begin{tikzpicture}
  \draw[\myblue] (0,0) -- (1,1);
\end{tikzpicture}
\caption{The \emph{first} drawing, with a brace in it.}
\label{fig:one}
\end{figure}

% \begin{tikzpicture}
%   \draw (0,0) circle (1);
% \end{tikzpicture}

An inline drawing \begin{tikzpicture}\draw (0,0) -- (1,0);\end{tikzpicture} in a sentence.

\end{document}
`

func TestEveryDrawingInAPaperIsFound(t *testing.T) {
	got := Pictures(paper)
	if len(got) != 2 {
		t.Fatalf("found %d drawings and the paper has two that are not commented out", len(got))
	}
	if !strings.Contains(got[0].Body, `\draw[\myblue]`) || !strings.HasPrefix(got[0].Body, `\begin{tikzpicture}`) {
		t.Errorf("the first drawing came out %q", got[0].Body)
	}
	if !strings.HasSuffix(got[0].Body, `\end{tikzpicture}`) {
		t.Errorf("the first drawing came out %q, and it keeps its own delimiters", got[0].Body)
	}
	if got[1].Caption != "" || got[1].Label != "" {
		t.Errorf("the inline drawing came out with a caption %q and a label %q, and it is in no figure", got[1].Caption, got[1].Label)
	}
}

// A drawing takes its caption and its number from the figure it is in, which is
// what the manifest entry and the body of the paper both need.
func TestADrawingTakesTheCaptionOfItsFigure(t *testing.T) {
	got := Pictures(paper)
	if got[0].Caption != `The \emph{first} drawing, with a brace in it.` {
		t.Errorf("the caption came out %q, so it was cut at the first brace", got[0].Caption)
	}
	if got[0].Label != "fig:one" {
		t.Errorf("the label came out %q", got[0].Label)
	}
}

// Authors leave the version of a drawing they did not use in the file, and
// compiling one would put a figure in the corpus that is not in the paper.
func TestADrawingThatIsCommentedOutIsNotADrawing(t *testing.T) {
	for _, p := range Pictures(paper) {
		if strings.Contains(p.Body, "circle") {
			t.Fatalf("the commented out drawing was compiled: %q", p.Body)
		}
	}
}

// A per cent sign an author escaped is a character in their prose, and treating
// it as a comment would throw away a drawing the paper prints.
func TestAnEscapedPerCentDoesNotCommentOutADrawing(t *testing.T) {
	tex := `Growth of 50\% here: \begin{tikzpicture}\draw (0,0);\end{tikzpicture}`
	if got := Pictures(tex); len(got) != 1 {
		t.Fatalf("found %d drawings on a line with an escaped per cent in it", len(got))
	}
}

// A message about a drawing has to name somewhere in the author's own file.
func TestADrawingSaysWhichLineItStartsOn(t *testing.T) {
	got := Pictures(paper)
	line := strings.Count(paper[:strings.Index(paper, `\begin{tikzpicture}`)], "\n") + 1
	if got[0].Line != line {
		t.Errorf("the first drawing says line %d and it starts on line %d", got[0].Line, line)
	}
}

// An environment nobody closed is a file this cannot read, and taking the rest
// of it as one drawing would compile the whole paper into one picture.
func TestAnUnclosedDrawingIsNotRead(t *testing.T) {
	tex := "\\begin{tikzpicture}\n\\draw (0,0) -- (1,1);\n\\section{Two}\n"
	if got := Pictures(tex); len(got) != 0 {
		t.Fatalf("read %d drawings out of a file with an unclosed one: %q", len(got), got[0].Body)
	}
}

// Most of arXiv draws nothing, and a run that tried every paper would spend a
// TeX process per paper finding that out.
func TestAPaperThatDrawsNothingSaysSo(t *testing.T) {
	if Uses("\\documentclass{article}\n\\usepackage{graphicx}\n") {
		t.Error("a paper that only includes images was taken for one that draws them")
	}
	if !Uses("\\usepackage{pgfplots}\n") {
		t.Error("pgfplots is TikZ without saying so")
	}
	if !Uses(paper) {
		t.Error("a paper with a tikzpicture in it does not draw its own pictures")
	}
}

// A drawing uses the author's macros, their colours and their libraries, so the
// preamble goes in whole and the class comes off the front.
func TestThePreambleIsKeptWholeAndTheClassIsNot(t *testing.T) {
	got := Preamble(paper)
	for _, want := range []string{`\usepackage{amsmath}`, `\usetikzlibrary{arrows.meta}`, `\newcommand{\myblue}{blue!40}`} {
		if !strings.Contains(got, want) {
			t.Errorf("the preamble lost %s:\n%s", want, got)
		}
	}
	if strings.Contains(got, `\documentclass`) {
		t.Errorf("the preamble kept the paper's class, and the document this builds has its own:\n%s", got)
	}
	if strings.Contains(got, `\section{One}`) {
		t.Errorf("the preamble ran on into the document:\n%s", got)
	}
}

func TestTheDocumentDrawsOnePictureAndNothingElse(t *testing.T) {
	doc := Standalone(Preamble(paper), Pictures(paper)[0].Body)
	if !strings.HasPrefix(doc, `\documentclass[tikz,border=2pt]{standalone}`) {
		t.Errorf("the document opens with:\n%s", doc)
	}
	if strings.Count(doc, `\begin{tikzpicture}`) != 1 {
		t.Errorf("the document draws more than the one picture:\n%s", doc)
	}
	if !strings.HasSuffix(doc, "\\end{document}\n") {
		t.Errorf("the document ends with:\n%s", doc)
	}
	// The preamble has to run after the class and before the drawing, or the
	// author's own macros are not defined when the drawing asks for them.
	if strings.Index(doc, `\newcommand{\myblue}`) > strings.Index(doc, `\begin{document}`) {
		t.Errorf("the preamble runs after the document started:\n%s", doc)
	}
}

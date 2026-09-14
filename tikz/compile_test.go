package tikz

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stub is a program that behaves the way one of the two TeX programs behaves and
// is neither of them.
//
// The tests need a compiler that fails on purpose, hangs on purpose and writes
// nothing on purpose, which a real TeX cannot be asked to do, and they have to
// run on a machine with no TeX installed because CI is one. What is under test
// here is this package: the flags it passes, the directory it runs in, the
// budget it enforces, and what it makes of what comes back.
func stub(t *testing.T, name, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// out picks the output path back out of dvisvgm's arguments, the way the real
// program does.
const out = `for a in "$@"; do case "$a" in --output=*) o="${a#--output=}";; esac; done`

// drawing writes a DVI the way latex does, which is a file named for the
// document beside it.
const drawing = `echo "This is a stub"; for a in "$@"; do case "$a" in *.tex) t="$a";; esac; done; : > "${t%.tex}.dvi"`

// svg is a dvisvgm that writes the picture it was asked for.
const svg = out + "\n" + `printf '<svg xmlns="http://www.w3.org/2000/svg" width="72pt" height="36pt" viewBox="0 0 72 36"><g transform="scale(2)"><path d="M0 0"/></g></svg>' > "$o"`

// works is a compiler with a stub in place of each of the two programs.
func works(t *testing.T) *Compiler {
	t.Helper()
	return &Compiler{LaTeX: stub(t, "latex", drawing), DVISVGM: stub(t, "dvisvgm", svg)}
}

// submission is a directory holding the author's files, which is where a
// drawing is compiled.
func submission(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "macros.tex"), []byte("% the author's own\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAPictureComesBackAsAnSVG(t *testing.T) {
	dir := submission(t)
	got, err := works(t).Compile(context.Background(), dir, Standalone("", `\begin{tikzpicture}\draw (0,0);\end{tikzpicture}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), "<svg") {
		t.Errorf("what came back is %q", got)
	}
}

// The submission is the author's files and the compile ran inside them, so
// everything it wrote has to be gone afterwards. A run that left its aux files
// behind would be a run the next extraction of the same paper reads as the
// author's.
func TestTheSubmissionIsLeftAsItWasFound(t *testing.T) {
	dir := submission(t)
	if _, err := works(t).Compile(context.Background(), dir, "\\documentclass{standalone}\n"); err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 || ents[0].Name() != "macros.tex" {
		names := make([]string, 0, len(ents))
		for _, e := range ents {
			names = append(names, e.Name())
		}
		t.Errorf("the submission holds %v and it held one file", names)
	}
}

// A drawing reads the author's macro files and includes the author's images,
// both written relative to the paper, so the compile has to run there.
func TestTheCompileRunsInTheSubmissionsOwnDirectory(t *testing.T) {
	dir := submission(t)
	c := &Compiler{
		LaTeX:   stub(t, "latex", `pwd > /tmp/ax-tikz-where; for a in "$@"; do case "$a" in *.tex) t="$a";; esac; done; : > "${t%.tex}.dvi"`),
		DVISVGM: stub(t, "dvisvgm", svg),
	}
	if _, err := c.Compile(context.Background(), dir, "x"); err != nil {
		t.Fatal(err)
	}
	where, err := os.ReadFile("/tmp/ax-tikz-where")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove("/tmp/ax-tikz-where")
	// A temporary directory is a symlink on macOS, so the two are compared by
	// what they resolve to rather than by their names.
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := filepath.EvalSymlinks(strings.TrimSpace(string(where)))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("it ran in %s and the submission is %s", got, want)
	}
}

// A tikzpicture can ask TeX to run a program, and this compiles a file a
// stranger uploaded to arXiv.
func TestShellEscapeIsOffAndSaysSo(t *testing.T) {
	dir := submission(t)
	c := &Compiler{
		LaTeX:   stub(t, "latex", `echo "$@" > args; for a in "$@"; do case "$a" in *.tex) t="$a";; esac; done; : > "${t%.tex}.dvi"`),
		DVISVGM: stub(t, "dvisvgm", svg),
	}
	// The stub writes the arguments into the submission, which the sweep does
	// not take away because it is not named for the document.
	if _, err := c.Compile(context.Background(), dir, "x"); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(filepath.Join(dir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "-no-shell-escape") {
		t.Errorf("latex was run with %q", args)
	}
	if !strings.Contains(string(args), "-halt-on-error") {
		t.Errorf("latex was run with %q, and a compile that stops at the first error is one that answers in seconds", args)
	}
}

// Without the font format dvisvgm draws every letter as a path, and a drawing
// whose labels are paths is one no screen reader can read, which is most of the
// reason this path is worth having.
func TestTheTextInTheDrawingStaysText(t *testing.T) {
	dir := submission(t)
	c := &Compiler{
		LaTeX:   stub(t, "latex", drawing),
		DVISVGM: stub(t, "dvisvgm", `echo "$@" > args; `+svg),
	}
	if _, err := c.Compile(context.Background(), dir, "x"); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(filepath.Join(dir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "--font-format=woff2") {
		t.Errorf("dvisvgm was run with %q", args)
	}
}

// A drawing with a loop in it compiles forever, and the corpus has the
// rendering's version of it to fall back on.
//
// The stub leaves a child of its own running, the way TeX does when it goes off
// to build a font, so this is also the test that the timeout is a timeout: a child
// holding the pipe open would have Compile wait out the whole sleep and still
// report the right error.
func TestADrawingThatNeverFinishesIsStopped(t *testing.T) {
	dir := submission(t)
	c := &Compiler{
		LaTeX:   stub(t, "latex", "sleep 30 & sleep 30"),
		DVISVGM: stub(t, "dvisvgm", svg),
		Timeout: 200 * time.Millisecond,
	}
	started := time.Now()
	_, err := c.Compile(context.Background(), dir, "x")
	took := time.Since(started)
	var slow *TooSlow
	if !errors.As(err, &slow) {
		t.Fatalf("a drawing that hangs came back with %v", err)
	}
	if !strings.Contains(slow.Error(), "the rendering's version of it is what gets published") {
		t.Errorf("the error reads %q", slow)
	}
	if took > 10*time.Second {
		t.Errorf("a drawing given 200ms was stopped after %s, so something it started was waited for", took)
	}
}

// TeX says no to a drawing every so often, and the tail of the log is where the
// reason is.
func TestADrawingTeXRefusesSaysWhy(t *testing.T) {
	dir := submission(t)
	c := &Compiler{
		LaTeX:   stub(t, "latex", `echo "! Undefined control sequence."; echo "l.7 \\mymacro"; exit 1`),
		DVISVGM: stub(t, "dvisvgm", svg),
	}
	_, err := c.Compile(context.Background(), dir, "x")
	var failed *Failed
	if !errors.As(err, &failed) {
		t.Fatalf("a drawing TeX refused came back with %v", err)
	}
	if !strings.Contains(failed.Error(), "Undefined control sequence") {
		t.Errorf("the error reads %q, and the reason is in the log", failed)
	}
}

// A TeX that exits happily and writes nothing is the case a caller would
// otherwise read as an empty picture.
func TestACompileThatWroteNothingIsAFailureAndNotAnEmptyPicture(t *testing.T) {
	dir := submission(t)
	c := &Compiler{
		LaTeX:   stub(t, "latex", "true"),
		DVISVGM: stub(t, "dvisvgm", svg),
	}
	_, err := c.Compile(context.Background(), dir, "x")
	if err == nil || !strings.Contains(err.Error(), "it wrote no dvi") {
		t.Fatalf("a compile that wrote nothing came back with %v", err)
	}
}

// The one dependency this project cannot vendor, so the message is what somebody
// has to act on.
func TestAMachineWithNoTeXSaysHowToGetOne(t *testing.T) {
	c := &Compiler{LaTeX: "ax-no-such-latex", DVISVGM: "ax-no-such-dvisvgm"}
	err := c.Available()
	var missing *NotInstalled
	if !errors.As(err, &missing) {
		t.Fatalf("a machine with no TeX came back with %v", err)
	}
	for _, want := range []string{"is not on the PATH", "mactex-no-gui", "texlive-pictures"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message reads %q and it does not say %s", err, want)
		}
	}
	if _, err := c.Compile(context.Background(), t.TempDir(), "x"); !errors.As(err, &missing) {
		t.Errorf("compiling on a machine with no TeX came back with %v", err)
	}
}

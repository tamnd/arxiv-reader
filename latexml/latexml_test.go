package latexml

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// stub is a program that behaves the way LaTeXML behaves and is not LaTeXML.
//
// The tests need a converter that fails on purpose, hangs on purpose and
// reports each of the four statuses, which the real one cannot be asked to do,
// and they have to run on a machine with no LaTeXML installed because CI is one.
// What is being tested here is this package: the flags it passes, the directory
// it runs in, the budget it enforces and what it makes of what comes back.
func stub(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "latexmlc")
	body := "#!/bin/sh\n" + script + "\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// dest picks the output path back out of the arguments, the way the real
// program does, so a stub can write where it was told to.
const dest = `for a in "$@"; do case "$a" in --dest=*) out="${a#--dest=}";; esac; done`

// submission is a directory with a file in it to convert.
func submission(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ms.tex"), []byte("\\documentclass{article}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestConvertWritesTheDocumentAndReadsItBack(t *testing.T) {
	c := &Converter{Binary: stub(t, dest+`
echo "Status:conversion:0"
printf '<html>a paper</html>' > "$out"`)}
	out := filepath.Join(t.TempDir(), "converted", "paper.html")

	res, err := c.Convert(context.Background(), submission(t), "ms.tex", out)
	if err != nil {
		t.Fatal(err)
	}
	if string(res.HTML) != "<html>a paper</html>" {
		t.Fatalf("the document is %q", res.HTML)
	}
	if res.Dest != out {
		t.Fatalf("wrote to %s", res.Dest)
	}
	if res.Status != StatusClean {
		t.Fatalf("status %d", res.Status)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal("the document is not on disk")
	}
}

// LaTeXML resolves every \input, \includegraphics and \bibliography relative to
// where it runs, so a conversion run from anywhere but the submission is a
// paper with its pictures and half its sections missing.
func TestConvertRunsInsideTheSubmission(t *testing.T) {
	c := &Converter{Binary: stub(t, dest+`
pwd > "$out"
echo "$@" >> "$out"`)}
	dir := submission(t)

	res, err := c.Convert(context.Background(), dir, "ms.tex", filepath.Join(t.TempDir(), "paper.html"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitN(strings.TrimSpace(string(res.HTML)), "\n", 2)
	// The temporary directory is a symlink on this platform, so the answer is
	// compared by what it resolves to rather than by its name.
	got, err := filepath.EvalSymlinks(lines[0])
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ran in %s, and the submission is in %s", got, want)
	}
	for _, flag := range []string{"--format=html5", "--mathtex", "--svg", "--nodefaultresources", "ms.tex"} {
		if !strings.Contains(lines[1], flag) {
			t.Fatalf("%s was not passed: %s", flag, lines[1])
		}
	}
}

// The case the whole source path depends on. LaTeXML meets something it cannot
// read, says so, writes the paper anyway with a marker where that thing was,
// and exits zero, so a runner that treated its own status as an exit code would
// throw away a paper that is almost entirely fine.
func TestConvertTreatsErrorsAsAResultAndNotAFailure(t *testing.T) {
	for _, tc := range []struct {
		say  string
		want int
	}{
		{"Status:conversion:0", StatusClean},
		{"Status:conversion:1", StatusWarnings},
		{"Status:conversion:2", StatusErrors},
		{"", StatusClean},
	} {
		c := &Converter{Binary: stub(t, dest+`
echo "`+tc.say+`"
printf 'a paper' > "$out"`)}
		res, err := c.Convert(context.Background(), submission(t), "ms.tex", filepath.Join(t.TempDir(), "paper.html"))
		if err != nil {
			t.Fatalf("%q: %v", tc.say, err)
		}
		if res.Status != tc.want {
			t.Fatalf("%q was read as status %d", tc.say, res.Status)
		}
	}
}

// The second pass, and the reason it is a second pass. A .sty is a program, so
// the conversion that reads them is the retry and the conversion that does not
// is what runs first.
func TestIncludeStylesIsPassedOnlyWhenItIsAskedFor(t *testing.T) {
	for _, want := range []bool{false, true} {
		c := &Converter{
			Binary: stub(t, dest+`
echo "$@" > "$out"`),
			IncludeStyles: want,
		}
		res, err := c.Convert(context.Background(), submission(t), "ms.tex", filepath.Join(t.TempDir(), "paper.html"))
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(string(res.HTML), "--includestyles"); got != want {
			t.Fatalf("IncludeStyles is %v and the arguments are %s", want, res.HTML)
		}
	}
}

// A paper that lost a package says so once and then says three hundred times
// that a command is undefined, so the line naming the package is the one worth
// pulling out of the log.
func TestMissingNamesThePackagesThatCouldNotBeRead(t *testing.T) {
	c := &Converter{Binary: stub(t, dest+`
echo "Warning:missing_file:tikz Can't find package tikz" >&2
echo "Error:undefined:\\tikzset Undefined control sequence" >&2
echo "Warning:missing_file:quantikz Can't find package quantikz" >&2
echo "Warning:missing_file:tikz Can't find package tikz" >&2
echo "Status:conversion:2" >&2
printf 'a paper' > "$out"`)}

	res, err := c.Convert(context.Background(), submission(t), "ms.tex", filepath.Join(t.TempDir(), "paper.html"))
	if err != nil {
		t.Fatal(err)
	}
	// In the order they were met, and once each, because the same package is
	// reported again every time something reaches for it.
	if want := []string{"tikz", "quantikz"}; !slices.Equal(res.Missing, want) {
		t.Fatalf("got %v, want %v", res.Missing, want)
	}
	if res.Status != StatusErrors {
		t.Fatalf("status %d", res.Status)
	}
}

func TestMissingIsEmptyWhenEveryPackageLoaded(t *testing.T) {
	c := &Converter{Binary: stub(t, dest+`
echo "Status:conversion:0" >&2
printf 'a paper' > "$out"`)}
	res, err := c.Convert(context.Background(), submission(t), "ms.tex", filepath.Join(t.TempDir(), "paper.html"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Missing) != 0 {
		t.Fatalf("got %v", res.Missing)
	}
}

func TestConvertReportsAConversionThatWroteNothing(t *testing.T) {
	c := &Converter{Binary: stub(t, `
echo "Fatal:perl:die Can't locate LaTeXML/Package/nonesuch.sty" >&2
exit 3`)}
	_, err := c.Convert(context.Background(), submission(t), "ms.tex", filepath.Join(t.TempDir(), "paper.html"))
	if err == nil {
		t.Fatal("a conversion that wrote nothing came back as a document")
	}
	// The line saying why is the only useful thing in a five hundred line log,
	// so an error that does not carry it sends a person to a file they have not
	// been told the name of.
	if !strings.Contains(err.Error(), "nonesuch.sty") {
		t.Fatalf("the error does not say what happened: %v", err)
	}
}

// A document left over from a previous run is the one way this could report a
// conversion that never happened, so it goes before the run and not after it.
func TestConvertDoesNotReadBackALastRunsDocument(t *testing.T) {
	out := filepath.Join(t.TempDir(), "paper.html")
	if err := os.WriteFile(out, []byte("<html>the paper as it converted last week</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Converter{Binary: stub(t, "exit 3")}

	if _, err := c.Convert(context.Background(), submission(t), "ms.tex", out); err == nil {
		t.Fatal("a stale document was read back as this run's output")
	}
}

// A submission that defeats LaTeXML tends to defeat it slowly rather than fail,
// and a batch of ten thousand papers cannot wait on one of them.
func TestConvertStopsAConversionThatRunsTooLong(t *testing.T) {
	c := &Converter{Binary: stub(t, "sleep 30"), Timeout: 200 * time.Millisecond}

	started := time.Now()
	_, err := c.Convert(context.Background(), submission(t), "ms.tex", filepath.Join(t.TempDir(), "paper.html"))
	var slow *TooSlow
	if !errors.As(err, &slow) {
		t.Fatalf("got %v, want a conversion that was stopped", err)
	}
	if slow.Main != "ms.tex" {
		t.Fatalf("the error is about %q", slow.Main)
	}
	// The stub sleeps for thirty seconds and holds the pipe while it does, so
	// this is also the check that the grace period is what bounds the wait
	// rather than the child deciding when to let go.
	if took := time.Since(started); took > 10*time.Second {
		t.Fatalf("waited %s on a conversion with a budget of 200ms", took)
	}
}

func TestNoLaTeXMLSaysHowToGetOne(t *testing.T) {
	c := &Converter{Binary: filepath.Join(t.TempDir(), "not-installed")}
	err := c.Available()
	var missing *NotInstalled
	if !errors.As(err, &missing) {
		t.Fatalf("got %v, want the binary not being there", err)
	}
	if !strings.Contains(err.Error(), "brew install latexml") {
		t.Fatalf("the error does not say how to fix it: %v", err)
	}
	// Convert asks the same question, because a batch is not the only way in.
	if _, err := c.Convert(context.Background(), submission(t), "ms.tex", filepath.Join(t.TempDir(), "paper.html")); !errors.As(err, &missing) {
		t.Fatalf("got %v", err)
	}
}

// Recorded rather than checked, so all that matters is that it is one line.
func TestVersionIsTheFirstLineOfWhatTheProgramSays(t *testing.T) {
	c := &Converter{Binary: stub(t, `
echo "LaTeXML version 0.8.8"
echo "and a second line nobody wants"`)}
	v, err := c.Version(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v != "LaTeXML version 0.8.8" {
		t.Fatalf("got %q", v)
	}
}

// A long paper is minutes of silence otherwise, and the lines LaTeXML writes as
// it goes are how a person watching knows which package it is stuck on.
func TestLogPassesEachLineOnAsItArrives(t *testing.T) {
	var lines []string
	c := &Converter{
		Binary: stub(t, dest+`
echo "Warning:undefined:\\mycommand" >&2
echo "Status:conversion:1" >&2
printf 'a paper' > "$out"`),
		Log: func(line string) { lines = append(lines, line) },
	}

	res, err := c.Convert(context.Background(), submission(t), "ms.tex", filepath.Join(t.TempDir(), "paper.html"))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || !strings.Contains(lines[0], "mycommand") {
		t.Fatalf("the callback got %v", lines)
	}
	// And the whole of it is kept as well, because the status is read off it.
	if res.Status != StatusWarnings {
		t.Fatalf("status %d, and the log went to the callback instead of the buffer", res.Status)
	}
}

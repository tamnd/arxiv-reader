// Package latexml runs LaTeXML over a submission.
//
// It is the one part of this project that shells out to somebody else's
// program, and it is worth being clear about why. arXiv's own renderings are
// made by LaTeXML, so a paper converted here comes back in the same markup as a
// paper fetched from the render path, with the same ltx_ classes and the same
// alttext holding the LaTeX the author typed. The reader that was written for
// the render path reads both, and the source path costs a new runner and no new
// parser.
//
// The flags are the difference between the two paths and they are chosen, not
// copied. arXiv runs LaTeXML with its own preload list and its own resource
// limits; this runs it with the defaults plus the four things the corpus needs,
// which are HTML5 output, the TeX kept beside every formula, pictures turned
// into SVG where LaTeXML can do it, and no stylesheet copied into the output
// directory. A paper LaTeXML rejected at arXiv can come out clean here, which
// is the whole reason a rejected rendering falls through to this path.
package latexml

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Binary is the program this package runs.
const Binary = "latexmlc"

// Timeout is how long one paper gets.
//
// Five minutes, which is the number in 2166-04. A TeX document can loop, and a
// submission that defeats LaTeXML tends to defeat it slowly rather than fail,
// so the budget is what turns a hung batch into one paper on another path. The
// NumPy paper, which is twelve pages with seventy three references, converts in
// about eighteen seconds on the machine this was written on, so five minutes is
// roughly sixteen times what an ordinary paper needs.
const Timeout = 300 * time.Second

// The status LaTeXML reports for a conversion, which is its own scale and not
// a process exit code.
const (
	// StatusClean is no obvious problems.
	StatusClean = 0
	// StatusWarnings is something LaTeXML worked around.
	StatusWarnings = 1
	// StatusErrors is something LaTeXML could not read. It still writes a
	// document, with a marker where the thing it could not read was, and
	// whether that matters is the reject rule's question rather than this
	// package's: one in a bibliography entry is a reference that prints badly
	// and one in a section body is a sentence of the paper that is gone.
	StatusErrors = 2
	// StatusFatal is a conversion that stopped.
	StatusFatal = 3
)

// NotInstalled is the error a machine without LaTeXML fails with.
//
// Its own type because it is not a fault in the paper, the corpus or the
// command, and because the answer is one line a person runs once.
type NotInstalled struct {
	Binary string
}

func (n *NotInstalled) Error() string {
	return fmt.Sprintf("latexml: %s is not on the PATH, and the source path is LaTeXML running here rather than at arXiv, so install it with brew install latexml or apt install latexml", n.Binary)
}

// TooSlow is the error a conversion that ran out of its budget fails with.
type TooSlow struct {
	Main  string
	After time.Duration
	Log   []byte
}

func (t *TooSlow) Error() string {
	return fmt.Sprintf("latexml: converting %s was still running after %s and was stopped, so this paper is on the native path", t.Main, t.After)
}

// Converter runs one conversion at a time.
type Converter struct {
	// Binary defaults to the package constant and exists so a test can point
	// at a script that behaves like LaTeXML and is not LaTeXML.
	Binary string
	// Timeout defaults to the package constant. Zero means the default rather
	// than no limit, because the dangerous value is the one that waits forever
	// and it is the one a caller should have to write down.
	Timeout time.Duration
	// Log, if set, is called with each line LaTeXML writes as it writes it. A
	// conversion of a long paper is quiet for a minute otherwise.
	Log func(line string)
}

// Result is one conversion.
type Result struct {
	// HTML is the document, read back off disk.
	HTML []byte
	// Dest is where it was written, which is also where the pictures LaTeXML
	// made or copied are.
	Dest string
	// Status is LaTeXML's own reading of how it went.
	Status int
	// Log is everything LaTeXML said, kept because the line explaining why a
	// macro was ignored is in it and nowhere else.
	Log  []byte
	Took time.Duration
}

// Available says whether LaTeXML can be run at all.
//
// Worth asking before a batch rather than after the first paper, because the
// answer is the same for every paper in it and it is a line the person running
// the command has to act on.
func (c *Converter) Available() error {
	if _, err := exec.LookPath(c.binary()); err != nil {
		return &NotInstalled{Binary: c.binary()}
	}
	return nil
}

// Version is what LaTeXML says it is.
//
// Recorded rather than checked. Two versions of LaTeXML convert the same paper
// differently, so a corpus that cannot say which one read a paper cannot
// explain why a re-extraction changed it.
func (c *Converter) Version(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, c.binary(), "--VERSION").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("latexml: %s --VERSION failed: %w", c.binary(), err)
	}
	return strings.TrimSpace(string(bytes.SplitN(out, []byte("\n"), 2)[0])), nil
}

// Convert reads one submission and writes a document.
//
// dir is the unpacked submission and main is the file inside it the document
// starts from, because LaTeXML resolves every \input, \includegraphics and
// \bibliography relative to where it is run. dest is outside that directory on
// purpose: LaTeXML copies the pictures a paper uses next to the document it
// writes, so a dest inside the submission would leave the tool's own output
// mixed in with the author's files and nobody able to tell which was which.
//
// A conversion with errors in it is a result and not a failure. LaTeXML writes
// a document anyway, with a marker where the thing it could not read was, and
// the caller decides. Only a conversion that wrote nothing is an error here.
func (c *Converter) Convert(ctx context.Context, dir, main, dest string) (Result, error) {
	if err := c.Available(); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return Result{}, err
	}
	// A stale document from a previous run would be read back as this run's
	// output if LaTeXML wrote nothing, which is the one way this could report a
	// conversion that never happened.
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		return Result{}, err
	}

	timeout := c.Timeout
	if timeout == 0 {
		timeout = Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.binary(),
		"--dest="+dest,
		"--format=html5",
		// The LaTeX the author typed, kept beside every formula. It is the
		// only form of the mathematics worth storing: the MathML next to it is
		// what a browser draws and it is regenerated at build time.
		"--mathtex",
		// TikZ and the picture environment, drawn as SVG rather than left as a
		// hole. Raster graphics need an image library LaTeXML may not have,
		// and a picture it could not convert is reported and not fatal.
		"--svg",
		// No stylesheet copied next to the output. The corpus publishes
		// Markdown and its own pages, so LaTeXML's CSS is three files nobody
		// reads and one more thing to keep out of git.
		"--nodefaultresources",
		main,
	)
	cmd.Dir = dir
	// One writer for both streams and not two, which is what keeps them one
	// log. Two writers means two pipes and two goroutines appending to the same
	// buffer, and the copy that finishes second truncates the buffer back to the
	// length it read at the start, so the interesting half of the log
	// disappears. The two streams also interleave the way LaTeXML wrote them
	// this way, which is how a warning lines up with the file it is about.
	log := &lineWriter{each: c.Log}
	cmd.Stdout = log
	cmd.Stderr = log
	// Killing latexmlc does not kill whatever it started, and a child holding
	// the other end of the pipe keeps the wait going long after the thing that
	// ran out of time was stopped. This caps that at a grace period.
	cmd.WaitDelay = grace

	started := time.Now()
	err := cmd.Run()
	took := time.Since(started)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return Result{}, &TooSlow{Main: main, After: timeout, Log: log.Bytes()}
	}
	body, readErr := os.ReadFile(dest)
	if err != nil && readErr != nil {
		return Result{}, fmt.Errorf("latexml: converting %s wrote no document: %w%s", main, err, tail(log.Bytes()))
	}
	if readErr != nil {
		return Result{}, fmt.Errorf("latexml: converting %s reported success and wrote no document to %s%s", main, dest, tail(log.Bytes()))
	}
	return Result{HTML: body, Dest: dest, Status: status(log.Bytes()), Log: log.Bytes(), Took: took}, nil
}

// grace is how long a stopped conversion gets to actually stop.
const grace = 2 * time.Second

func (c *Converter) binary() string {
	if c.Binary != "" {
		return c.Binary
	}
	return Binary
}

// reports is the line LaTeXML ends with, which carries its own status and not
// the process exit code. The two are different numbers: a conversion with
// errors in it exits zero, because it wrote a document.
var reports = regexp.MustCompile(`(?m)^Status:conversion:(\d+)`)

func status(log []byte) int {
	m := reports.FindSubmatch(log)
	if m == nil {
		return StatusClean
	}
	n, err := strconv.Atoi(string(m[1]))
	if err != nil {
		return StatusClean
	}
	return n
}

// tail is the last of the log, for an error message to carry.
//
// A LaTeXML log runs to hundreds of lines of packages being loaded and the
// reason it stopped is at the end of it, so an error that carried the whole
// thing would bury the sentence somebody needs.
func tail(log []byte) string {
	lines := strings.Split(strings.TrimRight(string(log), "\n"), "\n")
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	if len(lines) == 0 {
		return ""
	}
	return ":\n  " + strings.Join(lines, "\n  ")
}

// lineWriter keeps the whole log and passes each finished line on as it
// arrives, if anybody asked for them.
type lineWriter struct {
	log  bytes.Buffer
	each func(string)
	part []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	n, err := w.log.Write(p)
	if w.each == nil {
		return n, err
	}
	w.part = append(w.part, p[:n]...)
	for {
		i := bytes.IndexByte(w.part, '\n')
		if i < 0 {
			break
		}
		w.each(string(w.part[:i]))
		w.part = w.part[i+1:]
	}
	return n, err
}

func (w *lineWriter) Bytes() []byte { return w.log.Bytes() }

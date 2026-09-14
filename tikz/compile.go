package tikz

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// LaTeX and DVISVGM are the two programs this needs.
//
// The DVI route and not the PDF one, because dvisvgm reads a DVI's text as text
// and keeps it as text in the SVG, which is the screen readable output that
// makes this path worth having at all.
const (
	LaTeX   = "latex"
	DVISVGM = "dvisvgm"
)

// Timeout is how long one picture gets.
//
// Two minutes, which is generous for a drawing and short next to the five a
// whole paper's conversion gets. A picture that takes longer than this is
// usually one whose author left a loop in it, and the corpus has the rendering's
// version of it to fall back on.
const Timeout = 120 * time.Second

// NotInstalled is the machine having no TeX.
type NotInstalled struct{ Binary string }

func (n *NotInstalled) Error() string {
	return fmt.Sprintf("tikz: %s is not on the PATH, and compiling a drawing from the author's own description needs a TeX installation here rather than at arXiv, so install one with brew install --cask mactex-no-gui or apt install texlive-pictures texlive-latex-extra dvisvgm", n.Binary)
}

// TooSlow is a picture that was still compiling when its time ran out.
type TooSlow struct {
	After time.Duration
	Log   []byte
}

func (t *TooSlow) Error() string {
	return fmt.Sprintf("tikz: the drawing was still compiling after %s and was stopped, so the rendering's version of it is what gets published", t.After)
}

// Failed is TeX or dvisvgm saying no.
//
// The tail of the log and not the whole of it, because a TeX log is four hundred
// lines of fonts and the error is in the last twenty. The whole log is on Log for
// anybody who wants it.
type Failed struct {
	Binary string
	Err    error
	Log    []byte
}

func (f *Failed) Error() string {
	s := fmt.Sprintf("tikz: %s could not draw the picture: %v", f.Binary, f.Err)
	if t := tail(f.Log, 20); t != "" {
		s += "\n" + t
	}
	return s
}

func (f *Failed) Unwrap() error { return f.Err }

// Compiler runs one picture at a time.
type Compiler struct {
	// LaTeX and DVISVGM default to the package constants and exist so a test can
	// point at a script that behaves like TeX and is not TeX.
	LaTeX   string
	DVISVGM string
	// Timeout defaults to the package constant. Zero means the default rather
	// than no limit, because the dangerous value is the one that waits forever
	// and it is the one a caller should have to write down.
	Timeout time.Duration
	// Log, if set, is called with each line either program writes.
	Log func(line string)
}

// Available says whether a picture can be compiled at all.
//
// Worth asking before a shard rather than after the first paper, because the
// answer is the same for every picture in it and it is a line the person running
// the command has to act on.
func (c *Compiler) Available() error {
	for _, bin := range []string{c.latex(), c.dvisvgm()} {
		if _, err := exec.LookPath(bin); err != nil {
			return &NotInstalled{Binary: bin}
		}
	}
	return nil
}

// Compile draws one document and gives back the SVG.
//
// It runs in the submission's own directory, because a drawing reads the
// author's macro files and includes the author's images and both are written
// relative to the paper. Everything it writes is named for the hash of the
// document and taken away afterwards, so a submission is left as it was found
// and two runs over the same picture do not see each other's files.
//
// Shell escape is off and says so on the command line. A tikzpicture can ask TeX
// to run a program, and this compiles a file a stranger uploaded to arXiv.
// 05-extract.md says to run this in a container, which is the other half of the
// same answer and is the operator's to arrange.
func (c *Compiler) Compile(ctx context.Context, dir, doc string) ([]byte, error) {
	if err := c.Available(); err != nil {
		return nil, err
	}
	name := "ax-tikz-" + digest(doc)
	tex := filepath.Join(dir, name+".tex")
	if err := os.WriteFile(tex, []byte(doc), 0o644); err != nil {
		return nil, err
	}
	defer c.sweep(dir, name)

	ctx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	started := time.Now()
	log, err := c.run(ctx, dir, c.latex(),
		"-interaction=nonstopmode", "-halt-on-error", "-no-shell-escape", "-file-line-error", name+".tex")
	if err != nil {
		return nil, c.wrap(c.latex(), err, log, started)
	}
	if _, err := os.Stat(filepath.Join(dir, name+".dvi")); err != nil {
		return nil, &Failed{Binary: c.latex(), Err: errors.New("it wrote no dvi"), Log: log}
	}
	// The font format is what keeps the text in the picture as text. Without it
	// dvisvgm draws every letter as a path, and a drawing whose labels are paths
	// is a drawing no screen reader can read and no translator can touch.
	out, err := c.run(ctx, dir, c.dvisvgm(),
		"--font-format=woff2", "--no-merge", "--output="+name+".svg", name+".dvi")
	if err != nil {
		return nil, c.wrap(c.dvisvgm(), err, append(log, out...), started)
	}
	svg, err := os.ReadFile(filepath.Join(dir, name+".svg"))
	if err != nil {
		return nil, &Failed{Binary: c.dvisvgm(), Err: errors.New("it wrote no svg"), Log: append(log, out...)}
	}
	return svg, nil
}

// wrap tells a program that failed apart from one that ran out of time.
//
// A killed process exits with an error of its own, so the deadline is what says
// which of the two happened rather than the exit status.
func (c *Compiler) wrap(bin string, err error, log []byte, started time.Time) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &TooSlow{After: time.Since(started), Log: log}
	}
	return &Failed{Binary: bin, Err: err, Log: log}
}

// run is one program in the submission's directory.
func (c *Compiler) run(ctx context.Context, dir, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	// TeX reads its own configuration and a submission's directory both, and a
	// paper that shipped a texmf.cnf would otherwise be telling this process how
	// to behave.
	cmd.Env = append(os.Environ(), "TEXMFCNF=", "openout_any=p", "openin_any=p")
	group(cmd)
	// The backstop for a kill that did not take. Without it Run waits on the pipe
	// rather than on the deadline, and the deadline is the whole point.
	cmd.WaitDelay = 2 * time.Second
	var buf bytes.Buffer
	w := &lines{buf: &buf, to: c.Log}
	cmd.Stdout, cmd.Stderr = w, w
	err := cmd.Run()
	w.flush()
	if ctx.Err() != nil {
		return buf.Bytes(), ctx.Err()
	}
	return buf.Bytes(), err
}

// sweep takes away everything the compile wrote.
//
// A submission is the author's files and this ran inside them, so the aux, the
// log, the dvi, the svg and the document itself all go. What the caller wanted
// is in memory by now.
func (c *Compiler) sweep(dir, name string) {
	matches, err := filepath.Glob(filepath.Join(dir, name+".*"))
	if err != nil {
		return
	}
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

func (c *Compiler) latex() string {
	if c.LaTeX != "" {
		return c.LaTeX
	}
	return LaTeX
}

func (c *Compiler) dvisvgm() string {
	if c.DVISVGM != "" {
		return c.DVISVGM
	}
	return DVISVGM
}

func (c *Compiler) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return Timeout
}

// digest names the files one compile writes, so that two pictures compiled in
// the same directory cannot collide and a run that was interrupted leaves files
// the next one over the same picture will clean up.
func digest(doc string) string {
	sum := sha256.Sum256([]byte(doc))
	return hex.EncodeToString(sum[:])[:12]
}

// tail is the last n lines of a log.
func tail(log []byte, n int) string {
	all := strings.Split(strings.TrimRight(string(log), "\n"), "\n")
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return strings.Join(all, "\n")
}

// lines writes a program's output to a buffer and to the caller a line at a
// time, because a compile is quiet otherwise and a picture that hangs looks the
// same as one that is working.
type lines struct {
	buf  *bytes.Buffer
	to   func(string)
	part []byte
}

func (w *lines) Write(p []byte) (int, error) {
	w.buf.Write(p)
	if w.to == nil {
		return len(p), nil
	}
	w.part = append(w.part, p...)
	for {
		i := bytes.IndexByte(w.part, '\n')
		if i < 0 {
			return len(p), nil
		}
		w.to(string(w.part[:i]))
		w.part = w.part[i+1:]
	}
}

func (w *lines) flush() {
	if w.to != nil && len(w.part) > 0 {
		w.to(string(w.part))
		w.part = nil
	}
}

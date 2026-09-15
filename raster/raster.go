// Package raster turns one page of a PDF into a picture.
//
// The third program this project shells out to, and the only one whose output is
// not text. It ships in poppler-utils beside pdftotext, so the machine that can
// run the native path can run this, and a corpus that has one has both.
//
// One page at a time and not a whole paper in one run, which is the shape the
// vision path needs. A page that came back badly is asked again at a higher
// resolution and the pages around it are not, so rasterising eighteen pages at
// six hundred dots because one of them was hard would be eighteen times the work
// and eighteen megabytes of pictures nobody looks at.
//
// Nothing here decides whether a paper belongs on the vision path or what a
// picture of a page means. It rasterises what it is told to rasterise, and the
// resolution ladder, the reading and the rules that judge a reading are the
// vision package's business.
package raster

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Binary is the program this package runs.
const Binary = "pdftoppm"

// Timeout is how long one page gets.
//
// Two minutes, which is fifty times what a page takes. A page of a typical
// article rasterises in under a second at three hundred dots and in about two
// and a half at six hundred, and the budget is here for the malformed file that
// sends poppler into a loop rather than for the ordinary case.
const Timeout = 120 * time.Second

// Format is the file this writes.
//
// PNG and not JPEG. A page of a paper is black text on white and PNG is lossless
// on exactly that, while JPEG puts a halo round every letter at the compression
// ratio that would make it smaller. What is being asked of these pictures is that
// something read the letters, so the letters are the last thing to spend quality
// on.
const Format = "png"

// NotInstalled is the error a machine without pdftoppm fails with.
//
// Its own type for the same reason pdftext.NotInstalled is: it is not a fault in
// the paper, the corpus or the command, and the answer is one line a person runs
// once.
type NotInstalled struct {
	Binary string
}

func (n *NotInstalled) Error() string {
	return fmt.Sprintf("raster: %s is not on the PATH, and the vision path reads pictures of pages, so install it with brew install poppler or apt install poppler-utils", n.Binary)
}

// TooSlow is the error a page that ran out of its budget fails with.
type TooSlow struct {
	File  string
	Page  int
	DPI   int
	After time.Duration
}

func (t *TooSlow) Error() string {
	return fmt.Sprintf("raster: page %d of %s was still rasterising at %d dots after %s and was stopped, which is a malformed PDF rather than a complicated page, so this one wants a person", t.Page, t.File, t.DPI, t.After)
}

// Unreadable is the error a page poppler refused fails with.
//
// A PDF that is encrypted, truncated or not a PDF at all, and a page the file
// does not have. Its own type so a batch can step over one paper rather than
// stopping, because a file whose bytes will not open is a fact about that paper.
type Unreadable struct {
	File string
	Page int
	Said string
}

func (u *Unreadable) Error() string {
	said := u.Said
	if said == "" {
		said = "and said nothing about why"
	}
	return fmt.Sprintf("raster: %s refused page %d of %s: %s", Binary, u.Page, u.File, said)
}

// Painter rasterises one page at a time.
type Painter struct {
	// Binary defaults to the package constant and exists so a test can point at
	// a script that behaves like pdftoppm and is not pdftoppm.
	Binary string
	// Timeout defaults to the package constant. Zero means the default rather
	// than no limit, for the same reason it does in the pdftext package: the
	// dangerous value is the one that waits forever, and it is the one a caller
	// should have to write down.
	Timeout time.Duration
}

// Available says whether pdftoppm can be run at all.
func (p *Painter) Available() error {
	if _, err := exec.LookPath(p.binary()); err != nil {
		return &NotInstalled{Binary: p.binary()}
	}
	return nil
}

// Version is what pdftoppm says it is.
//
// Recorded rather than checked, the same way pdftotext's version and LaTeXML's
// are. Poppler renders a page differently between releases, so a corpus that
// cannot say which version drew the picture a model read cannot explain why
// reading the same page again moved a word.
func (p *Painter) Version(ctx context.Context) (string, error) {
	// The banner goes to stderr, which is where poppler has always put it, and
	// unlike pdftotext this program exits zero after printing it.
	out, _ := exec.CommandContext(ctx, p.binary(), "-v").CombinedOutput()
	line := strings.TrimSpace(string(bytes.SplitN(out, []byte("\n"), 2)[0]))
	if line == "" {
		return "", fmt.Errorf("raster: %s -v said nothing, so this is not the program it is meant to be", p.binary())
	}
	return line, nil
}

// Name is what a picture of one page at one resolution is called.
//
// The page and the resolution are both in the name because both are answers to
// the question of what somebody is looking at. A page that was asked again at a
// higher resolution leaves two files behind, and a directory where the second
// overwrote the first is a directory that cannot say what the model was shown.
func Name(page, dpi int) string {
	return fmt.Sprintf("p%03d@%d.%s", page, dpi, Format)
}

// Paint draws one page and returns the file it wrote.
//
// The file is not drawn again when it is already there. A page picture is
// entirely determined by the PDF, the page and the resolution, so the second run
// over a paper that stopped halfway through costs nothing, and that is most of
// what makes the vision path resumable at all.
func (p *Painter) Paint(ctx context.Context, pdf string, page, dpi int, dir string) (string, error) {
	if err := p.Available(); err != nil {
		return "", err
	}
	if page < 1 {
		return "", fmt.Errorf("raster: %d is not a page, and pages are counted from one", page)
	}
	if dpi < 1 {
		return "", fmt.Errorf("raster: %d is not a resolution", dpi)
	}
	if _, err := os.Stat(pdf); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	out := filepath.Join(dir, Name(page, dpi))
	if info, err := os.Stat(out); err == nil && info.Size() > 0 {
		return out, nil
	}

	ctx, cancel := context.WithTimeout(ctx, p.timeout())
	defer cancel()
	// -singlefile is what makes the name predictable. Without it poppler names
	// the file after the page and pads the number to the width of the paper's
	// page count, so a caller would have to know how many pages the file has to
	// know what was written. -q keeps the per page complaints out of the way and
	// the ones that matter are on stderr, where this reads them.
	prefix := strings.TrimSuffix(out, "."+Format)
	cmd := exec.CommandContext(ctx, p.binary(),
		"-"+Format, "-r", fmt.Sprint(dpi), "-f", fmt.Sprint(page), "-l", fmt.Sprint(page),
		"-singlefile", "-q", pdf, prefix)
	var errs bytes.Buffer
	cmd.Stderr = &errs
	// poppler starts nothing of its own, so killing the one process is the whole
	// job, and WaitDelay is here for the case where the kill does not take.
	cmd.WaitDelay = 2 * time.Second

	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", &TooSlow{File: pdf, Page: page, DPI: dpi, After: p.timeout()}
	}
	if err != nil {
		return "", &Unreadable{File: pdf, Page: page, Said: said(errs.Bytes(), err)}
	}
	// A run that said nothing and wrote nothing is the case a caller must never
	// be handed quietly, because the next thing to happen would be a model being
	// shown a file that is not there.
	if info, statErr := os.Stat(out); statErr != nil || info.Size() == 0 {
		return "", &Unreadable{File: pdf, Page: page, Said: fmt.Sprintf("it exited cleanly and wrote no picture to %s", out)}
	}
	return out, nil
}

// said is the sentence to put in an Unreadable, which is poppler's own complaint
// when it made one and the process error when it did not.
func said(errs []byte, err error) string {
	for _, line := range strings.Split(string(errs), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return err.Error()
}

func (p *Painter) binary() string {
	if p.Binary != "" {
		return p.Binary
	}
	return Binary
}

func (p *Painter) timeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return Timeout
}

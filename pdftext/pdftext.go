// Package pdftext reads the text layer of a PDF.
//
// The second program this project shells out to, and the smaller of the two by
// a long way. LaTeXML reads a submission and produces a document with a
// structure in it. pdftotext reads a PDF and produces the characters that were
// typeset on each page, in the order and roughly the position they were printed
// in, and nothing else. That is the whole of what this path has to work with,
// and being clear about it is most of why the path is worth having: the text was
// never guessed by anything, so the sentences are the author's sentences, and
// everything above a sentence has to be recovered by something here.
//
// The flag that matters is -layout. Without it a two column paper comes back
// with the left column and the right column interleaved line by line, which is
// unreadable and unrecoverable. With it the columns stay apart, at the cost of
// runs of spaces where the gutter was, and those are what the structure recovery
// reads.
//
// This package does not decide whether a paper should be on this path. It says
// how much text each page holds and lets the caller decide, because a PDF with
// no usable text layer is a paper for the vision path and that is a routing
// decision rather than a fact about the file.
package pdftext

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"
)

// Binary is the program this package runs.
//
// It ships in poppler-utils, which is one install on every platform this runs
// on, and it is the same program the other two projects in this family use for
// the same path.
const Binary = "pdftotext"

// Timeout is how long one PDF gets.
//
// Two minutes, which is twenty times what a long paper takes. pdftotext is C++
// and it is fast: a forty page paper comes back in well under a second on the
// machine this was written on. The budget is here for the malformed file that
// sends it into a loop rather than for the ordinary case, and a PDF that cannot
// be read in two minutes is a paper for a person to look at.
const Timeout = 120 * time.Second

// MinChars is how much text a page holds before it counts as typeset.
//
// The number has to clear arXiv's own stamp and nothing else. arXiv prints the
// identifier, the category and the date down the left margin of every PDF it
// serves, which is around forty characters, and a scan of a typescript comes
// back with exactly that and no more. Four hundred is a short paragraph, which
// is less than any real page of a paper and ten times the stamp.
const MinChars = 400

// MinShare is the fraction of a paper's pages that have to be typeset.
//
// Not all of them. A paper whose figures are full page images has pages with
// nothing on them but a caption, and a paper with a plate section at the back
// has a run of them, and neither is a scan. Three fifths is enough to say the
// text layer is real while still refusing a scan with a typeset cover sheet
// stapled to the front, which is what a 1990s submission often is.
const MinShare = 0.6

// NotInstalled is the error a machine without pdftotext fails with.
//
// Its own type for the same reason latexml.NotInstalled is: it is not a fault in
// the paper, the corpus or the command, and the answer is one line a person runs
// once.
type NotInstalled struct {
	Binary string
}

func (n *NotInstalled) Error() string {
	return fmt.Sprintf("pdftext: %s is not on the PATH, and the native path is reading a PDF's own text layer, so install it with brew install poppler or apt install poppler-utils", n.Binary)
}

// TooSlow is the error a read that ran out of its budget fails with.
type TooSlow struct {
	File  string
	After time.Duration
}

func (t *TooSlow) Error() string {
	return fmt.Sprintf("pdftext: reading %s was still running after %s and was stopped, which is a malformed PDF rather than a long paper, so this one wants a person", t.File, t.After)
}

// Unreadable is the error a file pdftotext refused fails with.
//
// A PDF that is encrypted, truncated or not a PDF at all. Its own type so a
// batch can step over one file the way it steps over a submission that turned
// out to be a PDF, because a paper whose bytes will not open is a fact about
// that paper and not a failure of the run.
type Unreadable struct {
	File string
	Said string
}

func (u *Unreadable) Error() string {
	said := u.Said
	if said == "" {
		said = "and said nothing about why"
	}
	return fmt.Sprintf("pdftext: %s refused %s: %s", Binary, u.File, said)
}

// Reader reads one PDF at a time.
type Reader struct {
	// Binary defaults to the package constant and exists so a test can point at
	// a script that behaves like pdftotext and is not pdftotext.
	Binary string
	// Timeout defaults to the package constant. Zero means the default rather
	// than no limit, for the same reason it does in the latexml package: the
	// dangerous value is the one that waits forever, and it is the one a caller
	// should have to write down.
	Timeout time.Duration
}

// Page is one page of a paper as it was printed.
type Page struct {
	// Number is the page's position in the file, counted from one. It is not the
	// number the paper prints on it, which can start anywhere and often does.
	Number int
	// Text is the characters that were typeset on the page, with the columns
	// kept apart and the gutters left as runs of spaces.
	Text string
}

// Chars is how much text the page holds, counting only what somebody could
// read.
//
// Spaces are not counted, and under -layout most of a page is spaces: a two
// column page is padded out to the width of the paper on every line. Counting
// bytes would make a mostly blank page look like a full one.
func (p Page) Chars() int {
	n := 0
	for _, r := range p.Text {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}

// Typeset says whether this page holds enough text to have been typeset rather
// than scanned.
func (p Page) Typeset() bool { return p.Chars() >= MinChars }

// Document is one PDF as pdftotext read it.
type Document struct {
	// File is the PDF this came out of.
	File string
	// Pages is every page in order, including the ones with nothing on them,
	// because a page count that skipped the blank ones would not be the paper's
	// page count.
	Pages []Page
	// Took is how long the read was.
	Took time.Duration
}

// Chars is how much text the whole paper holds.
func (d *Document) Chars() int {
	n := 0
	for _, p := range d.Pages {
		n += p.Chars()
	}
	return n
}

// Typeset is how many pages hold enough text to have been typeset.
func (d *Document) Typeset() int {
	n := 0
	for _, p := range d.Pages {
		if p.Typeset() {
			n++
		}
	}
	return n
}

// Born says whether this PDF has a text layer worth extracting.
//
// Born digital, which is the phrase for a PDF that was typeset into rather than
// scanned into. A paper that fails this is not a broken file and is not a broken
// paper: it is a scan of a typescript, or a submission made of page images, and
// it belongs on the vision path where a model reads the pictures.
func (d *Document) Born() bool {
	if len(d.Pages) == 0 {
		return false
	}
	return float64(d.Typeset()) >= MinShare*float64(len(d.Pages))
}

// Why says what the page counts came to, in a sentence.
//
// The judgement on its own is not enough to act on. A paper that missed by one
// page is worth a person's look and a paper with nothing on any page is a scan,
// and the two get the same answer from Born.
func (d *Document) Why() string {
	switch {
	case len(d.Pages) == 0:
		return "holds no pages at all, so there is nothing to read and nothing to say about why"
	case d.Born():
		return fmt.Sprintf("holds text on %d of its %d pages, which is a PDF that was typeset rather than scanned", d.Typeset(), len(d.Pages))
	case d.Typeset() == 0:
		return fmt.Sprintf("holds no text on any of its %d pages beyond what arXiv stamped down the margin, so it is a scan or a submission made of page images, and it is on the vision path", len(d.Pages))
	default:
		return fmt.Sprintf("holds text on %d of its %d pages, which is under the %.0f per cent this path needs, so the pages with nothing on them are images and it is on the vision path", d.Typeset(), len(d.Pages), MinShare*100)
	}
}

// Text is the whole paper, with a form feed between pages the way pdftotext
// wrote it.
//
// The form feeds are kept because the page a line was on is the one piece of
// position information this path has, and it is what the front matter's page
// count and the rules that read it are about.
func (d *Document) Text() string {
	var b strings.Builder
	for i, p := range d.Pages {
		if i > 0 {
			b.WriteString("\f")
		}
		b.WriteString(p.Text)
	}
	return b.String()
}

// Available says whether pdftotext can be run at all.
func (r *Reader) Available() error {
	if _, err := exec.LookPath(r.binary()); err != nil {
		return &NotInstalled{Binary: r.binary()}
	}
	return nil
}

// Version is what pdftotext says it is.
//
// Recorded rather than checked, the same way LaTeXML's version is. Poppler
// changes how it lays a page out between releases, so a corpus that cannot say
// which version read a paper cannot explain why re-reading it moved the text.
func (r *Reader) Version(ctx context.Context) (string, error) {
	// pdftotext prints its version banner on stderr and exits non-zero, which is
	// how the program has always behaved, so the output is what is read and the
	// status is not.
	out, _ := exec.CommandContext(ctx, r.binary(), "-v").CombinedOutput()
	line := strings.TrimSpace(string(bytes.SplitN(out, []byte("\n"), 2)[0]))
	if line == "" {
		return "", fmt.Errorf("pdftext: %s -v said nothing, so this is not the program it is meant to be", r.binary())
	}
	return line, nil
}

// Read is the text layer of one PDF.
//
// The output goes to standard output rather than to a file beside the PDF,
// because work/ holds arXiv's bytes and this is a reading of them: what is
// worth keeping is the Markdown the extraction writes, and a second copy of the
// same characters in a .txt is a file nothing reads and everything has to be
// kept in step with.
func (r *Reader) Read(ctx context.Context, file string) (*Document, error) {
	if err := r.Available(); err != nil {
		return nil, err
	}
	if _, err := os.Stat(file); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()

	// -layout keeps the columns apart, which is the whole reason this is usable
	// on a two column paper. -enc UTF-8 is what the corpus is in. -eol unix is
	// so a paper read on one machine is byte for byte the paper read on another.
	// -q keeps poppler's per page complaints out of the text, and the ones that
	// matter are on stderr where this reads them.
	cmd := exec.CommandContext(ctx, r.binary(), "-layout", "-enc", "UTF-8", "-eol", "unix", "-q", file, "-")
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	// The TikZ compiler needs a process group because TeX starts programs of its
	// own and killing only TeX leaves those holding the pipe. pdftotext starts
	// nothing, so a kill of the one process is the whole job, and WaitDelay is
	// here for the case where the kill does not take.
	cmd.WaitDelay = 2 * time.Second

	start := time.Now()
	err := cmd.Run()
	took := time.Since(start)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, &TooSlow{File: file, After: r.timeout()}
	}
	if err != nil {
		return nil, &Unreadable{File: file, Said: said(errs.Bytes(), err)}
	}
	return &Document{File: file, Pages: Pages(out.Bytes()), Took: took}, nil
}

// Pages splits pdftotext's output into the pages it was printed on.
//
// pdftotext writes a form feed at the end of every page, including the last, so
// a three page paper comes back as three chunks and one empty one. The trailing
// empty chunk is dropped and an empty chunk anywhere else is kept, because a
// blank page in the middle of a paper is a page of the paper.
func Pages(out []byte) []Page {
	text := strings.TrimSuffix(string(out), "\n")
	if text == "" {
		return nil
	}
	chunks := strings.Split(text, "\f")
	if last := len(chunks) - 1; strings.TrimSpace(chunks[last]) == "" {
		chunks = chunks[:last]
	}
	pages := make([]Page, 0, len(chunks))
	for i, c := range chunks {
		pages = append(pages, Page{Number: i + 1, Text: c})
	}
	return pages
}

// said is the sentence to put in an Unreadable, which is poppler's own
// complaint when it made one and the process error when it did not.
func said(errs []byte, err error) string {
	for _, line := range strings.Split(string(errs), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return err.Error()
}

func (r *Reader) binary() string {
	if r.Binary != "" {
		return r.Binary
	}
	return Binary
}

func (r *Reader) timeout() time.Duration {
	if r.Timeout > 0 {
		return r.Timeout
	}
	return Timeout
}

// Package vision reads a page of a paper by looking at a picture of it.
//
// The last of the four paths and the one that exists because the other three have
// run out. A paper on this path has no arXiv rendering, no source this project
// could convert, and no text layer in its PDF worth reading, which in practice
// means it was scanned, or typeset by something that wrote its glyphs without
// saying which characters they were, or submitted as a PDF and nothing else. There
// are tens of thousands of them and most are old, and the alternative to this path
// is leaving them out of the corpus.
//
// What is different about this path is that nothing it produces is derived from
// the paper by a rule. The other three paths transform: markup becomes blocks,
// printed characters become paragraphs, and a mistake is a mistake in a rule that
// can be found and fixed for every paper at once. This path asks something to read
// a picture and write down what it saw, and a mistake is a sentence that is not in
// the paper, in one paper, with nothing in the output saying so.
//
// So the whole of this package is arranged around not trusting the answer. The
// model is an external program rather than a library, so which program read a page
// is a fact on the disk. Which model and which prompt are recorded per paper, so a
// page read by a model nobody can name is a page that cannot be re-read. Every
// page goes through the nine acceptance rules in rules.go before it is kept, and a
// page that fails them is asked again at a higher resolution rather than published
// with a note. And a page that fails at every resolution on the ladder is left out
// and said to be left out, because a corpus with a hole in it is honest and a
// corpus with an invented paragraph in it is not.
package vision

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Ladder is the resolutions a page is asked at, in order.
//
// Three hundred dots is what a page of an article wants: it is the resolution
// scanners have used for text since scanners existed, a ten point body face comes
// out about forty pixels tall, and a page is about a megabyte. Four hundred is for
// the page that came back with a subscript wrong or a table column merged, which is
// the common failure and is a failure of small type rather than of the page. Six
// hundred is for the scanned page of a 1994 preprint, where the paper was
// photocopied before it was scanned and the fine strokes are half gone.
//
// Stopping at six hundred rather than going on, because above it the picture stops
// getting better: a scan made at three hundred dots has no more detail to show at
// twelve hundred, and all that grows is the file. A page that six hundred cannot
// read is a page a person has to look at.
var Ladder = []int{300, 400, 600}

// DefaultTimeout is how long one page gets with the model.
//
// Five minutes, which is long. A page of dense mathematics is thousands of tokens
// of LaTeX and a model that is being careful about a table is slower than one that
// is not, so the budget is set where a slow answer is still an answer and a hung
// one is not.
const DefaultTimeout = 5 * time.Minute

// DefaultPrompt is what the model is asked to do with a page.
//
// Written out here rather than composed from options, and hashed into every paper's
// record, because the prompt is half of what produced the text. A corpus that
// changed the wording and cannot say which papers were read under which wording
// cannot explain why two papers from the same month disagree about how a displayed
// equation is written.
//
// Everything in it is either a thing that has to be said or a thing this project
// learned by reading what came back when it was not said. Write only what is
// printed, because a model asked to read a page will otherwise finish a sentence
// the page truncated. Reading order, because a two column page read straight across
// is two half sentences. Nothing else, because a model that is pleased with itself
// writes "Here is the transcription" and the corpus publishes it.
const DefaultPrompt = `Write out the text of this page of an academic paper exactly as it is printed.

Follow these rules.

Write only what is printed on this page. Do not finish a sentence that runs off the bottom of the page, do not fill in a word that is unclear, and do not add anything that is not there.

Write it in reading order. If the page is in two columns, write all of the left column and then all of the right column.

Write a heading as a Markdown heading. Use one hash for a numbered section, two for a subsection, three for anything below that, and keep the printed number as part of the heading text.

Write mathematics as LaTeX. Inline mathematics goes between single dollars. A displayed equation goes on its own lines between double dollars, and if the page printed a number beside it, put that number in a \tag at the end of the mathematics.

Write a table as a Markdown pipe table, with the header row separated by a row of dashes. If the table has a caption, write the caption as a paragraph beside the table, starting with the word the page prints, so Table 3.

For a figure, write only its caption, starting with the word the page prints, so Figure 2. Do not describe the picture.

Write a code listing or an algorithm in a fenced code block.

Write each entry of a reference list on its own line, starting with the number the page prints in square brackets.

Leave out the running head, the page number, the journal stamp in the margin, the arXiv identifier printed down the side, and any watermark.

Write nothing else. No preface, no summary, no note about what you did, no apology.

If the page has nothing printed on it, write nothing at all.`

// PromptSHA256 is the hash of a prompt, as the record writes it.
//
// Hex of the whole thing and not a version number, because a prompt that was
// edited and not renumbered is exactly the case a version number misses.
func PromptSHA256(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:])
}

// NotInstalled is the error a machine without the reading program fails with.
type NotInstalled struct {
	Program string
}

func (n *NotInstalled) Error() string {
	if n.Program == "" {
		return "vision: no reading program was named, and the vision path cannot read a page without one, so pass -reader with a program that takes a picture and writes Markdown"
	}
	return fmt.Sprintf("vision: %s is not a program that can be run, and the vision path reads pages with it", n.Program)
}

// TooSlow is the error a page that ran out of its budget fails with.
type TooSlow struct {
	Image string
	After time.Duration
}

func (t *TooSlow) Error() string {
	return fmt.Sprintf("vision: nothing came back for %s after %s, so this page was given up on rather than waited on", t.Image, t.After)
}

// Unread is the error a page the program would not read fails with.
//
// Its own type because it is the ordinary failure of this path and not a fault in
// the corpus. A model that refused a page, a request that was rate limited, a key
// that expired: all of them are one page nobody read, and a batch steps over the
// paper and records why rather than stopping.
type Unread struct {
	Image string
	Said  string
}

func (u *Unread) Error() string {
	said := u.Said
	if said == "" {
		said = "and said nothing about why"
	}
	return fmt.Sprintf("vision: the reader would not read %s: %s", u.Image, said)
}

// Reader is the program that turns a picture of a page into Markdown.
//
// A program and not a client library, which is the decision this whole package
// rests on. It means the model is behind an interface this project does not own, so
// swapping one for another is a flag rather than a dependency; it means CI can
// exercise every line of this path with a shell script that prints a fixed page, so
// the tests cost nothing and need no network; and it means the thing that talks to
// a paid service is a file somebody can read.
//
// The contract is four lines. The program is run with the path to a PNG as its one
// argument. The prompt arrives on its standard input. The Markdown of the page is
// expected on its standard output, and anything on standard error is a complaint to
// be repeated. AX_VISION_MODEL holds the model to use, because which model is being
// asked is this package's business and how to reach it is the program's.
type Reader struct {
	// Program is what gets run. There is no default: a path that spends money
	// should not have one.
	Program string
	// Model is what the program is told to ask, and what the record says read the
	// page. Required for the same reason: a page read by an unnamed model is a page
	// nobody can read again the same way.
	Model string
	// Prompt defaults to DefaultPrompt.
	Prompt string
	// Timeout defaults to DefaultTimeout. Zero means the default and not no limit.
	Timeout time.Duration
}

// Available says whether this reader can be run at all.
func (r *Reader) Available() error {
	if strings.TrimSpace(r.Program) == "" {
		return &NotInstalled{}
	}
	if strings.ContainsRune(r.Program, os.PathSeparator) {
		if info, err := os.Stat(r.Program); err != nil || info.IsDir() {
			return &NotInstalled{Program: r.Program}
		}
		return nil
	}
	if _, err := exec.LookPath(r.Program); err != nil {
		return &NotInstalled{Program: r.Program}
	}
	return nil
}

// Read is one page of Markdown from one picture of a page.
//
// What comes back is returned as it arrived apart from having its ends trimmed.
// Nothing here judges it, because judging it is what the rules are for and a page
// that is wrong in a way this package can name should be named by a rule with an
// identifier rather than dropped quietly here.
func (r *Reader) Read(ctx context.Context, image string) (string, error) {
	if err := r.Available(); err != nil {
		return "", err
	}
	if strings.TrimSpace(r.Model) == "" {
		return "", fmt.Errorf("vision: no model was named, and a page read by a model the corpus cannot name is a page nobody can read again, so pass -model")
	}
	if _, err := os.Stat(image); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()
	cmd := exec.CommandContext(ctx, r.Program, image)
	cmd.Stdin = strings.NewReader(r.prompt())
	var out, errs bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errs
	// The model's own environment, plus the two things this package has to say. A
	// program that talks to a service needs the whole environment for its
	// credentials, so this adds rather than replaces.
	cmd.Env = append(os.Environ(), "AX_VISION_MODEL="+r.Model, "AX_VISION_IMAGE="+image)
	// A reading program is usually something that holds an HTTP request open, and a
	// killed process with a request in flight can take a moment to notice.
	cmd.WaitDelay = 5 * time.Second

	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", &TooSlow{Image: image, After: r.timeout()}
	}
	if err != nil {
		return "", &Unread{Image: image, Said: said(errs.Bytes(), err)}
	}
	return strings.TrimSpace(out.String()), nil
}

// said is the sentence to put in an Unread, which is the program's own complaint
// when it made one and the process error when it did not.
func said(errs []byte, err error) string {
	for _, line := range strings.Split(string(errs), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return err.Error()
}

func (r *Reader) prompt() string {
	if strings.TrimSpace(r.Prompt) != "" {
		return r.Prompt
	}
	return DefaultPrompt
}

func (r *Reader) timeout() time.Duration {
	if r.Timeout > 0 {
		return r.Timeout
	}
	return DefaultTimeout
}

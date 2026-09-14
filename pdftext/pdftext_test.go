package pdftext

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stub is a program that behaves the way pdftotext behaves and is not pdftotext.
//
// The tests need a reader that fails on purpose, hangs on purpose and writes a
// chosen number of pages on purpose, and they have to run on a machine with no
// poppler installed because CI is one. What is under test here is this package:
// the flags it passes, the budget it enforces, and what it makes of what comes
// back.
func stub(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pdftotext")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// file is a PDF as far as this package is concerned, which is a path that
// exists. Everything about what is inside it comes back from the program.
func file(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "2312.00752v2.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// typeset is a page holding as much text as a page of a paper does, which is
// well over the threshold, with the runs of spaces -layout leaves in a gutter.
func typeset() string {
	var b strings.Builder
	for i := 0; i < 20; i++ {
		b.WriteString("the argument is laid out at length     and it continues in the other column\n")
	}
	return b.String()
}

// scanned is a page arXiv stamped down the margin and nothing else, which is
// what a scan of a typescript comes back as.
func scanned() string {
	return "a\nr\nX\ni\nv\n:\n2\n3\n1\n2\n.\n0\n0\n7\n5\n2\nv\n2\n"
}

// pdftotext writes a form feed at the end of every page, including the last, so
// the output of a three page paper ends with one and has to not read as four.
func TestPagesDropTheEmptyChunkAtTheEnd(t *testing.T) {
	pages := Pages([]byte("one\f two\fthree\f"))
	if len(pages) != 3 {
		t.Fatalf("read %d pages out of three", len(pages))
	}
	for i, p := range pages {
		if p.Number != i+1 {
			t.Errorf("the page at index %d calls itself %d", i, p.Number)
		}
	}
}

// A blank page in the middle of a paper is a page of the paper, and a page count
// that skipped it would not be the count the paper prints.
func TestABlankPageInTheMiddleIsStillAPage(t *testing.T) {
	pages := Pages([]byte("one\f\fthree\f"))
	if len(pages) != 3 {
		t.Fatalf("read %d pages, and the blank one in the middle is one of them", len(pages))
	}
	if pages[1].Chars() != 0 {
		t.Errorf("the blank page holds %d characters", pages[1].Chars())
	}
}

func TestPagesOfNothingIsNoPages(t *testing.T) {
	if pages := Pages(nil); pages != nil {
		t.Fatalf("read %d pages out of nothing at all", len(pages))
	}
}

// Under -layout most of a page is spaces, because every line is padded out to
// the width of the paper. Counting bytes would make a page with a caption on it
// look like a page of prose.
func TestSpacesAreNotText(t *testing.T) {
	p := Page{Text: strings.Repeat(" ", 4000) + "\n\n\t" + "Figure 1: a diagram."}
	if got := p.Chars(); got != 17 {
		t.Fatalf("counted %d characters in a page holding one caption", got)
	}
	if p.Typeset() {
		t.Error("a page holding one caption was read as typeset")
	}
}

func TestAPaperTypesetOnEveryPageIsBornDigital(t *testing.T) {
	d := &Document{Pages: Pages([]byte(typeset() + "\f" + typeset() + "\f"))}
	if !d.Born() {
		t.Fatalf("%s", d.Why())
	}
	if d.Typeset() != 2 {
		t.Errorf("%d of its pages are typeset", d.Typeset())
	}
	if !strings.Contains(d.Why(), "typeset rather than scanned") {
		t.Errorf("it says it %s", d.Why())
	}
}

// The threshold has to clear arXiv's own stamp and nothing else, because the
// stamp is the whole of what a scan comes back with and a threshold under it
// would read every scan on arXiv as a paper this path can do.
func TestAScanWithNothingButTheStampIsNotBornDigital(t *testing.T) {
	d := &Document{Pages: Pages([]byte(scanned() + "\f" + scanned() + "\f" + scanned() + "\f"))}
	if d.Born() {
		t.Fatal("a scan carrying only the margin stamp was read as typeset")
	}
	if !strings.Contains(d.Why(), "no text on any of its 3 pages") {
		t.Errorf("it says it %s", d.Why())
	}
	if !strings.Contains(d.Why(), "vision path") {
		t.Errorf("the sentence does not say where the paper goes: %s", d.Why())
	}
}

// A paper whose figures are full page images has pages with nothing on them but
// a caption, and that is not a scan. The share is what tells the two apart, so
// it is worth a case either side of it.
func TestSomePagesOfImagesAreStillABornDigitalPaper(t *testing.T) {
	four := typeset() + "\f" + typeset() + "\f" + typeset() + "\f" + typeset() + "\f"
	plates := scanned() + "\f" + scanned() + "\f"

	six := &Document{Pages: Pages([]byte(four + plates))}
	if !six.Born() {
		t.Errorf("four typeset pages and two plates: %s", six.Why())
	}
	seven := &Document{Pages: Pages([]byte(four + plates + scanned() + "\f"))}
	if seven.Born() {
		t.Errorf("four typeset pages and three plates: %s", seven.Why())
	}
	if !strings.Contains(seven.Why(), "under the 60 per cent") {
		t.Errorf("it says it %s", seven.Why())
	}
}

func TestAPDFWithNoPagesIsNotBornDigital(t *testing.T) {
	d := &Document{}
	if d.Born() {
		t.Fatal("a PDF with no pages in it was read as typeset")
	}
	if !strings.Contains(d.Why(), "no pages at all") {
		t.Errorf("it says it %s", d.Why())
	}
}

// The page a line was on is the one piece of position information this path has,
// so the form feeds have to survive being split and put back.
func TestTheTextGoesBackTogetherWithItsPageBreaks(t *testing.T) {
	const out = "one\f two\fthree"
	d := &Document{Pages: Pages([]byte(out + "\f"))}
	if d.Text() != out {
		t.Fatalf("got %q, want %q", d.Text(), out)
	}
}

// -layout is the flag the whole path depends on, because without it a two column
// paper comes back with the columns interleaved line by line and there is nothing
// left to recover. A test that only checked the text came back would pass with it
// dropped.
func TestTheFlagsSayLayoutAndUTF8AndUnixLineEndings(t *testing.T) {
	// The stub prints what it was called with where the text of a page would be,
	// which is the one place a test can read it from.
	pdf := file(t)
	doc, err := (&Reader{Binary: stub(t, `echo "$@"; printf '\f'`)}).Read(context.Background(), pdf)
	if err != nil {
		t.Fatal(err)
	}
	args := doc.Pages[0].Text
	for _, want := range []string{"-layout", "-enc UTF-8", "-eol unix", "-q", pdf, " -"} {
		if !strings.Contains(args, want) {
			t.Errorf("%q is not in %q", want, args)
		}
	}
}

func TestReadSplitsWhatTheProgramWroteIntoPages(t *testing.T) {
	r := &Reader{Binary: stub(t, `printf 'page one\fpage two\f'`)}
	doc, err := r.Read(context.Background(), file(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) != 2 || doc.Pages[1].Text != "page two" {
		t.Fatalf("read %v", doc.Pages)
	}
	if doc.File == "" {
		t.Error("the document does not say which file it came out of")
	}
}

// A PDF that is encrypted, truncated or not a PDF at all. Its own type so a
// batch steps over one paper rather than stopping, and it carries poppler's own
// sentence because that sentence is what says which of the three it was.
func TestAFileTheProgramRefusedSaysWhatItSaid(t *testing.T) {
	r := &Reader{Binary: stub(t, `echo "Command Line Error: Incorrect password" > /dev/stderr; exit 1`)}
	_, err := r.Read(context.Background(), file(t))
	var bad *Unreadable
	if !errors.As(err, &bad) {
		t.Fatalf("got %v, want a file the program refused", err)
	}
	if !strings.Contains(bad.Error(), "Incorrect password") {
		t.Errorf("the error reads %q", bad.Error())
	}
}

// A program that failed and said nothing still has to produce a sentence, and
// the process error is the only thing left to make one out of.
func TestAFileTheProgramRefusedSilentlyStillSaysSomething(t *testing.T) {
	r := &Reader{Binary: stub(t, `exit 3`)}
	_, err := r.Read(context.Background(), file(t))
	var bad *Unreadable
	if !errors.As(err, &bad) {
		t.Fatalf("got %v, want a file the program refused", err)
	}
	if bad.Said == "" {
		t.Error("the error says nothing about why")
	}
}

func TestAReadThatRanOutOfItsBudgetIsStopped(t *testing.T) {
	r := &Reader{Binary: stub(t, `sleep 30`), Timeout: 100 * time.Millisecond}
	start := time.Now()
	_, err := r.Read(context.Background(), file(t))
	var slow *TooSlow
	if !errors.As(err, &slow) {
		t.Fatalf("got %v, want a read that ran out of its budget", err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("the budget was a tenth of a second and the read took %s", took)
	}
}

func TestAMachineWithoutTheProgramSaysHowToInstallIt(t *testing.T) {
	r := &Reader{Binary: filepath.Join(t.TempDir(), "not-here")}
	err := r.Available()
	var gone *NotInstalled
	if !errors.As(err, &gone) {
		t.Fatalf("got %v, want a missing program", err)
	}
	if !strings.Contains(err.Error(), "brew install poppler") {
		t.Errorf("the error reads %q", err)
	}
	// Read asks the same question first, so a caller that skipped Available
	// gets the same sentence rather than a failure to execute.
	if _, err := r.Read(context.Background(), file(t)); !errors.As(err, &gone) {
		t.Errorf("Read got %v", err)
	}
}

// A PDF that is not on disk is the ordinary state of a fresh clone, because
// work/ is not committed. It comes back as the file system's own error so that a
// caller can tell it apart from a file the program refused.
func TestAPDFThatIsNotThereFailsBeforeTheProgramRuns(t *testing.T) {
	r := &Reader{Binary: stub(t, `printf 'nothing to read\f'`)}
	_, err := r.Read(context.Background(), filepath.Join(t.TempDir(), "missing.pdf"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("got %v, want the file not being there", err)
	}
}

// pdftotext prints its version on stderr and exits non-zero, which is how the
// program has always behaved, so reading the status instead of the output would
// make every machine look like it had the wrong program.
func TestTheVersionIsReadOffStderrAndNotOffTheExitStatus(t *testing.T) {
	r := &Reader{Binary: stub(t, `echo "pdftotext version 26.09.0" > /dev/stderr; exit 99`)}
	got, err := r.Version(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "pdftotext version 26.09.0" {
		t.Fatalf("got %q", got)
	}
}

func TestAProgramThatSaysNothingAboutItsVersionIsNotTheRightProgram(t *testing.T) {
	r := &Reader{Binary: stub(t, `exit 0`)}
	if _, err := r.Version(context.Background()); err == nil {
		t.Fatal("a program that printed no version banner was accepted")
	}
}

// Zero means the package default and not no limit, because the dangerous value
// is the one that waits forever and it is the one a caller should have to write
// down.
func TestAnUnsetTimeoutIsTheDefaultAndNotForever(t *testing.T) {
	if got := (&Reader{}).timeout(); got != Timeout {
		t.Fatalf("an unset timeout is %s", got)
	}
	if got := (&Reader{Timeout: time.Second}).timeout(); got != time.Second {
		t.Fatalf("a timeout of a second is %s", got)
	}
}

func TestAnUnsetBinaryIsTheOneThisPackageIsAbout(t *testing.T) {
	if got := (&Reader{}).binary(); got != Binary {
		t.Fatalf("an unset binary is %q", got)
	}
}

// The threshold is a number with an argument behind it, and the argument is that
// it clears arXiv's stamp by a wide margin. A change that brought it near the
// stamp would make every scan on arXiv read as a paper this path can do, and
// nothing else in the package would notice.
func TestTheThresholdClearsTheMarginStampSeveralTimesOver(t *testing.T) {
	stamp := Page{Text: scanned()}.Chars()
	if stamp == 0 {
		t.Fatal("the stamp fixture holds no characters")
	}
	if MinChars < 5*stamp {
		t.Errorf("the threshold is %d and arXiv's stamp is %d characters, which is too close to tell a scan from a paper", MinChars, stamp)
	}
}

func TestEveryErrorHereNamesTheFileOrTheProgram(t *testing.T) {
	for _, err := range []error{
		&NotInstalled{Binary: "pdftotext"},
		&TooSlow{File: "work/pdf/2312/2312.00752v2.pdf", After: Timeout},
		&Unreadable{File: "work/pdf/2312/2312.00752v2.pdf", Said: "Command Line Error"},
		&Unreadable{File: "work/pdf/2312/2312.00752v2.pdf"},
	} {
		s := err.Error()
		if !strings.HasPrefix(s, "pdftext: ") {
			t.Errorf("%q does not say which package it came from", s)
		}
		if !strings.Contains(s, "pdftotext") && !strings.Contains(s, "2312.00752") {
			t.Errorf("%q names neither the file nor the program", s)
		}
	}
	// The sentence a caller reads has to be a sentence, and an empty field in
	// the middle of one is the way that stops being true.
	if s := (&Unreadable{File: "a.pdf"}).Error(); strings.Contains(s, ": :") {
		t.Errorf("%q has a hole in it", s)
	}
}

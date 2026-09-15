package raster

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stub is a program that behaves the way pdftoppm behaves and is not pdftoppm.
//
// The same arrangement the pdftext tests use and for the same reason: CI has no
// poppler, the tests need a rasteriser that fails on purpose and hangs on purpose,
// and what is under test is this package rather than poppler. The script is handed
// the flags this package passes, so a stub that writes a file at "$8" is a stub that
// only works if the argument order is what the package thinks it is.
func stub(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pdftoppm")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// file is a PDF as far as this package is concerned, which is a path that exists.
func file(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "9901.00001v1.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// writes is a stub that writes a picture where the package told it to, which is
// the last argument with the format's extension put back on.
//
// This is the one fact about pdftoppm's interface the package depends on: with
// -singlefile the output is the prefix plus the extension and nothing else, so a
// stub that honours that is a stub that catches the package getting it wrong.
const writes = `for last; do :; done; printf 'PNG' > "$last.png"`

func TestNameSaysThePageAndTheResolution(t *testing.T) {
	if got := Name(3, 300); got != "p003@300.png" {
		t.Errorf("page three at three hundred dots is called %q", got)
	}
	// A paper with more pages than three digits can hold still sorts, because the
	// width only pads and does not truncate.
	if got := Name(1204, 600); got != "p1204@600.png" {
		t.Errorf("page 1204 is called %q", got)
	}
}

func TestPaintWritesThePictureThePackageNames(t *testing.T) {
	dir := t.TempDir()
	p := &Painter{Binary: stub(t, writes)}
	out, err := p.Paint(context.Background(), file(t), 7, 300, dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "p007@300.png"); out != want {
		t.Errorf("it says it wrote %s and this package names that picture %s", out, want)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("it returned a path with nothing at it: %v", err)
	}
}

// A page that has already been drawn is not drawn again, which is most of what
// makes the vision path resumable: a run that stopped on page thirty of forty
// redraws nothing.
func TestPaintLeavesAPictureThatIsAlreadyThere(t *testing.T) {
	dir := t.TempDir()
	held := filepath.Join(dir, Name(2, 400))
	if err := os.WriteFile(held, []byte("the picture from last time"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A stub that would fail if it ran at all, so the test can only pass if the
	// package did not run it.
	p := &Painter{Binary: stub(t, "exit 3")}
	out, err := p.Paint(context.Background(), file(t), 2, 400, dir)
	if err != nil {
		t.Fatalf("it drew a page that was already drawn: %v", err)
	}
	if out != held {
		t.Errorf("it returned %s and the picture is at %s", out, held)
	}
	b, err := os.ReadFile(held)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "the picture from last time" {
		t.Errorf("the picture was overwritten, and it now holds %q", b)
	}
}

// An empty file is not a picture. A rasteriser that was killed halfway through
// leaves one behind, and treating it as done would mean a page nobody can read
// and nothing saying why.
func TestPaintDrawsOverAPictureOfNoBytes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, Name(1, 300)), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	p := &Painter{Binary: stub(t, writes)}
	out, err := p.Paint(context.Background(), file(t), 1, 300, dir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Error("the empty picture from the interrupted run is still empty, so it was taken for a finished one")
	}
}

// The flags matter, and a package that passed the resolution where the page goes
// would ask for a page of a resolution. The stub writes down what it was given.
func TestPaintAsksForTheOnePageAtTheOneResolution(t *testing.T) {
	dir := t.TempDir()
	said := filepath.Join(t.TempDir(), "said")
	p := &Painter{Binary: stub(t, `printf '%s\n' "$@" > `+said+`; `+writes)}
	if _, err := p.Paint(context.Background(), file(t), 9, 600, dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(said)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Fields(string(b))
	for _, want := range []string{"-png", "-r", "600", "-f", "9", "-l", "9", "-singlefile"} {
		found := false
		for _, arg := range args {
			if arg == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("it did not pass %s, and what it passed was %v", want, args)
		}
	}
}

// poppler exits 99 with "Wrong page range given" for a page the file does not
// have, and what a caller needs out of that is one paper to step over rather than
// a run that stops.
func TestPaintReportsAPageTheFileDoesNotHave(t *testing.T) {
	p := &Painter{Binary: stub(t, `echo "Wrong page range given: the first page (99) can not be after the last page (18)." >&2; exit 99`)}
	_, err := p.Paint(context.Background(), file(t), 99, 300, t.TempDir())
	var bad *Unreadable
	if !errors.As(err, &bad) {
		t.Fatalf("a page the file does not have came back as %T: %v", err, err)
	}
	if !strings.Contains(bad.Said, "Wrong page range") {
		t.Errorf("it did not repeat what poppler said, and it said %q", bad.Said)
	}
	if bad.Page != 99 {
		t.Errorf("the error is about page %d", bad.Page)
	}
}

// A rasteriser that exits cleanly and writes nothing is the one case a caller
// must never be handed quietly, because the next thing that happens is a model
// being shown a file that is not there.
func TestPaintRefusesASilentFailure(t *testing.T) {
	p := &Painter{Binary: stub(t, "exit 0")}
	_, err := p.Paint(context.Background(), file(t), 1, 300, t.TempDir())
	var bad *Unreadable
	if !errors.As(err, &bad) {
		t.Fatalf("a clean exit with no picture came back as %T: %v", err, err)
	}
	if !strings.Contains(bad.Said, "wrote no picture") {
		t.Errorf("the message does not say what happened: %q", bad.Said)
	}
}

func TestPaintStopsAPageThatRunsTooLong(t *testing.T) {
	p := &Painter{Binary: stub(t, "sleep 30"), Timeout: 100 * time.Millisecond}
	_, err := p.Paint(context.Background(), file(t), 1, 300, t.TempDir())
	var slow *TooSlow
	if !errors.As(err, &slow) {
		t.Fatalf("a rasteriser that hung came back as %T: %v", err, err)
	}
	if slow.DPI != 300 || slow.Page != 1 {
		t.Errorf("the error is about page %d at %d dots", slow.Page, slow.DPI)
	}
}

func TestPaintRefusesAPageBeforeTheFirstOne(t *testing.T) {
	p := &Painter{Binary: stub(t, writes)}
	if _, err := p.Paint(context.Background(), file(t), 0, 300, t.TempDir()); err == nil {
		t.Error("page zero was accepted, and pages are counted from one")
	}
	if _, err := p.Paint(context.Background(), file(t), 1, 0, t.TempDir()); err == nil {
		t.Error("a resolution of zero was accepted")
	}
}

func TestAvailableSaysWhatToInstall(t *testing.T) {
	p := &Painter{Binary: filepath.Join(t.TempDir(), "not-a-program")}
	err := p.Available()
	var missing *NotInstalled
	if !errors.As(err, &missing) {
		t.Fatalf("a missing rasteriser came back as %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "poppler") {
		t.Errorf("the message does not say what to install: %q", err)
	}
}

// pdftoppm prints its banner on stderr and exits zero, unlike pdftotext, which
// exits non-zero. A version reader written for the other one would come back empty
// here, and an empty version is a corpus that cannot say what drew its pictures.
func TestVersionReadsTheBannerOffStandardError(t *testing.T) {
	p := &Painter{Binary: stub(t, `echo "pdftoppm version 26.09.0" >&2; exit 0`)}
	got, err := p.Version(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "pdftoppm version 26.09.0" {
		t.Errorf("it read the version as %q", got)
	}
}

func TestVersionRefusesAProgramThatSaysNothing(t *testing.T) {
	p := &Painter{Binary: stub(t, "exit 0")}
	if _, err := p.Version(context.Background()); err == nil {
		t.Error("a program that printed no banner was taken for pdftoppm")
	}
}

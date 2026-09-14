package source

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// body is a document with both of the lines that make one, which is what every
// candidate in these tests is built out of.
const body = "\\documentclass{article}\n\\begin{document}\nA paper about nothing in particular.\n\\end{document}\n"

func gz(t *testing.T, b []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w := gzip.NewWriter(&out)
	if _, err := w.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// tarball is a submission, as arXiv serves it. The order is the order the
// entries are written in, because a tar has an order and the main file is not
// always the first thing in it.
func tarball(t *testing.T, files ...File) []byte {
	t.Helper()
	var out bytes.Buffer
	w := tar.NewWriter(&out)
	for _, f := range files {
		if err := w.WriteHeader(&tar.Header{Name: f.Name, Mode: 0o644, Size: int64(len(f.Data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(f.Data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return gz(t, out.Bytes())
}

// opened is a submission of the files named, read back the way a caller reads
// it.
func opened(t *testing.T, files ...File) *Bundle {
	t.Helper()
	b, err := Open(tarball(t, files...))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func text(name, s string) File { return File{Name: name, Data: []byte(s)} }

func TestOpenReadsATarball(t *testing.T) {
	b := opened(t, text("ms.tex", body), text("fig1.pdf", "not really a pdf"))
	if !b.Tarred {
		t.Fatal("a tar was read as a single file")
	}
	if got := strings.Join(b.Names(), " "); got != "fig1.pdf ms.tex" {
		t.Fatalf("holds %s", got)
	}
	if data, ok := b.Find("ms.tex"); !ok || string(data) != body {
		t.Fatalf("ms.tex came back as %q, %v", data, ok)
	}
}

// The other ordinary submission, which is one TeX file with no tar around it.
// It arrives with no name, because the tar is what carries names, so the answer
// to the main file question is the only file there is.
func TestOpenReadsASingleTeXFile(t *testing.T) {
	b, err := Open(gz(t, []byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	if b.Tarred {
		t.Fatal("a bare TeX file was read as a tar")
	}
	if len(b.Files) != 1 || b.Files[0].Name != "paper.tex" {
		t.Fatalf("holds %v", b.Names())
	}
	main, err := b.Main()
	if err != nil || main != "paper.tex" {
		t.Fatalf("got %q, %v", main, err)
	}
}

// A PDF the author made themselves is a real submission and not a broken
// download, so it has to come back as a fact about the paper. arXiv serves some
// of them gzipped and some of them not, and both are the same fact.
func TestOpenSaysSoWhenTheSubmissionIsAPDF(t *testing.T) {
	raw := []byte("%PDF-1.5\nbinary rubbish follows")
	for what, in := range map[string][]byte{"bare": raw, "gzipped": gz(t, raw)} {
		_, err := Open(in)
		var pdf *PDFOnly
		if !errors.As(err, &pdf) {
			t.Fatalf("%s: got %v, want a PDF only submission", what, err)
		}
		if !strings.Contains(err.Error(), "native path") {
			t.Fatalf("%s: the error does not name the path it belongs on: %v", what, err)
		}
	}
}

func TestOpenRefusesBytesThatAreNotAnEPrintAtAll(t *testing.T) {
	if _, err := Open([]byte("<html>an error page</html>")); err == nil {
		t.Fatal("an HTML page was read as a submission")
	}
	if _, err := Open(gz(t, []byte("a note to the moderators"))); err == nil {
		t.Fatal("a file with no TeX in it was read as TeX")
	}
}

// The oldest bug in archive handling. An entry naming a path outside the
// submission is refused rather than trimmed into shape, because unpacking it
// writes wherever it says.
func TestOpenRefusesAnEntryThatPointsOutsideTheSubmission(t *testing.T) {
	for _, name := range []string{"../ms.tex", "/etc/passwd", "a/../../b.tex"} {
		if _, err := Open(tarball(t, text(name, body))); err == nil {
			t.Fatalf("%q was read as a file of the submission", name)
		}
	}
}

func TestMainTakesTheREADMEsWordInEitherFormat(t *testing.T) {
	for what, readme := range map[string]File{
		"json": text("00README.json", `{"sources":[{"filename":"second.tex","usage":"toplevelfile"}]}`),
		"xxx":  text("00README.XXX", "second.tex toplevelfile\nfirst.tex ignore\n"),
	} {
		b := opened(t, readme, text("first.tex", body), text("second.tex", body))
		main, err := b.Main()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if main != "second.tex" {
			t.Fatalf("%s: got %q, and the submitter said second.tex", what, main)
		}
	}
}

// The submitter saying so outright beats every guess in this package, so a
// README naming a file that is not there is worth stopping on rather than
// quietly guessing past.
func TestMainReportsAREADMENamingAFileThatIsNotThere(t *testing.T) {
	b := opened(t, text("00README.XXX", "gone.tex toplevelfile\n"), text("ms.tex", body))
	if _, err := b.Main(); err == nil || !strings.Contains(err.Error(), "gone.tex") {
		t.Fatalf("got %v", err)
	}
}

func TestMainFindsTheOneFileThatBeginsADocument(t *testing.T) {
	b := opened(t,
		text("sections/intro.tex", "The introduction, which begins no document.\n"),
		text("thesis.tex", body),
		text("refs.bib", "@article{a, title={A}}"),
	)
	main, err := b.Main()
	if err != nil || main != "thesis.tex" {
		t.Fatalf("got %q, %v", main, err)
	}
}

// A preamble in a comment is a preamble somebody turned off, and a submission
// where the real main file is chosen by a commented out line is a submission
// this tool reads wrong every time.
func TestMainStepsOverACommentedOutPreamble(t *testing.T) {
	b := opened(t, text("old.tex", "% \\documentclass{article}\n% \\begin{document}\n"), text("ms.tex", body))
	main, err := b.Main()
	if err != nil || main != "ms.tex" {
		t.Fatalf("got %q, %v", main, err)
	}
}

// The case the include graph is for. Every chapter carries its own preamble so
// that it compiles alone, so every chapter looks like a document, and the paper
// is the one nothing else reads.
func TestMainDropsAChapterAnotherFileIncludes(t *testing.T) {
	b := opened(t,
		text("chapter1.tex", body),
		text("chapter2.tex", body),
		text("everything.tex", "\\documentclass{book}\n\\begin{document}\n\\include{chapter1}\n\\input{chapter2.tex}\n\\end{document}\n"),
	)
	main, err := b.Main()
	if err != nil || main != "everything.tex" {
		t.Fatalf("got %q, %v", main, err)
	}
}

// Two candidates that nothing else includes and nothing else tells apart, so
// the convention decides. It is the last pass because it is only a convention.
func TestMainFallsBackToTheNameThatIsConventional(t *testing.T) {
	b := opened(t, text("cover-letter.tex", body), text("ms.tex", body))
	main, err := b.Main()
	if err != nil || main != "ms.tex" {
		t.Fatalf("got %q, %v", main, err)
	}
}

// Two papers in one upload, which is a submission a person has to look at. The
// error names both files, because naming neither would leave them opening the
// tarball by hand to find out what it is asking about.
func TestMainReportsTwoPapersInOneUpload(t *testing.T) {
	b := opened(t, text("first-paper.tex", body), text("second-paper.tex", body))
	_, err := b.Main()
	if err == nil {
		t.Fatal("picked one of two papers")
	}
	for _, name := range []string{"first-paper.tex", "second-paper.tex"} {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("the error does not name %s: %v", name, err)
		}
	}
}

func TestMainReportsASubmissionWithNoDocumentInIt(t *testing.T) {
	b := opened(t, text("notes.tex", "Some notes, and no document.\n"))
	if _, err := b.Main(); err == nil || !strings.Contains(err.Error(), "notes.tex") {
		t.Fatalf("got %v", err)
	}
}

func TestWriteUnpacksTheSubmission(t *testing.T) {
	dir := t.TempDir()
	b := opened(t, text("ms.tex", body), text("figures/one.pdf", "a picture"))
	if err := b.Write(dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "figures", "one.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "a picture" {
		t.Fatalf("the file holds %q", got)
	}
}

// Files is exported, so a bundle a caller built themselves can hold anything,
// and the check that stopped a tar entry escaping has to be here as well.
func TestWriteRefusesToWriteOutsideTheDirectory(t *testing.T) {
	dir := t.TempDir()
	b := &Bundle{Files: []File{text("../escaped.tex", body)}}
	if err := b.Write(dir); err == nil {
		t.Fatal("wrote outside the directory it was given")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escaped.tex")); err == nil {
		t.Fatal("the file is there")
	}
}

func TestReadSaysHowToGetAnEPrintThatIsNotThere(t *testing.T) {
	_, err := Read(filepath.Join(t.TempDir(), "2312.00752v2.gz"))
	if err == nil || !strings.Contains(err.Error(), "ax fetch source") {
		t.Fatalf("got %v", err)
	}
}

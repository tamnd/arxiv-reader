package extract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fileSet(names ...string) []File {
	out := make([]File, 0, len(names))
	for i, name := range names {
		out = append(out, File{
			Name: name,
			Doc: Document{
				Front: Front{Paper: "2501.00001", Section: i, Kind: "section"},
				Body:  "The body of " + name + ".\n",
			},
		})
	}
	return out
}

func states(results []Result) map[string]State {
	out := map[string]State{}
	for _, r := range results {
		out[r.Name] = r.State
	}
	return out
}

// git status after a re-run of a finished paper is empty, and that is the test
// that this stage is honest rather than a claim about it.
func TestWritingTwiceWritesOnce(t *testing.T) {
	dir := t.TempDir()
	files := fileSet("00_front.md", "01_introduction.md")
	first, err := Write(dir, files, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range first {
		if r.State != StateWritten {
			t.Fatalf("%s came back %s on a first write", r.Name, r.State)
		}
	}
	second, err := Write(dir, files, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range second {
		if r.State != StateUnchanged {
			t.Fatalf("%s came back %s on a second write", r.Name, r.State)
		}
	}
}

// A person's work outranks a re-run of a converter, and force is the way past
// that and is deliberately not the default.
func TestACorrectionIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	files := fileSet("00_front.md")
	if _, err := Write(dir, files, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "00_front.md")
	correct(t, path, "The body of 00_front.md.", "The corrected body.")

	results, err := Write(dir, files, false)
	if err != nil {
		t.Fatal(err)
	}
	if states(results)["00_front.md"] != StateProtected {
		t.Fatalf("got %s, want protected", states(results)["00_front.md"])
	}
	if !strings.Contains(readFile(t, path), "The corrected body.") {
		t.Fatal("the correction was thrown away")
	}

	forced, err := Write(dir, files, true)
	if err != nil {
		t.Fatal(err)
	}
	if states(forced)["00_front.md"] != StateWritten {
		t.Fatalf("force did not overwrite, got %s", states(forced)["00_front.md"])
	}
}

// A file marked edited has been decided about by a person, which is a stronger
// claim than a hash that does not match.
func TestAFileMarkedEditedIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	files := fileSet("00_front.md")
	if _, err := Write(dir, files, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Accept(dir, "00_front.md"); err != nil {
		t.Fatal(err)
	}
	edited := files
	edited[0].Doc.Body = "Something else entirely.\n"
	results, err := Write(dir, edited, false)
	if err != nil {
		t.Fatal(err)
	}
	if states(results)["00_front.md"] != StateProtected {
		t.Fatalf("got %s, want protected", states(results)["00_front.md"])
	}
}

// A re-extraction that produces one section where there were two has to take
// the second file away, because a content plane holding a section the paper no
// longer has is a corpus publishing something arXiv does not.
func TestAFileThatIsNoLongerPartOfThePaperIsRemoved(t *testing.T) {
	dir := t.TempDir()
	if _, err := Write(dir, fileSet("00_front.md", "01_introduction.md"), false); err != nil {
		t.Fatal(err)
	}
	results, err := Write(dir, fileSet("00_front.md"), false)
	if err != nil {
		t.Fatal(err)
	}
	if states(results)["01_introduction.md"] != StateRemoved {
		t.Fatalf("got %s, want removed", states(results)["01_introduction.md"])
	}
	if _, err := os.Stat(filepath.Join(dir, "01_introduction.md")); !os.IsNotExist(err) {
		t.Fatal("the file is still there")
	}
}

// Somebody who put notes.md in a paper's directory did so on purpose, and a
// converter is not entitled to an opinion about it.
func TestOnlyFilesThisPackageWroteAreRemoved(t *testing.T) {
	dir := t.TempDir()
	if _, err := Write(dir, fileSet("00_front.md"), false); err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(notes, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(dir, fileSet("00_front.md"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(notes); err != nil {
		t.Fatalf("notes.md was taken away: %v", err)
	}
}

func TestARemovedFileThatWasCorrectedIsKept(t *testing.T) {
	dir := t.TempDir()
	if _, err := Write(dir, fileSet("00_front.md", "01_introduction.md"), false); err != nil {
		t.Fatal(err)
	}
	correct(t, filepath.Join(dir, "01_introduction.md"), "The body of 01_introduction.md.", "Mine now.")
	results, err := Write(dir, fileSet("00_front.md"), false)
	if err != nil {
		t.Fatal(err)
	}
	if states(results)["01_introduction.md"] != StateProtected {
		t.Fatalf("got %s, want protected", states(results)["01_introduction.md"])
	}
	if _, err := os.Stat(filepath.Join(dir, "01_introduction.md")); err != nil {
		t.Fatal("a corrected file was taken away")
	}
}

func TestCheckFindsACorrection(t *testing.T) {
	dir := t.TempDir()
	if _, err := Write(dir, fileSet("00_front.md", "01_introduction.md"), false); err != nil {
		t.Fatal(err)
	}
	correct(t, filepath.Join(dir, "01_introduction.md"), "The body of 01_introduction.md.", "Fixed a typo.")
	results, err := Check(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := states(results)
	if got["00_front.md"] != StateUnchanged {
		t.Errorf("an untouched file came back %s", got["00_front.md"])
	}
	if got["01_introduction.md"] != StateProtected {
		t.Errorf("a corrected file came back %s", got["01_introduction.md"])
	}
}

// After accepting, the correction is the file. The next extraction refuses to
// overwrite it because somebody decided rather than because something looks
// wrong, and those are different reasons.
func TestAcceptRestampsAndMarksEdited(t *testing.T) {
	dir := t.TempDir()
	if _, err := Write(dir, fileSet("00_front.md"), false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "00_front.md")
	correct(t, path, "The body of 00_front.md.", "Fixed a typo.")

	changed, err := Accept(dir, "00_front.md")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("accepting a corrected file changed nothing")
	}
	d, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if d.Corrected() {
		t.Fatal("the hash was not restamped")
	}
	if !d.Front.Edited {
		t.Fatal("the file was not marked edited")
	}
	if !strings.Contains(d.Body, "Fixed a typo.") {
		t.Fatal("the correction is gone")
	}
	// Accepting a file that has already been accepted is not a change, so a
	// second run does not touch the disk and does not report anything.
	again, err := Accept(dir, "00_front.md")
	if err != nil {
		t.Fatal(err)
	}
	if again {
		t.Fatal("accepting twice changed the file the second time")
	}
}

// A file on disk this cannot read is not this tool's to overwrite.
func TestAFileThatCannotBeReadIsLeftAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "00_front.md")
	if err := os.WriteFile(path, []byte("not a content file at all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	results, err := Write(dir, fileSet("00_front.md"), false)
	if err != nil {
		t.Fatal(err)
	}
	if states(results)["00_front.md"] != StateProtected {
		t.Fatalf("got %s, want protected", states(results)["00_front.md"])
	}
}

func correct(t *testing.T, path, from, to string) {
	t.Helper()
	b := readFile(t, path)
	if !strings.Contains(b, from) {
		t.Fatalf("%s does not contain %q", path, from)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(b, from, to, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

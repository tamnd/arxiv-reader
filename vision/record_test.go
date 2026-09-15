package vision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// read writes a record with one page in it and gives back the directory it is in.
func read(t *testing.T, text string) (string, *Record) {
	t.Helper()
	dir := t.TempDir()
	name, sum, err := Write(dir, 1, text)
	if err != nil {
		t.Fatal(err)
	}
	rec := &Record{Paper: "hep-th/9901001", Version: 1, Model: "a-model", Prompt: PromptSHA256(DefaultPrompt)}
	rec.Put(Entry{Page: 1, DPI: 300, File: name, SHA256: sum, Chars: len(text), Tried: []int{300}, Read: time.Now()})
	if err := rec.Save(dir); err != nil {
		t.Fatal(err)
	}
	return dir, rec
}

func TestSaveAndLoadComeBackTheSame(t *testing.T) {
	dir, rec := read(t, "# 1 Introduction\n\nthe page says this.")
	back, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if back.Paper != rec.Paper || back.Model != rec.Model || back.Prompt != rec.Prompt {
		t.Errorf("the record came back as %+v", back)
	}
	e, ok := back.Find(1)
	if !ok {
		t.Fatal("page one is not in the record that was just written with it in")
	}
	if e.DPI != 300 || e.File != "p001.md" || !e.Accepted() {
		t.Errorf("the entry came back as %+v", e)
	}
}

// A record that is not there is an empty record, because the first run over a
// paper has nothing to read and having to create the file first would mean every
// caller had that branch in it.
func TestLoadOfNothingIsEmpty(t *testing.T) {
	rec, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Pages) != 0 {
		t.Errorf("a record that was never written has %d pages in it", len(rec.Pages))
	}
}

// The three conditions on reusing a reading, one test each, because each of them
// is a way for a run to publish text the record cannot speak for.
func TestDoneWantsTheSameModel(t *testing.T) {
	dir, rec := read(t, "the page says this, at some length, so there is something to reuse.")
	if _, ok := rec.Done(dir, 1, "another-model", rec.Prompt); ok {
		t.Error("a reading made by one model was offered to a run using another")
	}
}

func TestDoneWantsTheSamePrompt(t *testing.T) {
	dir, rec := read(t, "the page says this, at some length, so there is something to reuse.")
	if _, ok := rec.Done(dir, 1, rec.Model, PromptSHA256("some other instructions")); ok {
		t.Error("a reading made under one prompt was offered to a run using another")
	}
}

func TestDoneNoticesAnEditedReading(t *testing.T) {
	dir, rec := read(t, "the page says this, at some length, so there is something to reuse.")
	if err := os.WriteFile(filepath.Join(dir, "p001.md"), []byte("somebody typed this in instead"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := rec.Done(dir, 1, rec.Model, rec.Prompt); ok {
		t.Error("a reading that no longer hashes to what the record says was reused anyway")
	}
}

func TestDoneReturnsTheReadingItKept(t *testing.T) {
	dir, rec := read(t, "the page says this, at some length, so there is something to reuse.")
	got, ok := rec.Done(dir, 1, rec.Model, rec.Prompt)
	if !ok {
		t.Fatal("a reading this run made itself was not offered back to it")
	}
	if !strings.HasPrefix(got, "the page says this") {
		t.Errorf("what came back was %q", got)
	}
}

// A page nothing could read still has an entry, and the entry is what says so. A
// record that dropped it would look the same as one that never asked, and the next
// run would pay for the page again.
func TestARefusedPageIsRecordedAndNotAccepted(t *testing.T) {
	dir := t.TempDir()
	rec := &Record{Paper: "hep-th/9901001", Version: 1, Model: "a-model", Prompt: "abc"}
	rec.Put(Entry{Page: 2, DPI: 600, Tried: []int{300, 400, 600}, Refused: []string{"V01: nothing came back for this page"}})
	if err := rec.Save(dir); err != nil {
		t.Fatal(err)
	}
	back, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := back.Find(2)
	if !ok {
		t.Fatal("the refused page left no entry behind")
	}
	if e.Accepted() {
		t.Error("a page with no reading is being treated as read")
	}
	if len(e.Tried) != 3 {
		t.Errorf("the entry says it tried %v, and the ladder has three rungs", e.Tried)
	}
	if len(e.Refused) == 0 {
		t.Error("the entry does not say why the page was refused, which is the only account of it there is")
	}
}

// -anyway is how a person accepts a page the rules refused, which is what a
// genuinely blank page needs, and the record keeps both facts.
func TestAPageKeptAnywayIsAcceptedAndStillSaysWhatWasWrong(t *testing.T) {
	e := Entry{Page: 3, File: "p003.md", SHA256: "abc", Refused: []string{"V01: nothing came back for this page"}, Anyway: true}
	if !e.Accepted() {
		t.Error("a page kept anyway is not being treated as read")
	}
	if len(e.Refused) == 0 {
		t.Error("a page kept anyway stopped saying what was wrong with it")
	}
}

// The readings come back indexed by page, with a gap where a page is missing, so
// that a paper which lost page seven still has page eight numbered eight.
func TestReadingsKeepThePageNumbersThePaperHas(t *testing.T) {
	dir := t.TempDir()
	rec := &Record{Model: "a-model", Prompt: "abc"}
	for _, page := range []int{1, 2, 4} {
		name, sum, err := Write(dir, page, "page "+PageName(page))
		if err != nil {
			t.Fatal(err)
		}
		rec.Put(Entry{Page: page, File: name, SHA256: sum})
	}
	rec.Put(Entry{Page: 3, Refused: []string{"V01: nothing came back for this page"}})
	got := rec.Readings(dir, 4)
	if len(got) != 4 {
		t.Fatalf("a four page paper came back as %d readings", len(got))
	}
	if got[2] != "" {
		t.Errorf("the page nothing read came back as %q", got[2])
	}
	if !strings.Contains(got[3], "p004") {
		t.Errorf("page four came back as %q, so the gap moved the pages after it", got[3])
	}
	if want := []int{3}; len(rec.Missing(4)) != 1 || rec.Missing(4)[0] != want[0] {
		t.Errorf("the missing pages are %v", rec.Missing(4))
	}
}

// A page that was never asked about is missing too, because a run that stopped
// halfway through has to be told what is left rather than what failed.
func TestMissingCountsThePagesNobodyAsked(t *testing.T) {
	rec := &Record{}
	rec.Put(Entry{Page: 1, File: "p001.md", SHA256: "abc"})
	if got := rec.Missing(3); len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Errorf("a three page paper with one page read is missing %v", got)
	}
}

func TestSaveSortsByPageSoARerunHasNoDiff(t *testing.T) {
	dir := t.TempDir()
	rec := &Record{Model: "a-model", Prompt: "abc"}
	for _, page := range []int{4, 1, 3, 2} {
		rec.Put(Entry{Page: page})
	}
	if err := rec.Save(dir); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(dir, RecordName))
	if err != nil {
		t.Fatal(err)
	}
	back, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := back.Save(dir); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(dir, RecordName))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Error("writing the same record twice produced two different files")
	}
	if !strings.Contains(string(first), "# What one vision run read") {
		t.Error("the record has no header saying what it is and who wrote it")
	}
}

func TestLoadRefusesAPageNumberThatIsNotOne(t *testing.T) {
	dir := t.TempDir()
	body := "pages:\n  - page: 0\n    dpi: 300\n"
	if err := os.WriteFile(filepath.Join(dir, RecordName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Error("a record with a page zero in it was accepted, and pages are counted from one")
	}
}

// The reading on disk ends with a newline whatever the model sent, because a file
// that does not is a file every tool complains about, and the hash in the record is
// of what is actually there.
func TestWriteEndsTheFileWithANewlineAndHashesWhatItWrote(t *testing.T) {
	dir := t.TempDir()
	name, sum, err := Write(dir, 5, "no newline at the end of this one")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if b[len(b)-1] != '\n' {
		t.Error("the reading does not end with a newline")
	}
	if Hash(b) != sum {
		t.Error("the hash in the record is not the hash of the file that was written")
	}
}

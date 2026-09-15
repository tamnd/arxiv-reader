package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/selection"
)

func TestDefaultLeavesTheMarkupPathsAlone(t *testing.T) {
	a := Default().Audit
	for _, p := range []selection.Path{selection.PathRender, selection.PathSource, selection.PathVision} {
		if got := a.Skipped(p); len(got) != 0 {
			t.Errorf("%s is expected to answer every rule and skips %v", p, got)
		}
	}
}

func TestDefaultDoesNotAskThePrintedPageForTeX(t *testing.T) {
	a := Default().Audit
	for _, rule := range []string{"M01", "M02", "M03", "M07", "M08", "M09", "M10", "M11", "M12", "M14"} {
		if a.Expects(selection.PathNative, rule) {
			t.Errorf("%s is asked of a paper read off a printed page, which holds what a formula was printed as and not the formula", rule)
		}
	}
}

func TestDefaultStillAsksThePrintedPageWhatItCanAnswer(t *testing.T) {
	// M05 reads the replacement character a file read with the wrong encoding
	// brings with it and M13 reads the file's own Markdown, and this path writes
	// both of those like any other.
	a := Default().Audit
	for _, rule := range []string{"M05", "M13"} {
		if !a.Expects(selection.PathNative, rule) {
			t.Errorf("%s is not asked of the native path, and it is a question that path can answer", rule)
		}
	}
}

func TestDefaultNeverExcusesALicenceRule(t *testing.T) {
	// A licence rule marked not applicable is a licence breach with a label on
	// it, so no rule in the S group is skipped whatever a path produces, and
	// neither is a figure rule about bytes that are on disk.
	a := Default().Audit
	for _, p := range selection.Paths {
		for _, rule := range a.Skipped(p) {
			if strings.HasPrefix(rule, "S") {
				t.Errorf("%s excuses the licence rule %s", p, rule)
			}
			for _, bytes := range []string{"F01", "F02", "F03", "F04", "F05", "F06", "F07", "F08", "F09"} {
				if rule == bytes {
					t.Errorf("%s excuses %s, which asks whether something is on disk that should not be", p, rule)
				}
			}
		}
	}
}

func TestDefaultDoesNotAskThePrintedPageForTables(t *testing.T) {
	a := Default().Audit
	for _, rule := range []string{"F10", "F11", "F12"} {
		if a.Expects(selection.PathNative, rule) {
			t.Errorf("%s is asked of a path that records a float's number and its caption and nothing else", rule)
		}
	}
}

func TestAPathNobodyRecordedExpectsEverything(t *testing.T) {
	// A paper whose front matter says nothing about how it was read is not a
	// paper to stop checking.
	a := Default().Audit
	for _, rule := range []string{"M01", "M14", "F11"} {
		if !a.Expects("", rule) {
			t.Errorf("%s is not asked of a paper with no path recorded", rule)
		}
	}
}

func TestARuleNobodyMentionedIsExpected(t *testing.T) {
	// The direction that matters. A rule added next year is expected on every
	// path until somebody says otherwise, so forgetting to edit this file leaves
	// a rule running where it should not rather than silently not running.
	a := Audit{Skip: map[selection.Path][]string{selection.PathNative: {"M01"}}}
	if !a.Expects(selection.PathNative, "M99") {
		t.Fatal("a rule this file has never heard of was skipped")
	}
}

func TestExpectsIgnoresCase(t *testing.T) {
	a := Audit{Skip: map[selection.Path][]string{selection.PathNative: {"m01"}}}
	if a.Expects(selection.PathNative, "M01") {
		t.Fatal("a rule written in lower case in the file was asked anyway")
	}
}

func TestLoadOfAMissingFileIsTheDefault(t *testing.T) {
	m, err := Load(filepath.Join(t.TempDir(), "selection.yaml"))
	if err != nil {
		t.Fatalf("a corpus with no policy of its own is not an error: %v", err)
	}
	if m.Audit.Expects(selection.PathNative, "M01") {
		t.Fatal("a corpus with no policy file was run with no policy at all")
	}
}

func TestLoadOfAFileThatSaysNothingAboutTheAuditIsTheDefault(t *testing.T) {
	// Somebody adding the selection weights to this file should not quietly turn
	// the M group loose on every paper read off a printed page.
	path := filepath.Join(t.TempDir(), "selection.yaml")
	if err := os.WriteFile(path, []byte("weights:\n  cited: 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Audit.Expects(selection.PathNative, "M01") {
		t.Fatal("a file with no audit section in it was read as an empty audit section")
	}
}

func TestLoadRefusesAPathThatIsNotOneOfTheFour(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selection.yaml")
	if err := os.WriteFile(path, []byte("audit:\n  skip:\n    ocr: [M01]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("a fifth path was accepted")
	}
	if !strings.Contains(err.Error(), "not one of the four paths") {
		t.Fatalf("says %q", err)
	}
}

func TestLoadRefusesAFileItCannotRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selection.yaml")
	if err := os.WriteFile(path, []byte("audit: [this is not a mapping]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("a file that is not this file was accepted")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifests", "selection.yaml")
	want := Default()
	if err := want.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range selection.Paths {
		a, b := got.Audit.Skipped(p), want.Audit.Skipped(p)
		if strings.Join(a, ",") != strings.Join(b, ",") {
			t.Fatalf("%s survived as %v and was %v", p, a, b)
		}
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The file says what it is for, because the person who opens it next is the
	// person deciding whether to change it.
	if !strings.HasPrefix(string(body), "#") {
		t.Fatalf("the file has no header on it:\n%s", body)
	}
}

func TestSkippedIsSortedAndDoesNotAliasTheManifest(t *testing.T) {
	a := Audit{Skip: map[selection.Path][]string{selection.PathNative: {"M14", "M01"}}}
	got := a.Skipped(selection.PathNative)
	if got[0] != "M01" || got[1] != "M14" {
		t.Fatalf("not sorted: %v", got)
	}
	got[0] = "zzz"
	if a.Skip[selection.PathNative][1] == "zzz" || a.Skip[selection.PathNative][0] == "zzz" {
		t.Fatal("the caller was handed the manifest's own slice")
	}
}

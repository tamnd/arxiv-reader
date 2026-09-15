package vision

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stub is a program that behaves the way a reader behaves and does not cost
// anything.
//
// The whole reason the reader is a program rather than a client library. Every
// line of this path can be exercised with a shell script, so the tests need no
// network, no key and no model, and CI runs them on every commit.
func stub(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "reader")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// picture is a PNG as far as this package is concerned, which is a path that
// exists.
func picture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "p001@300.png")
	if err := os.WriteFile(path, []byte("PNG"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadReturnsWhatCameBack(t *testing.T) {
	r := &Reader{Program: stub(t, `printf '# 1 Introduction\n\nthe page says this.\n'`), Model: "a-model"}
	got, err := r.Read(context.Background(), picture(t))
	if err != nil {
		t.Fatal(err)
	}
	if got != "# 1 Introduction\n\nthe page says this." {
		t.Errorf("the reading came back as %q", got)
	}
}

// The four terms of the contract, checked one at a time, because a reader written
// against a contract this package does not keep is a reader that gets a blank page
// and no explanation.
func TestReadKeepsItsSideOfTheContract(t *testing.T) {
	said := filepath.Join(t.TempDir(), "said")
	r := &Reader{
		Program: stub(t, `{ echo "argv: $1"; echo "model: $AX_VISION_MODEL"; echo "image: $AX_VISION_IMAGE"; echo "stdin:"; cat; } > `+said),
		Model:   "a-model",
		Prompt:  "write down what is on the page",
	}
	image := picture(t)
	if _, err := r.Read(context.Background(), image); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(said)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{
		"argv: " + image,
		"model: a-model",
		"image: " + image,
		"write down what is on the page",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the program was not told %q, and what it saw was:\n%s", want, got)
		}
	}
}

// A reader with no prompt of its own gets the one in this package, because a run
// that sent no prompt at all would get whatever the program felt like and the
// corpus would record a hash of nothing.
func TestReadSendsTheBuiltInPromptWhenThereIsNoOther(t *testing.T) {
	said := filepath.Join(t.TempDir(), "said")
	r := &Reader{Program: stub(t, `cat > `+said), Model: "a-model"}
	if _, err := r.Read(context.Background(), picture(t)); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(said)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != DefaultPrompt {
		t.Error("a reader with no prompt of its own sent something other than the built in prompt")
	}
}

func TestReadRefusesWithNoModelNamed(t *testing.T) {
	r := &Reader{Program: stub(t, `echo anything`)}
	_, err := r.Read(context.Background(), picture(t))
	if err == nil {
		t.Fatal("a page was read with no model named, so the corpus would not be able to say what read it")
	}
	if !strings.Contains(err.Error(), "-model") {
		t.Errorf("the message does not say what to do: %q", err)
	}
}

func TestReadReportsWhatTheProgramComplainedAbout(t *testing.T) {
	r := &Reader{Program: stub(t, `echo "429 too many requests" >&2; exit 1`), Model: "a-model"}
	_, err := r.Read(context.Background(), picture(t))
	var unread *Unread
	if !errors.As(err, &unread) {
		t.Fatalf("a refused page came back as %T: %v", err, err)
	}
	if !strings.Contains(unread.Said, "429") {
		t.Errorf("it did not repeat the complaint, and it said %q", unread.Said)
	}
}

func TestReadStopsAPageThatRunsTooLong(t *testing.T) {
	r := &Reader{Program: stub(t, "sleep 30"), Model: "a-model", Timeout: 100 * time.Millisecond}
	_, err := r.Read(context.Background(), picture(t))
	var slow *TooSlow
	if !errors.As(err, &slow) {
		t.Fatalf("a reader that hung came back as %T: %v", err, err)
	}
}

func TestAvailableRefusesAReaderNobodyNamed(t *testing.T) {
	r := &Reader{Model: "a-model"}
	err := r.Available()
	var missing *NotInstalled
	if !errors.As(err, &missing) {
		t.Fatalf("a reader with no program came back as %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "-reader") {
		t.Errorf("the message does not say what to pass: %q", err)
	}
}

func TestAvailableRefusesAProgramThatIsNotThere(t *testing.T) {
	r := &Reader{Program: filepath.Join(t.TempDir(), "not-a-program"), Model: "a-model"}
	if err := r.Available(); err == nil {
		t.Error("a program that is not there was taken for a reader")
	}
}

// The hash is of the whole prompt, so an edit of one word is a different hash and
// every paper read under the old wording can be told apart from the new ones.
func TestPromptHashFollowsEveryWord(t *testing.T) {
	a := PromptSHA256("write down what is on the page")
	b := PromptSHA256("write down what is on the page.")
	if a == b {
		t.Error("two prompts that differ by a full stop hash the same")
	}
	if len(a) != 64 {
		t.Errorf("the hash is %d characters, and a hex sha256 is 64", len(a))
	}
}

// The ladder climbs. A ladder out of order would ask a hard page at six hundred
// dots and then at three hundred, and the reading that got kept would be the worse
// one.
func TestLadderClimbs(t *testing.T) {
	for i := 1; i < len(Ladder); i++ {
		if Ladder[i] <= Ladder[i-1] {
			t.Errorf("the ladder goes %d then %d", Ladder[i-1], Ladder[i])
		}
	}
	if Ladder[0] != 300 {
		t.Errorf("the ladder starts at %d dots, and a page of text wants three hundred", Ladder[0])
	}
}

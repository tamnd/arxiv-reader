package extract

import (
	"strings"
	"testing"
)

func files(t *testing.T, name string) []File {
	t.Helper()
	fs, err := Files(parse(t, name), Front{Paper: "2501.00001", Version: "v3", Lang: "en"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func TestOneFilePerTopLevelSection(t *testing.T) {
	fs := files(t, "rendering.html")
	want := []string{"00_front.md", "01_introduction.md", "02_results.md"}
	if len(fs) != len(want) {
		t.Fatalf("got %d files, want %d", len(fs), len(want))
	}
	for i, name := range want {
		if fs[i].Name != name {
			t.Errorf("file %d is %s, want %s", i, fs[i].Name, name)
		}
	}
}

// The title goes in the front matter and not in the body, because a section
// that is a whole file has its title where the file's metadata is and repeating
// it would give every page two titles.
func TestASectionTitleIsInTheFrontMatterAndNotTheBody(t *testing.T) {
	fs := files(t, "rendering.html")
	body := fs[1].Doc
	if body.Front.SectionTitle != "Introduction" || body.Front.Section != 1 || body.Front.Kind != "section" {
		t.Fatalf("the front matter says %+v", body.Front)
	}
	if strings.HasPrefix(body.Body, "#") {
		t.Fatalf("the body opens with a heading:\n%s", body.Body)
	}
	if body.Front.LocalID != "s1" {
		t.Fatalf("the file is called %q, want s1", body.Front.LocalID)
	}
}

func TestTheFrontFileHoldsTheAbstract(t *testing.T) {
	fs := files(t, "rendering.html")
	front := fs[0].Doc
	if front.Front.Kind != "front" || front.Front.Section != 0 || front.Front.LocalID != "front" {
		t.Fatalf("the front matter says %+v", front.Front)
	}
	if !strings.Contains(front.Body, "We say nothing at all") {
		t.Fatalf("the abstract is missing:\n%s", front.Body)
	}
}

// The counts in the front matter have to be the counts of what is in the file
// beside it, which is why they are collected by the emitter rather than by a
// second walk over the model.
func TestTheFrontMatterCountsWhatIsInTheFile(t *testing.T) {
	fs := files(t, "rendering.html")
	results := fs[2].Doc.Front
	if got := strings.Join(results.Figures, ","); got != "fig-2,fig-3" {
		t.Errorf("figures are %q, want fig-2,fig-3", got)
	}
	if got := strings.Join(results.Tables, ","); got != "tab-1" {
		t.Errorf("tables are %q, want tab-1", got)
	}
	if got := strings.Join(results.Statements, ","); got != "thm-1" {
		t.Errorf("statements are %q, want thm-1", got)
	}
	if results.CodeBlocks != 1 {
		t.Errorf("counted %d code blocks, want 1", results.CodeBlocks)
	}
	intro := fs[1].Doc.Front
	if intro.Equations != 3 {
		t.Errorf("counted %d equations in the introduction, want 3", intro.Equations)
	}
	// Every file carries at least one object, which is the section or the front
	// the file itself is.
	for _, f := range fs {
		if f.Doc.Front.Objects < 1 {
			t.Errorf("%s counts %d objects", f.Name, f.Doc.Front.Objects)
		}
	}
}

// One namer for the whole paper, because a local identifier is unique within a
// paper and not within a file.
func TestIdentifiersAreUniqueAcrossTheWholePaper(t *testing.T) {
	seen := map[string]string{}
	for _, f := range files(t, "rendering.html") {
		for _, id := range ids(f.Doc.Body) {
			if other, ok := seen[id]; ok {
				t.Errorf("%s is in both %s and %s", id, other, f.Name)
			}
			seen[id] = f.Name
		}
	}
}

func ids(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		i := strings.Index(line, "{#")
		if i < 0 {
			continue
		}
		rest := line[i+2:]
		if j := strings.IndexAny(rest, " }"); j > 0 {
			out = append(out, rest[:j])
		}
	}
	return out
}

// A reference in section 1 to a figure in section 2 can only be rewritten once
// section 2 has been walked, which is why every file is built before any of
// them is finished.
func TestAForwardReferenceIsRewritten(t *testing.T) {
	fs := files(t, "rendering.html")
	for _, f := range fs {
		for _, line := range strings.Split(f.Doc.Body, "\n") {
			if strings.Contains(line, "](#S") {
				t.Errorf("%s still points at a rendering anchor: %s", f.Name, line)
			}
		}
	}
}

// A rendering LaTeXML could not finish is thrown away and the paper falls
// through to the source path, so the splitter refuses it rather than writing
// half a paper out.
func TestFilesRefusesARejectedPaper(t *testing.T) {
	if _, err := Files(parse(t, "faulty.html"), Front{}, nil); err == nil {
		t.Fatal("split a paper that should have been rejected")
	}
}

func TestFilesRefusesNothing(t *testing.T) {
	if _, err := Files(nil, Front{}, nil); err == nil {
		t.Fatal("split a paper that is not there")
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Introduction":                                "introduction",
		"Selective State Space Models":                "selective_state_space_models",
		"3.1 What Nothing Is":                         "3_1_what_nothing_is",
		"Hardware-aware Algorithm":                    "hardware_aware_algorithm",
		"Discussion: Selection":                       "discussion_selection",
		"A Very Long Title That Runs On Past The Cap": "a_very_long_title_that_runs_on_past_the",
		// A non-ascii letter is dropped rather than transliterated, because a
		// file name is not the place to guess at somebody's alphabet. A title
		// with nothing ascii left in it falls back to section, and so does an
		// empty one.
		"Über Nichts": "ber_nichts",
		"数学":          "section",
		"":            "section",
		// A title that is mathematics keeps the macro names, which is not
		// pretty and is the most readable thing available. The number in front
		// of the file is what orders it and what makes it unique.
		"$\\mathcal{O}(n)$": "mathcal_o_n",
	}
	for in, want := range cases {
		if got := slug(in); got != want {
			t.Errorf("%q is %q, want %q", in, got, want)
		}
	}
}

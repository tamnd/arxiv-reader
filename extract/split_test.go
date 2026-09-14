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

// grouped is one numbered equation in the shape LaTeXML writes most of them:
// an equation group whose table carries an identifier nothing references and
// whose tbody carries the one every cross reference points at.
const grouped = `<!DOCTYPE html><html><body><article class="ltx_document">
<h1 class="ltx_title ltx_title_document">A Paper About Nothing</h1>
<section id="S1" class="ltx_section">
<h2 class="ltx_title ltx_title_section"><span class="ltx_tag ltx_tag_section">1 </span>One</h2>
<div id="S1.p1" class="ltx_para">
<table id="S1.EGx1" class="ltx_equationgroup ltx_eqn_align ltx_eqn_table">
<tbody id="S1.E4"><tr class="ltx_equation ltx_eqn_row ltx_align_baseline">
<td class="ltx_eqn_cell ltx_align_center"><math id="S1.E4.m1" class="ltx_Math" alttext="x=y" display="inline"><semantics><mi>x</mi><annotation encoding="application/x-tex">x=y</annotation></semantics></math></td>
<td class="ltx_eqn_cell ltx_eqn_eqno ltx_align_middle"><span class="ltx_tag ltx_tag_equation">(4)</span></td>
</tr></tbody></table>
</div>
<div id="S1.p2" class="ltx_para"><p id="S1.p2.1" class="ltx_p">It follows from <a href="#S1.E4" class="ltx_ref">(4)</a> directly.</p></div>
</section></article></body></html>`

// The number is on the tbody and so is the identifier, and the table around it
// is referenced by nothing. Audit rule T12 found this on KAN, where two links
// into equations of section four pointed at anchors the rewrite had never been
// told about.
func TestALinkIntoAGroupedEquationIsRewritten(t *testing.T) {
	p, err := Parse([]byte(grouped), "2501.00001", 1)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := Files(p, Front{Paper: "2501.00001", Version: "v1", Lang: "en"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	body := fs[1].Doc.Body
	if !strings.Contains(body, "{#eq-4") {
		t.Fatalf("the equation has no identifier:\n%s", body)
	}
	if !strings.Contains(body, "](#eq-4)") {
		t.Fatalf("the link was not rewritten:\n%s", body)
	}
}

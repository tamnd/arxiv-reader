package extract

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// fragment parses a piece of markup and hands back the element the caller
// wrote, rather than the html, head and body the parser adds around it.
func fragment(t *testing.T, markup string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader("<div id=\"root\">" + markup + "</div>"))
	if err != nil {
		t.Fatal(err)
	}
	n := firstID(doc, "root")
	if n == nil {
		t.Fatalf("the fragment did not survive parsing: %s", markup)
	}
	return n
}

func TestInline(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		want  string
		about string
	}{{
		name: "a sentence split across lines comes back as a sentence",
		in:   "We say nothing at\nall, at some\nlength.",
		want: "We say nothing at all, at some length.",
	}, {
		name: "mathematics is the author's LaTeX and not the MathML",
		in:   `<math alttext="O(n)" display="inline"><semantics><mi>O</mi><annotation encoding="application/x-tex">O(n)</annotation></semantics></math>`,
		want: "$O(n)$",
	}, {
		name: "a display equation is set on its own",
		in:   `in <math alttext="x=1" display="block"><mi>x</mi></math> here`,
		want: `in \[x=1\] here`,
	}, {
		name: "mathematics with no alttext falls back to the annotation",
		in:   `<math display="inline"><semantics><mi>x</mi><annotation encoding="application/x-tex">x^2</annotation></semantics></math>`,
		want: "$x^2$",
	}, {
		name: "mathematics with neither is dropped rather than guessed at",
		in:   `a <math display="inline"><mi>x</mi></math> b`,
		want: "a b",
	}, {
		name: "bold, italic and typewriter",
		in:   `<span class="ltx_text ltx_font_bold">a</span> <em>b</em> <span class="ltx_text ltx_font_typewriter">c</span>`,
		want: "**a** *b* `c`",
	}, {
		name: "a link keeps where it points",
		in:   `see <a href="#bib.bibx1" class="ltx_ref">Nobody (1999)</a>`,
		want: "see [Nobody (1999)](#bib.bibx1)",
	}, {
		name: "a link with nowhere to point is just its words",
		in:   `see <a class="ltx_ref">Nobody</a>`,
		want: "see Nobody",
	}, {
		name: "a tag is the caller's business",
		in:   `<span class="ltx_tag ltx_tag_item">&bull;</span>the item`,
		want: "the item",
	}, {
		name: "a footnote leaves its mark and takes its text away",
		in:   `word<span class="ltx_note"><sup class="ltx_note_mark">1</sup><span class="ltx_note_content">a whole footnote</span></span> next`,
		want: "word1 next",
	}, {
		name:  "Markdown punctuation the author typed is escaped",
		in:    `a_b *c* [d] &lt;e&gt; \f`,
		want:  `a\_b \*c\* \[d\] \<e\> \\f`,
		about: "the emphasis above is written as markup, so a literal asterisk here is one the author typed",
	}, {
		name:  "a non-breaking space is a space",
		in:    "Figure\u00a01",
		want:  "Figure 1",
		about: "two words that look identical have to compare equal",
	}, {
		name: "zero width characters are dropped",
		in:   "a\u200bb\u2060c",
		want: "abc",
	}, {
		name: "a line break is a space and not a line break",
		in:   "one<br/>two",
		want: "one two",
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := inline(fragment(t, c.in)); got != c.want {
				t.Fatalf("got %q\nwant %q\n%s", got, c.want, c.about)
			}
		})
	}
}

func TestInlineOfNothingIsNothing(t *testing.T) {
	if got := inline(nil); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestTagOf(t *testing.T) {
	cases := map[string]string{
		"Figure 1:":    "1",
		"Table 12: ":   "12",
		"Algorithm 1":  "1",
		"Theorem 3.1.": "3.1",
		"(1a)":         "1a",
		"(4)":          "4",
		"3.1 ":         "3.1",
		"Appendix B":   "B",
		"":             "",
	}
	for in, want := range cases {
		if got := tagOf(fragment(t, in)); got != want {
			t.Errorf("%q gave %q, want %q", in, got, want)
		}
	}
}

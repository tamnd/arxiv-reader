package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTeXToHTML(t *testing.T) {
	refs := func(l string) (Ref, bool) {
		if l == "lem:a" {
			return Ref{Href: "#AB12", Text: "2.1", Kind: "Lemma"}, true
		}
		return Ref{}, false
	}
	for _, c := range []struct{ in, want string }{
		{`Let $x<y$ be \emph{small}.`, `<p>Let <span class="math">x&lt;y</span> be <em>small</em>.</p>`},
		{`By \cref{lem:a} and \eqref{eq:b}.`, `<p>By <a class="ref" href="#AB12">Lemma 2.1</a> and <span class="ref missing" title="eq:b">(?)</span>.</p>`},
		{"One.\n\nTwo.", `<p>One.</p><p>Two.</p>`},
		{`\begin{align}a&=b\label{x}\\c&=d\end{align}`, `<div class="math display">\begin{aligned}a&amp;=b\\c&amp;=d\end{aligned}</div>`},
		{`\[ x \]`, `<div class="math display">x</div>`},
		{`\begin{itemize}\item[(a)] one \item two\end{itemize}`, `<ul><li><p><span class="label">(a)</span> one </p></li><li><p>two</p></li></ul>`},
		{`See \cite[\S 3]{ran}.`, `<p>See <span class="cite">[ran, § 3]</span>.</p>`},
		{`{\bf bold} text`, `<p><strong>bold</strong> text</p>`},
		{`Erd\H{o}s and G\"odel --- ` + "`quoted'", "<p>Erdo\u030bs and Go\u0308del — ‘quoted’</p>"},
		{`<script>`, `<p>&lt;script&gt;</p>`},
		{`\href{javascript:alert(1)}{x} \url{https://a.b/c}`, `<p>x <a href="https://a.b/c" rel="nofollow">https://a.b/c</a></p>`},
		{`\begin{tikzcd}A\arrow[r]&B\end{tikzcd}`, `<div class="omitted">The paper has a diagram here, which this page does not show.</div>`},
		{`\unknowncmd{kept} text`, `<p>kept text</p>`},
	} {
		got := TeXToHTML(c.in, refs)
		got = strings.ReplaceAll(got, "́", "")
		want := strings.ReplaceAll(c.want, "́", "")
		if got != want {
			t.Errorf("%q\n got %q\nwant %q", c.in, got, want)
		}
	}
}

func TestBuild(t *testing.T) {
	root, out := t.TempDir(), t.TempDir()
	write := func(p, body string) {
		t.Helper()
		p = filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("metadata/2401.jsonl", `{"id":"2401.00001","title":"Open paper","abstract":"On $x$.","authors":[{"surname":"Noether","forename":"Emmy"}],"categories":["math.AG"],"versions":[{"version":1,"created":"2024-01-02T00:00:00Z","licence":"cc-by"}],"source":"oai","harvested":"2024-01-03"}
{"id":"2401.00002","title":"Closed paper","abstract":"","authors":[{"surname":"Hilbert","forename":"David"}],"categories":["math.NT"],"versions":[{"version":1,"created":"2024-01-04T00:00:00Z","licence":"arxiv-1.0"}],"source":"oai","harvested":"2024-01-05"}
`)
	write("analysis/objects/2401/2401.00001.jsonl", `{"tag":"AAAA","kind":"definition","paper":"2401.00001","version":"1","local":"definition-1","label":"def:a","number":"1.1","order":1,"title":"Definition","body_md":"A thing is \\emph{good}.","labels":["def:a"],"item":"definition","scope":"paper","path":"source","confidence":"certain"}
{"tag":"BBBB","kind":"statement","paper":"2401.00001","version":"1","local":"theorem-1","number":"1.2","order":2,"title":"Theorem","name":"Main","body_md":"By Definition~\\ref{def:a}, $x$ is good.","item":"statement/theorem","scope":"paper","path":"source","confidence":"certain"}
{"tag":"CCCC","kind":"proof","paper":"2401.00001","version":"1","local":"proof-1","order":3,"title":"Proof","body_md":"Clear.","item":"proof","proves":"theorem-1","scope":"paper","path":"source","confidence":"certain"}
`)
	write("analysis/objects/2401/2401.00002.jsonl", `{"tag":"DDDD","kind":"statement","paper":"2401.00002","version":"1","local":"lemma-1","number":"1","order":1,"title":"Lemma","body_md":"A secret text.","item":"statement/lemma","scope":"paper","path":"source","confidence":"certain"}
`)
	write("analysis/edges/2401/2401.00001.jsonl", `{"from":"2401.00001#theorem-1","to":"2401.00001#definition-1","predicate":"uses","method":"ref","confidence":"certain","evidence":{"version":1},"extractor":"t"}
`)
	st, err := Build(Options{Corpus: root, Out: out, URL: "https://example.org"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Papers != 2 || st.WithText != 1 || st.Items != 4 {
		t.Fatalf("stats %+v", st)
	}
	read := func(p string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	open := read("paper/2401.00001/index.html")
	for _, want := range []string{`A thing is <em>good</em>.`, `<a class="ref" href="#AAAA">1.1</a>`, `<details class="proof" id="CCCC">`, `Theorem 1.2</a> <span class="iname">(Main)</span>`, `href="https://creativecommons.org/licenses/by/4.0/"`, `Emmy Noether`, `<span class="lab">Uses</span> <a href="/paper/2401.00001/#AAAA">Definition 1.1</a>`} {
		if !strings.Contains(open, want) {
			t.Errorf("the open paper has no %q", want)
		}
	}
	closed := read("paper/2401.00002/index.html")
	if strings.Contains(closed, "secret") || !strings.Contains(closed, "Lemma 1") || !strings.Contains(closed, "does not let this site show the text") {
		t.Errorf("the closed paper shows its text, or no structure")
	}
	for _, p := range []string{"index.html", "papers/index.html", "kind/theorems/index.html", "category/math.AG/index.html", "month/2401/index.html", "licences/index.html", "search.json", "assets/katex/katex.min.js", "404.html"} {
		read(p)
	}
	if _, err := Build(Options{Corpus: root, Out: out}); err == nil {
		t.Error("a build into a directory that is not empty did not fail")
	}
}

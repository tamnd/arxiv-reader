package analysis

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/arxiv-reader/census"
	"github.com/tamnd/arxiv-reader/source"
	"github.com/tamnd/arxiv-reader/tex"
)

func writeWaivers(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "waivers.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAMissingWaiverFileIsNoWaivers(t *testing.T) {
	w, err := ReadWaivers(filepath.Join(t.TempDir(), "none.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := w.For("2312.00001", 1); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestAWaiverNeedsAReasonFromTheClosedList(t *testing.T) {
	for name, body := range map[string]string{
		"reason":  "waivers:\n  - {paper: 2312.00001v1, occurrence: env-1, reason: boring, by: Ann}\n",
		"person":  "waivers:\n  - {paper: 2312.00001v1, occurrence: env-1, reason: layout}\n",
		"version": "waivers:\n  - {paper: 2312.00001, occurrence: env-1, reason: layout, by: Ann}\n",
		"twice":   "waivers:\n  - {paper: 2312.00001v1, occurrence: env-1, reason: layout, by: Ann}\n  - {paper: 2312.00001v1, occurrence: 2312.00001v1#env-1, reason: empty, by: Ann}\n",
		"bug":     "waivers:\n  - {paper: 2312.00001v1, occurrence: env-1, reason: 'bug:x', by: Ann}\n",
		"rule":    "rules:\n  - {name: box, env: quotebox, reason: layout}\n",
	} {
		if _, err := ReadWaivers(writeWaivers(t, body)); err == nil {
			t.Errorf("%s: the file is read with no error", name)
		}
	}
}

func TestWaiversExcuseOccurrencesThatNoObjectAccountsFor(t *testing.T) {
	w, err := ReadWaivers(writeWaivers(t, `waivers:
  - {paper: test.w v1, occurrence: x, reason: layout, by: Ann}
  - {paper: test.wv1, occurrence: test.wv1#env-1, reason: empty, by: Ann}
  - {paper: test.wv2, occurrence: env-2, reason: 'bug:12', by: Ann}
rules:
  - {name: boxed, env: quotebox, reason: layout, approved: Ann}
`))
	if err != nil {
		t.Fatal(err)
	}
	src := `\documentclass{article}
\newtheorem{theorem}{Theorem}
\newtheorem{quotebox}{Quote}
\begin{document}
\begin{theorem}\end{theorem}
\begin{quotebox}A quotation.\end{quotebox}
\begin{theorem}Real.\end{theorem}
\end{document}`
	docs, err := tex.Read(&source.Bundle{Files: []source.File{{Name: "main.tex", Data: []byte(src)}}})
	if err != nil {
		t.Fatal(err)
	}
	m := DefaultManifests()
	c := census.Run(census.Input{Paper: "test.w", Version: 1, Extractor: "test", Doc: docs[0], Set: m.Resolver.Resolve(docs[0]), Titles: m.Resolver.Titles})
	r := c.Match(nil, w.For("test.w", 1))
	var waived, gaps []string
	for _, x := range r.Waivers {
		waived = append(waived, x.Occurrence+" "+x.Reason+" "+x.By)
	}
	for _, g := range r.Gaps {
		gaps = append(gaps, g.Occurrence)
	}
	if got := strings.Join(waived, ", "); got != "test.wv1#env-1 empty Ann, test.wv1#env-2 layout rule:boxed" {
		t.Errorf("waived %q", got)
	}
	if got := strings.Join(gaps, ", "); got != "test.wv1#env-3" {
		t.Errorf("gaps %q", got)
	}
}

package audit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalysisRules(t *testing.T) {
	root := t.TempDir()
	put := func(name, body string) {
		t.Helper()
		f := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("analysis/census/2312/2312.00001v1.json", `{"paper":"2312.00001","version":1,"extractor":"","main":["a.tex"],"declarations":[{"env":"foo","title":"Foo","item":"unknown","numbered":true,"from":"newtheorem"}],"occurrences":{"env":1},"matched":{},"gaps":[{"occurrence":"env-1","result":"missing","pattern":"env/newtheorem/article"}],"waivers":[],"numbers":{},"textrefs":{},"strength":"normal"}`)
	put("analysis/objects/2312/2312.00001.jsonl", `{"kind":"statement","paper":"2312.00001","version":"1","local":"theorem-1","file":"","order":0,"path":"source","confidence":"certain","item":"statement/theorem","proof_status":"here","body_md":"Every x.","extractor":"ax/0"}
{"kind":"statement","paper":"2312.00001","version":"1","local":"lemma-1","file":"","order":1,"path":"source","item":"statement/lemma","extractor":"ax/0"}
`)
	put("analysis/objects/2312/2312.00002.jsonl", `{"kind":"statement","paper":"2312.00002","version":"1","local":"theorem-1","file":"","order":0,"path":"source","confidence":"certain","item":"statement/theorem","proof_status":"none","extractor":"ax/0"}
`)
	put("analysis/stopped/2312/2312.00001v1.json", `{"paper":"2312.00001","version":1,"state":"stopped","reason":"failed","by":"ax/0","on":"2026-10-27"}`)
	put("analysis/stopped/2312/2312.00003v2.json", `{"paper":"2312.00003","version":2,"state":"stopped","reason":"tired","by":"ax/0","on":"2026-10-27"}`)
	put("analysis/stopped/2312/2312.00004v1.json", `{"paper":"2312.00004","version":1,"state":"stopped","reason":"no-source","by":"ax/0","on":"2026-10-27"}`)
	a := Analysis{Root: root, Cap: 10, Text: func(string, int) (bool, bool) { return false, true }}
	rep, err := a.Run()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"D01": 2, "D02": 2, "D03": 1, "D04": 2, "D06": 1, "D13": 1, "D15": 1}
	for _, r := range rep.Results {
		if r.Total != want[r.Rule.ID] {
			t.Errorf("%s: %d findings, want %d: %+v", r.Rule.ID, r.Total, want[r.Rule.ID], r.Findings)
		}
	}
	if !rep.Failed() {
		t.Error("the report does not fail")
	}
}

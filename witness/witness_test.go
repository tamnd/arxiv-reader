package witness

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
)

func TestSplitName(t *testing.T) {
	for name, want := range map[string][2]string{
		"Proposition 2.1 (Ruelle inequality)": {"proposition", "2.1"},
		"Theorem A":                           {"theorem", "A"},
		"Lemma 2.4.":                          {"lemma", "2.4"},
		"Corollary":                           {"corollary", ""},
		"Main Theorem":                        {"main", ""},
		"Theorem IV":                          {"theorem", "IV"},
		"Lemma 3.2a":                          {"lemma", "3.2a"},
		"":                                    {"", ""},
	} {
		k, n := SplitName(name)
		if k != want[0] || n != want[1] {
			t.Errorf("SplitName(%q) = %q %q, want %q %q", name, k, n, want[0], want[1])
		}
	}
}

func TestSplitVersion(t *testing.T) {
	for ref, want := range map[string]struct {
		id string
		v  int
	}{"2511.12345v3": {"2511.12345", 3}, "math/0101001v1": {"math/0101001", 1}, "2511.12345": {"2511.12345", 0}} {
		id, v := SplitVersion(ref)
		if id != want.id || v != want.v {
			t.Errorf("SplitVersion(%q) = %q %d", ref, id, v)
		}
	}
}

func keepAll(paper string) (string, bool) {
	if strings.HasPrefix(paper, "9999") {
		return "", false
	}
	return paper[:4], true
}

func TestReadTheoremGraphKeepsStructureOnly(t *testing.T) {
	papers := "paper_id,kind,arxiv_id,license\nu1,paper,2401.00001v2,cc-by\nu2,paper,9999.00001,cc-by\nu3,paper,2401.00002,\n"
	statements := "statement_id,paper_id,formality,kind,body,proof,ordinal,ref,label\n" +
		"s1,u1,informal,theorem,\"A body\nover two lines, with a comma\",,1,1.2,thm\n" +
		"s2,u1,informal,lemma,,,0,1.1,\n" +
		"s3,u1,informal,definition,,,2,,\n" +
		"s4,u2,informal,theorem,,,0,1,\n" +
		"s5,u3,informal,theorem,,,0,1,\n"
	res, err := ReadTheoremGraph(strings.NewReader(papers), strings.NewReader(statements), keepAll)
	if err != nil {
		t.Fatal(err)
	}
	if res.Read != 5 || res.Kept != 4 || res.Papers() != 2 {
		t.Fatalf("read %d, kept %d, papers %d", res.Read, res.Kept, res.Papers())
	}
	var got Record
	for _, r := range res.ByShard["2401"] {
		if r.Paper == "2401.00001" {
			got = r
		}
	}
	want := Record{Paper: "2401.00001", Version: 2, Count: 3, Kinds: map[string]int{"theorem": 1, "lemma": 1, "definition": 1}, Numbers: []string{"1.1", "1.2"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestReadTheoremSearchReadsTwoColumns(t *testing.T) {
	type row struct {
		TheoremID int64  `parquet:"theorem_id"`
		PaperID   string `parquet:"paper_id"`
		Name      string `parquet:"name"`
		Body      string `parquet:"body"`
	}
	var buf bytes.Buffer
	if err := parquet.Write(&buf, []row{
		{1, "2401.00001v1", "Theorem 1", "old"},
		{2, "2401.00001v2", "Theorem 1", "new"},
		{3, "2401.00001v2", "Lemma 2.3 (Key)", "new"},
		{4, "9999.00001v1", "Theorem 1", "out of scope"},
		{5, "2401.00003v1", "Claim", ""},
	}); err != nil {
		t.Fatal(err)
	}
	res, err := ReadTheoremSearch(bytes.NewReader(buf.Bytes()), int64(buf.Len()), keepAll)
	if err != nil {
		t.Fatal(err)
	}
	if res.Read != 5 || res.Kept != 4 {
		t.Fatalf("read %d, kept %d", res.Read, res.Kept)
	}
	byPaper := map[string]Record{}
	for _, r := range res.ByShard["2401"] {
		byPaper[r.Paper] = r
	}
	if r := byPaper["2401.00001"]; r.Version != 2 || r.Count != 2 || !reflect.DeepEqual(r.Numbers, []string{"1", "2.3"}) {
		t.Errorf("a later version must replace the counts of an earlier one, got %+v", r)
	}
	if r := byPaper["2401.00003"]; r.Count != 1 || r.Kinds["claim"] != 1 || len(r.Numbers) != 0 {
		t.Errorf("got %+v", r)
	}
}

func TestWriteAndReadAMonth(t *testing.T) {
	root := t.TempDir()
	file := Path(root, TheoremGraph, "2401")
	recs := []Record{{Paper: "2401.00002", Count: 1, Kinds: map[string]int{"theorem": 1}}, {Paper: "2401.00001", Version: 1, Count: 2, Kinds: map[string]int{"lemma": 2}, Numbers: []string{"1", "2"}}}
	if err := Write(file, recs); err != nil {
		t.Fatal(err)
	}
	if filepath.Base(filepath.Dir(file)) != TheoremGraph {
		t.Errorf("the path is %s", file)
	}
	c := &Cache{Root: root}
	got, err := c.Counts("2401", "2401.00001")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, map[string]int{TheoremGraph: 2}) {
		t.Errorf("got %v", got)
	}
	if got, _ := c.Counts("2402", "2402.00001"); got != nil {
		t.Errorf("a month with no file gives %v", got)
	}
}

func TestARemoteFileReadsWithRanges(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789"), blockSize/5)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "f", time.Time{}, bytes.NewReader(data))
	}))
	defer srv.Close()
	h := &Hub{Base: srv.URL}
	f, err := h.ReaderAt(context.Background(), srv.URL+"/f")
	if err != nil {
		t.Fatal(err)
	}
	if f.Size != int64(len(data)) {
		t.Fatalf("size %d, want %d", f.Size, len(data))
	}
	p := make([]byte, 20)
	off := int64(blockSize - 7)
	if _, err := f.ReadAt(p, off); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(p, data[off:off+20]) {
		t.Errorf("a read over two blocks gives %q", p)
	}
	if n, err := f.ReadAt(p, f.Size-5); n != 5 || err != io.EOF {
		t.Errorf("a read at the end gives %d %v", n, err)
	}
}

func TestAStreamStartsAgainWhereItWasCut(t *testing.T) {
	data := []byte(strings.Repeat("abcdefghij", 1000))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "" {
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			w.WriteHeader(http.StatusOK)
			hj, _ := w.(http.Hijacker)
			w.Write(data[:3000])
			w.(http.Flusher).Flush()
			if hj != nil {
				c, _, _ := hj.Hijack()
				c.Close()
			}
			return
		}
		http.ServeContent(w, r, "f", time.Time{}, bytes.NewReader(data))
	}))
	defer srv.Close()
	h := &Hub{Base: srv.URL}
	f, err := h.Open(context.Background(), srv.URL+"/f")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("got %d bytes, want %d", len(got), len(data))
	}
}

package witness

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/parquet-go/parquet-go"
)

// Source is where an index is read from: the dataset on Hugging Face and its
// files. The import pins the revision, so that a second import of the same
// revision gives the same files.
type Source struct {
	Dataset string
	Files   []string
}

// Sources is the dataset of each index.
var Sources = map[string]Source{
	TheoremSearch: {Dataset: "uw-math-ai/theorem-search-dataset-permissive", Files: []string{"theorem.parquet"}},
	TheoremGraph:  {Dataset: "uw-math-ai/math-graph", Files: []string{"paper_arxiv.csv", "statement_informal.csv"}},
}

// Import is the record of one import, as analysis/witness/<index>/import.json.
type Import struct {
	Index    string `json:"index"`
	Dataset  string `json:"dataset"`
	Revision string `json:"revision"`
	Licence  string `json:"licence"`
	On       string `json:"on"`
	By       string `json:"by"`
	// Read is the results that the import read, and Kept the ones of papers
	// in scope.
	Read   int `json:"read"`
	Kept   int `json:"kept"`
	Papers int `json:"papers"`
}

// Hub reads datasets from Hugging Face.
type Hub struct {
	Client    *http.Client
	UserAgent string
	// Base is https://huggingface.co, and a test sets it to a test server.
	Base string
}

func (h *Hub) base() string {
	if h.Base != "" {
		return h.Base
	}
	return "https://huggingface.co"
}

func (h *Hub) client() *http.Client {
	if h.Client != nil {
		return h.Client
	}
	return http.DefaultClient
}

func (h *Hub) do(ctx context.Context, url, rng string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if h.UserAgent != "" {
		req.Header.Set("User-Agent", h.UserAgent)
	}
	if rng != "" {
		req.Header.Set("Range", rng)
	}
	resp, err := h.client().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return resp, nil
}

// Revision is the current revision of a dataset and its licence tag.
func (h *Hub) Revision(ctx context.Context, dataset string) (sha, licence string, err error) {
	resp, err := h.do(ctx, h.base()+"/api/datasets/"+dataset, "")
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	var info struct {
		SHA      string `json:"sha"`
		CardData struct {
			License string `json:"license"`
		} `json:"cardData"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<22)).Decode(&info); err != nil {
		return "", "", err
	}
	if info.SHA == "" {
		return "", "", fmt.Errorf("%s: the hub gives no revision", dataset)
	}
	return info.SHA, info.CardData.License, nil
}

// URL is the address of a file of a dataset at a revision.
func (h *Hub) URL(dataset, revision, file string) string {
	return fmt.Sprintf("%s/datasets/%s/resolve/%s/%s", h.base(), dataset, revision, file)
}

// Open streams a file from the start. When the server ends the stream
// before the end, the stream starts again at the same byte with a range
// request, up to ten times in a row.
func (h *Hub) Open(ctx context.Context, url string) (io.ReadCloser, error) {
	resp, err := h.do(ctx, url, "")
	if err != nil {
		return nil, err
	}
	return &stream{hub: h, ctx: ctx, url: url, body: resp.Body, size: resp.ContentLength}, nil
}

type stream struct {
	hub   *Hub
	ctx   context.Context
	url   string
	body  io.ReadCloser
	off   int64
	size  int64
	tries int
}

func (s *stream) Read(p []byte) (int, error) {
	for {
		n, err := s.body.Read(p)
		s.off += int64(n)
		if n > 0 {
			s.tries = 0
		}
		if err == nil || (errors.Is(err, io.EOF) && (s.size < 0 || s.off >= s.size)) {
			return n, err
		}
		if n > 0 {
			return n, nil
		}
		if s.ctx.Err() != nil || s.tries >= 10 {
			return 0, err
		}
		s.tries++
		s.body.Close()
		resp, rerr := s.hub.do(s.ctx, s.url, fmt.Sprintf("bytes=%d-", s.off))
		if rerr != nil {
			return 0, fmt.Errorf("%w, and the stream did not start again at byte %d: %v", err, s.off, rerr)
		}
		if resp.StatusCode != http.StatusPartialContent {
			resp.Body.Close()
			return 0, fmt.Errorf("%w, and the server does not give a range to start again at byte %d", err, s.off)
		}
		s.body = resp.Body
	}
}

func (s *stream) Close() error { return s.body.Close() }

// ReaderAt reads a remote file with range requests, so that a parquet file is
// read a column at a time and is not written to the disk.
func (h *Hub) ReaderAt(ctx context.Context, url string) (*RemoteFile, error) {
	resp, err := h.do(ctx, url, "bytes=0-0")
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	cr := resp.Header.Get("Content-Range")
	i := strings.LastIndex(cr, "/")
	if i < 0 {
		return nil, fmt.Errorf("%s: the server gives no size in %q", url, cr)
	}
	size, err := strconv.ParseInt(cr[i+1:], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%s: the size %q: %w", url, cr, err)
	}
	return &RemoteFile{hub: h, ctx: ctx, url: url, Size: size, blocks: map[int64][]byte{}}, nil
}

// RemoteFile is a remote file with a cache of a few large blocks, because a
// parquet reader asks for many small ranges.
type RemoteFile struct {
	hub  *Hub
	ctx  context.Context
	url  string
	Size int64

	mu     sync.Mutex
	blocks map[int64][]byte
	order  []int64
}

const (
	blockSize = 8 << 20
	keep      = 6
)

func (f *RemoteFile) block(n int64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if b, ok := f.blocks[n]; ok {
		return b, nil
	}
	from := n * blockSize
	to := min(from+blockSize, f.Size) - 1
	resp, err := f.hub.do(f.ctx, f.url, fmt.Sprintf("bytes=%d-%d", from, to))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if int64(len(b)) != to-from+1 {
		return nil, fmt.Errorf("%s: asked for %d bytes at %d and got %d", f.url, to-from+1, from, len(b))
	}
	f.blocks[n] = b
	f.order = append(f.order, n)
	if len(f.order) > keep {
		delete(f.blocks, f.order[0])
		f.order = f.order[1:]
	}
	return b, nil
}

// ReadAt reads len(p) bytes at off.
func (f *RemoteFile) ReadAt(p []byte, off int64) (int, error) {
	if off >= f.Size {
		return 0, io.EOF
	}
	n := 0
	for n < len(p) && off < f.Size {
		b, err := f.block(off / blockSize)
		if err != nil {
			return n, err
		}
		c := copy(p[n:], b[off%blockSize:])
		n += c
		off += int64(c)
	}
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// Keep tells if a paper is in scope, and gives its month.
type Keep func(paper string) (shard string, ok bool)

// Result is the witness records of an import, by month.
type Result struct {
	ByShard    map[string][]Record
	Read, Kept int
}

// Papers is the count of the papers in the result.
func (r *Result) Papers() int {
	n := 0
	for _, recs := range r.ByShard {
		n += len(recs)
	}
	return n
}

// ReadTheoremSearch reads theorem.parquet of TheoremSearch. Only the columns
// paper_id and name are read. paper_id is the id with the version, and name
// is the head, as "Lemma 2.4".
func ReadTheoremSearch(r io.ReaderAt, size int64, keep Keep) (*Result, error) {
	recs := map[string]*Record{}
	shards := map[string]string{}
	res := &Result{ByShard: map[string][]Record{}}
	err := readParquet(r, size, []string{"paper_id", "name"}, func(v []string) error {
		res.Read++
		paper, version := SplitVersion(v[0])
		shard, ok := keep(paper)
		if !ok {
			return nil
		}
		res.Kept++
		rec := recs[paper]
		if rec == nil {
			rec = &Record{Paper: paper, Version: version}
			recs[paper] = rec
			shards[paper] = shard
		}
		if version > rec.Version {
			// A later version replaces the counts of an earlier one.
			*rec = Record{Paper: paper, Version: version}
		} else if version < rec.Version {
			return nil
		}
		kind, number := SplitName(v[1])
		rec.Add(kind, number)
		return nil
	})
	if err != nil {
		return nil, err
	}
	for paper, rec := range recs {
		res.ByShard[shards[paper]] = append(res.ByShard[shards[paper]], *rec)
	}
	return res, nil
}

// readParquet reads some columns of a parquet file, a row group at a time,
// and calls fn with the values of each row. A null is an empty string.
func readParquet(r io.ReaderAt, size int64, cols []string, fn func([]string) error) error {
	f, err := parquet.OpenFile(r, size, parquet.SkipPageIndex(true), parquet.SkipBloomFilters(true), parquet.ReadBufferSize(4<<20))
	if err != nil {
		return err
	}
	idx := make([]int, len(cols))
	for i, c := range cols {
		leaf, ok := f.Schema().Lookup(c)
		if !ok {
			return fmt.Errorf("the parquet file has no column %s", c)
		}
		idx[i] = leaf.ColumnIndex
	}
	for _, rg := range f.RowGroups() {
		chunks := rg.ColumnChunks()
		vals := make([][]string, len(cols))
		for i, ci := range idx {
			pages := chunks[ci].Pages()
			for {
				p, err := pages.ReadPage()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					pages.Close()
					return err
				}
				buf := make([]parquet.Value, p.NumValues())
				n, err := p.Values().ReadValues(buf)
				if err != nil && !errors.Is(err, io.EOF) {
					pages.Close()
					return err
				}
				for _, v := range buf[:n] {
					if v.IsNull() {
						vals[i] = append(vals[i], "")
					} else {
						vals[i] = append(vals[i], string(v.ByteArray()))
					}
				}
				parquet.Release(p)
			}
			pages.Close()
		}
		for row := range vals[0] {
			v := make([]string, len(cols))
			for i := range cols {
				if row < len(vals[i]) {
					v[i] = vals[i][row]
				}
			}
			if err := fn(v); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReadTheoremGraph reads paper_arxiv.csv and statement_informal.csv of
// TheoremGraph. Only the ids, the kind, the ordinal and the printed number
// are kept. The text columns are read past and not kept.
func ReadTheoremGraph(papers, statements io.Reader, keep Keep) (*Result, error) {
	type paperOf struct {
		id      string
		version int
		shard   string
	}
	byUUID := map[string]paperOf{}
	err := readCSV(papers, []string{"paper_id", "arxiv_id"}, func(v []string) error {
		paper, version := SplitVersion(strings.TrimSpace(v[1]))
		if paper == "" {
			return nil
		}
		if shard, ok := keep(paper); ok {
			byUUID[v[0]] = paperOf{paper, version, shard}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("paper_arxiv.csv: %w", err)
	}
	type numbered struct {
		ord int32
		ref string
	}
	recs := map[string]*Record{}
	nums := map[string][]numbered{}
	res := &Result{ByShard: map[string][]Record{}}
	err = readCSV(statements, []string{"paper_id", "formality", "kind", "ordinal", "ref"}, func(v []string) error {
		res.Read++
		p, ok := byUUID[v[0]]
		if !ok || v[1] == "formal" {
			return nil
		}
		res.Kept++
		rec := recs[p.id]
		if rec == nil {
			rec = &Record{Paper: p.id, Version: p.version}
			recs[p.id] = rec
		}
		rec.Add(v[2], "")
		if ref := strings.TrimSpace(v[4]); ref != "" {
			ord, _ := strconv.Atoi(v[3])
			nums[p.id] = append(nums[p.id], numbered{int32(ord), ref})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("statement_informal.csv: %w", err)
	}
	shardOf := map[string]string{}
	for _, p := range byUUID {
		shardOf[p.id] = p.shard
	}
	for id, rec := range recs {
		ns := nums[id]
		sort.SliceStable(ns, func(i, j int) bool { return ns[i].ord < ns[j].ord })
		for _, n := range ns {
			rec.Numbers = append(rec.Numbers, n.ref)
		}
		res.ByShard[shardOf[id]] = append(res.ByShard[shardOf[id]], *rec)
	}
	return res, nil
}

// readCSV reads some columns of a CSV file with a header row.
func readCSV(r io.Reader, cols []string, fn func([]string) error) error {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	cr.ReuseRecord = true
	head, err := cr.Read()
	if err != nil {
		return err
	}
	idx := make([]int, len(cols))
	for i, c := range cols {
		idx[i] = -1
		for j, h := range head {
			if strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")) == c {
				idx[i] = j
			}
		}
		if idx[i] < 0 {
			return fmt.Errorf("the file has no column %s", c)
		}
	}
	v := make([]string, len(cols))
	for {
		row, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		for i, j := range idx {
			v[i] = ""
			if j < len(row) {
				v[i] = row[j]
			}
		}
		if err := fn(v); err != nil {
			return err
		}
	}
}

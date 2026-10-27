// Package site builds the reading site of the corpus, the static wiki at
// arxiv.tamnd.com.
//
// The site shows each extracted paper with its items in the order of the paper:
// the definitions, the statements, the proofs, the remarks and the problems.
// It shows the text of an item only when the licence of the version lets the
// corpus publish it (corpus.Access.MayPublishText). For the other papers it
// shows the structure, which the corpus publishes under CC BY, and a link to
// arXiv. Each page with text names the authors, the version, the licence and
// the changes that the corpus made.
//
// The build reads the corpus only. It makes no request, and it calls no model.
// The formulas go to the page as TeX, and KaTeX renders them in the browser.
package site

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/edges"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/objects"
)

//go:embed assets templates
var files embed.FS

// MaxFiles is the limit of files in one deploy of Cloudflare Pages, with a
// margin, and MaxFileSize the limit of the size of one file.
const (
	MaxFiles    = 19500
	MaxFileSize = 24 << 20
)

// Options are the inputs of a build.
type Options struct {
	Corpus string // the corpus checkout
	Out    string // the directory to write the site to; it must be empty or not exist
	URL    string // the address of the site, as https://arxiv.tamnd.com
	Commit string // the commit of the corpus, for the footer
	Date   time.Time
}

// Stats is what a build wrote.
type Stats struct {
	Papers    int // papers with a page
	Items     int // items on the pages
	WithText  int // papers whose text the site shows
	Files     int // files written
	Bytes     int64
	Licences  map[corpus.Licence]int
	NoLicence int // papers whose version has no licence in the metadata
}

// kindGroup is one index of items, as the page of all the definitions.
type kindGroup struct {
	Slug  string
	Title string
	Color string // the group of the items, for the colour
	Of    func(*item) bool
	Items []*item
}

type paper struct {
	ID      string
	PathID  string
	Shard   string
	Meta    metadata.Record
	HasMeta bool
	Version int
	Date    time.Time
	Licence corpus.Licence
	Access  corpus.Access
	Text    bool
	Items   []*item // the items of the paper, without the proofs that follow a statement
	All     []*item
	Counts  map[string]int
	Title   template.HTML
	Abstr   template.HTML
}

type item struct {
	objects.Record
	Paper  *paper
	Group  string // statement, proof, definition, remark, example, problem, conjecture, other
	Type   string // the type of the item, as Lemma
	Head   string // the head, as Lemma 2.1
	Body   template.HTML
	Name   template.HTML
	Proofs []*item
	Uses   []link
	UsedBy []link
	Proved *item // the statement that a proof proves
}

type link struct {
	Href string
	Text string
}

type site struct {
	opt     Options
	tmpl    *template.Template
	papers  []*paper
	byID    map[string]*paper
	cats    map[string][]*paper
	months  map[string][]*paper
	groups  []*kindGroup
	census  map[string]any
	stats   Stats
	written map[string]bool
}

// Build writes the site.
func Build(opt Options) (Stats, error) {
	if opt.Corpus == "" || opt.Out == "" {
		return Stats{}, errors.New("the build needs the corpus and the output directory")
	}
	if opt.Date.IsZero() {
		opt.Date = time.Now().UTC()
	}
	opt.URL = strings.TrimRight(opt.URL, "/")
	if ents, err := os.ReadDir(opt.Out); err == nil && len(ents) > 0 {
		return Stats{}, fmt.Errorf("%s is not empty, and the build writes to an empty directory only", opt.Out)
	}
	s := &site{opt: opt, byID: map[string]*paper{}, cats: map[string][]*paper{}, months: map[string][]*paper{}, written: map[string]bool{}}
	s.stats.Licences = map[corpus.Licence]int{}
	t, err := template.New("").Funcs(funcs(opt)).ParseFS(files, "templates/*.html")
	if err != nil {
		return Stats{}, err
	}
	s.tmpl = t
	if err := s.load(); err != nil {
		return Stats{}, err
	}
	if err := s.write(); err != nil {
		return s.stats, err
	}
	return s.stats, nil
}

func funcs(opt Options) template.FuncMap {
	return template.FuncMap{
		"url":   func(p string) string { return opt.URL + p },
		"lower": strings.ToLower,
		"plural": func(n int, one, many string) string {
			if n == 1 {
				return one
			}
			return many
		},
		"num": func(n int) string {
			s := strconv.Itoa(n)
			for i := len(s) - 3; i > 0; i -= 3 {
				s = s[:i] + "," + s[i:]
			}
			return s
		},
		"date":  func(t time.Time) string { return t.Format("2 January 2006") },
		"month": monthName,
		"spdx":  func(l corpus.Licence) string { return l.SPDX() },
		"lurl":  func(l corpus.Licence) string { return l.URL() },
		"lname": licenceName,
		"authors": func(as []metadata.Author) string {
			var out []string
			for _, a := range as {
				out = append(out, strings.TrimSpace(a.Forename+" "+a.Surname+" "+a.Suffix))
			}
			return joinNames(out)
		},
	}
}

func joinNames(ns []string) string {
	switch len(ns) {
	case 0:
		return ""
	case 1:
		return ns[0]
	case 2:
		return ns[0] + " and " + ns[1]
	}
	return strings.Join(ns[:len(ns)-1], ", ") + " and " + ns[len(ns)-1]
}

func monthName(shard string) string {
	t, err := time.Parse("0601", shard)
	if err != nil {
		return shard
	}
	return t.Format("January 2006")
}

func licenceName(l corpus.Licence) string {
	switch l {
	case corpus.LicenceCC0:
		return "CC0 1.0"
	case "":
		return "no licence on record"
	}
	if spdx := l.SPDX(); spdx != "" {
		return strings.ReplaceAll(strings.TrimPrefix(spdx, "LicenseRef-"), "-", " ")
	}
	return string(l)
}

// groupOf is the group of an item, which sets its colour.
func groupOf(r objects.Record) string {
	it := r.Item
	if i := strings.IndexByte(it, '/'); i >= 0 {
		it = it[:i]
	}
	switch it {
	case "statement", "proof", "definition", "remark", "example", "problem", "conjecture":
		return it
	case "counterexample":
		return "example"
	case "context":
		return "remark"
	}
	return "other"
}

func typeOf(r objects.Record) string {
	if t := strings.TrimSpace(r.Title); t != "" {
		if !strings.Contains(t, `\`) {
			return t
		}
		// A head that is a command, as \proofname, or that has commands.
		if t = plain(t); t != "" && !strings.HasPrefix(strings.TrimSpace(r.Title), `\`) {
			return t
		}
	}
	t := r.Subkind
	if i := strings.LastIndexByte(r.Item, '/'); i >= 0 {
		t = r.Item[i+1:]
	}
	if t == "" || t == "unknown" {
		t = r.Kind
	}
	return strings.ToUpper(t[:1]) + t[1:]
}

func (s *site) load() error {
	if b, err := os.ReadFile(filepath.Join(s.opt.Corpus, "reports", "census.json")); err == nil {
		_ = json.Unmarshal(b, &s.census)
	}
	objs, err := filepath.Glob(filepath.Join(s.opt.Corpus, "analysis", "objects", "*", "*.jsonl"))
	if err != nil {
		return err
	}
	sort.Strings(objs)
	shards := map[string]bool{}
	for _, f := range objs {
		p, err := s.readObjects(f)
		if err != nil {
			return err
		}
		if p == nil {
			continue
		}
		s.papers = append(s.papers, p)
		s.byID[p.ID] = p
		shards[p.Shard] = true
	}
	plane := metadata.Plane{Root: s.opt.Corpus}
	for sh := range shards {
		err := plane.Scan(sh, func(r metadata.Record) error {
			if p, ok := s.byID[r.ID]; ok {
				p.Meta, p.HasMeta = r, true
			}
			return nil
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	for _, p := range s.papers {
		s.setPaper(p)
		s.stats.Papers++
		s.stats.Items += len(p.All)
		if p.Text {
			s.stats.WithText++
		}
	}
	for _, p := range s.papers {
		if err := s.readEdges(p); err != nil {
			return err
		}
		s.attach(p)
	}
	sort.SliceStable(s.papers, func(i, j int) bool {
		if !s.papers[i].Date.Equal(s.papers[j].Date) {
			return s.papers[i].Date.After(s.papers[j].Date)
		}
		return s.papers[i].ID > s.papers[j].ID
	})
	for _, p := range s.papers {
		s.months[p.Shard] = append(s.months[p.Shard], p)
		for _, c := range p.Meta.Categories {
			s.cats[c] = append(s.cats[c], p)
		}
	}
	s.groups = []*kindGroup{
		{Slug: "definitions", Color: "definition", Title: "Definitions", Of: func(i *item) bool { return i.Group == "definition" }},
		{Slug: "theorems", Color: "statement", Title: "Theorems", Of: func(i *item) bool { return i.Group == "statement" && strings.HasSuffix(i.Item, "/theorem") }},
		{Slug: "propositions", Color: "statement", Title: "Propositions", Of: func(i *item) bool { return strings.HasSuffix(i.Item, "/proposition") }},
		{Slug: "lemmas", Color: "statement", Title: "Lemmas", Of: func(i *item) bool { return strings.HasSuffix(i.Item, "/lemma") }},
		{Slug: "corollaries", Color: "statement", Title: "Corollaries", Of: func(i *item) bool { return strings.HasSuffix(i.Item, "/corollary") }},
		{Slug: "other-statements", Color: "statement", Title: "Claims, facts and other statements", Of: func(i *item) bool {
			return i.Group == "statement" && !strings.HasSuffix(i.Item, "/theorem") && !strings.HasSuffix(i.Item, "/proposition") && !strings.HasSuffix(i.Item, "/lemma") && !strings.HasSuffix(i.Item, "/corollary")
		}},
		{Slug: "conjectures", Color: "conjecture", Title: "Conjectures", Of: func(i *item) bool { return i.Group == "conjecture" }},
		{Slug: "problems", Color: "problem", Title: "Problems and questions", Of: func(i *item) bool { return i.Group == "problem" }},
		{Slug: "examples", Color: "example", Title: "Examples", Of: func(i *item) bool { return i.Group == "example" }},
		{Slug: "remarks", Color: "remark", Title: "Remarks", Of: func(i *item) bool { return i.Group == "remark" }},
	}
	for _, p := range s.papers {
		for _, it := range p.Items {
			for _, g := range s.groups {
				if g.Of(it) {
					g.Items = append(g.Items, it)
					break
				}
			}
		}
	}
	return nil
}

// readObjects reads the objects file of one paper and keeps the items of the
// highest version in it.
func (s *site) readObjects(file string) (*paper, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var recs []objects.Record
	best := 0
	for _, line := range bytes.Split(b, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var r objects.Record
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		v, _ := strconv.Atoi(r.Version)
		if v > best {
			best, recs = v, recs[:0]
		}
		if v == best {
			recs = append(recs, r)
		}
	}
	if len(recs) == 0 {
		return nil, nil
	}
	id, err := axid.Parse(recs[0].Paper)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	p := &paper{ID: recs[0].Paper, PathID: corpus.PathID(id), Shard: corpus.Shard(id), Version: best, Counts: map[string]int{}}
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].Order < recs[j].Order })
	for _, r := range recs {
		if r.Scope == "local" {
			continue
		}
		it := &item{Record: r, Paper: p, Group: groupOf(r), Type: typeOf(r)}
		it.Head = it.Type
		if r.Number != "" {
			it.Head += " " + r.Number
		}
		p.All = append(p.All, it)
	}
	return p, nil
}

func (s *site) setPaper(p *paper) {
	if v, ok := p.Meta.VersionAt(p.Version); ok {
		p.Licence, p.Date = v.Licence, v.Created
	}
	if p.Date.IsZero() && len(p.Meta.Versions) > 0 {
		p.Date = p.Meta.Versions[0].Created
	}
	if p.Licence == "" {
		s.stats.NoLicence++
	} else {
		p.Access = corpus.AccessFor(p.Licence)
		p.Text = p.Access.MayPublishText()
	}
	s.stats.Licences[p.Licence]++
	title := p.Meta.Title
	if title == "" {
		title = "arXiv:" + p.ID
	}
	p.Title = inline(title, nil)
	if p.Meta.Abstract != "" {
		p.Abstr = template.HTML(TeXToHTML(p.Meta.Abstract, nil))
	}
	labels := map[string]*item{}
	for _, it := range p.All {
		for _, l := range append([]string{it.Label}, it.Labels...) {
			if l != "" && labels[l] == nil {
				labels[l] = it
			}
		}
	}
	refs := func(l string) (Ref, bool) {
		it, ok := labels[l]
		if !ok {
			return Ref{}, false
		}
		text := it.Number
		if text == "" {
			text = it.Type
		}
		return Ref{Href: "#" + it.anchor(), Text: text, Kind: it.Type}, true
	}
	for _, it := range p.All {
		if it.Record.Name != "" {
			it.Name = inline(it.Record.Name, refs)
		}
		if p.Text {
			it.Body = template.HTML(TeXToHTML(it.BodyMD, refs))
		}
	}
}

// inline converts a short text, as a title, and keeps it out of a paragraph.
func inline(tex string, refs func(string) (Ref, bool)) template.HTML {
	h := TeXToHTML(tex, refs)
	h = strings.TrimSuffix(strings.TrimPrefix(h, "<p>"), "</p>")
	return template.HTML(strings.ReplaceAll(h, "</p><p>", " "))
}

// plain is the text of a short piece of TeX, with no tags.
func plain(tex string) string {
	h := TeXToHTML(tex, nil)
	h = tagRe.ReplaceAllString(h, "")
	return strings.Join(strings.Fields(html.UnescapeString(h)), " ")
}

var tagRe = regexp.MustCompile(`<[^>]*>`)

func (it *item) anchor() string {
	if it.Tag != "" {
		return it.Tag
	}
	return it.Local
}

func (it *item) Anchor() string { return it.anchor() }

func (it *item) Href() string { return "/paper/" + it.Paper.PathID + "/#" + it.anchor() }

func (s *site) readEdges(p *paper) error {
	id, _ := axid.Parse(p.ID)
	es, err := edges.ReadFile(corpus.EdgesPath(s.opt.Corpus, id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	local := map[string]*item{}
	for _, it := range p.All {
		local[it.Local] = it
	}
	find := func(ref string) *item {
		paperID, loc, ok := strings.Cut(ref, "#")
		if !ok {
			return nil
		}
		if paperID == p.ID {
			return local[loc]
		}
		q := s.byID[paperID]
		if q == nil {
			return nil
		}
		for _, it := range q.All {
			if it.Local == loc {
				return it
			}
		}
		return nil
	}
	seen := map[string]bool{}
	for _, e := range es {
		if e.Predicate == edges.Uses || e.Predicate == "cites-result" {
			from, to := find(e.From), find(e.To)
			if from == nil || to == nil || from == to || seen[e.From+" "+e.To] {
				continue
			}
			seen[e.From+" "+e.To] = true
			from.Uses = append(from.Uses, link{Href: to.Href(), Text: to.Head})
			to.UsedBy = append(to.UsedBy, link{Href: from.Href(), Text: from.Head})
		}
	}
	return nil
}

// attach puts each proof after the statement that it proves.
func (s *site) attach(p *paper) {
	local := map[string]*item{}
	for _, it := range p.All {
		local[it.Local] = it
	}
	for _, it := range p.All {
		p.Counts[it.Group]++
		if it.Group == "proof" && it.Proves != "" {
			if st := local[it.Proves]; st != nil && st != it {
				st.Proofs = append(st.Proofs, it)
				it.Proved = st
				continue
			}
		}
		p.Items = append(p.Items, it)
	}
}

// page is the data of the layout.
type page struct {
	Site    *site
	Title   string
	Desc    string
	Path    string
	Section string
	Paper   *paper
	Group   *kindGroup
	Shard   string
	Cat     string
	Papers  []*paper
	Data    any
}

func (s *site) Commit() string { return s.opt.Commit }
func (s *site) Date() string   { return s.opt.Date.Format("2 January 2006") }
func (s *site) Stats() Stats   { return s.stats }
func (s *site) Groups() []*kindGroup {
	return s.groups
}
func (s *site) Count(group string) int {
	n := 0
	for _, p := range s.papers {
		n += p.Counts[group]
	}
	return n
}
func (s *site) Census(key string) int {
	if v, ok := s.census[key].(float64); ok {
		return int(v)
	}
	return 0
}
func (s *site) Recent() []*paper {
	if len(s.papers) > 12 {
		return s.papers[:12]
	}
	return s.papers
}

type catCount struct {
	Cat    string
	Papers int
}

func (s *site) Categories() []catCount {
	var out []catCount
	for c, ps := range s.cats {
		out = append(out, catCount{c, len(ps)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Papers != out[j].Papers {
			return out[i].Papers > out[j].Papers
		}
		return out[i].Cat < out[j].Cat
	})
	return out
}

type monthPapers struct {
	Shard  string
	Papers []*paper
}

func (s *site) Months() []monthPapers {
	var out []monthPapers
	for sh, ps := range s.months {
		out = append(out, monthPapers{sh, ps})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Shard > out[j].Shard })
	return out
}

type licenceRow struct {
	Licence corpus.Licence
	Access  corpus.Access
	Text    bool
	Papers  int
}

func (s *site) LicenceRows() []licenceRow {
	var out []licenceRow
	for _, l := range append(append([]corpus.Licence{}, corpus.Licences...), "") {
		n := s.stats.Licences[l]
		if n == 0 {
			continue
		}
		r := licenceRow{Licence: l, Papers: n}
		if l != "" {
			r.Access = corpus.AccessFor(l)
			r.Text = r.Access.MayPublishText()
		}
		out = append(out, r)
	}
	return out
}

func (p *paper) Arxiv() string     { return fmt.Sprintf("%sv%d", p.ID, p.Version) }
func (p *paper) Total() int        { return len(p.All) }
func (p *paper) Published() string { l, _ := corpus.PublishedLicence(p.Licence); return licenceName(l) }
func (p *paper) PublishedURL() string {
	l, _ := corpus.PublishedLicence(p.Licence)
	return l.URL()
}

func (s *site) write() error {
	if err := os.MkdirAll(s.opt.Out, 0o755); err != nil {
		return err
	}
	if err := s.copyAssets(); err != nil {
		return err
	}
	pages := []page{
		{Path: "/", Title: "arXiv reader", Section: "home", Desc: "The definitions, theorems and proofs of mathematics papers on arXiv, read from the LaTeX source."},
		{Path: "/papers/", Title: "All papers", Section: "papers"},
		{Path: "/categories/", Title: "Categories", Section: "categories"},
		{Path: "/licences/", Title: "Licences", Section: "licences"},
		{Path: "/about/", Title: "About", Section: "about"},
	}
	tmpl := map[string]string{"home": "home.html", "papers": "papers.html", "categories": "categories.html", "licences": "licences.html", "about": "about.html"}
	for _, pg := range pages {
		pg.Site = s
		if err := s.render(pg.Path, tmpl[pg.Section], pg); err != nil {
			return err
		}
	}
	for _, p := range s.papers {
		desc := fmt.Sprintf("%d items of arXiv:%s", len(p.All), p.ID)
		pg := page{Site: s, Path: "/paper/" + p.PathID + "/", Title: plainTitle(p), Section: "paper", Paper: p, Desc: desc}
		if err := s.render(pg.Path, "paper.html", pg); err != nil {
			return err
		}
	}
	for _, g := range s.groups {
		pg := page{Site: s, Path: "/kind/" + g.Slug + "/", Title: g.Title, Section: "kind", Group: g}
		if err := s.render(pg.Path, "kind.html", pg); err != nil {
			return err
		}
	}
	for c, ps := range s.cats {
		pg := page{Site: s, Path: "/category/" + c + "/", Title: c, Section: "category", Cat: c, Papers: ps}
		if err := s.render(pg.Path, "list.html", pg); err != nil {
			return err
		}
	}
	for sh, ps := range s.months {
		pg := page{Site: s, Path: "/month/" + sh + "/", Title: monthName(sh), Section: "month", Shard: sh, Papers: ps}
		if err := s.render(pg.Path, "list.html", pg); err != nil {
			return err
		}
	}
	if err := s.render("/404.html", "404.html", page{Site: s, Path: "/404.html", Title: "Not found", Section: "404"}); err != nil {
		return err
	}
	if err := s.writeSearch(); err != nil {
		return err
	}
	if err := s.writeFile("/robots.txt", []byte("User-agent: *\nAllow: /\nSitemap: "+s.opt.URL+"/sitemap.txt\n")); err != nil {
		return err
	}
	var urls []string
	for p := range s.written {
		if strings.HasSuffix(p, "/index.html") {
			urls = append(urls, s.opt.URL+strings.TrimSuffix(p, "index.html"))
		}
	}
	sort.Strings(urls)
	if err := s.writeFile("/sitemap.txt", []byte(strings.Join(urls, "\n")+"\n")); err != nil {
		return err
	}
	return s.writeFile("/_headers", []byte("/assets/*\n  Cache-Control: public, max-age=604800\n/search.json\n  Cache-Control: public, max-age=600\n"))
}

func plainTitle(p *paper) string {
	t := strings.Join(strings.Fields(p.Meta.Title), " ")
	if t == "" {
		t = "arXiv:" + p.ID
	}
	return t
}

func (s *site) render(p, name string, data page) error {
	var b bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&b, name, data); err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	if strings.HasSuffix(p, "/") {
		p += "index.html"
	}
	return s.writeFile(p, b.Bytes())
}

func (s *site) writeFile(p string, b []byte) error {
	if s.written[p] {
		return fmt.Errorf("the build writes %s twice", p)
	}
	if len(b) > MaxFileSize {
		return fmt.Errorf("%s has %d bytes, and Cloudflare Pages takes %d at most", p, len(b), MaxFileSize)
	}
	s.written[p] = true
	s.stats.Files++
	s.stats.Bytes += int64(len(b))
	if s.stats.Files > MaxFiles {
		return fmt.Errorf("the site has more than %d files, and Cloudflare Pages takes 20,000 at most", MaxFiles)
	}
	file := filepath.Join(s.opt.Out, filepath.FromSlash(strings.TrimPrefix(p, "/")))
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return os.WriteFile(file, b, 0o644)
}

func (s *site) copyAssets() error {
	return fs.WalkDir(files, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := files.ReadFile(p)
		if err != nil {
			return err
		}
		return s.writeFile("/"+p, b)
	})
}

type searchEntry struct {
	T string `json:"t"`           // the text to show
	S string `json:"s,omitempty"` // the paper, for an item
	K string `json:"k"`           // paper, or the group of the item
	U string `json:"u"`
}

// writeSearch writes the index of the search box: each paper, and each item
// with a name, as Theorem 2.1 (Riemann-Roch).
func (s *site) writeSearch() error {
	var es []searchEntry
	for _, p := range s.papers {
		es = append(es, searchEntry{T: plainTitle(p), S: p.ID + " " + authorsLine(p.Meta.Authors), K: "paper", U: "/paper/" + p.PathID + "/"})
	}
	for _, p := range s.papers {
		for _, it := range p.All {
			if it.Record.Name == "" {
				continue
			}
			es = append(es, searchEntry{T: it.Head + " (" + strings.Join(strings.Fields(it.Record.Name), " ") + ")", S: plainTitle(p), K: it.Group, U: it.Href()})
		}
	}
	b, err := json.Marshal(es)
	if err != nil {
		return err
	}
	return s.writeFile("/search.json", b)
}

func authorsLine(as []metadata.Author) string {
	var out []string
	for _, a := range as {
		out = append(out, a.Surname)
	}
	return strings.Join(out, ", ")
}

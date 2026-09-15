package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/graph"
	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/objects"
	"github.com/tamnd/arxiv-reader/refs"
)

// runGraph builds and reads the connected web.
//
// 2166-08 is the whole of what this does and its first paragraph is worth
// repeating: a pile of three million papers is not connected, and a corpus where
// a theorem links to the definition it uses, in another paper, by another
// author, is. The edges are what make the difference, and most of them will
// never be found, so the ones that are have to say how confident they are.
func runGraph(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: ax graph build [-all|-shard <yymm>|<id> ...], ax graph node <uri>, ax graph out <id>, or ax graph in <id>")
	}
	switch args[0] {
	case "build":
		return graphBuild(args[1:])
	case "node":
		return graphNode(args[1:])
	case "out":
		return graphOut(args[1:])
	case "in":
		return graphIn(args[1:])
	default:
		return fmt.Errorf("unknown graph subcommand %q, the subcommands are build, node, out and in", args[0])
	}
}

// graphBuild writes the edges of one paper, one month or the whole plane.
func graphBuild(args []string) error {
	fs := flag.NewFlagSet("ax graph build", flag.ContinueOnError)
	shard := fs.String("shard", "", "build one month, as yymm")
	all := fs.Bool("all", false, "build every month the metadata plane holds")
	lang := fs.String("lang", "en", "the language directory to read")
	dry := fs.Bool("n", false, "say what was found and write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if named := len(rest) > 0; (*all && (named || *shard != "")) || (*shard != "" && named) {
		return errors.New("build a month, the whole plane, or the papers you name, and not two of those at once")
	}
	if !*all && *shard == "" && len(rest) == 0 {
		return errors.New("usage: ax graph build [-all|-shard <yymm>|<id> ...]")
	}
	r, err := newGraphRun(*lang, *dry)
	if err != nil {
		return err
	}
	switch {
	case *all:
		shards, err := r.plane.Shards()
		if err != nil {
			return err
		}
		if len(shards) == 0 {
			return fmt.Errorf("%s holds no metadata, so there is nothing to build a graph over: run ax harvest first", r.plane.Dir())
		}
		for _, s := range shards {
			if err := r.build(s, nil); err != nil {
				return err
			}
		}
		return nil
	case *shard != "":
		if !metadata.ValidShard(*shard) {
			return fmt.Errorf("%q is not a shard, which is four digits of year and month", *shard)
		}
		return r.build(*shard, nil)
	}
	only := map[string]map[string]bool{}
	var order []string
	for _, ref := range rest {
		id, err := axid.Parse(ref)
		if err != nil {
			return err
		}
		s := corpus.Shard(id)
		if only[s] == nil {
			only[s] = map[string]bool{}
			order = append(order, s)
		}
		only[s][id.Canonical] = true
	}
	for _, s := range order {
		if err := r.build(s, only[s]); err != nil {
			return err
		}
	}
	return nil
}

// graphRun is one build, holding what every paper in it needs.
type graphRun struct {
	plane metadata.Plane
	pat   *refs.Locators
	lang  string
	at    string
	dry   bool
	// read is the object records this run has already read, including the ones
	// it read to resolve somebody else's locator. A paper cited by fifty papers
	// in one month would otherwise be read fifty times, and a locator is
	// resolved against the whole of the cited paper's objects.
	read map[string]objects.Paper
}

func newGraphRun(lang string, dry bool) (*graphRun, error) {
	plane := metadata.Plane{Root: corpusRoot()}
	pat, err := refs.LoadLocators(corpus.LocatorsPath(plane.Root))
	if err != nil {
		return nil, err
	}
	return &graphRun{
		plane: plane,
		pat:   pat,
		lang:  lang,
		at:    time.Now().UTC().Format("2006-01-02"),
		dry:   dry,
		read:  map[string]objects.Paper{},
	}, nil
}

// build writes one shard's edges.
//
// One month at a time, because a month is what a shard file is and holding the
// whole corpus in memory to write it a month at a time would be the same work
// with a worse failure mode.
func (r *graphRun) build(shard string, only map[string]bool) error {
	recs, err := r.plane.Read(shard)
	if err != nil {
		return err
	}
	var fresh []graph.Edge
	var papers []string
	extracted := 0
	for _, rec := range recs {
		if only != nil && !only[rec.ID] {
			continue
		}
		delete(only, rec.ID)
		papers = append(papers, rec.ID)
		fresh = append(fresh, graph.Metadata(rec, r.at)...)
		p, ok := r.objects(rec.ID)
		if !ok {
			continue
		}
		extracted++
		fresh = append(fresh, graph.Content(p, r.pat, r.objects, r.at)...)
	}
	for id := range only {
		return fmt.Errorf("%s is not in the plane at %s, so there is nothing to say about it", id, r.plane.Dir())
	}
	if len(papers) == 0 {
		fmt.Fprintf(os.Stderr, "%s holds no papers\n", r.plane.Path(shard))
		return nil
	}
	if r.dry {
		reportGraph(shard, fresh, len(papers), extracted)
		return nil
	}
	path := corpus.GraphPath(r.plane.Root, shard)
	stored, err := graph.Load(path)
	if err != nil {
		return err
	}
	edges := graph.Merge(stored, fresh, papers)
	changed, err := graph.Save(path, edges)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s in %s, %s of %s extracted%s\n",
		prose.Count(len(edges), "edge"), path, prose.Thousands(extracted), prose.Count(len(papers), "paper"), unchanged(changed))
	return nil
}

// objects reads one paper out of the content plane, or says it is not there.
//
// A paper nobody has extracted has no content edges, which is the ordinary case
// for all but a thousand papers and not a fault. A paper whose files are there
// and cannot be read is treated the same way here, because this is also what
// answers a locator pointing at somebody else's paper, and one unreadable paper
// should not stop the graph of the paper that cited it. The unreadable one fails
// loudly when its own shard is built.
func (r *graphRun) objects(paper string) (objects.Paper, bool) {
	if p, held := r.read[paper]; held {
		return p, p.Paper != ""
	}
	p := objects.Paper{}
	defer func() { r.read[paper] = p }()
	id, err := axid.Parse(paper)
	if err != nil {
		return p, false
	}
	if _, err := os.Stat(corpus.ContentDir(r.plane.Root, r.lang, id)); err != nil {
		return p, false
	}
	built, err := objects.Build(r.plane.Root, r.lang, id)
	if err != nil {
		return p, false
	}
	p = built
	return p, true
}

// reportGraph is what a dry run prints.
func reportGraph(shard string, edges []graph.Edge, papers, extracted int) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "%s\t%s\t%s\t%s extracted\n", shard, prose.Count(len(edges), "edge"),
		prose.Count(papers, "paper"), prose.Thousands(extracted))
	predicate, confidence := graph.Counts(edges)
	for _, p := range graph.Names() {
		fmt.Fprintf(tw, "  %s\t%d\n", p, predicate[p])
	}
	for _, c := range graph.Confidences {
		fmt.Fprintf(tw, "  %s\t%d\n", c, confidence[c])
	}
	tw.Flush()
}

// graphNode says what one node is.
func graphNode(args []string) error {
	fs := flag.NewFlagSet("ax graph node", flag.ContinueOnError)
	incoming := fs.Bool("in", false, "count what points at it, which reads every shard")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return errors.New("usage: ax graph node [-in] <uri>, with the flag in front of the uri")
	}
	uri, err := node(rest[0])
	if err != nil {
		return err
	}
	root := corpusRoot()
	kind, _ := graph.Kind(uri)
	fmt.Printf("uri    %s\n", uri)
	fmt.Printf("kind   %s\n", kind)
	if paper := graph.PaperOf(uri); paper != "" {
		if err := title(root, paper); err != nil {
			return err
		}
	}
	out, err := match(root, uri, subject, filter{})
	if err != nil {
		return err
	}
	fmt.Printf("out    %s\n", summary(out))
	if !*incoming {
		fmt.Println("in     run ax graph in to count these, which reads every shard")
		return nil
	}
	in, err := match(root, uri, object, filter{})
	if err != nil {
		return err
	}
	fmt.Printf("in     %s\n", summary(in))
	return nil
}

// title prints what the corpus knows about a paper beside its node.
func title(root, paper string) error {
	id, err := axid.Parse(paper)
	if err != nil {
		return err
	}
	plane := metadata.Plane{Root: root}
	recs, err := plane.Read(corpus.Shard(id))
	if err != nil {
		return err
	}
	for _, rec := range recs {
		if rec.ID != id.Canonical {
			continue
		}
		fmt.Printf("title  %s\n", prose.Clip(rec.Title, 72))
		break
	}
	return nil
}

// summary is an edge set counted by predicate, in table order.
func summary(edges []graph.Edge) string {
	predicate, _ := graph.Counts(edges)
	var parts []string
	for _, p := range graph.Names() {
		if predicate[p] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", predicate[p], p))
		}
	}
	if len(parts) == 0 {
		return "nothing"
	}
	return strings.Join(parts, ", ")
}

// graphOut prints the edges a node asserts.
func graphOut(args []string) error { return graphEdges("out", subject, args) }

// graphIn prints the edges that point at a node.
//
// Incoming citations are what make a record paper's page useful and they come
// free from inverting the edge list, which is the whole of what this is. It
// reads every shard, because an edge lives in the file of the paper that
// asserted it and what points at this one could be asserted by anybody.
func graphIn(args []string) error { return graphEdges("in", object, args) }

// end is which end of an edge a query matches on.
type end int

const (
	subject end = iota
	object
)

// filter is what a query keeps.
type filter struct {
	predicate string
	conf      map[string]bool
	top       int
}

func graphEdges(name string, at end, args []string) error {
	fs := flag.NewFlagSet("ax graph "+name, flag.ContinueOnError)
	predicate := fs.String("predicate", "", "keep one predicate, so "+strings.Join(graph.Names(), ", "))
	conf := fs.String("conf", "", "keep these confidences, comma separated")
	top := fs.Int("top", 50, "how many to print, and 0 for all of them")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return fmt.Errorf("usage: ax graph %s [-predicate <p>] [-conf <c>] [-top <n>] <id>, with the flags in front of the id", name)
	}
	uri, err := node(rest[0])
	if err != nil {
		return err
	}
	f := filter{predicate: *predicate, top: *top}
	if *predicate != "" {
		if _, ok := graph.Lookup(*predicate); !ok {
			return fmt.Errorf("%q is not a predicate, and the seven are %s", *predicate, strings.Join(graph.Names(), ", "))
		}
	}
	if *conf != "" {
		f.conf = map[string]bool{}
		for _, c := range strings.Split(*conf, ",") {
			c = strings.TrimSpace(c)
			if !confidence(c) {
				return fmt.Errorf("%q is not a confidence, and the three are %s", c, strings.Join(graph.Confidences, ", "))
			}
			f.conf[c] = true
		}
	}
	edges, err := match(corpusRoot(), uri, at, f)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, e := range edges {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", e.S, e.P, e.O, e.Conf, e.Via, e.Locator)
	}
	return tw.Flush()
}

func confidence(c string) bool {
	for _, v := range graph.Confidences {
		if v == c {
			return true
		}
	}
	return false
}

// node turns what somebody typed into a node URI.
//
// An ax:// uri is taken as it is and anything else is read as a paper
// reference, because the thing anybody types is an arXiv id and making them
// write the scheme every time would be a tax on the common case.
func node(s string) (string, error) {
	if strings.HasPrefix(s, graph.Scheme) {
		if _, ok := graph.Kind(s); !ok {
			return "", fmt.Errorf("%q is not a node in this graph", s)
		}
		return s, nil
	}
	id, err := axid.Parse(s)
	if err != nil {
		return "", err
	}
	return graph.Paper(id.Canonical), nil
}

// match reads the edges at one end of a node.
//
// A paper URI matches its objects too, so ax graph out on a paper is everything
// the paper and everything inside it says. That is what somebody asking what a
// paper cites means, and asking for the paper alone is asking for the
// bibliography, which is a different question and a different command.
func match(root, uri string, at end, f filter) ([]graph.Edge, error) {
	paper := graph.PaperOf(uri)
	whole := paper != "" && uri == graph.Paper(paper)
	shards, err := graphShards(root)
	if err != nil {
		return nil, err
	}
	// An outgoing query knows which file to read, because an edge lives in the
	// shard of the paper that asserted it. An incoming one does not and reads
	// them all.
	if at == subject && paper != "" {
		id, err := axid.Parse(paper)
		if err != nil {
			return nil, err
		}
		shards = []string{corpus.Shard(id)}
	}
	var out []graph.Edge
	for _, shard := range shards {
		edges, err := graph.Load(corpus.GraphPath(root, shard))
		if err != nil {
			return nil, err
		}
		for _, e := range edges {
			side := e.S
			if at == object {
				side = e.O
			}
			if !(side == uri || (whole && graph.PaperOf(side) == paper)) {
				continue
			}
			if f.predicate != "" && e.P != f.predicate {
				continue
			}
			if f.conf != nil && !f.conf[e.Conf] {
				continue
			}
			out = append(out, e)
			if f.top > 0 && len(out) == f.top {
				return out, nil
			}
		}
	}
	return out, nil
}

// graphShards is every month the corpus has edges for, in order.
func graphShards(root string) ([]string, error) {
	des, err := os.ReadDir(filepath.Join(root, "graph"))
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%s holds no edges, so nothing has been built yet: run ax graph build", filepath.Join(root, "graph"))
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, de := range des {
		if de.IsDir() || filepath.Ext(de.Name()) != ".jsonl" {
			continue
		}
		out = append(out, strings.TrimSuffix(de.Name(), ".jsonl"))
	}
	sort.Strings(out)
	return out, nil
}

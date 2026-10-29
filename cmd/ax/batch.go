package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
	iofs "io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/analysis"
	"github.com/tamnd/arxiv-reader/census"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/fetch"
	"github.com/tamnd/arxiv-reader/licence"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/source"
	"github.com/tamnd/arxiv-reader/witness"
)

// renderFrom is the first month that arXiv rendered. It does not render the
// versions before it.
var renderFrom = time.Date(2023, 12, 1, 0, 0, 0, 0, time.UTC)

// frontEndLimit is the time limit of the front end for each paper (AX3.1).
const frontEndLimit = 60 * time.Second

// censusBatch takes the latest version of each paper in a list to the state
// extracted, or to stopped with a reason. For each paper it reads the licence
// of the version from the abs page, fetches the e-print and the render, writes
// the census record and the object records, and removes the downloads. A
// paper with a census record or a stop record is not done again, so a batch
// that stopped can start again with the same command. With -redo, the batch
// does each paper again, for example after a fix to the census.
func censusBatch(args []string) error {
	fs := flag.NewFlagSet("ax census batch", flag.ContinueOnError)
	list := fs.String("list", "", "a file with one id at the start of each line, as manifests/gold/x-v1.txt")
	limit := fs.Int("limit", 0, "stop after this many papers that needed work")
	pace := fs.Duration("pace", fetch.Pace, "the gap to leave between requests to the website")
	keep := fs.Bool("keep", false, "keep the e-print and the render in work/ after the census")
	redo := fs.Bool("redo", false, "do each paper again, also when it has a census record or a stop record")
	accept := fs.Bool("accept", false, "record the new bytes of a source that changed at arXiv, instead of going on to the next paper")
	norender := fs.Bool("no-render", false, "do not fetch the render, so the census has the source as its only witness")
	every := fs.Int("render-every", 1, "fetch the render for one paper in this many, chosen by its id, so a batch makes fewer requests")
	dry := fs.Bool("n", false, "print the count of the papers and the first ten, and do nothing")
	scopeList := fs.String("scope", "", "take the open and share-alike papers of this scope list, as math, in the order of AX2.6")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ids := fs.Args()
	if *scopeList != "" {
		more, err := analysisQueue(corpusRoot(), *scopeList)
		if err != nil {
			return err
		}
		ids = append(ids, more...)
	}
	if *list != "" {
		more, err := readIDs(*list)
		if err != nil {
			return err
		}
		ids = append(ids, more...)
	}
	if len(ids) == 0 {
		return errors.New("usage: ax census batch [-list file] [-scope math] [-limit n] [-keep] [-no-render] [<id> ...]")
	}
	if *pace < fetch.Pace {
		return fmt.Errorf("a pace of %s is faster than the fifteen seconds arXiv asks for on the website, and this reads the website", *pace)
	}
	if *dry {
		fmt.Printf("%d papers in the batch\n", len(ids))
		for _, id := range ids[:min(10, len(ids))] {
			fmt.Println(id)
		}
		return nil
	}
	root := corpusRoot()
	plane := metadata.Plane{Root: root}
	manifestPath := corpus.SourcesPath(root)
	manifest, err := fetch.Load(manifestPath)
	if err != nil {
		return err
	}
	waivers, err := analysis.ReadWaivers(corpus.WaiversPath(root))
	if err != nil {
		return err
	}
	b := &batch{
		root:     root,
		waivers:  waivers,
		witness:  &witness.Cache{Root: root},
		plane:    plane,
		m:        &manifest,
		pace:     *pace,
		keep:     *keep,
		redo:     *redo,
		accept:   *accept,
		render:   !*norender,
		every:    max(*every, 1),
		resolver: &licence.Resolver{UserAgent: userAgent(), Pace: *pace},
		fetcher:  &fetch.Fetcher{UserAgent: userAgent(), Pace: *pace},
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	counts := map[string]int{}
	worked := 0
	for i, ref := range ids {
		if ctx.Err() != nil {
			break
		}
		if *limit > 0 && worked >= *limit {
			break
		}
		state, did, err := b.one(ctx, ref)
		if changedAtSource(err) {
			fmt.Fprintf(os.Stderr, "ax: %s: %v, so the batch goes on with the next paper\n", ref, err)
			state, err = "changed", nil
		}
		var missing *notInPlane
		if errors.As(err, &missing) {
			fmt.Fprintf(os.Stderr, "ax: %v, so the batch goes on with the next paper\n", err)
			state, err = "not in the plane", nil
		}
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			return fmt.Errorf("%s: %w", ref, err)
		}
		counts[state]++
		if did {
			worked++
			if err := manifest.Save(manifestPath); err != nil {
				return err
			}
			fmt.Printf("%d/%d %s %s\n", i+1, len(ids), ref, state)
		}
	}
	fmt.Printf("%d papers: %d extracted before, %d extracted now", len(ids), counts["done"], counts["extracted"])
	for _, r := range analysis.StopReasons {
		if n := counts["stopped: "+r]; n > 0 {
			fmt.Printf(", %d stopped: %s", n, r)
		}
	}
	if n := counts["changed"]; n > 0 {
		fmt.Printf(", %d changed at the source", n)
	}
	if n := counts["not in the plane"]; n > 0 {
		fmt.Printf(", %d not in the plane", n)
	}
	fmt.Println()
	return ctx.Err()
}

// changedAtSource tells if the error is a source that changed at arXiv. That
// is evidence that the manifest keeps, so the batch writes nothing for the
// paper and goes on with the next one. A file that changed on disk is a fault
// of this machine, and it stops the batch.
func changedAtSource(err error) bool {
	var changed *fetch.Changed
	return errors.As(err, &changed) && changed.Where == "at the source"
}

type batch struct {
	root     string
	waivers  *analysis.Waivers
	witness  *witness.Cache
	plane    metadata.Plane
	m        *fetch.Manifest
	pace     time.Duration
	keep     bool
	redo     bool
	accept   bool
	render   bool
	every    int
	resolver *licence.Resolver
	fetcher  *fetch.Fetcher
	last     time.Time
}

// wait keeps one pace over the resolver and the fetcher, because they read the
// same host and each one keeps only its own pace.
func (b *batch) wait(ctx context.Context) error {
	if d := time.Until(b.last.Add(b.pace)); d > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
	}
	b.last = time.Now()
	return nil
}

// sampled tells if the paper is one of the papers in every that get a
// render. The choice is by a hash of the id, so a paper that is done again
// gets the same answer.
func sampled(id string, every int) bool {
	if every <= 1 {
		return true
	}
	h := fnv.New32a()
	h.Write([]byte(id))
	return h.Sum32()%uint32(every) == 0
}

// one takes one paper as far as it goes. It gives the state, and did is true
// when it did work.
func (b *batch) one(ctx context.Context, ref string) (state string, did bool, err error) {
	id, err := axid.Parse(ref)
	if err != nil {
		return "", false, err
	}
	rec, err := record(b.plane, id)
	if err != nil {
		return "", false, err
	}
	latest, ok := rec.Latest()
	if !ok {
		return "", false, fmt.Errorf("the record has no versions")
	}
	v := latest.Version
	if !b.redo && (exists(corpus.CensusPath(b.root, id, v)) || exists(corpus.StopPath(b.root, id, v))) {
		return "done", false, nil
	}
	ver := fmt.Sprintf("%sv%d", rec.ID, v)
	if latest.LicenceFrom != metadata.SourceAbs {
		if err := b.wait(ctx); err != nil {
			return "", false, err
		}
		if err := resolveOne(ctx, b.resolver, b.plane, ver, false, true); err != nil {
			return "", true, err
		}
		if rec, err = record(b.plane, id); err != nil {
			return "", true, err
		}
		latest, _ = rec.VersionAt(v)
	}
	if !corpus.AccessFor(latest.Licence).MayPublishText() {
		return b.stop(id, rec.ID, v, analysis.StopLicence, fmt.Sprintf("the abs page gives %s for v%d", latest.Licence, v))
	}

	order := fetch.Order{Record: rec, Version: v, Accept: b.accept}
	if _, cached := b.m.Find(rec.ID, v, fetch.RouteSource); !cached || !exists(corpus.EPrintPath(b.root, id, v)) {
		if err := b.wait(ctx); err != nil {
			return "", true, err
		}
	}
	res, err := b.fetcher.EPrint(ctx, b.root, b.m, order)
	var noEPrint *fetch.NoEPrint
	var tooLarge *fetch.TooLarge
	if errors.As(err, &tooLarge) {
		return b.stop(id, rec.ID, v, analysis.StopFailed, err.Error())
	}
	if errors.As(err, &noEPrint) {
		reason := analysis.StopNoSource
		if strings.Contains(strings.ToLower(rec.Comments), "withdrawn") {
			reason = analysis.StopWithdrawn
		}
		return b.stop(id, rec.ID, v, reason, err.Error())
	}
	if err != nil {
		return "", true, err
	}
	eprint := filepath.Join(b.root, filepath.FromSlash(res.Entry.Path))
	defer b.remove(eprint)

	bundle, err := source.Read(eprint)
	var pdfOnly *source.PDFOnly
	if errors.As(err, &pdfOnly) {
		res.Entry.Holds = fetch.HoldsPDF
		b.m.Put(res.Entry)
		return b.stop(id, rec.ID, v, analysis.StopNoSource, "the e-print is a PDF and has no TeX source")
	}
	if err != nil {
		return b.stop(id, rec.ID, v, analysis.StopFailed, "the e-print does not read: "+err.Error())
	}
	res.Entry.Holds = fetch.HoldsTeX
	b.m.Put(res.Entry)

	p := analysis.Paper{ID: rec.ID, Version: v, Extractor: "ax/" + Version, Text: true, Waivers: b.waivers.For(rec.ID, v)}
	if p.Outside, err = b.witness.Counts(corpus.Shard(id), rec.ID); err != nil {
		return "", true, err
	}
	if b.render && sampled(rec.ID, b.every) && !latest.Created.Before(renderFrom) {
		if err := b.wait(ctx); err != nil {
			return "", true, err
		}
		r, err := b.fetcher.Render(ctx, b.root, b.m, order)
		var unrendered *fetch.NotRendered
		var tooLarge *fetch.TooLarge
		switch {
		case errors.As(err, &unrendered):
		case errors.As(err, &tooLarge):
			// The census goes on with the source only, as for a paper with no
			// rendering.
			fmt.Fprintf(os.Stderr, "ax: %s: %v, so the census has no render\n", ver, err)
		case err != nil:
			return "", true, err
		default:
			page := filepath.Join(b.root, filepath.FromSlash(r.Entry.Path))
			defer b.remove(page)
			if body, err := os.ReadFile(page); err == nil {
				if parsed, err := extract.Parse(body, rec.ID, v); err == nil {
					p.Render = census.FromRender(parsed)
				}
			}
		}
	}

	type answer struct {
		res *analysis.Result
		err error
	}
	done := make(chan answer, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- answer{err: fmt.Errorf("the front end panicked: %v", r)}
			}
		}()
		r, err := analysis.Analyze(bundle, p, nil)
		done <- answer{r, err}
	}()
	var got answer
	select {
	case got = <-done:
	case <-time.After(frontEndLimit):
		return b.stop(id, rec.ID, v, analysis.StopFailed, fmt.Sprintf("the front end did not finish in %s", frontEndLimit))
	}
	if got.err != nil {
		return b.stop(id, rec.ID, v, analysis.StopFailed, got.err.Error())
	}
	if err := writeAnalysis(b.root, rec.ID, v, got.res); err != nil {
		return "", true, err
	}
	if b.redo {
		// The paper stopped before and reads now.
		if err := os.Remove(corpus.StopPath(b.root, id, v)); err != nil && !errors.Is(err, iofs.ErrNotExist) {
			return "", true, err
		}
	}
	return "extracted", true, nil
}

func (b *batch) stop(id axid.ID, paper string, version int, reason, detail string) (string, bool, error) {
	if b.redo && exists(corpus.CensusPath(b.root, id, version)) {
		// A paper done again keeps its census record when it stops now, so
		// that a failed fetch does not remove the work of an earlier run.
		fmt.Fprintf(os.Stderr, "%sv%d stopped (%s: %s) and keeps its census record\n", paper, version, reason, detail)
		return analysis.Stopped + ": " + reason, true, nil
	}
	s := analysis.Stop{Paper: paper, Version: version, State: analysis.Stopped, Reason: reason, Detail: detail, By: "ax/" + Version, On: time.Now().UTC().Format("2006-01-02")}
	out, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", true, err
	}
	if err := writeFile(corpus.StopPath(b.root, id, version), append(out, '\n')); err != nil {
		return "", true, err
	}
	return analysis.Stopped + ": " + reason, true, nil
}

// remove removes one download from work/, unless the batch keeps them. The
// manifest keeps its hash, so a fetch of it later is checked against it.
func (b *batch) remove(file string) {
	if b.keep {
		return
	}
	work := filepath.Join(b.root, "work") + string(filepath.Separator)
	if !strings.HasPrefix(file, work) {
		return
	}
	if err := os.Remove(file); err != nil && !errors.Is(err, iofs.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "%s: %v\n", file, err)
	}
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// readIDs reads the first field of each line of a file, and skips the empty
// lines and the comment lines.
func readIDs(file string) ([]string, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, strings.Fields(line)[0])
	}
	return out, sc.Err()
}

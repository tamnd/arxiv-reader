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

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/extract"
	"github.com/tamnd/arxiv-reader/fetch"
	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/metadata"
)

func runExtract(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: ax extract <render|source|native> -n <id>v<n> [...]")
	}
	switch args[0] {
	case "render":
		return extractRender(args[1:])
	case "source":
		return extractSource(args[1:])
	case "native":
		return extractNative(args[1:])
	default:
		return fmt.Errorf("unknown extract subcommand %q, and the paths are render, source and native", args[0])
	}
}

// extractRender reads a cached rendering and writes the paper into the content
// plane.
//
// Reading and writing are one command and two steps, with the reject rule
// between them. A rendering LaTeXML could not finish is thrown away and the
// paper falls through to the source path, and nothing of it is written, because
// a half written paper in the content plane is worse than no paper at all.
//
// -n does the reading and stops, which is how a paper is looked at before it is
// committed to anything.
func extractRender(args []string) error {
	fs := flag.NewFlagSet("ax extract render", flag.ContinueOnError)
	dry := fs.Bool("n", false, "report what the rendering holds and write nothing")
	outline := fs.Bool("outline", false, "print every heading and block, and not just the counts")
	force := fs.Bool("force", false, "overwrite a file somebody has corrected by hand")
	lang := fs.String("lang", "en", "the language directory to write into")
	if err := fs.Parse(args); err != nil {
		return err
	}
	refs := fs.Args()
	if len(refs) == 0 {
		return errors.New("usage: ax extract render [-n] <id>v<n> [...]")
	}

	root := corpusRoot()
	plane := metadata.Plane{Root: root}
	manifest, err := fetch.Load(corpus.SourcesPath(root))
	if err != nil {
		return err
	}

	rejected := 0
	for _, ref := range refs {
		id, err := axid.Parse(ref)
		if err != nil {
			return err
		}
		p, entry, err := readRendering(root, manifest, id, ref)
		if err != nil {
			return err
		}
		report(p, *outline)
		if r, bad := p.Reject(); bad {
			rejected++
			fmt.Fprintf(os.Stderr, "%sv%d: %v\n", p.ID, p.Version, r)
			for _, f := range r.Faults {
				fmt.Fprintf(os.Stderr, "  at %s: %s\n", f.Where, f.Text)
			}
			continue
		}
		if *dry {
			continue
		}
		if err := writePaper(plane, id, p, entry, *lang, *force); err != nil {
			return err
		}
	}
	if rejected > 0 {
		return fmt.Errorf("%d of %d renderings are too broken to use, and those papers are on the source path", rejected, len(refs))
	}
	return nil
}

// writePaper runs the gate, builds the front matter and puts the files down.
func writePaper(plane metadata.Plane, id axid.ID, p *extract.Paper, entry fetch.Source, lang string, force bool) error {
	rec, err := record(plane, id)
	if err != nil {
		return err
	}
	front, err := frontMatter(rec, id, p, entry, lang)
	if err != nil {
		return err
	}
	pics, err := decided(plane.Root, id, p.Version)
	if err != nil {
		return err
	}
	files, err := extract.Files(p, front, pics)
	if err != nil {
		return err
	}
	dir := corpus.ContentDir(plane.Root, lang, id)
	results, err := extract.Write(dir, files, force)
	if err != nil {
		return err
	}
	return reportWrite(dir, results, entry.Route)
}

// document is what the markup being read is called, which is not the same word
// on the two paths.
//
// It is the same markup either way, and a person who is told their rendering
// disagrees with the plane when they never fetched a rendering has been sent to
// look in the wrong place.
func document(route fetch.Route) string {
	switch route {
	case fetch.RouteSource:
		return "conversion"
	case fetch.RouteNative:
		return "text layer"
	}
	return "rendering"
}

// frontMatter fills in everything a content file says about itself that is not
// in the file.
//
// The gate runs here and not only in ax fetch. Fetching and extracting are
// separate runs and the licence can be re-resolved between them, so a paper
// whose v1 licence was corrected after its rendering was downloaded has to be
// refused at the point something is about to be published rather than at the
// point something was downloaded.
func frontMatter(rec metadata.Record, id axid.ID, p *extract.Paper, entry fetch.Source, lang string) (extract.Front, error) {
	if err := fetch.Gate(rec, id.Version); err != nil {
		return extract.Front{}, err
	}
	v, _ := rec.VersionAt(id.Version)
	// The manifest says which licence was in force when the bytes were
	// downloaded. A disagreement means the licence was re-resolved since, and
	// the bytes on disk are of a version whose terms nobody has checked against
	// the ones being published under now.
	if entry.Licence != v.Licence {
		return extract.Front{}, fmt.Errorf("%s was fetched under %s and the plane now says %s, so run ax fetch %s %s again before publishing it", entry.Ref(), entry.Licence, v.Licence, entry.Route, entry.Ref())
	}
	// A rendering states a licence of its own, in the info box at the top of the
	// page. It is arXiv saying what one version is under, which is the same kind
	// of statement the abs page makes, so a disagreement is one of the two being
	// stale and neither is safe to publish on. A conversion of the submitter's
	// own files carries no such box, so on the source path this checks nothing
	// and the plane is the only statement there is.
	if label, ok := corpus.LicenceFromLabel(p.Licence); ok && label != v.Licence {
		return extract.Front{}, fmt.Errorf("%s is recorded as %s and its %s says %q, which is %s, so one of the two is of a different version", entry.Ref(), v.Licence, document(entry.Route), p.Licence, label)
	} else if !ok && p.Licence != "" {
		fmt.Fprintf(os.Stderr, "%s: the %s labels its licence %q, which this does not recognise, so the crosscheck was skipped\n", entry.Ref(), document(entry.Route), p.Licence)
	}
	out, err := corpus.PublishedLicence(v.Licence)
	if err != nil {
		return extract.Front{}, err
	}

	f := extract.Front{
		Paper:           rec.ID,
		Version:         fmt.Sprintf("v%d", id.Version),
		Title:           rec.Title,
		Submitted:       v.Created.UTC().Format("2006-01-02"),
		Announced:       announced(rec, v),
		PrimaryCategory: rec.Primary(),
		Categories:      rec.Categories,
		MSCClass:        rec.MSCClass,
		ACMClass:        rec.ACMClass,
		DOI:             rec.DOI,
		JournalRef:      rec.JournalRef,
		Access:          string(corpus.AccessFor(v.Licence)),
		Licence:         out.SPDX(),
		LicenceOfSource: string(v.Licence),
		LicenceFrom:     string(v.LicenceFrom),
		Lang:            lang,
		// Which surface this paper was read from, which is the one field of the
		// front matter that says how the file below it was made. A reader
		// comparing two papers needs it, because a rendering is what arXiv made
		// of a submission and a conversion is what this project made of the same
		// submission, and they are not always the same document.
		Path:         string(entry.Route),
		SourceURL:    entry.URL,
		SourceSHA256: entry.SHA256,
	}
	// The metadata plane's author list and not the document's. What is in the
	// document is a byline laid out for a page, with thanks notes and
	// affiliations in it, and arXiv holds the list of people.
	for _, a := range rec.Authors {
		f.Authors = append(f.Authors, a.String())
	}
	// Unless the plane has none. The arXivRaw format OAI-PMH serves the versions
	// and the licence in does not carry a parsed author list, so a record that
	// has only been through that pass knows nothing about who wrote the paper.
	// A byline read off the rendering is worse than the plane's list and much
	// better than an empty one, and the note says which of the two this is.
	if len(f.Authors) == 0 && len(p.Authors) > 0 {
		for _, a := range p.Authors {
			f.Authors = append(f.Authors, a.Name)
		}
		fmt.Fprintf(os.Stderr, "%s: the metadata plane has no authors for %s, so the byline was read off the %s, and an authors pass with ax harvest oai -format arXiv will replace it\n", entry.Ref(), rec.ID, document(entry.Route))
	}
	if f.Categories == nil {
		f.Categories = []string{}
	}
	if f.Authors == nil {
		f.Authors = []string{}
	}
	return f, nil
}

// announced is the month the paper first appeared, which is the month its v1
// went out and not the month this version did.
//
// It is what the shard is named after, so taking it from the version in hand
// would put announced: 2024-05 on a file that lives under 2312, and the first
// person to sort by it would get a paper list that disagrees with the
// directories it came out of.
func announced(rec metadata.Record, v metadata.Version) string {
	if len(rec.Versions) > 0 && !rec.Versions[0].Created.IsZero() {
		return rec.Versions[0].Created.UTC().Format("2006-01")
	}
	return v.Created.UTC().Format("2006-01")
}

func reportWrite(dir string, results []extract.Result, route fetch.Route) error {
	counts := map[extract.State]int{}
	for _, r := range results {
		counts[r.State]++
		if r.State == extract.StateProtected {
			fmt.Fprintf(os.Stderr, "%s was left alone because %s\n", filepath.Join(dir, r.Name), r.Why)
			continue
		}
		fmt.Printf("  %-8s %s\n", r.State, r.Name)
	}
	fmt.Printf("%d files in %s: %d written, %d unchanged, %d removed, %d left alone\n",
		len(results), dir, counts[extract.StateWritten], counts[extract.StateUnchanged],
		counts[extract.StateRemoved], counts[extract.StateProtected])
	if counts[extract.StateProtected] > 0 {
		return fmt.Errorf("%s been corrected by hand, so run ax split -accept to keep the correction or ax extract %s -force to throw it away", prose.Count(counts[extract.StateProtected], "file has"), route)
	}
	return nil
}

// readRendering finds a cached rendering and parses it.
//
// The manifest and not the disk says where the file is. A rendering nobody
// recorded is a rendering nobody knows the licence of, and reading one off the
// disk because it happens to be there is how a corpus ends up publishing
// something it was never given.
func readRendering(root string, m fetch.Manifest, id axid.ID, ref string) (*extract.Paper, fetch.Source, error) {
	if id.Version < 1 {
		return nil, fetch.Source{}, fmt.Errorf("%s names no version, and a rendering is of one version", ref)
	}
	entry, ok := m.Find(id.Canonical, id.Version, fetch.RouteRender)
	if !ok {
		return nil, entry, fmt.Errorf("the manifest has no rendering of %sv%d, so run ax fetch render %sv%d first", id.Canonical, id.Version, id.Canonical, id.Version)
	}
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(entry.Path)))
	if err != nil {
		return nil, entry, fmt.Errorf("%s is in the manifest but not on disk, so run ax fetch render %s again: %w", entry.Ref(), entry.Ref(), err)
	}
	p, err := extract.Parse(body, id.Canonical, id.Version)
	if err != nil {
		return nil, entry, err
	}
	// arXiv serves /html/<id>v<n> and a request for a version it has no
	// rendering of can land on a different one. Extracting v4 into a corpus
	// that decided v1 was the cc-by version would be republishing something
	// nobody was given the right to republish, so this stops rather than
	// reports.
	if got := p.StampVersion(); got != 0 && got != id.Version {
		return nil, entry, fmt.Errorf("the rendering cached for %s says it is of v%d, so either arXiv served a different version or the file has been swapped: %q", entry.Ref(), got, p.Stamp)
	}
	return p, entry, nil
}

func report(p *extract.Paper, outline bool) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "%sv%d\t%s\n", p.ID, p.Version, p.Stamp)
	fmt.Fprintf(tw, "  licence\t%s\n", p.Licence)
	fmt.Fprintf(tw, "  title\t%s\n", p.Title)
	fmt.Fprintf(tw, "  authors\t%s\n", authorNames(p.Authors))
	fmt.Fprintf(tw, "  abstract\t%d block\n", len(p.Abstract))
	fmt.Fprintf(tw, "  headings\t%d\n", p.Headings())
	fmt.Fprintf(tw, "  blocks\t%s\n", counted(p.Counts()))
	fmt.Fprintf(tw, "  references\t%d\n", len(p.Bibliography))
	fmt.Fprintf(tw, "  unparsed\t%d\n", p.Unparsed)
	fmt.Fprintf(tw, "  labels\t%s\n", labelled(p))
	fmt.Fprintf(tw, "  faults\t%s\n", where(p.Faults))
	tw.Flush()
	if outline {
		printOutline(p)
	}
}

func authorNames(as []extract.Author) string {
	var out []string
	for _, a := range as {
		out = append(out, a.Name)
	}
	if len(out) == 0 {
		return "none in the rendering, and the metadata plane has the list that counts"
	}
	return strings.Join(out, ", ")
}

func counted(counts map[extract.Kind]int) string {
	var kinds []string
	for k := range counts {
		kinds = append(kinds, string(k))
	}
	sort.Strings(kinds)
	var out []string
	for _, k := range kinds {
		out = append(out, fmt.Sprintf("%d %s", counts[extract.Kind(k)], k))
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, ", ")
}

// labelled says how much of a paper carries the author's own \label.
//
// None is the ordinary answer and not a fault. The label is only in LaTeXML's
// intermediate XML, so it is on the source path and nowhere else, and a paper
// read off arXiv's rendering has no way to get it.
func labelled(p *extract.Paper) string {
	n := p.Labelled()
	if n == 0 {
		return "none"
	}
	return fmt.Sprintf("%d", n)
}

// where says how many conversion errors there are and which ones matter.
//
// The count on its own says nothing. One in the bibliography is a reference
// that prints badly and one in a section body is a sentence of the paper that
// is gone, so the two are counted apart.
func where(faults []extract.Fault) string {
	if len(faults) == 0 {
		return "none"
	}
	body := 0
	for _, f := range faults {
		if f.Where != "bibliography" {
			body++
		}
	}
	return fmt.Sprintf("%d, %d of them inside the body of the paper", len(faults), body)
}

func printOutline(p *extract.Paper) {
	var blocks func(bs []extract.Block, pad string)
	blocks = func(bs []extract.Block, pad string) {
		for _, b := range bs {
			fmt.Printf("%s%s\n", pad, describe(b))
			blocks(b.Blocks, pad+"  ")
		}
	}
	var sections func(ss []extract.Section, pad string)
	sections = func(ss []extract.Section, pad string) {
		for _, s := range ss {
			fmt.Printf("%s%s %s [%s]\n", pad, s.Tag, s.Title, s.ID)
			blocks(s.Blocks, pad+"  ")
			sections(s.Sections, pad+"  ")
		}
	}
	fmt.Println()
	blocks(p.Abstract, "  ")
	sections(p.Sections, "")
}

// describe is one line about a block, short enough to read a whole paper's
// worth of them.
func describe(b extract.Block) string {
	var about string
	switch {
	case len(b.Rows) > 0:
		about = fmt.Sprintf("%d rows", len(b.Rows))
	case len(b.Images) > 0:
		about = fmt.Sprintf("%d images", len(b.Images))
	case len(b.Items) > 0:
		about = fmt.Sprintf("%d items", len(b.Items))
	case b.Caption != "":
		about = b.Caption
	case b.Text == "" && len(b.Blocks) > 0:
		about = fmt.Sprintf("%d nested", len(b.Blocks))
	default:
		about = b.Text
	}
	// A theorem prints as whatever the author called it, so a paper full of
	// lemmas and claims reads as one rather than as a column of the word
	// theorem.
	kind := string(b.Kind)
	if b.Env != "" {
		kind = b.Env
	}
	return fmt.Sprintf("%-10s %-8s %s", kind, b.Tag, clip(about, 96))
}

func clip(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

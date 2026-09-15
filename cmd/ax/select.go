package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/selection"
)

// runSelect is the selection manifest: which papers are in the content plane, and
// why.
//
// Three million papers cannot be extracted and should not be, so the content plane
// is a selection out of the metadata plane. That selection is the most consequential
// judgement in the project, which is why it is a committed file with a reason on
// every row rather than whatever list somebody happened to run the extractor over.
func runSelect(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ax select add|drop|explain|list|reasons|seed|status")
	}
	switch args[0] {
	case "add":
		return selectAdd(args[1:])
	case "drop":
		return selectDrop(args[1:])
	case "explain":
		return selectExplain(args[1:])
	case "list":
		return selectList(args[1:])
	case "status":
		return selectStatus(args[1:])
	case "seed":
		return selectSeed(args[1:])
	case "suggest":
		return errors.New("select suggest is the citation closure over manifests/refs, so it arrives with the reference work")
	case "reasons":
		return selectReasons(args[1:])
	default:
		return fmt.Errorf("unknown select subcommand %q, which is add, drop, explain, list, reasons, seed or status", args[0])
	}
}

// selectAdd writes one paper into the selection.
//
// The licence gate is asked here and not later, because the seed is licence first
// and subject second and this is the moment that principle is either enforced or
// abandoned. A paper whose licence permits nothing but its record cannot be selected
// at all, and a paper nobody has resolved a licence for is refused separately: empty
// is not the same as unknown, and selecting on the strength of a field nobody filled
// in is how a corpus republishes something it may not.
func selectAdd(args []string) error {
	fs := flag.NewFlagSet("ax select add", flag.ContinueOnError)
	why := fs.String("reason", "", "one of the six reasons, which are "+selection.ReasonNames())
	cited := fs.Int("cited", 0, "how many papers inside the corpus are on the other end, for the two closure reasons")
	category := fs.String("category", "", "the primary category category-canon ranked it in")
	year := fs.Int("year", 0, "the year category-canon ranked it for")
	rank := fs.Int("rank", 0, "where category-canon put it")
	issue := fs.Int("issue", 0, "the issue a request was made in")
	by := fs.String("by", "", "who asked for it, or who put it on the seed list")
	list := fs.String("collection", "", "the reading list in collections.yaml this is a member of")
	score := fs.Float64("score", 0, "the queue order, which decides nothing else")
	langs := fs.String("languages", "", "the languages it exists in, comma separated")
	status := fs.String("status", string(selection.StatusSelected), "how far it has got, which is "+selection.StatusNames())
	if err := fs.Parse(args); err != nil {
		return err
	}
	refs := fs.Args()
	if len(refs) == 0 {
		return errors.New("usage: ax select add -reason <reason> <id>v<n> [...]")
	}
	reason, err := selection.ParseReason(*why)
	if err != nil {
		return err
	}
	at, err := selection.ParseStatus(*status)
	if err != nil {
		return err
	}

	root := corpusRoot()
	plane := metadata.Plane{Root: root}
	path := corpus.SelectedPath(root)
	m, err := selection.Load(path)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		id, err := axid.Parse(ref)
		if err != nil {
			return err
		}
		if id.Version < 1 {
			return fmt.Errorf("%s names no version, and the content plane holds one version of a paper", ref)
		}
		rec, err := record(plane, id)
		if err != nil {
			return err
		}
		v, ok := rec.VersionAt(id.Version)
		if !ok {
			return fmt.Errorf("%s has no v%d in the plane, so there is nothing saying what its licence is", id.Canonical, id.Version)
		}
		if v.Licence == "" {
			return fmt.Errorf("%s has no licence on record, and nobody has looked, so run ax licence resolve %s first", id.Canonical, ref)
		}
		access := corpus.AccessFor(v.Licence)
		if !access.MayPublishText() {
			return fmt.Errorf("%s is %s, so the corpus may publish its record and nothing else, and a paper like that cannot be in the content plane", ref, v.Licence)
		}
		e := selection.Entry{
			ID: id.Canonical, Version: id.Version, Reason: reason,
			Cited: *cited, Category: *category, Year: *year, Rank: *rank,
			Issue: *issue, By: *by, Collection: *list, Score: *score,
			Added: selection.Today(), Status: at,
			Languages: selection.Languages(strings.Split(*langs, ",")),
		}
		// The date an entry already has is kept, because when it was chosen is a
		// fact about the corpus and re-running this to correct a rank should not
		// rewrite it.
		if held, ok := m.Find(id.Canonical); ok {
			e.Added = held.Added
		}
		if err := e.Check(); err != nil {
			return err
		}
		m.Put(e)
		fmt.Printf("%s  %s  %s\n", e.Ref(), e.Reason, e.Explain())
	}
	return m.Save(path)
}

// selectDrop takes a paper out of the selection.
//
// Not a takedown. A takedown is somebody exercising a right over a paper that was
// published, it deletes the files and leaves a tombstone, and it lives in the licence
// gate. This is for a paper that was chosen and should not have been.
func selectDrop(args []string) error {
	fs := flag.NewFlagSet("ax select drop", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) == 0 {
		return errors.New("usage: ax select drop <id> [...]")
	}
	root := corpusRoot()
	path := corpus.SelectedPath(root)
	m, err := selection.Load(path)
	if err != nil {
		return err
	}
	for _, ref := range fs.Args() {
		id, err := axid.Parse(ref)
		if err != nil {
			return err
		}
		held, ok := m.Find(id.Canonical)
		if !ok {
			return fmt.Errorf("%s is not in the selection at %s", id.Canonical, path)
		}
		if selection.Reached(held.Status, selection.StatusExtracted) {
			return fmt.Errorf("%s is %s, so files for it are in the corpus, and taking it out of the selection would leave them with nothing saying why they are there", held.Ref(), held.Status)
		}
		m.Drop(id.Canonical)
		fmt.Printf("%s  dropped, which was %s\n", held.Ref(), held.Reason)
	}
	return m.Save(path)
}

// selectExplain prints the sentence behind the word.
func selectExplain(args []string) error {
	fs := flag.NewFlagSet("ax select explain", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) == 0 {
		return errors.New("usage: ax select explain <id> [...]")
	}
	root := corpusRoot()
	m, err := selection.Load(corpus.SelectedPath(root))
	if err != nil {
		return err
	}
	for i, ref := range fs.Args() {
		id, err := axid.Parse(ref)
		if err != nil {
			return err
		}
		e, ok := m.Find(id.Canonical)
		if !ok {
			return fmt.Errorf("%s is not in the content plane, so there is nothing to explain", id.Canonical)
		}
		if i > 0 {
			fmt.Println()
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "%s\t\n", e.Ref())
		fmt.Fprintf(tw, "  reason\t%s\n", e.Reason)
		fmt.Fprintf(tw, "  because\t%s\n", e.Explain())
		fmt.Fprintf(tw, "  means\t%s\n", e.Reason.Says())
		fmt.Fprintf(tw, "  added\t%s\n", e.Added)
		fmt.Fprintf(tw, "  status\t%s\n", e.Status)
		if e.Score > 0 {
			fmt.Fprintf(tw, "  score\t%.2f\n", e.Score)
		}
		if len(e.Languages) > 0 {
			fmt.Fprintf(tw, "  languages\t%s\n", strings.Join(e.Languages, ", "))
		}
		tw.Flush()
	}
	return nil
}

// selectList prints the selection, with the counts that are actually asked for.
func selectList(args []string) error {
	fs := flag.NewFlagSet("ax select list", flag.ContinueOnError)
	why := fs.String("reason", "", "only the papers under one reason")
	status := fs.String("status", "", "only the papers at one status")
	from := fs.String("at-least", "", "only the papers that have got at least this far")
	quiet := fs.Bool("q", false, "print the counts and not the papers")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("ax select list takes no arguments, so drop %s", fs.Args()[0])
	}
	var want selection.Reason
	if *why != "" {
		r, err := selection.ParseReason(*why)
		if err != nil {
			return err
		}
		want = r
	}
	var at, least selection.Status
	if *status != "" {
		s, err := selection.ParseStatus(*status)
		if err != nil {
			return err
		}
		at = s
	}
	if *from != "" {
		s, err := selection.ParseStatus(*from)
		if err != nil {
			return err
		}
		least = s
	}

	root := corpusRoot()
	path := corpus.SelectedPath(root)
	m, err := selection.Load(path)
	if err != nil {
		return err
	}
	kept := make([]selection.Entry, 0, len(m.Selected))
	for _, e := range m.Selected {
		switch {
		case want != "" && e.Reason != want:
		case at != "" && e.Status != at:
		case least != "" && !selection.Reached(e.Status, least):
		default:
			kept = append(kept, e)
		}
	}
	if !*quiet {
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		for _, e := range kept {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", e.Ref(), e.Reason, e.Status, e.Explain())
		}
		tw.Flush()
		if len(kept) > 0 {
			fmt.Println()
		}
	}
	// The counts are over the whole manifest and not over the filter, because a
	// count of what the filter kept is a number the caller already has.
	reasons := m.ByReason()
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, r := range selection.Reasons {
		fmt.Fprintf(tw, "%s\t%d\n", r, reasons[r])
	}
	tw.Flush()
	states := m.ByStatus()
	parts := make([]string, 0, len(selection.Statuses))
	for _, s := range selection.Statuses {
		if states[s] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", states[s], s))
		}
	}
	fmt.Printf("\n%s in the content plane", prose.Count(len(m.Selected), "paper"))
	if len(parts) > 0 {
		fmt.Printf(": %s", strings.Join(parts, ", "))
	}
	if len(kept) != len(m.Selected) {
		fmt.Printf(", and %d shown", len(kept))
	}
	fmt.Println()
	return nil
}

// selectStatus records how far a paper has got.
//
// A status that goes backwards is refused, because the rungs are the pipeline and a
// paper does not become unextracted. The exception a caller will eventually want,
// which is a paper re-extracted from scratch, is a different operation and it should
// have to say so.
func selectStatus(args []string) error {
	fs := flag.NewFlagSet("ax select status", flag.ContinueOnError)
	back := fs.Bool("back", false, "allow a status that goes backwards, for a paper being read again from the beginning")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 2 {
		return errors.New("usage: ax select status <status> <id> [...], where status is " + selection.StatusNames())
	}
	at, err := selection.ParseStatus(rest[0])
	if err != nil {
		return err
	}
	root := corpusRoot()
	path := corpus.SelectedPath(root)
	m, err := selection.Load(path)
	if err != nil {
		return err
	}
	for _, ref := range rest[1:] {
		id, err := axid.Parse(ref)
		if err != nil {
			return err
		}
		e, ok := m.Find(id.Canonical)
		if !ok {
			return fmt.Errorf("%s is not in the selection at %s, so run ax select add first", id.Canonical, path)
		}
		if selection.Reached(e.Status, at) && e.Status != at && !*back {
			return fmt.Errorf("%s is already %s, which is further on than %s, so pass -back if it is being read again from the beginning", e.Ref(), e.Status, at)
		}
		was := e.Status
		e.Status = at
		m.Put(e)
		fmt.Printf("%s  %s, which was %s\n", e.Ref(), at, was)
	}
	return m.Save(path)
}

// selectReasons prints the six and what each of them means.
func selectReasons(args []string) error {
	fs := flag.NewFlagSet("ax select reasons", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("ax select reasons takes no arguments, so drop %s", fs.Args()[0])
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, r := range selection.Reasons {
		fmt.Fprintf(tw, "%s\t%s\n", r, r.Says())
	}
	tw.Flush()
	fmt.Printf("\nSix reasons and no seventh, in the order they were added.\n")
	fmt.Printf("A paper nobody can put under one of them is a paper somebody wants for a reason they have not written down.\n")
	return nil
}

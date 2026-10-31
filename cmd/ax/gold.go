package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	iofs "io/fs"
	"os"
	"strings"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/census"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/edges"
	"github.com/tamnd/arxiv-reader/gold"
	"github.com/tamnd/arxiv-reader/licence"
	"github.com/tamnd/arxiv-reader/mdrec"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/objects"
	"github.com/tamnd/arxiv-reader/scope"
)

// runGold is ax gold: the gold sets of AX11.2.
func runGold(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ax gold <draw|propose|mark|score>")
	}
	switch args[0] {
	case "draw":
		return goldDraw(args[1:])
	case "propose":
		return goldPropose(args[1:])
	case "mark":
		return goldMark(args[1:])
	case "score":
		return goldScore(args[1:])
	}
	return fmt.Errorf("unknown gold subcommand %q", args[0])
}

func setFlags(fs *flag.FlagSet) (set, version *string) {
	set = fs.String("set", "x", "the gold set: x for the extraction or e for the edges")
	version = fs.String("version", "v1", "the version of the set")
	return set, version
}

func checkSet(set string) error {
	switch strings.ToLower(set) {
	case "x", "e":
		return nil
	}
	return fmt.Errorf("%q is not a gold set that ax knows, want x or e", set)
}

// goldDraw draws set X from the open papers of the math scope (AX11.2).
func goldDraw(args []string) error {
	fs := flag.NewFlagSet("ax gold draw", flag.ContinueOnError)
	set, version := setFlags(fs)
	seed := fs.String("seed", "", "the seed of the draw, and the set and its version when empty")
	dry := fs.Bool("n", false, "draw and count, but write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := checkSet(*set); err != nil {
		return err
	}
	if *seed == "" {
		*seed = strings.ToLower(*set) + "-" + *version
	}
	root := corpusRoot()
	if strings.ToLower(*set) == "e" {
		return goldDrawE(root, *version, *seed, *dry)
	}
	in, err := scope.Read(scope.Path(root, scope.Math))
	if err != nil {
		return fmt.Errorf("%w, so run ax select scope first", err)
	}
	plane := metadata.Plane{Root: root}
	var pool []gold.Candidate
	err = plane.ScanAll(func(shard string, r metadata.Record) error {
		if !in[r.ID] {
			return nil
		}
		if l, _ := licence.Latest(r); licence.ClassOf(l) != licence.ClassOpen {
			return nil
		}
		month, err := metadata.ShardMonth(shard)
		if err != nil {
			return err
		}
		pool = append(pool, gold.Candidate{ID: r.ID, Year: month.Year(), Code: gold.CodeOf(r)})
		return nil
	})
	if err != nil {
		return err
	}
	picks := gold.Draw(pool, gold.PlanX, *seed)
	groups := map[string]int{}
	for _, p := range picks {
		groups[p.Group]++
	}
	fmt.Printf("%d papers drawn from %d open papers in scope\n", len(picks), len(pool))
	for _, g := range gold.PlanX.Groups {
		fmt.Printf("  %-10s %4d of %d\n", g.Name, groups[g.Name], g.Size)
	}
	if *dry {
		return nil
	}
	path := gold.ListPath(root, *set, *version)
	header := fmt.Sprintf("gold set %s %s: %d papers drawn from %d open papers of scope-math with the seed %q on %s. The columns are the id, the group and the MSC code at two digits, or the primary category when the paper gives no MSC code.",
		strings.ToUpper(*set), *version, len(picks), len(pool), *seed, time.Now().UTC().Format("2006-01-02"))
	for _, g := range gold.PlanX.Groups {
		if n := groups[g.Name]; n < g.Size {
			header += fmt.Sprintf(" The group %s has %d of %d papers, because the scope has only %d open papers in it. The rest of the group are record papers, which wait for a marker that may see their text (AQ08) and get marks with no text (AX11.1).", g.Name, n, g.Size, n)
		}
	}
	if err := gold.WriteList(path, header, picks); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "written to %s\n", path)
	return nil
}

func readObjectsOf(root string, id axid.ID) ([]objects.Record, error) {
	return readObjectFile(corpus.AnalysisObjectsPath(root, id))
}

// readObjectFile reads an objects file of the analysis plane, or nothing when
// there is no file.
func readObjectFile(file string) ([]objects.Record, error) {
	b, err := mdrec.ReadLines(file)
	if errors.Is(err, iofs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []objects.Record
	for line := range bytes.SplitSeq(b, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var r objects.Record
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func groupOf(root, set, version, paper string) string {
	picks, err := gold.ReadList(gold.ListPath(root, set, version))
	if err != nil {
		return ""
	}
	for _, p := range picks {
		if p.ID == paper {
			return p.Group
		}
	}
	return ""
}

// goldPropose writes a proposed gold record from the object records of a
// version. It does not write over a marked record.
func goldPropose(args []string) error {
	fs := flag.NewFlagSet("ax gold propose", flag.ContinueOnError)
	set, version := setFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := checkSet(*set); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: ax gold propose [-set x|e] [-version v1] <id>v<n>")
	}
	paper, v, err := splitVersion(fs.Arg(0))
	if err != nil {
		return err
	}
	id, err := axid.Parse(paper)
	if err != nil {
		return err
	}
	root := corpusRoot()
	path := corpus.GoldPath(root, *set, *version, id, v)
	if old, err := gold.Read(path); err == nil && old.State == gold.Marked {
		return fmt.Errorf("%s is marked by %s, and a proposal does not write over it", path, old.By)
	}
	recs, err := readObjectsOf(root, id)
	if err != nil {
		return err
	}
	if len(gold.Items(recs, fmt.Sprint(v))) == 0 {
		fmt.Fprintf(os.Stderr, "%sv%d has no object records with an item kind, so the proposal has no marks\n", paper, v)
	}
	r := gold.Propose(strings.ToUpper(*set), *version, groupOf(root, *set, *version, paper), "ax/"+Version, paper, v, recs)
	if strings.ToLower(*set) == "e" {
		es, err := readEdgesOf(root, id)
		if err != nil {
			return err
		}
		r = gold.ProposeE(*version, groupOf(root, *set, *version, paper), "ax/"+Version, paper, v, recs, es)
		fmt.Printf("%sv%d: %d uses proposed\n", paper, v, len(r.Uses))
	}
	if err := r.Write(path); err != nil {
		return err
	}
	fmt.Printf("%sv%d: %d marks proposed, written to %s\n", paper, v, len(r.Marks), path)
	return nil
}

// goldMark records that a person decided each mark of a record.
func goldMark(args []string) error {
	fs := flag.NewFlagSet("ax gold mark", flag.ContinueOnError)
	set, version := setFlags(fs)
	by := fs.String("by", "", "the person who read the paper and decided each mark")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := checkSet(*set); err != nil {
		return err
	}
	if fs.NArg() != 1 || strings.TrimSpace(*by) == "" {
		return errors.New("usage: ax gold mark [-set x|e] [-version v1] -by <person> <id>v<n>")
	}
	paper, v, err := splitVersion(fs.Arg(0))
	if err != nil {
		return err
	}
	id, err := axid.Parse(paper)
	if err != nil {
		return err
	}
	path := corpus.GoldPath(corpusRoot(), *set, *version, id, v)
	r, err := gold.Read(path)
	if err != nil {
		return fmt.Errorf("%w, so run ax gold propose first and correct the marks", err)
	}
	r.State, r.By, r.On = gold.Marked, *by, time.Now().UTC().Format("2006-01-02")
	if err := r.Write(path); err != nil {
		return err
	}
	fmt.Printf("%sv%d: %d marks, marked by %s\n", paper, v, len(r.Marks), *by)
	return nil
}

// goldScore scores the object records against the marked records of a set.
func goldScore(args []string) error {
	fs := flag.NewFlagSet("ax gold score", flag.ContinueOnError)
	set, version := setFlags(fs)
	report := fs.String("report", "", "write the markdown report here as well")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := checkSet(*set); err != nil {
		return err
	}
	root := corpusRoot()
	picks, err := gold.ReadList(gold.ListPath(root, *set, *version))
	if err != nil {
		return fmt.Errorf("%w, so run ax gold draw first", err)
	}
	if strings.ToLower(*set) == "e" {
		return goldScoreE(root, *version, picks, *report)
	}
	s := gold.Score{Set: strings.ToUpper(*set), SetVersion: *version, Extractor: "ax/" + Version, Papers: len(picks)}
	for _, p := range picks {
		id, err := axid.Parse(p.ID)
		if err != nil {
			return err
		}
		recs, err := readObjectsOf(root, id)
		if err != nil {
			return err
		}
		v := latestVersion(recs)
		if v == 0 {
			continue
		}
		if c, err := os.ReadFile(corpus.CensusPath(root, id, v)); err == nil {
			var rec census.Record
			if json.Unmarshal(c, &rec) == nil {
				s.Strength.Of++
				if rec.Strength == "strong" || rec.Strength == "normal" {
					s.Strength.Hit++
				}
			}
		}
		g, err := gold.Read(corpus.GoldPath(root, *set, *version, id, v))
		if errors.Is(err, iofs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if g.State != gold.Marked {
			s.Proposed++
			continue
		}
		s.Marked++
		s.MarkedBy = countBy(s.MarkedBy, g.By)
		s.Add(gold.ScorePaper(g, recs))
	}
	md := s.Markdown()
	fmt.Print(md)
	if *report != "" {
		if err := writeFile(*report, []byte(md)); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "written to %s\n", *report)
	}
	return nil
}

// goldDrawE draws set E from the papers of set X that have a proof object.
func goldDrawE(root, version, seed string, dry bool) error {
	x, err := gold.ReadList(gold.ListPath(root, "x", "v1"))
	if err != nil {
		return fmt.Errorf("%w, so draw set X first, because set E is drawn from it", err)
	}
	hasProof := func(paper string) bool {
		id, err := axid.Parse(paper)
		if err != nil {
			return false
		}
		recs, err := readObjectsOf(root, id)
		if err != nil {
			return false
		}
		for _, r := range recs {
			if r.Kind == "proof" && r.Path == "source" {
				return true
			}
		}
		return false
	}
	picks := gold.DrawE(x, hasProof, seed, gold.PerGroupE)
	groups := map[string]int{}
	for _, p := range picks {
		groups[p.Group]++
	}
	fmt.Printf("%d papers drawn from the %d papers of set X v1\n", len(picks), len(x))
	for _, g := range gold.PlanX.Groups {
		fmt.Printf("  %-10s %4d of %d\n", g.Name, groups[g.Name], gold.PerGroupE)
	}
	if dry {
		return nil
	}
	header := fmt.Sprintf("gold set E %s: %d papers drawn from set X v1 with the seed %q on %s. Each group of set X gives the first %d papers in the order of the seed that have a proof object. A group with fewer papers has fewer extracted papers with proofs, and the draw runs again when more of set X is extracted. The columns are the id, the group and the MSC code at two digits, or the primary category when the paper gives no MSC code.",
		version, len(picks), seed, time.Now().UTC().Format("2006-01-02"), gold.PerGroupE)
	path := gold.ListPath(root, "e", version)
	if err := gold.WriteList(path, header, picks); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "written to %s\n", path)
	return nil
}

// goldScoreE scores the edges against the marked records of set E.
func goldScoreE(root, version string, picks []gold.Pick, report string) error {
	s := gold.EdgeScore{SetVersion: version, Extractor: "ax/" + Version, Papers: len(picks)}
	for _, p := range picks {
		id, err := axid.Parse(p.ID)
		if err != nil {
			return err
		}
		recs, err := readObjectsOf(root, id)
		if err != nil {
			return err
		}
		v := latestVersion(recs)
		if v == 0 {
			continue
		}
		g, err := gold.Read(corpus.GoldPath(root, "e", version, id, v))
		if errors.Is(err, iofs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if g.State != gold.Marked {
			s.Proposed++
			continue
		}
		s.Marked++
		s.MarkedBy = countBy(s.MarkedBy, g.By)
		es, err := readEdgesOf(root, id)
		if err != nil {
			return err
		}
		s.AddPaper(gold.InItems(g, recs), es)
	}
	md := s.Markdown()
	fmt.Print(md)
	if report != "" {
		if err := writeFile(report, []byte(md)); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "written to %s\n", report)
	}
	return nil
}

// readEdgesOf reads the edges of a paper, or nothing when it has none.
func readEdgesOf(root string, id axid.ID) ([]edges.Edge, error) {
	es, err := edges.ReadFile(corpus.EdgesPath(root, id))
	if errors.Is(err, iofs.ErrNotExist) {
		return nil, nil
	}
	return es, err
}

// latestVersion is the latest version that has object records of the source
// path, or 0.
func latestVersion(recs []objects.Record) int {
	v := 0
	for _, r := range recs {
		if r.Path != "source" {
			continue
		}
		if n := atoiOr(r.Version); n > v {
			v = n
		}
	}
	return v
}

func atoiOr(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// countBy adds one record marked by by to the counts.
func countBy(m map[string]int, by string) map[string]int {
	if m == nil {
		m = map[string]int{}
	}
	m[by]++
	return m
}

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/policy"
	"github.com/tamnd/arxiv-reader/seed"
	"github.com/tamnd/arxiv-reader/selection"
)

// selectSeed is the two halves of the hand written seed list.
//
// --propose writes a candidate list, a person edits it, and --commit records
// who edited it and when. The two are separate commands and not one command
// with a confirmation, because the editing is the point: what a program can do
// here is narrow three million papers down to a pool that is legal to publish
// and spread across the archives and the years, and what it cannot do is say
// which of them are worth reading.
func selectSeed(args []string) error {
	fs := flag.NewFlagSet("ax select seed", flag.ContinueOnError)
	propose := fs.Bool("propose", false, "write a candidate list to manifests/seed.yaml")
	commit := fs.Bool("commit", false, "read manifests/seed.yaml and put what is left into the selection")
	class := fs.String("class", "", "the access classes to draw from, comma separated, and the three that permit the text by default")
	top := fs.Int("top", 3, "how many papers to offer per archive and year")
	by := fs.String("by", "", "who edited the list, and git's own user.name when this is unset")
	dry := fs.Bool("n", false, "say what would happen and write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("usage: ax select seed --propose [-class ...] [-top n], or ax select seed --commit")
	}
	switch {
	case *propose && *commit:
		return errors.New("--propose writes the candidate list and --commit reads it back after somebody has edited it, so running both in one go would commit a list nobody has looked at")
	case *propose:
		return seedPropose(*class, *top, *dry)
	case *commit:
		return seedCommit(*by, *dry)
	}
	return errors.New("usage: ax select seed --propose [-class ...] [-top n], or ax select seed --commit")
}

func seedPropose(class string, top int, dry bool) error {
	classes, err := parseClasses(class)
	if err != nil {
		return err
	}
	root := corpusRoot()
	pol, err := policy.Load(corpus.PolicyPath(root))
	if err != nil {
		return err
	}
	p, err := seed.Propose(metadata.Plane{Root: root}, seed.Options{
		Classes: classes, Top: top, Weights: pol.Selection, Now: time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	fmt.Print(p.Text())
	if dry {
		return nil
	}
	path := corpus.SeedPath(root)
	// A proposal that would overwrite one somebody has already committed is
	// refused rather than merged. The committed file is the record of who chose
	// the corpus, and a second proposal on top of it would leave a file whose
	// commit block describes rows that are no longer in it.
	if held, err := seed.Load(path); err == nil && held.Commit != nil {
		return fmt.Errorf("%s was committed by %s on %s, so move it aside before proposing over it", path, held.Commit.By, held.Commit.At)
	}
	if err := p.Save(path); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "written to %s, which is yours to edit\n", corpus.SeedPath(""))
	return nil
}

func seedCommit(by string, dry bool) error {
	root := corpusRoot()
	path := corpus.SeedPath(root)
	p, err := seed.Load(path)
	if err != nil {
		return fmt.Errorf("%s is not there or will not read, so run ax select seed --propose first: %w", path, err)
	}
	if p.Commit != nil {
		return fmt.Errorf("%s was already committed by %s on %s, and committing it twice would credit the second person with the first one's edits", path, p.Commit.By, p.Commit.At)
	}
	if len(p.Seed) == 0 {
		return fmt.Errorf("%s offers no papers, and a seed of nothing is an edit somebody should undo rather than commit", path)
	}
	who := by
	if who == "" {
		who = gitWho(root)
	}
	if who == "" {
		return errors.New("nobody is named as having edited this list, so set git's user.name and user.email or pass -by, because a seed with no name on it is the judgement this project is least able to check and the least able to attribute")
	}

	plane := metadata.Plane{Root: root}
	sel := corpus.SelectedPath(root)
	m, err := selection.Load(sel)
	if err != nil {
		return err
	}
	// The gate is re-asked here against the metadata plane rather than trusted
	// from the file, because the file is one a person has edited by hand and
	// this is the one decision the project cannot afford to get wrong. A row
	// somebody pasted in, or a licence that has been resolved differently since
	// the proposal was built, is caught here and nowhere else.
	entries := p.Entries(who, selection.Today())
	for i, e := range entries {
		id, err := axid.Parse(e.Ref())
		if err != nil {
			return err
		}
		rec, err := record(plane, id)
		if err != nil {
			return fmt.Errorf("%s is on the seed list and not in the metadata plane: %w", e.ID, err)
		}
		v, ok := rec.VersionAt(e.Version)
		if !ok {
			return fmt.Errorf("%s has no v%d in the plane, so there is nothing saying what its licence is", e.ID, e.Version)
		}
		if v.Licence == "" {
			return fmt.Errorf("%s has no licence on record, and nobody has looked, so run ax licence resolve %s first", e.ID, e.Ref())
		}
		if !corpus.AccessFor(v.Licence).MayPublishText() {
			return fmt.Errorf("%s is %s, so the corpus may publish its record and nothing else, and a paper like that cannot be in the content plane", e.Ref(), v.Licence)
		}
		if err := e.Check(); err != nil {
			return err
		}
		entries[i] = e
	}

	added, held := 0, 0
	for _, e := range entries {
		if _, ok := m.Find(e.ID); ok {
			// A paper already in the selection keeps the reason it is in for
			// and the date it was chosen. Being on a seed list is not a second
			// reason and it is not a later date.
			held++
			continue
		}
		added++
		if dry {
			continue
		}
		m.Put(e)
	}
	fmt.Printf("%s on the list, %s added, %s already in the selection\n", prose.Count(len(entries), "paper"), prose.Thousands(added), prose.Thousands(held))
	if dry {
		fmt.Fprintln(os.Stderr, "a dry run, so nothing was written")
		return nil
	}
	if err := m.Save(sel); err != nil {
		return err
	}
	p.Commit = &seed.Commit{By: who, At: selection.Today(), Kept: len(p.Seed)}
	if err := p.Save(path); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s and %s, committed by %s\n", corpus.SelectedPath(""), corpus.SeedPath(""), who)
	return nil
}

// parseClasses reads the -class list.
func parseClasses(s string) ([]corpus.Access, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var out []corpus.Access
	for _, name := range strings.Split(s, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		a := corpus.Access(name)
		switch a {
		case corpus.AccessOpen, corpus.AccessShareAlike, corpus.AccessVerbatim:
			out = append(out, a)
		case corpus.AccessRecord:
			return nil, errors.New("record is the class of a paper the corpus may hold the metadata of and nothing else, so a paper in it is not a candidate for the content plane at all")
		default:
			return nil, fmt.Errorf("%q is not one of the access classes, which are open, share-alike, verbatim and record", name)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("-class names no class, so drop the flag to draw from the three that permit the text")
	}
	return out, nil
}

// gitWho is the name and address git would put on a commit here.
//
// Read from git rather than from the environment, because the question this
// answers is who chose these papers and the answer wants to be the same string
// that appears beside the commit which adds them.
func gitWho(root string) string {
	name := gitConfig(root, "user.name")
	mail := gitConfig(root, "user.email")
	switch {
	case name != "" && mail != "":
		return fmt.Sprintf("%s <%s>", name, mail)
	case name != "":
		return name
	}
	return mail
}

func gitConfig(root, key string) string {
	cmd := exec.Command("git", "config", "--get", key)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

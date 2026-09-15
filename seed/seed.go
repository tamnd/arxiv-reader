// Package seed builds the candidate list the first thousand papers come out of.
//
// The seed is licence first and subject second, which inverts how a reading
// list is normally built, and the inversion is forced rather than chosen: there
// is no point putting a paper on a list this project may not publish. So the
// order of work is the licence gate, then a score, then a person.
//
// The person is not decoration. Read what Score says about its own terms: at
// seed time three of the four are nought, because the corpus is empty so there
// is no inside citation count, nobody publishes an outside one this can read,
// and the extraction path is not decided until after selection. What comes out
// of a proposal is a pool that is legal to publish and spread across the
// archives and the years, and the judgement about which papers are worth
// reading is supplied by whoever edits the file. That is what 2166-04 means by
// a hand written seed list, and building the pool is the part a program can do.
package seed

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/policy"
	"github.com/tamnd/arxiv-reader/selection"
)

// Candidate is one paper the proposal offers.
type Candidate struct {
	ID      string `yaml:"id"`
	Version int    `yaml:"version"`
	// Title is here for the person doing the editing and for nothing else.
	// Nothing reads it back, and ax select seed --commit takes the title from
	// the metadata plane like everything else does.
	Title string `yaml:"title"`
	// Archive and Year are the group this was the Rank th best candidate in.
	Archive  string         `yaml:"archive"`
	Category string         `yaml:"category"`
	Year     int            `yaml:"year"`
	Rank     int            `yaml:"rank"`
	Score    float64        `yaml:"score"`
	Licence  corpus.Licence `yaml:"licence"`
	Access   corpus.Access  `yaml:"access"`
}

// Group is one archive and year, and how the proposal treated it.
type Group struct {
	Archive string `yaml:"archive"`
	Year    int    `yaml:"year"`
	// Eligible is how many papers passed the licence gate here and Taken is how
	// many of them the proposal offers, which is Top or fewer.
	Eligible int `yaml:"eligible"`
	Taken    int `yaml:"taken"`
}

// Commit is who turned the proposal into a selection, and when.
type Commit struct {
	By   string `yaml:"by"`
	At   string `yaml:"at"`
	Kept int    `yaml:"kept"`
}

// Proposal is the whole of manifests/seed.yaml.
type Proposal struct {
	Built   string           `yaml:"built"`
	Classes []corpus.Access  `yaml:"classes"`
	Top     int              `yaml:"top"`
	Weights policy.Selection `yaml:"weights"`
	Counts  Counts           `yaml:"counts"`
	Groups  []Group          `yaml:"groups"`
	Seed    []Candidate      `yaml:"seed"`
	Commit  *Commit          `yaml:"commit,omitempty"`
}

// Counts is what the scan saw, which is the part of a proposal that says how
// much of arXiv it was drawn from.
type Counts struct {
	// Records is every record scanned and Eligible is how many of them the
	// licence gate let through.
	Records  int `yaml:"records"`
	Eligible int `yaml:"eligible"`
	// Unresolved is records whose latest version has no licence on it, which is
	// a paper nobody has run ax licence resolve over rather than a paper that
	// failed the gate. The two are counted apart on purpose, because a large
	// unresolved count means the proposal was drawn from a fraction of arXiv
	// and the pool is narrower than it looks.
	Unresolved int `yaml:"unresolved"`
	// Refused is records whose licence permits the record and nothing more.
	Refused int `yaml:"refused"`
	// Offered is the length of the candidate list.
	Offered int `yaml:"offered"`
}

// Options are the arguments to a proposal.
type Options struct {
	// Classes is the access classes a paper may be in to be offered. Empty
	// means the three that permit the text, which is what 2166-04 runs.
	Classes []corpus.Access
	// Top is how many papers to offer per archive and year.
	Top int
	// Weights orders the candidates inside a group.
	Weights policy.Selection
	// Now is what the age is measured against, so that a test is not a
	// different test next year.
	Now time.Time
}

// Classes is the default filter, which is every class that permits the text.
//
// A record class paper is not a candidate for the content plane at all, since
// the corpus may hold its metadata, its structure, its tags and its graph edges
// and nothing else, and all of that comes from the metadata plane without the
// paper ever being selected.
var Classes = []corpus.Access{corpus.AccessOpen, corpus.AccessShareAlike, corpus.AccessVerbatim}

// Score orders the queue and decides nothing else.
//
// The four terms are the four in 2166-04 and three of them are nought at seed
// time. That is worth saying plainly rather than burying in a weighted sum.
//
// The inside citation count is nought because the content plane is empty, which
// is the whole situation a seed exists to get out of. The outside count is
// nought because the metadata plane carries no citation counts: the Cornell
// snapshot does not have them and arXiv does not publish them, so until a
// citation index is joined onto the plane there is nothing to read. The vision
// penalty is nought because the extraction path is decided by ax path decide,
// which runs after selection and not before it.
//
// So a seed proposal is ordered by age within its group, and the person editing
// the file supplies the judgement the citation count would have supplied. Every
// term is here anyway, because ax select suggest fills two of them in over a
// corpus that has papers in it, and the weights are meant to be the same
// weights rather than a second set that drifts.
func Score(inside, outside int, age float64, vision bool, w policy.Selection) float64 {
	if w.AgeCap > 0 && age > float64(w.AgeCap) {
		age = float64(w.AgeCap)
	}
	if age < 0 {
		age = 0
	}
	score := float64(inside)*w.Inside + float64(outside)*w.Outside + age*w.Age
	if vision {
		score += w.Vision
	}
	return score
}

// Age is how many years old a paper is, from its first version.
//
// The first version and not the latest, because a paper revised last month is
// not a new paper, and the thing the age term is standing in for is how long
// the work has been available to be built on.
func Age(r metadata.Record, now time.Time) float64 {
	if len(r.Versions) == 0 {
		return 0
	}
	first := r.Versions[0].Created
	if first.IsZero() {
		return 0
	}
	return now.Sub(first).Hours() / (24 * 365.25)
}

// Propose walks the metadata plane and builds the candidate list.
func Propose(p metadata.Plane, opt Options) (Proposal, error) {
	if opt.Top < 1 {
		return Proposal{}, errors.New("seed: a proposal that offers no papers per group is an empty file, so top has to be at least 1")
	}
	classes := opt.Classes
	if len(classes) == 0 {
		classes = Classes
	}
	for _, c := range classes {
		if !known(c) {
			return Proposal{}, fmt.Errorf("seed: %q is not one of the four access classes, which are open, share-alike, verbatim and record", c)
		}
	}
	out := Proposal{
		Built:   selection.Today(),
		Classes: classes,
		Top:     opt.Top,
		Weights: opt.Weights,
	}
	pools := map[string][]Candidate{}
	err := p.ScanAll(func(_ string, r metadata.Record) error {
		out.Counts.Records++
		v, ok := r.Latest()
		if !ok {
			return nil
		}
		if v.Licence == "" {
			out.Counts.Unresolved++
			return nil
		}
		access := corpus.AccessFor(v.Licence)
		if !wanted(classes, access) {
			out.Counts.Refused++
			return nil
		}
		out.Counts.Eligible++
		year := 0
		if len(r.Versions) > 0 && !r.Versions[0].Created.IsZero() {
			year = r.Versions[0].Created.Year()
		}
		c := Candidate{
			ID: r.ID, Version: v.Version, Title: r.Title,
			Archive: r.Archive(), Category: r.Primary(), Year: year,
			Score:   round(Score(0, 0, Age(r, opt.Now), false, opt.Weights)),
			Licence: v.Licence, Access: access,
		}
		key := fmt.Sprintf("%s/%04d", c.Archive, c.Year)
		pools[key] = append(pools[key], c)
		return nil
	})
	if err != nil {
		return Proposal{}, err
	}

	for _, pool := range pools {
		// Highest score first, and then by identifier, because two papers from
		// the same month score within days of each other and a proposal that
		// reorders itself between two runs over the same plane is a proposal
		// nobody can review as a diff.
		sort.Slice(pool, func(i, j int) bool {
			if pool[i].Score != pool[j].Score {
				return pool[i].Score > pool[j].Score
			}
			return pool[i].ID < pool[j].ID
		})
		taken := pool
		if len(taken) > opt.Top {
			taken = taken[:opt.Top]
		}
		for i := range taken {
			taken[i].Rank = i + 1
		}
		out.Seed = append(out.Seed, taken...)
		out.Groups = append(out.Groups, Group{
			Archive: taken[0].Archive, Year: taken[0].Year,
			Eligible: len(pool), Taken: len(taken),
		})
	}
	sort.Slice(out.Groups, func(i, j int) bool { return groupLess(out.Groups[i], out.Groups[j]) })
	sort.Slice(out.Seed, func(i, j int) bool {
		a, b := out.Seed[i], out.Seed[j]
		if a.Archive != b.Archive {
			return a.Archive < b.Archive
		}
		if a.Year != b.Year {
			return a.Year < b.Year
		}
		return a.Rank < b.Rank
	})
	out.Counts.Offered = len(out.Seed)
	return out, nil
}

// round cuts the score to two places.
//
// The age term is a count of days divided by 365.25, so an unrounded score is
// fifteen digits of arithmetic noise in a file somebody is meant to read. Two
// places is more precision than the number deserves already.
func round(f float64) float64 {
	return math.Round(f*100) / 100
}

func groupLess(a, b Group) bool {
	if a.Archive != b.Archive {
		return a.Archive < b.Archive
	}
	return a.Year < b.Year
}

func wanted(classes []corpus.Access, a corpus.Access) bool {
	for _, c := range classes {
		if c == a {
			return true
		}
	}
	return false
}

func known(a corpus.Access) bool {
	switch a {
	case corpus.AccessOpen, corpus.AccessShareAlike, corpus.AccessVerbatim, corpus.AccessRecord:
		return true
	}
	return false
}

// Entries is the proposal as selection entries, ready for selected.yaml.
//
// The licence is not re-read here. The caller re-asks the gate against the
// metadata plane before writing any of these, because a proposal is a file a
// person has edited and a file a person has edited is not a file to trust with
// the one decision this project cannot get wrong.
func (p Proposal) Entries(by, today string) []selection.Entry {
	out := make([]selection.Entry, 0, len(p.Seed))
	for _, c := range p.Seed {
		out = append(out, selection.Entry{
			ID: c.ID, Version: c.Version, Reason: selection.Seed,
			Category: c.Category, Year: c.Year, Rank: c.Rank, Score: c.Score,
			By: by, Added: today, Status: selection.StatusSelected,
		})
	}
	return out
}

// Load reads a proposal off disk.
func Load(path string) (Proposal, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Proposal{}, err
	}
	var p Proposal
	if err := yaml.Unmarshal(b, &p); err != nil {
		return Proposal{}, fmt.Errorf("seed: %s: %w", path, err)
	}
	for i, c := range p.Seed {
		if c.ID == "" {
			return Proposal{}, fmt.Errorf("seed: %s: row %d has no id", path, i+1)
		}
		if c.Version < 1 {
			return Proposal{}, fmt.Errorf("seed: %s: %s names no version, and the content plane holds one version of a paper", path, c.ID)
		}
	}
	return p, nil
}

// Save writes a proposal, creating the directory if it is missing.
func (p Proposal) Save(path string) error {
	body, err := yaml.Marshal(p)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(Header), body...), 0o644)
}

// Header is what somebody opening the file for the first time reads.
const Header = `# The seed: a candidate list, not a selection.
#
# Every paper in here is one the licence gate says this corpus may publish, and
# that is the only claim the file makes. Delete the rows that do not belong, and
# then run ax select seed --commit, which re-asks the licence gate against the
# metadata plane and writes what is left into manifests/selected.yaml.
#
# The score orders the candidates inside a group and decides nothing else. Three
# of its four terms are nought at seed time: there is no inside citation count
# because the content plane is empty, there is no outside one because the
# metadata plane carries none, and the extraction path is decided after
# selection rather than before it. What is left is the age of the paper, so the
# order inside a group is oldest first and is not a ranking of anything.
#
# Which is to say the judgement is yours. The program picked a pool that is
# legal to publish and spread across the archives and the years, and choosing
# what is worth reading out of it is the part it cannot do.
`

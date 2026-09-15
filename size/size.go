// Package size weighs the corpus against the day it has to be split in two.
//
// The content plane will outgrow one repository. The plan for that is written
// down in the spec rather than improvised later: the trigger is a checkout over
// 20 GB or a clone over fifteen minutes, whichever comes first, and at that
// point the content plane splits by year into peer repositories while the
// metadata, the manifests, the tags, the graph and the reports stay where they
// are. What this package does is watch the two numbers, because a split that
// gets noticed when somebody complains the clone is slow is a split that
// happens in a hurry.
//
// Nothing here writes a report. A committed report of the checkout size changes
// the checkout size, so it would never settle, and the answer is about a
// machine and a connection rather than about the corpus.
package size

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Trigger is the checkout the spec splits the corpus at.
const Trigger int64 = 20 << 30

// CloneTrigger is the other half of the trigger and Rate is the connection it
// is measured on.
//
// A stated rate and not a measurement, because the clone that matters is
// somebody else's on a connection nobody here can test. Ten megabytes a second
// is an ordinary home connection, and it is a named constant so that the number
// this prints can be argued with rather than believed.
const (
	CloneTrigger = 15 * time.Minute
	Rate         = 10 << 20
)

// Planes are the committed directories, in the order the spec lists them.
//
// work/ is not here and neither is .git. work/ holds arXiv's own bytes, is
// gitignored in this project and in the corpus, and a fresh checkout does not
// carry it. .git is counted separately because it is what a clone transfers
// while the planes are what a disk holds, and those are the two halves of the
// trigger.
var Planes = []string{"content", "figures", "tables", "metadata", "tags", "refs", "graph", "manifests", "reports"}

// Fixed are the planes that do not grow with the selection.
//
// The metadata plane holds every paper arXiv has whether it was selected or
// not, so it is the same size for a corpus of ten papers and a corpus of ten
// thousand. The reports are one file each however many papers they counted.
// Both take up room in the checkout and both are counted in it, but charging
// them to the papers would say that one paper costs what the whole of arXiv's
// metadata costs, and the projection built on that would be nonsense.
var Fixed = map[string]bool{"metadata": true, "reports": true}

// Corpus is one checkout weighed.
type Corpus struct {
	Root string
	// Checkout is every committed plane added up, and Objects is what a clone
	// would have to transfer.
	Checkout int64
	Objects  int64
	// Scaling is the part of the checkout that grows with the selection, which
	// is the checkout less the planes named in Fixed.
	Scaling int64
	// Planes is one row per plane that has files in it, largest first.
	Planes []Plane
	// Years is one row per year of the content plane, largest first, which is
	// the order a split would move them in.
	Years []Year
	// Papers is how many papers the content plane holds, counted in whichever
	// language has the most of them so that a half translated corpus is not
	// counted twice.
	Papers int
}

// Plane is one committed directory.
type Plane struct {
	Name  string
	Bytes int64
	Files int
}

// Year is one year of the content plane, every language of it.
type Year struct {
	Year   string
	Bytes  int64
	Papers int
}

// Measure walks the checkout.
//
// It walks the disk rather than asking git, because the question is what is
// there now and not what was committed. A corpus half way through a run has
// files git has never seen and they take up the same room.
func Measure(root string) (Corpus, error) {
	out := Corpus{Root: root}
	for _, name := range Planes {
		bytes, files, err := walk(filepath.Join(root, name))
		if err != nil {
			return Corpus{}, err
		}
		out.Checkout += bytes
		if !Fixed[name] {
			out.Scaling += bytes
		}
		if files == 0 {
			continue
		}
		out.Planes = append(out.Planes, Plane{Name: name, Bytes: bytes, Files: files})
	}
	sort.SliceStable(out.Planes, func(i, j int) bool { return out.Planes[i].Bytes > out.Planes[j].Bytes })

	// The objects and not the whole of .git, because a clone transfers the
	// history and then builds the index, the logs and the working tree itself.
	objects, _, err := walk(filepath.Join(root, ".git", "objects"))
	if err != nil {
		return Corpus{}, err
	}
	out.Objects = objects

	years, papers, err := content(root)
	if err != nil {
		return Corpus{}, err
	}
	out.Years, out.Papers = years, papers
	return out, nil
}

// walk adds up one directory, and a directory that is not there weighs nothing.
func walk(dir string) (int64, int, error) {
	var bytes int64
	var files int
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			// A file that went away between the listing and the stat is a run
			// happening in the next terminal, and it is not worth failing a
			// measurement over. Anything else is a real problem with the disk.
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		bytes += info.Size()
		files++
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return bytes, files, nil
}

// content divides the content plane the way a split would divide it.
//
// Every language in one row, because a split moves a year and takes every
// translation of that year with it. A paper is counted once per year however
// many languages hold it, and the corpus total is the fullest single language,
// which is the count of papers rather than the count of files.
func content(root string) ([]Year, int, error) {
	dir := filepath.Join(root, "content")
	langs, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	bytes := map[string]int64{}
	inYear := map[string]map[string]bool{}
	inLang := map[string]int{}
	for _, lang := range langs {
		if !lang.IsDir() {
			continue
		}
		shards, err := os.ReadDir(filepath.Join(dir, lang.Name()))
		if err != nil {
			return nil, 0, err
		}
		for _, shard := range shards {
			if !shard.IsDir() {
				continue
			}
			at := filepath.Join(dir, lang.Name(), shard.Name())
			n, _, err := walk(at)
			if err != nil {
				return nil, 0, err
			}
			year := YearOf(shard.Name())
			bytes[year] += n
			papers, err := os.ReadDir(at)
			if err != nil {
				return nil, 0, err
			}
			if inYear[year] == nil {
				inYear[year] = map[string]bool{}
			}
			for _, p := range papers {
				if !p.IsDir() {
					continue
				}
				inYear[year][shard.Name()+"/"+p.Name()] = true
				inLang[lang.Name()]++
			}
		}
	}
	var out []Year
	for year, n := range bytes {
		out = append(out, Year{Year: year, Bytes: n, Papers: len(inYear[year])})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Bytes != out[j].Bytes {
			return out[i].Bytes > out[j].Bytes
		}
		return out[i].Year < out[j].Year
	})
	most := 0
	for _, n := range inLang {
		if n > most {
			most = n
		}
	}
	return out, most, nil
}

// YearOf is the year a shard is in.
//
// arXiv opened in August 1991 and the shard carries two digits of the year, so
// 91 and up is the nineteen hundreds and everything else is the two thousands.
// That rule holds until 2091, by which time somebody else can decide what to do
// about it.
func YearOf(shard string) string {
	if len(shard) < 4 {
		return "unknown"
	}
	for i := 0; i < 4; i++ {
		if shard[i] < '0' || shard[i] > '9' {
			return "unknown"
		}
	}
	if shard[:2] >= "91" {
		return "19" + shard[:2]
	}
	return "20" + shard[:2]
}

// Clone is how long a clone of this repository would take at Rate.
func (c Corpus) Clone() time.Duration {
	return time.Duration(float64(c.Objects) / Rate * float64(time.Second)).Round(time.Second)
}

// Share is how much of the checkout trigger has been used.
func (c Corpus) Share() float64 { return float64(c.Checkout) / float64(Trigger) }

// CloneShare is how much of the clone trigger has been used.
func (c Corpus) CloneShare() float64 { return float64(c.Clone()) / float64(CloneTrigger) }

// Nearest is whichever half of the trigger is closer, and how close it is.
//
// The trigger is whichever comes first, so the honest single number to watch is
// the larger of the two shares and not the one that happens to be tidier.
func (c Corpus) Nearest() (string, float64) {
	if cs := c.CloneShare(); cs > c.Share() {
		return "the clone", cs
	}
	return "the checkout", c.Share()
}

// Tripped says whether the corpus has reached the trigger, and on which half.
func (c Corpus) Tripped() (bool, string) {
	checkout, clone := c.Checkout >= Trigger, c.Clone() >= CloneTrigger
	switch {
	case checkout && clone:
		return true, "both halves of the trigger"
	case checkout:
		return true, "the checkout"
	case clone:
		return true, "the clone"
	}
	return false, ""
}

// PerPaper is what one paper costs the checkout, and nought over an empty
// content plane.
//
// The scaling planes and not the whole checkout, for the reason given at Fixed.
func (c Corpus) PerPaper() int64 {
	if c.Papers <= 0 {
		return 0
	}
	return c.Scaling / int64(c.Papers)
}

// Headroom is how many more papers fit before the checkout trigger.
//
// At the current cost per paper, which is a projection and not a promise. The
// backfill is older papers, older papers are shorter, and a corpus that starts
// translating gets three more languages of the same papers. The number is worth
// watching anyway, because what it is for is noticing the month the slope
// changes rather than predicting a date.
func (c Corpus) Headroom() int64 {
	per := c.PerPaper()
	if per <= 0 || c.Checkout >= Trigger {
		return 0
	}
	return (Trigger - c.Checkout) / per
}

// share is a part of the checkout, and -1 when there is no checkout, which is
// what prose.Percent prints as n/a.
//
// It is here rather than borrowed from prose because these are int64 and the
// reports the prose helper serves count papers and rules.
func share(part, whole int64) float64 {
	if whole <= 0 {
		return -1
	}
	return float64(part) / float64(whole)
}

// First is the year a split would move first, which is the largest one.
func (c Corpus) First() (Year, bool) {
	if len(c.Years) == 0 {
		return Year{}, false
	}
	return c.Years[0], true
}

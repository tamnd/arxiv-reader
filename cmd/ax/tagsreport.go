package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/tags"
)

// tagsReport writes reports/tags.md, which is the corpus's tags counted and its
// fall through rate to pass four.
//
// 12-plan.md asks for that rate to be recorded, and the reason is that it is the
// one number that says whether the permanent names in this corpus are being read
// off or guessed at. The gate in ax tags diff refuses one paper at a time and
// only when somebody runs it. This is the corpus answering the same question
// before a run hits the line.
//
// Every revision is matched again here rather than read back from what the
// matcher said when a register was carried, because the matcher is the thing
// being measured and a number kept from an older one would answer about that.
func tagsReport(args []string) error {
	fs := flag.NewFlagSet("ax tags report", flag.ContinueOnError)
	out := fs.String("o", "", "where to write the report, and the default is reports/tags.md in the corpus")
	quiet := fs.Bool("q", false, "write the file and print nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("ax tags report takes no arguments, so drop %s", fs.Args()[0])
	}
	root := corpusRoot()
	papers, err := registered(root)
	if err != nil {
		return err
	}
	census := tags.Census{Generated: time.Now()}
	for _, id := range papers {
		row, err := countPaper(root, id)
		if err != nil {
			return err
		}
		census.Papers = append(census.Papers, row)
	}

	path := *out
	if path == "" {
		path = filepath.Join(root, "reports", "tags.md")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(census.Markdown()), 0o644); err != nil {
		return err
	}
	if !*quiet {
		fmt.Print(census.Text())
		fmt.Fprintf(os.Stderr, "written to %s\n", path)
	}
	return nil
}

// countPaper is one paper's register and every revision of it the cache can
// answer.
//
// A version the cache does not hold is skipped rather than fetched. This
// command reads what is on the disk and does not go to the network, because a
// report that fetched a few hundred megabytes of e-prints to print a percentage
// is a report nobody runs twice.
func countPaper(root string, id axid.ID) (tags.PaperTags, error) {
	reg, err := tags.LoadRegister(corpus.TagsPath(root, id))
	if err != nil {
		return tags.PaperTags{}, err
	}
	row := tags.PaperTags{ID: id.Canonical, Version: reg.Version}
	for _, e := range reg.Entries {
		if e.Gone != "" {
			row.Buried++
			continue
		}
		row.Live++
	}
	runs, err := tags.LoadRuns(corpus.RunsPath(root, id))
	if err != nil {
		return tags.PaperTags{}, err
	}
	row.Runs = len(runs)
	row.Cached = cached(root, id)

	// Consecutive pairs, because that is the comparison the corpus actually
	// makes: a register is carried from the version it was assigned against onto
	// the next one, and v1 against v4 is a diff nothing ever runs.
	for i := 1; i < len(row.Cached); i++ {
		from, to := row.Cached[i-1], row.Cached[i]
		was, err := objectsAt(root, id, from)
		if err != nil {
			return tags.PaperTags{}, err
		}
		now, err := objectsAt(root, id, to)
		if err != nil {
			return tags.PaperTags{}, err
		}
		row.Revisions = append(row.Revisions, tags.Revise(from, to, len(was), tags.Match(was, now)))
	}
	return row, nil
}

// registered is every paper with a register, in identifier order.
//
// The tags directory and not the content plane, because the subject of this
// report is the register. A paper with content and no register has nothing here
// to count, and one with a register and no content still has its tags and they
// are still promises.
func registered(root string) ([]axid.ID, error) {
	dir := filepath.Join(root, "tags")
	shards, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []axid.ID
	for _, shard := range shards {
		if !shard.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(dir, shard.Name()))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			name, ok := strings.CutSuffix(f.Name(), ".tags")
			if f.IsDir() || !ok {
				continue
			}
			id, err := paperID(name)
			if err != nil {
				return nil, fmt.Errorf("ax tags report: %s/%s is not a paper: %w", shard.Name(), f.Name(), err)
			}
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Canonical < out[j].Canonical })
	return out, nil
}

// paperID reads an identifier back out of the name a file on disk carries.
//
// An old style identifier has a slash in it and the path form writes that as a
// hyphen, so it has to be turned back before it is an identifier again. A new
// style one has neither and goes through the first parse.
func paperID(name string) (axid.ID, error) {
	id, err := axid.Parse(name)
	if err == nil {
		return id, nil
	}
	return axid.Parse(strings.Replace(name, "-", "/", 1))
}

// cached is every version of a paper the work directory holds, oldest first.
//
// The conversion and the rendering both count, because either one is enough to
// read a version's objects out of and paperAt takes whichever is there. A
// version held in neither is a version this report cannot compare, and saying
// which versions are cached is how the by paper table explains why a paper with
// four versions has one revision in it.
func cached(root string, id axid.ID) []int {
	seen := map[int]bool{}
	for _, g := range []string{
		corpus.ConvertedPath(root, id, -1),
		corpus.RenderPath(root, id, -1),
	} {
		// The paths are built with version -1 and the number put back as a star,
		// so the glob is the layout package's own answer about where these live
		// rather than a second copy of it here. The last occurrence, because the
		// corpus root is somebody's directory and may spell anything.
		matches, err := filepath.Glob(star(g))
		if err != nil {
			continue
		}
		for _, m := range matches {
			if v, ok := versionIn(m, id); ok {
				seen[v] = true
			}
		}
	}
	out := make([]int, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}

// star turns the version this file asked the layout package for into a glob.
func star(p string) string {
	i := strings.LastIndex(p, "v-1")
	if i < 0 {
		return p
	}
	return p[:i] + "v*" + p[i+len("v-1"):]
}

// versionIn reads the version out of a cached file's path.
func versionIn(p string, id axid.ID) (int, bool) {
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		rest, ok := strings.CutPrefix(part, corpus.PathID(id)+"v")
		if !ok {
			continue
		}
		rest = strings.TrimSuffix(rest, ".html")
		v, err := strconv.Atoi(rest)
		if err != nil || v < 1 {
			return 0, false
		}
		return v, true
	}
	return 0, false
}

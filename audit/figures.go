package audit

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/figures"
	"github.com/tamnd/arxiv-reader/tables"
)

// figureRules read the pictures and the tables.
//
// Three things rather than one, and the split is what the group is about. A
// figure is a decision in the manifest, bytes on disk and a line in a body, and
// the ways a corpus goes wrong here are the ways those three disagree: a body
// pointing at a picture nobody decided about, a manifest entry for bytes that
// are not there, bytes that are there and should not be. A table is two files
// and the rules are that both exist and that they say the same thing.
//
// F04 is in 2166-10 and is not here. It says nothing under figures/ is
// untracked, which is a question for git, and this tool is run over a directory
// that is a checkout on one machine and an unpacked archive on the next. It
// goes with G08, the other rule that reads git history, in M4.
var figureRules = []Rule{
	{
		ID: "F01", Group: GroupFigures, Hard: true,
		Says: "every figure a file references exists on disk",
		Why:  "A picture arXiv serves is referenced by its path on arXiv until ax figures has decided about it, and the extractor leaves that path alone on purpose so that the decision is made in one place. So this rule is also the one that says ax figures has not been run: a body still pointing at arXiv is a body whose pictures nobody has established the permission for.",
	},
	{
		ID: "F02", Group: GroupFigures, Hard: true,
		Says: "no figure under 100 by 100 pixels",
		Why:  "A twenty pixel picture is a spacer, a bullet or a rule the converter read as a figure, and committing it puts a piece of page furniture in the corpus under the paper's licence.",
	},
	{
		ID: "F03", Group: GroupFigures, Hard: true,
		Says: "no figure over 500 KB",
	},
	{
		ID: "F05", Group: GroupFigures, Hard: true,
		Says: "no two figures within one paper have the same bytes",
		Why:  "Per paper and not across the corpus. The same institutional logo and the same standard schematic appear in thousands of papers, and a corpus wide byte identity check would report normal practice tens of thousands of times. Inside one paper it is a panel committed twice.",
	},
	{
		ID: "F06", Group: GroupFigures, Hard: true,
		Says: "no figure that covers more than three quarters of a page",
		Why:  "A picture the size of a page is a scan of the page, and a corpus of those is a mirror of somebody's PDF rather than a reading of the paper. Only a file that states its own resolution is judged, because a physical size cannot be guessed from a pixel count.",
	},
	{
		ID: "F07", Group: GroupFigures, Hard: true,
		Says: "the figure manifest loads, and every numbered figure in it has a caption",
		Why:  "This is also the rule that reads the manifest at all, the way G01 reads the register, so a manifest that does not load is reported here once rather than failing six rules in a row. A numbered figure with no caption is a picture nobody can identify without opening the paper on arXiv, which is the thing the corpus exists to avoid. Numbered, because a paper that prints one caption over four panels gives three of them no caption of their own and the float above them says what they are.",
	},
	{
		ID: "F08", Group: GroupFigures, Hard: true,
		Says: "every figure of a record paper is absent",
		Why:  "A record paper is one whose licence lets the corpus hold the metadata, the structure and the tags and nothing else. Its figures are the part a reader misses most and the part a leak is least defensible for.",
	},
	{
		ID: "F09", Group: GroupFigures, Hard: true,
		Says: "no figure suspected or confirmed to be third party has its bytes committed",
		Why:  "Deliberately conservative and deliberately hard. Suspected is treated exactly as confirmed, because the cost of being wrong one way is a missing picture and the cost of being wrong the other way is republishing somebody else's copyrighted work under a licence they never granted. The caption, the number and the tag are published either way.",
	},
	{
		ID: "F10", Group: GroupFigures,
		Says: "every figure the paper numbers is present",
		Why:  "Soft, because a paper refers to a figure in another paper often enough that this cannot fail a build. A number the prose sends a reader to and that the corpus does not have is a float the extraction dropped.",
	},
	{
		ID: "F11", Group: GroupFigures, Hard: true,
		Says: "every table exists twice, as <n>.md and as <n>.tex",
		Why:  "The Markdown is what a reader and a translator see and it cannot express a multi column header or a rule under part of a row. The markup is what that loss is measured against, so half a pair is a table with nothing to check it. A paper whose sections hold tables and whose tables directory does not is a paper ax tables has not been run over, and that is this rule too.",
	},
	{
		ID: "F12", Group: GroupFigures, Hard: true,
		Says: "a table's .md and .tex agree on row count, column count and every numeric cell",
		Why:  "The numbers are what makes keeping a table twice worth the disk. A mismatch means one of the two was edited, or a translation touched a numeric cell, and a table is where a paper's measured results live.",
	},
}

// pictures runs the figure and table rules over one paper.
//
// The manifest, the bodies and the two directories on disk are read once each
// and every rule in the group is answered off them, because they are four reads
// of the same paper and a group that walked them once per rule would be eleven.
func (c Content) pictures(col *collector, id axid.ID, files []content) error {
	shard := corpus.Shard(id)
	dir := corpus.ContentDir("", c.lang(), id)
	here := func(rule string, what string, args ...any) {
		col.add(Finding{Rule: rule, File: dir, Shard: shard, ID: id.Canonical, What: fmt.Sprintf(what, args...)})
	}

	c.referenced(col, id, files)
	c.numbered(col, id, files)
	if err := c.tabled(col, id, files); err != nil {
		return err
	}

	// F07 owns the manifest the way G01 owns the register and T01 owns a
	// content file's shape: a file that does not load is one finding here and
	// not six across the group.
	//
	// The caption is checked by the loader and not again here. It is the same
	// arrangement the tables package is for: one definition of what is wrong,
	// read by the tool that writes the file and by the rule that checks it, so
	// the two cannot come to disagree about it.
	col.checked("F07")
	m, err := figures.Load(corpus.FiguresPath(c.Root, shard))
	if err != nil {
		here("F07", "%v", err)
		return nil
	}
	mine := m.Figures[:0:0]
	for _, f := range m.Figures {
		if f.Paper == id.Canonical && f.Version == version(files) {
			mine = append(mine, f)
		}
	}

	manifest := corpus.FiguresPath("", shard)
	at := func(rule string, what string, args ...any) {
		col.add(Finding{Rule: rule, File: manifest, Shard: shard, ID: id.Canonical, What: fmt.Sprintf(what, args...)})
	}
	seen := map[string]string{}
	for _, f := range mine {
		col.checked("F09")
		if f.Committed() && (f.ThirdParty == figures.Suspected || f.ThirdParty == figures.Confirmed) {
			at("F09", "commits %s, which is %s of being somebody else's: %s", f.Source, f.ThirdParty, f.Why)
		}
		if !f.Committed() {
			// Everything below is about bytes that are in the corpus, and a
			// withheld figure has none. Its entry is the record that the
			// decision was made, which is what the entry is for.
			continue
		}

		// The other side of F01. The bodies point at the picture through the
		// manifest, so an entry that says committed with nothing behind it is
		// the same hole reached from the record rather than from the copy, and
		// it is a hole even in the sections that do not show the figure.
		col.checked("F01")
		if !c.onDisk(f.File) {
			at("F01", "records %s as committed to %s and there is no such file", f.Source, f.File)
		}

		col.checked("F02")
		if f.Width > 0 && f.Height > 0 && (f.Width < figures.MinSide || f.Height < figures.MinSide) {
			at("F02", "commits %s at %d by %d pixels, and a figure is at least %d on both sides", f.Source, f.Width, f.Height, figures.MinSide)
		}

		col.checked("F03")
		if f.Bytes > figures.SizeCap {
			at("F03", "commits %s at %s, and the cap is %s", f.Source, kb(f.Bytes), kb(figures.SizeCap))
		}

		col.checked("F06")
		if f.PageFrom != "" && f.PageFraction > figures.PageCap {
			at("F06", "commits %s at %.0f%% of a page, measured from %s", f.Source, f.PageFraction*100, f.PageFrom)
		}

		col.checked("F05")
		if first, ok := seen[f.SHA256]; ok {
			at("F05", "commits %s and %s, which are the same bytes twice", first, f.Source)
		} else {
			seen[f.SHA256] = f.Source
		}
	}

	// F08 is about the paper rather than about a figure, and it is checked for
	// every paper because the question is asked of every paper: a record paper
	// with no figures in the corpus is the rule passing and not the rule
	// sitting out.
	col.checked("F08")
	if held(files) == corpus.AccessRecord {
		for _, f := range mine {
			if f.Committed() {
				at("F08", "is a record paper, whose figures the corpus may not publish, and %s is committed anyway", f.Source)
			}
		}
	}
	return nil
}

// referenced is F01 over the bodies.
//
// Read off the masked body, so an image line inside a fenced block is a listing
// about Markdown and not a figure this corpus is missing.
func (c Content) referenced(col *collector, id axid.ID, files []content) {
	shard := corpus.Shard(id)
	for _, f := range files {
		col.checked("F01")
		at := func(rule string, line int, what string, args ...any) {
			col.add(Finding{Rule: rule, File: f.path, Shard: shard, Line: line, ID: id.Canonical, What: fmt.Sprintf(what, args...)})
		}
		for i, line := range strings.Split(f.masked, "\n") {
			for _, m := range image.FindAllStringSubmatch(line, -1) {
				src := m[1]
				if !strings.HasPrefix(src, "/") {
					at("F01", i+1, "shows %s, which is where the picture sits on arXiv and not a file in this corpus, so nothing has decided whether it may be published here", src)
					continue
				}
				if !c.onDisk(src) {
					at("F01", i+1, "shows %s and there is no such file", src)
				}
			}
		}
	}
}

// onDisk says whether a path rooted at the corpus is a file in it.
func (c Content) onDisk(rooted string) bool {
	if rooted == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(c.Root, filepath.FromSlash(strings.TrimPrefix(rooted, "/"))))
	return err == nil
}

// numbered is F10: a figure the prose sends a reader to that the paper does not
// have.
//
// The number as the paper prints it, because that is what the reader is told to
// look for. A panel is found through its identifier instead, since a paper that
// prints Figure 6 over two pictures refers to them as 6a and 6b and neither is
// a number the labels carry.
func (c Content) numbered(col *collector, id axid.ID, files []content) {
	shard := corpus.Shard(id)
	printed, local := map[string]bool{}, map[string]bool{}
	for _, f := range files {
		for _, m := range figureLabel.FindAllStringSubmatch(f.doc.Body, -1) {
			printed[strings.TrimSpace(m[1])] = true
		}
		for _, m := range figureLocal.FindAllStringSubmatch(f.doc.Body, -1) {
			local[strings.ToLower(m[1])] = true
		}
	}
	col.checked("F10")
	said := map[string]bool{}
	for _, f := range files {
		for i, line := range strings.Split(f.masked, "\n") {
			for _, m := range figureRef.FindAllStringSubmatch(line, -1) {
				n := strings.Trim(m[1], ".")
				if n == "" || printed[n] || said[n] {
					continue
				}
				if local["fig-"+strings.ReplaceAll(strings.ToLower(n), ".", "-")] {
					continue
				}
				said[n] = true
				col.add(Finding{Rule: "F10", File: f.path, Shard: shard, Line: i + 1, ID: id.Canonical,
					What: fmt.Sprintf("sends a reader to Figure %s, and this paper has no figure with that number in it", n)})
			}
		}
	}
}

// tabled is F11 and F12 over the pair of files each table is kept as.
//
// The count in the sections is checked against the count on disk first, because
// the commonest way for a table to fail to exist twice is for it to exist no
// times at all, and that is a paper ax tables has not been run over rather than
// one pair with a half missing.
func (c Content) tabled(col *collector, id axid.ID, files []content) error {
	shard := corpus.Shard(id)
	dir := corpus.TablesDir(c.Root, id)
	names, err := tables.Names(dir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	inBodies := 0
	for _, f := range files {
		inBodies += tables.Count(f.masked)
	}
	if inBodies == 0 && len(names) == 0 {
		return nil
	}

	// One direction only. A directory holding more pairs than the sections do
	// is a table the paper cut between versions, and ax tables takes those away
	// itself. A section holding a table that is nowhere on disk is a table that
	// is kept once or not at all, which is what this rule is about.
	col.checked("F11")
	if inBodies > len(names) {
		col.add(Finding{Rule: "F11", File: corpus.ContentDir("", c.lang(), id), Shard: shard, ID: id.Canonical,
			What: fmt.Sprintf("lays out %s in its sections and keeps %s under %s",
				plural(inBodies, "table"), plural(len(names), "table"), corpus.TablesDir("", id))})
	}
	for _, name := range names {
		why, err := tables.Agree(dir, name)
		if err != nil {
			return err
		}
		if why == nil || why.Rule == "F12" {
			col.checked("F12")
		}
		if why == nil {
			continue
		}
		col.add(Finding{Rule: why.Rule, File: path.Join(corpus.TablesDir("", id), name+".md"), Shard: shard, ID: id.Canonical, What: why.What})
	}
	return nil
}

// version is the version of the paper the content was written from.
//
// Off the front matter rather than off the manifest, because the manifest can
// hold the figures of two versions and only one of them is the version on disk.
// Zero for a paper whose files say nothing, which matches no entry, and a paper
// with no version in its front matter has a T02 finding already.
func version(files []content) int {
	for _, f := range files {
		if n, err := strconv.Atoi(strings.TrimPrefix(f.doc.Front.Version, "v")); err == nil {
			return n
		}
	}
	return 0
}

// held is the access class the content claims for itself.
//
// The licence the article carries is what it is derived from, because that is
// arXiv's own field and the access line is this corpus's reading of it. Either
// one saying record is enough: they disagreeing is an S rule's finding, and a
// figure rule that waited for them to agree would publish the figure.
func held(files []content) corpus.Access {
	for _, f := range files {
		if f.doc.Front.Access == string(corpus.AccessRecord) {
			return corpus.AccessRecord
		}
		if l, err := corpus.ParseLicence(f.doc.Front.LicenceOfSource); err == nil && corpus.AccessFor(l) == corpus.AccessRecord {
			return corpus.AccessRecord
		}
	}
	return ""
}

func kb(n int) string { return fmt.Sprintf("%.0f KB", float64(n)/1024) }

// image is a Markdown image line, figureLabel is the heading of a numbered
// float, figureLocal is the identifier of any float, and figureRef is the prose
// sending a reader to one.
//
// The reference form takes a capital or a digit and then anything that looks
// like a number, which is Figure 3, Figure 2.1 and Figure B.1, because a paper
// numbering its appendix figures by letter is ordinary. The label form reads
// the same string out of the line the float opens with.
var (
	image       = regexp.MustCompile(`!\[[^\]]*\]\(([^)\s]+)`)
	figureLabel = regexp.MustCompile(`(?m)^\*\*Figure\s+([^*]+)\*\*\s*\{#`)
	figureLocal = regexp.MustCompile(`\{#(fig-[0-9A-Za-z-]+)`)
	figureRef   = regexp.MustCompile(`\bFigures?\s+([0-9A-Z][0-9A-Za-z.]*)`)
)

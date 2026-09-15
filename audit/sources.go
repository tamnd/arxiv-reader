package audit

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
)

// sourceRules are the group S rules that read a content file.
//
// Five of the twelve in 2166-10 that are about the content plane rather than
// the metadata plane, which are the five a paper's files and its record answer
// between them. S02 and S06 are about translated files, one that a
// no-derivatives paper has none and one that a translation carries the licence
// the propagation table gives it, and nothing is translated yet, so both arrive
// with M8. S03 asks git what it tracks and S11 reads a takedown manifest, and
// neither of those two things exists here yet.
//
// Every one of them is hard, which is the whole group's rule: a soft licence
// check is a licence breach with a warning next to it. S08 and S09 are hard for
// a different reason: they do not ask whether a paper was read well, they ask
// whether the file that was read was the paper, and the answer to that is yes or
// no.
var sourceRules = []Rule{
	{
		ID: "S01", Group: GroupSources, Hard: true,
		Says: "no content file exists for a paper the corpus may not publish the text of",
		Why:  "The gate in ax fetch and ax extract render is what stops this happening and this is the check that it did. A record paper is one whose licence lets the corpus hold the metadata, the structure and the tags and nothing else, and a licence nothing recognises is treated as one of those, because the cost of guessing wrong is republishing somebody's paper without their permission. The file's own access line and the licence it says the article carries are both read, since a file whose two answers disagree is a file nobody can act on.",
	},
	{
		ID: "S04", Group: GroupSources, Hard: true,
		Says: "every paper in the content plane has a record in the metadata plane",
		Why:  "A content file is written out of a record, so a paper with content and no record is a record deleted from under it or a paper somebody put there by hand. It is also the rule the rest of this group leans on: every other question here is asked of the record, so a paper reported by this one is a paper the others step over rather than four findings about the same missing line.",
	},
	{
		ID: "S07", Group: GroupSources, Hard: true,
		Says: "every content file names the version it was taken from, and the plane has that version",
		Why:  "A licence belongs to a version. A file that does not say which version it holds cannot have its licence checked by anything, and a file naming a version the plane has never heard of has had its licence checked against a version that does not exist.",
	},
	{
		ID: "S08", Group: GroupSources, Hard: true,
		Says: "no content file is longer than the source it claims to come from could hold",
		Why:  "Only a file read off a printed page can be measured this way, because only that file knows how many pages it was read off, and it is the path where the measurement is worth taking. A section that says it came off two pages and holds forty pages of prose is a paper the page numbering was lost in, or two papers in one file, or a reader that ran past the end of the document it was given. Nothing typeset holds this much text on a page, so this is not a judgement about how dense a paper is.",
	},
	{
		ID: "S09", Group: GroupSources, Hard: true,
		Says: "the pages a paper was read off carry as much text as a paper's pages do",
		Why:  "The other half of the same question, and asked of the paper rather than of a file, because several sections of a paper are printed on one page and a short section that shares a page with the rest of the paper is ordinary. A PDF whose text layer is a cover sheet, a paywall notice or the output of a scanner that found nothing comes back as a handful of characters over twenty pages, and that is a paper that is not the paper it names. The floor is far below what any typeset page carries, so a paper of plates and a paper that is mostly tables both pass it, and a paper that fails it is one nobody should have extracted on this path at all.",
	},
	{
		ID: "S10", Group: GroupSources, Hard: true,
		Says: "every content file's licence was read off the abs page",
		Why:  "Every bulk surface states one licence for a whole paper, which was checked against Kaggle, the Hugging Face mirror and OAI-PMH on 2026-09-14 and is written up in 2166-01. Only the abs page states a licence for a version. A content file whose licence came from anywhere else is a file whose permission nobody has established, and ax licence resolve is what establishes it.",
	},
	{
		ID: "S12", Group: GroupSources, Hard: true,
		Says: "every content file's licence is the one the plane holds for the version it names",
		Why:  "The version trap. A paper relicensed at v3 has a v1 under the old terms, so a corpus that reads the latest licence and publishes the version it extracted has published one version under another version's permission. The file's two licence fields are read against each other as well, because the licence a file is published under is derived from the licence the article carries and a file where those two disagree was written by somebody rather than by this tool.",
	},
}

// own is one paper of the content plane waiting for its record.
//
// The files state what they are: the version they hold, the licence the article
// carries and the access class this corpus read off it. The record is what says
// whether any of that is true, and it is one line of the metadata plane, so the
// questions are collected here and answered a shard at a time. See pending.
type own struct {
	shard string
	id    string
	// dir is the paper's directory, which is what S04 reports against, because
	// the finding is about the paper and not about one of its files.
	dir   string
	files []stated
}

// stated is one file's account of itself.
type stated struct {
	path string
	// version is the number the file names, and 0 when it names none, which S07
	// has already reported and nothing else asks about.
	version         int
	licenceOfSource string
	// known is whether that licence is one arXiv issues. A file naming
	// something else has been reported by S01 already, and holding it against
	// the record would report it a second time under another rule's name.
	known  bool
	access string
}

// sources runs the group S rules over one paper.
//
// The half that reads the file runs here and the half that reads the record is
// held back, which is the same shape the reference rules are in and for the
// same reason: the plane holds three million records and a question asked one
// paper at a time reads it once per paper.
func (c Content) sources(col *collector, id axid.ID, files []content, hold *pending) {
	shard := corpus.Shard(id)
	mine := own{shard: shard, id: id.Canonical, dir: corpus.ContentDir("", c.lang(), id)}
	// What S09 is measured on: the characters of every file that says which pages
	// it came off, and the first and the last of those pages.
	read, counted, firstPage, lastPage := 0, 0, 0, 0
	for _, f := range files {
		at := func(rule string, what string, args ...any) {
			col.add(Finding{Rule: rule, File: f.path, Shard: shard, ID: id.Canonical, What: fmt.Sprintf(what, args...)})
		}
		front := f.doc.Front

		col.checked("S10")
		switch from := front.LicenceFrom; from {
		case string(metadata.SourceAbs):
		case "":
			at("S10", "does not say where its licence was read from")
		default:
			at("S10", "carries a licence read from %s, which states one licence for a whole paper, and ax licence resolve is what reads the one for this version", from)
		}

		col.checked("S07")
		version, bad := versionOf(front.Version)
		if bad != nil {
			at("S07", "%v", bad)
		}

		// The licence the article carries is arXiv's field and the access line
		// is this corpus's reading of it, so the two disagreeing is one of them
		// having been edited. Read here rather than in F08, which asks the same
		// question of a picture and is allowed to take either answer.
		col.checked("S01")
		source, unknown := corpus.ParseLicence(front.LicenceOfSource)
		switch {
		case front.LicenceOfSource == "":
			at("S01", "does not say what licence the article carries")
		case unknown != nil:
			at("S01", "says the article is under %q, which is not a licence arXiv issues", front.LicenceOfSource)
		case front.Access != string(corpus.AccessFor(source)):
			at("S01", "says it is access %s and the article is under %s, which is access %s", front.Access, source, corpus.AccessFor(source))
		case !corpus.AccessFor(source).MayPublishText():
			at("S01", "is under %s, which is access %s, and the corpus may hold the metadata, the structure and the tags of such a paper and not its text", source, corpus.AccessFor(source))
		}

		// The published licence is derived from the licence the article carries
		// and is not a second reading of it, so this is the derivation run
		// again rather than a table written out here. A licence nothing may be
		// published under has no answer to derive, and that file is S01's.
		col.checked("S12")
		known := unknown == nil && front.LicenceOfSource != ""
		if known {
			if out, none := corpus.PublishedLicence(source); none == nil && front.Licence != out.SPDX() {
				at("S12", "is published as %s and the article is under %s, which this corpus publishes as %s", front.Licence, source, out.SPDX())
			}
		}

		// Only the native path fills source_pages in, so on a corpus with no
		// paper read off a printed page this never looks and reports that it
		// never ran, which is the honest answer and not a pass.
		if pages := pageCount(front.SourcePages); pages > 0 {
			held := len([]rune(f.doc.Body))
			col.checked("S08")
			if held > pages*mostPerPage {
				at("S08", "holds %d characters and says it was read off %s, which is %d a page, and no page carries more than %d", held, pageWord(pages), held/pages, mostPerPage)
			}
			read, counted = read+held, counted+1
			if a, b := pageSpan(front.SourcePages); a > 0 {
				if firstPage == 0 || a < firstPage {
					firstPage = a
				}
				if b > lastPage {
					lastPage = b
				}
			}
		}

		mine.files = append(mine.files, stated{path: f.path, version: version, licenceOfSource: front.LicenceOfSource, known: known, access: front.Access})
	}
	// S09 is asked of the paper and not of a file, which S08 above it is. Several
	// sections of a paper are printed on one page, so a file's own characters over
	// its own pages is not a density: a paper that prints "The authors declare no
	// competing interests." under a heading of its own has a section of forty
	// characters that says it was read off one page, and that page holds the rest
	// of the paper as well. What this rule is for is a PDF whose text layer was a
	// cover sheet, which is the whole paper coming back as a handful of characters
	// over twenty pages, and that question only has an answer at the paper.
	if firstPage > 0 {
		pages := lastPage - firstPage + 1
		col.checked("S09")
		if read < pages*leastPerPage {
			col.add(Finding{Rule: "S09", File: mine.dir, Shard: shard, ID: id.Canonical, What: fmt.Sprintf("holds %d characters over %s and says they were read off %s, which is %d a page, and a page of a paper carries at least %d", read, fileWord(counted), pageWord(pages), read/pages, leastPerPage)})
		}
	}
	hold.own = append(hold.own, mine)
}

// records answers the half of group S that needs the paper's own record.
//
// Called once per shard with the records that shard turned out to hold, which
// is what keeps the plane read once. A paper with no record is S04's finding
// and nothing else is asked about it, because every other question here is a
// question about the record.
func (c Content) records(col *collector, papers []own, found map[string]metadata.Record) {
	for _, p := range papers {
		col.checked("S04")
		rec, ok := found[p.id]
		if !ok {
			col.add(Finding{Rule: "S04", File: p.dir, Shard: p.shard, ID: p.id, What: "has content and the metadata plane has no record for it"})
			continue
		}
		for _, f := range p.files {
			at := func(rule string, what string, args ...any) {
				col.add(Finding{Rule: rule, File: f.path, Shard: p.shard, ID: p.id, What: fmt.Sprintf(what, args...)})
			}
			if f.version == 0 {
				continue
			}
			v, ok := rec.VersionAt(f.version)
			if !ok {
				at("S07", "says it is of v%d and the plane holds %s of that paper", f.version, versions(rec))
				continue
			}
			if f.known && f.licenceOfSource != string(v.Licence) {
				what := fmt.Sprintf("says the article is under %s and the plane says v%d is under %s", f.licenceOfSource, f.version, v.Licence)
				// The trap has a shape, and naming it is the difference between
				// a finding somebody fixes and a finding somebody argues with.
				if last, ok := rec.Latest(); ok && last.Version != f.version && f.licenceOfSource == string(last.Licence) {
					what += fmt.Sprintf(", which is the licence of v%d, the latest", last.Version)
				}
				at("S12", "%s", what)
			}
		}
	}
}

const (
	// mostPerPage is the characters a printed page can hold, for S08.
	//
	// Twelve thousand. A page of a two column paper measured off the text layer
	// carries three to four thousand characters, a page of dense tabular matter
	// gets to about eight, and twelve is past anything a typesetter has ever put
	// on a page. This is a ceiling on an impossible file and not an opinion about
	// a dense one.
	mostPerPage = 12000
	// leastPerPage is the characters a page of a paper carries, for S09.
	//
	// Two hundred, which is one paragraph. A page holding one full width figure
	// and its caption is above it, a page of plates with running heads is around
	// it, and the thing this catches is far below both: a cover sheet, a paywall
	// notice, or the nothing a scanner's text layer holds, spread over a paper's
	// worth of pages. The floor is per page and the measurement is over the whole
	// paper, so a plate section is averaged against the prose around it and a
	// section of one sentence is not a finding about anything.
	leastPerPage = 200
)

// pageWord is a page count the way a finding reads it out.
func pageWord(n int) string {
	if n == 1 {
		return "1 page"
	}
	return fmt.Sprintf("%d pages", n)
}

// fileWord is the same for a file count.
func fileWord(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}

// pageCount is how many pages a source_pages range names, and nought for a file
// that names none.
//
// The two forms are "7" for one page and "3-7" for a range, which is what span
// writes, and anything else is a field somebody edited. A field nobody can read
// counts as no field rather than as a finding of its own, because S08 and S09 are
// about the paper and not about the punctuation in the front matter.
func pageCount(pages string) int {
	a, b := pageSpan(pages)
	if a == 0 {
		return 0
	}
	return b - a + 1
}

// pageSpan is the first and the last page a source_pages range names, and nought
// for a field nobody can read.
func pageSpan(pages string) (int, int) {
	s := strings.TrimSpace(pages)
	if s == "" {
		return 0, 0
	}
	first, last := s, s
	if i := strings.Index(s, "-"); i > 0 {
		first, last = s[:i], s[i+1:]
	}
	a, err := strconv.Atoi(strings.TrimSpace(first))
	if err != nil || a < 1 {
		return 0, 0
	}
	b, err := strconv.Atoi(strings.TrimSpace(last))
	if err != nil || b < a {
		return 0, 0
	}
	return a, b
}

// versionOf reads the number out of the version a file names.
func versionOf(raw string) (int, error) {
	if raw == "" {
		return 0, fmt.Errorf("names no version, and a licence belongs to a version")
	}
	n, err := strconv.Atoi(strings.TrimPrefix(raw, "v"))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("names version %q, which is not a version number", raw)
	}
	return n, nil
}

// versions is the list of versions a record holds, for a finding to print.
func versions(rec metadata.Record) string {
	if len(rec.Versions) == 0 {
		return "no version at all"
	}
	var out []string
	for _, v := range rec.Versions {
		out = append(out, fmt.Sprintf("v%d", v.Version))
	}
	return strings.Join(out, ", ")
}

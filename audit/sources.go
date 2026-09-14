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
// neither of those two things exists here yet. S08 and S09 measure a body
// against the pages it was read off, which is a count only the native path has,
// and that path is M4. A rule registered before it can run is a rule everybody
// believes is working.
//
// Every one of them is hard, which is the whole group's rule: a soft licence
// check is a licence breach with a warning next to it.
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

		mine.files = append(mine.files, stated{path: f.path, version: version, licenceOfSource: front.LicenceOfSource, known: known, access: front.Access})
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

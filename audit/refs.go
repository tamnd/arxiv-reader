package audit

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
	"github.com/tamnd/arxiv-reader/refs"
	"github.com/tamnd/arxiv-reader/tags"
)

// refRules read a paper's bibliography and the citations that point into it.
//
// Six of the nine in 2166-10. R07 is the acquire selection report, which is a
// paper cited by three or more papers in the content plane and not in it, and
// nothing writes that report yet. R08 is about a rendered reference section and
// nothing renders one yet. R09 compares a paper's resolution rate against its
// category's median, which needs the baselines M06 needs, so both arrive
// together in M6. A rule registered before it can run is a rule everybody
// believes is working.
//
// Two of the six ask the metadata plane a question rather than the paper, and
// they are the reason this file has a second half. See pending.
var refRules = []Rule{
	{
		ID: "R01", Group: GroupRefs, Hard: true,
		Says: "every arXiv id written down in this corpus names a paper the metadata plane has",
		Why:  "The plane covers all of arXiv, so an identifier has nowhere to hide: a hallucinated one is caught outright and a mistyped one is caught at the character. Read out of the bodies and out of the bibliography both, because an id in a reference is an id somebody will click. An id in a month the plane does not hold yet is not judged, since that is a fact about how much of arXiv has been fetched and group S is where the plane's own coverage is answered.",
	},
	{
		ID: "R02", Group: GroupRefs, Hard: true,
		Says: "every citation in a body has an entry in that paper's bibliography",
		Why:  "A citation comes out of the render path as a link to a bibliography anchor, and an anchor with nothing behind it is a reference the reader is invited to follow into a blank. It is also what a paper nobody has run ax refs over looks like, which is the answer wanted: content is committed with its bibliography beside it.",
	},
	{
		ID: "R03", Group: GroupRefs, Hard: true,
		Says: "every paper's bibliography loads, and every entry in it keeps the line the paper printed",
		Why:  "This is the rule that reads the manifest at all, the way G01 reads the register and F07 reads the figure manifest, so a file that does not load is reported here once rather than failing four rules in a row. What an entry has to keep is refs.Load's definition and not a second one written here: the fields are a reading of a bibliography style and any of them can be empty, but the text the paper printed is what gets published and an entry without it publishes nothing.",
	},
	{
		ID: "R04", Group: GroupRefs,
		Says: "every resolved reference still resolves that way",
		Why:  "The manifest says a reference was matched to a paper, and the matcher has moved since. Re-running the ladder over the record the entry points at is the only thing that says whether the edge would be built again today, and an edge that would not is either a matcher that has tightened or a resolution that was always wrong. Soft, because a tightened matcher is a change to this tool and not a defect in the corpus.",
	},
	{
		ID: "R05", Group: GroupRefs, Hard: true,
		Says: "no reference resolves to the paper that is citing it",
		Why:  "A paper citing itself is ordinary and a bibliography entry that resolves to the paper it sits in is not. It is a loop in the graph that every traversal has to special case, and it is what a title match on a paper's own title looks like from the outside.",
	},
	{
		ID: "R06", Group: GroupRefs,
		Says: "no citation runs forward in time by more than two years",
		Why:  "A paper cannot cite work that does not exist yet, so a 2019 paper with a 2024 reference in it has a year read off the wrong part of the entry. Two years of slack because a preprint cited in 2019 is published in 2021 and a style that prints the publication year is not wrong to. Soft, because the year is a reading of prose and the entry is published either way.",
	},
}

// pending is what the reference rules hold back until every paper has been
// read.
//
// Two of them ask the metadata plane a question. R01 asks whether an identifier
// somebody wrote down exists at all, and R04 asks whether a resolved entry
// would resolve the same way against the record it points at. The plane holds
// three million records, so asking either question one paper at a time would
// mean reading the plane once per paper. The questions are collected here
// instead and the plane is read once at the end, which is the shape the
// resolver itself is built in and for the same reason.
type pending struct {
	// named is every arXiv id the corpus wrote down, with the finding to report
	// if the plane turns out not to have it.
	named []mention
	// again is every resolved entry, with the record it claims.
	again []recheck
}

type mention struct {
	shard string
	id    string
	find  Finding
}

type recheck struct {
	shard string
	paper string
	entry refs.Entry
	find  Finding
}

// bibliography runs the reference rules and T12 over one paper.
func (c Content) bibliography(col *collector, id axid.ID, files []content, hold *pending) error {
	shard := corpus.Shard(id)
	name := corpus.RefsPath("", id)
	at := func(rule string, what string, args ...any) {
		col.add(Finding{Rule: rule, File: name, Shard: shard, ID: id.Canonical, What: fmt.Sprintf(what, args...)})
	}

	manifest, err := refs.Load(corpus.RefsPath(c.Root, id))
	switch {
	case err != nil:
		col.checked("R03")
		at("R03", "%v", err)
		return nil
	case manifest.Paper != "" || len(manifest.Entries) > 0:
		col.checked("R03")
	}
	entries := map[string]bool{}
	for _, e := range manifest.Entries {
		entries[e.ID] = true
	}

	// One mention of an id per paper, however many times the paper writes it
	// down. A bibliography that names a preprint the plane has not got names it
	// in the entry and again in the resolution, and that is one hole and not
	// two.
	said := map[string]bool{}
	names := func(raw string, where Finding) {
		got, err := axid.Parse(raw)
		if err != nil {
			col.checked("R01")
			where.What += raw + ", which is not an arXiv identifier at all"
			col.add(where)
			return
		}
		if said[got.Canonical] {
			return
		}
		said[got.Canonical] = true
		where.What += got.Canonical + ", and the metadata plane has no paper with that identifier"
		hold.named = append(hold.named, mention{shard: corpus.Shard(got), id: got.Canonical, find: where})
	}

	// R01 counts a check when it judges an identifier and not when it reads a
	// file, so a corpus with no metadata plane leaves it not run rather than
	// passing it. A rule that passes because it had nothing to compare against
	// is the thing the four states exist to stop.
	c.anchors(col, id, files, entries)
	for _, f := range files {
		for i, line := range strings.Split(f.masked, "\n") {
			for _, m := range arxivRef.FindAllStringSubmatch(line, -1) {
				names(m[1], Finding{Rule: "R01", File: f.path, Shard: shard, Line: i + 1, ID: id.Canonical, What: "names arXiv:"})
			}
		}
	}

	for _, e := range manifest.Entries {
		if e.ArXiv != "" {
			names(e.ArXiv, Finding{Rule: "R01", File: name, Shard: shard, ID: id.Canonical, What: e.ID + " names arXiv:"})
		}
		if e.Year != 0 {
			col.checked("R06")
			if e.Year > id.Year+2 {
				at("R06", "%s cites work dated %d, and this paper was submitted in %d", e.ID, e.Year, id.Year)
			}
		}
		if e.Resolved == "" {
			continue
		}
		col.checked("R05")
		to, err := axid.Parse(e.Resolved)
		switch {
		case err != nil:
			col.checked("R04")
			at("R04", "resolves %s to %q, which is not an arXiv identifier at all", e.ID, e.Resolved)
			continue
		case to.Canonical == id.Canonical:
			at("R05", "resolves %s to %s, which is the paper doing the citing", e.ID, to.Canonical)
			continue
		}
		names(to.Canonical, Finding{Rule: "R01", File: name, Shard: shard, ID: id.Canonical, What: e.ID + " resolves to "})
		hold.again = append(hold.again, recheck{
			shard: corpus.Shard(to), paper: to.Canonical, entry: e,
			find: Finding{Rule: "R04", File: name, Shard: shard, ID: id.Canonical},
		})
	}
	return nil
}

// anchors is R02 and T12, which are the same walk over the bodies.
//
// A link into the paper's own anchors is either a citation or a cross
// reference, and which of the two it is decides which rule answers for it. The
// bibliography anchors are told apart by refs.Bibliographic, which is the
// function ax refs cites reads them with, because a rule that disagreed with
// the command about which anchors are citations would report every citation in
// the corpus as a broken link.
func (c Content) anchors(col *collector, id axid.ID, files []content, entries map[string]bool) {
	shard := corpus.Shard(id)
	local := map[string]bool{}
	for _, f := range files {
		if f.doc.Front.LocalID != "" {
			local[f.doc.Front.LocalID] = true
		}
		for _, m := range anchorDef.FindAllStringSubmatch(f.doc.Body, -1) {
			local[m[1]] = true
		}
	}
	for _, f := range files {
		col.checked("R02")
		col.checked("T12")
		for i, line := range strings.Split(f.masked, "\n") {
			for _, m := range anchorLink.FindAllStringSubmatch(line, -1) {
				anchor := m[1]
				find := func(rule, what string, args ...any) {
					col.add(Finding{Rule: rule, File: f.path, Shard: shard, Line: i + 1, ID: id.Canonical, What: fmt.Sprintf(what, args...)})
				}
				switch {
				case refs.Bibliographic(anchor):
					if !entries[anchor] {
						find("R02", "cites %s, and this paper's bibliography has no entry with that id", anchor)
					}
				case tags.Tag(anchor).Valid():
					// A link into a bare tag is G02's, which is the rule about
					// a reference that names a tag and not the paper it is in.
					// It is not an identifier in this paper either, so without
					// this the same link would be reported twice and the second
					// report would be the less useful one.
				case !local[anchor]:
					find("T12", "links to #%s, which is neither an identifier in this paper nor an entry in its bibliography", anchor)
				}
			}
		}
	}
}

// plane answers the two rules that need the metadata plane, once, for the whole
// run.
//
// Only the months that were asked about are read. A run over one paper wants a
// handful of shards and a run over the whole content plane wants most of them,
// and either way each one is read once.
func (c Content) plane(col *collector, hold *pending) error {
	if !col.wanted("R01") && !col.wanted("R04") {
		return nil
	}
	p := metadata.Plane{Root: c.Root}
	shards, err := p.Shards()
	if err != nil {
		return err
	}
	// A month the plane has not got is a month nothing can be said about. The
	// alternative is reporting every reference to a preprint from a year that
	// has not been fetched yet, which would be thousands of findings about the
	// metadata plane's coverage reported against the content plane's papers.
	holds := map[string]bool{}
	for _, s := range shards {
		holds[s] = true
	}
	named, again := map[string][]mention{}, map[string][]recheck{}
	for _, m := range hold.named {
		if holds[m.shard] {
			named[m.shard] = append(named[m.shard], m)
		}
	}
	for _, r := range hold.again {
		if holds[r.shard] {
			again[r.shard] = append(again[r.shard], r)
		}
	}
	var months []string
	for s := range named {
		months = append(months, s)
	}
	for s := range again {
		if _, ok := named[s]; !ok {
			months = append(months, s)
		}
	}
	sort.Strings(months)

	for _, shard := range months {
		want := map[string]bool{}
		for _, m := range named[shard] {
			want[m.id] = true
		}
		for _, r := range again[shard] {
			want[r.paper] = true
		}
		found := map[string]metadata.Record{}
		if err := p.Scan(shard, func(rec metadata.Record) error {
			if want[rec.ID] {
				found[rec.ID] = rec
			}
			return nil
		}); err != nil {
			return err
		}
		for _, m := range named[shard] {
			col.checked("R01")
			if _, ok := found[m.id]; !ok {
				col.add(m.find)
			}
		}
		for _, r := range again[shard] {
			rec, ok := found[r.paper]
			if !ok {
				// R01 has already said that this identifier is not in the
				// plane, and a resolution to a paper that does not exist is
				// that one fact and not two.
				continue
			}
			col.checked("R04")
			if why, agreed := matches(r.entry, rec); !agreed {
				r.find.What = fmt.Sprintf("resolves %s to %s, and the matcher does not agree: %s", r.entry.ID, r.paper, why)
				col.add(r.find)
			}
		}
	}
	return nil
}

// matches runs the resolver's own ladder over one entry and one record.
//
// The resolver is built to be shown a stream of records and asked which of them
// matched, so showing it a stream of one asks whether this entry and this
// record match, in the code that decides it everywhere else. A near miss comes
// back with the reason the threshold refused it, which is the sentence somebody
// reading the finding wants.
func matches(e refs.Entry, rec metadata.Record) (string, bool) {
	r := refs.NewResolver()
	r.Want("entry", e)
	r.Offer(rec)
	if _, ok := r.Matches()["entry"]; ok {
		return "", true
	}
	for _, m := range r.Misses() {
		if m.Key == "entry" {
			return m.Why, false
		}
	}
	return "nothing in the entry matches that record", false
}

// arxivRef is an arXiv id written into prose, anchorDef is an identifier a body
// hands out and anchorLink is a link into one.
//
// The id form takes both id styles and only reads one that says it is an arXiv
// id, in the citation form or as one of the site's own paths. A bare 2501.00001
// in a body is a number, and a rule that read every number that way would spend
// its time on page ranges and equation numbers.
//
// The id form is deliberately not anchored, because an identifier is read
// wherever a sentence puts it. CodeQL reads the arxiv.org in it as a host check
// that could be bypassed by a URL carrying that host somewhere in the middle,
// which would matter if anything here decided access from it. Nothing does. This
// scans prose for a mention, and what says the paper exists is R01 asking the
// metadata plane and not the shape of the text it was written in.
var (
	arxivRef   = regexp.MustCompile(`(?i)arxiv(?:\.org/(?:abs|pdf|html)/|[:\s]\s*)((?:\d{4}\.\d{4,5})|(?:[a-z-]+(?:\.[A-Za-z]{2})?/\d{7}))`)
	anchorDef  = regexp.MustCompile(`\{#([^\s}]+)`)
	anchorLink = regexp.MustCompile(`\]\(#([^)\s]+)\)`)
)

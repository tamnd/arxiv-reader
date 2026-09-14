package licence

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/harvest"
	"github.com/tamnd/arxiv-reader/internal/prose"
	"github.com/tamnd/arxiv-reader/metadata"
)

// Pile is the Common Pile's arXiv collection.
//
// The Common Pile is a corpus of openly licensed text, and its arXiv collection
// is the closest thing there is to a second answer to the question this project
// asks. Somebody else read arXiv's licence field, decided which papers they were
// allowed to republish in full, and published the result. That makes it worth
// comparing against, and it makes it worth saying plainly what the comparison
// is and is not.
//
// It is not a check against a truth. Their identifiers carry no version and
// neither does the licence any bulk surface serves, so both sides are reading
// one paper level licence and calling it the paper's. Two readings of the same
// upstream field agreeing tells us almost nothing, because a shared source can
// be wrong in the same way twice. Disagreement is the whole of the signal.
//
// There are two disagreements worth the walk. They call a paper open and this
// corpus does not, which means one of us is wrong about a paper somebody is
// already republishing in full. Or they hold a paper this corpus has no record
// of at all, which means the harvest is short. The reverse, a paper this corpus
// calls open that they did not take, is not a disagreement: they filtered for
// their own purposes and a paper missing from their collection is not a claim
// about its licence.
const Pile = "common-pile/arxiv_papers"

// PileRows is how many of their rows to ask for at a time.
//
// Ten rather than the hundred the metadata mirror uses, because their rows carry
// the full text of the paper. The columns parameter is accepted and ignored by
// the rows endpoint, so there is no way to ask for the identifier and the
// licence alone, and a row measured about fifty six kilobytes in September 2026,
// which makes a hundred of them five and a half megabytes over the wire to read
// two short strings from each.
const PileRows = 10

// Claim is one third party's reading of one paper's licence.
type Claim struct {
	// ID is the canonical identifier. Their ids carry no version, which is the
	// same limitation this corpus has on every bulk surface.
	ID string
	// Label is exactly what they wrote, kept so a disagreement can quote them
	// rather than quote this package's reading of them.
	Label string
	// Licence is Label mapped onto the arXiv licence set.
	Licence corpus.Licence
}

// Access is the access class their claim implies.
func (c Claim) Access() corpus.Access { return corpus.AccessFor(c.Licence) }

// PileLicence reads one of the Common Pile's licence labels.
//
// They write a human readable name, then a hyphen, then the deed, and the deed
// is the part that means something. One label breaks the pattern and carries no
// URL at all, which is the bare string "Public Domain".
//
// An unrecognised label is an error rather than an unknown licence. This is a
// disagreement report, and a label nobody has mapped would otherwise turn up as
// a disagreement with every paper that carries it, which would be this package
// failing to read rather than the two corpora disagreeing.
func PileLicence(label string) (corpus.Licence, error) {
	s := strings.TrimSpace(label)
	if s == "" {
		return "", fmt.Errorf("licence: the row states no licence")
	}
	if i := strings.LastIndex(s, " - http"); i >= 0 {
		l, err := corpus.LicenceFromURL(s[i+len(" - "):])
		if err != nil {
			return "", fmt.Errorf("licence: %q: %w", label, err)
		}
		return l, nil
	}
	// arXiv's public domain option predates CC0 and arXiv records it as a
	// creativecommons.org/licenses/publicdomain URL, which is already the same
	// value everywhere else in this package.
	if strings.EqualFold(s, "public domain") {
		return corpus.LicenceCC0, nil
	}
	return "", fmt.Errorf("licence: %q is not a label the Common Pile was seen to use", label)
}

// Reader walks the Common Pile through the Hugging Face datasets server.
type Reader struct {
	// Rows is the paced, backed off fetcher. Its Dataset defaults to Pile.
	Rows *harvest.Rows
	// Log, if set, is called once per page with the running counts.
	Log func(page, claims, total int)
}

// Reading is what one walk saw, whether or not anything was compared.
type Reading struct {
	// Dataset is what was read.
	Dataset string
	// Rows is how many of their rows were read.
	Rows int
	// Total is how many rows the dataset holds, which the server states on
	// every page.
	Total int
	// Truncated is rows the server shortened, which are skipped rather than
	// read. A shortened metadata cell would be a licence read off half a
	// string.
	Truncated int
	// Unreadable is the rows whose licence label this package could not map.
	Unreadable []Unreadable
}

// Unreadable is one row that could not be turned into a claim.
type Unreadable struct {
	ID    string
	Label string
	Why   string
}

// pileRow is one of their rows, with the columns this report reads.
//
// The text column is deliberately absent. It arrives whether or not it is asked
// for, and it is a whole paper, so naming it here would keep every paper in
// memory for the length of a page to no purpose.
type pileRow struct {
	ID       string `json:"id"`
	Metadata struct {
		Licence string `json:"license"`
		URL     string `json:"url"`
	} `json:"metadata"`
}

// pilePage is one page of the rows API, with their row shape.
type pilePage struct {
	Rows []struct {
		Index     int      `json:"row_idx"`
		Row       pileRow  `json:"row"`
		Truncated []string `json:"truncated_cells"`
	} `json:"rows"`
	Total int    `json:"num_rows_total"`
	Error string `json:"error"`
}

// Walk reads their rows in order and calls fn once per claim.
//
// The limit is required by the caller and not defaulted here, because the
// collection is 75,747 rows at ten rows a request and three seconds a request,
// which is about six hours. A walk that long should be asked for.
//
// The order is theirs, which is identifier order, so a limited walk reads the
// oldest papers they hold rather than a sample of them. The report says so.
func (r *Reader) Walk(ctx context.Context, offset, limit int, fn func(Claim) error) (Reading, error) {
	reading := Reading{Dataset: r.dataset()}
	if offset < 0 {
		return reading, fmt.Errorf("licence: offset %d is before the start of the collection", offset)
	}
	if limit < 1 {
		return reading, fmt.Errorf("licence: %d rows is not a walk, and the whole collection is about six hours at their pace, so a limit has to be asked for", limit)
	}

	page := 0
	claims := 0
	for reading.Rows < limit {
		length := PileRows
		if n := limit - reading.Rows; n < length {
			length = n
		}
		body, err := r.Rows.Page(ctx, offset+reading.Rows, length)
		if err != nil {
			return reading, err
		}
		var got pilePage
		if err := json.Unmarshal(body, &got); err != nil {
			return reading, fmt.Errorf("licence: the page at offset %d is not the JSON we expected: %w", offset+reading.Rows, err)
		}
		if got.Error != "" {
			return reading, fmt.Errorf("licence: hugging face answered: %s", strings.TrimSpace(got.Error))
		}
		reading.Total = got.Total
		// An empty page is the end of the collection. The server does not say
		// there is no more, it just stops having rows.
		if len(got.Rows) == 0 {
			return reading, nil
		}
		page++

		for _, row := range got.Rows {
			reading.Rows++
			if len(row.Truncated) > 0 {
				reading.Truncated++
				continue
			}
			id, err := axid.Parse(row.Row.ID)
			if err != nil {
				reading.Unreadable = append(reading.Unreadable, Unreadable{ID: row.Row.ID, Label: row.Row.Metadata.Licence, Why: "the identifier does not parse"})
				continue
			}
			l, err := PileLicence(row.Row.Metadata.Licence)
			if err != nil {
				reading.Unreadable = append(reading.Unreadable, Unreadable{ID: id.Canonical, Label: row.Row.Metadata.Licence, Why: "the licence label is not one we map"})
				continue
			}
			claims++
			if err := fn(Claim{ID: id.Canonical, Label: row.Row.Metadata.Licence, Licence: l}); err != nil {
				return reading, err
			}
		}
		if r.Log != nil {
			r.Log(page, claims, reading.Total)
		}
		// The end of the collection, without spending a request to be told the
		// next page is empty.
		if reading.Total > 0 && offset+reading.Rows >= reading.Total {
			return reading, nil
		}
	}
	return reading, nil
}

func (r *Reader) dataset() string {
	if r.Rows != nil && r.Rows.Dataset != "" {
		return r.Rows.Dataset
	}
	return Pile
}

// Crosscheck is the finished comparison.
type Crosscheck struct {
	// Generated is when the comparison was made.
	Generated time.Time
	// Reading is what the walk over their collection saw.
	Reading Reading
	// Claims is how many of their rows became a claim.
	Claims int
	// Held is how many of those claims name a paper the metadata plane has a
	// record for, so how many were compared at all.
	Held int
	// Agreed is how many of those read the same licence on both sides.
	Agreed int
	// Unresolved is claims the plane holds but carries no licence for, which is
	// nothing to compare rather than a disagreement.
	Unresolved []string
	// Disagreements is the papers the two sides read differently, widest
	// disagreement first.
	Disagreements []Disagreement
	// Absent is the papers they hold that the plane has no record of, which is
	// a gap in the harvest and not a licence question.
	Absent []string
}

// Disagreement is one paper the two corpora read differently.
type Disagreement struct {
	ID string
	// Ours is the latest version's licence in the metadata plane.
	Ours corpus.Licence
	// From is the surface ours came from.
	From metadata.Source
	// Theirs is their label, mapped.
	Theirs corpus.Licence
	// Label is their label as they wrote it.
	Label string
}

// Wider reports whether their reading permits republishing the English text and
// ours does not.
//
// This is the disagreement that costs something. They are republishing the full
// text of the paper, so if this corpus is right about the licence then they are
// republishing text they were not granted the right to. If they are right then
// this corpus is refusing a paper it could have used. Either way somebody should
// read the abs page, which is what ax licence resolve is for.
func (d Disagreement) Wider() bool {
	return corpus.AccessFor(d.Theirs).MayPublishText() && !corpus.AccessFor(d.Ours).MayPublishText()
}

// Compare reads a set of claims against the metadata plane.
//
// Only the months the claims fall in are read. The plane is three million
// records and the claims are at most seventy five thousand, so scanning the
// whole plane to find them would be reading the corpus to answer a question
// about a tenth of a percent of it.
func Compare(p metadata.Plane, claims []Claim) (Crosscheck, error) {
	out := Crosscheck{Claims: len(claims)}
	if len(claims) == 0 {
		return out, nil
	}

	want := make(map[string]Claim, len(claims))
	months := map[string]bool{}
	for _, c := range claims {
		id, err := axid.Parse(c.ID)
		if err != nil {
			return out, fmt.Errorf("licence: %w", err)
		}
		want[id.Canonical] = c
		months[corpus.Shard(id)] = true
	}

	have, err := p.Shards()
	if err != nil {
		return out, err
	}
	held := map[string]bool{}
	for _, shard := range have {
		held[shard] = true
	}

	for shard := range months {
		// A month the plane does not hold is not an error. It means the harvest
		// has not reached it, and every claim in it comes out as absent below.
		if !held[shard] {
			continue
		}
		err := p.Scan(shard, func(r metadata.Record) error {
			claim, ok := want[r.ID]
			if !ok {
				return nil
			}
			delete(want, r.ID)
			out.Held++
			ours, from := Latest(r)
			if ours == "" {
				out.Unresolved = append(out.Unresolved, r.ID)
				return nil
			}
			if ours == claim.Licence {
				out.Agreed++
				return nil
			}
			out.Disagreements = append(out.Disagreements, Disagreement{
				ID: r.ID, Ours: ours, From: from, Theirs: claim.Licence, Label: claim.Label,
			})
			return nil
		})
		if err != nil {
			return out, err
		}
	}

	for id := range want {
		out.Absent = append(out.Absent, id)
	}
	sort.Strings(out.Absent)
	sort.Strings(out.Unresolved)
	sort.Slice(out.Disagreements, func(i, j int) bool {
		a, b := out.Disagreements[i], out.Disagreements[j]
		if a.Wider() != b.Wider() {
			return a.Wider()
		}
		return a.ID < b.ID
	})
	return out, nil
}

// Wider is how many disagreements run in the direction that costs something.
func (c Crosscheck) Wider() int {
	n := 0
	for _, d := range c.Disagreements {
		if d.Wider() {
			n++
		}
	}
	return n
}

// Check walks the collection and compares what it read, in one call.
func Check(ctx context.Context, r *Reader, p metadata.Plane, offset, limit int) (Crosscheck, error) {
	var claims []Claim
	reading, err := r.Walk(ctx, offset, limit, func(c Claim) error {
		claims = append(claims, c)
		return nil
	})
	if err != nil {
		return Crosscheck{Reading: reading}, err
	}
	out, err := Compare(p, claims)
	if err != nil {
		return out, err
	}
	out.Reading = reading
	return out, nil
}

// listCap is how many identifiers a list in the report prints before it says
// how many more there are. A report nobody scrolls to the end of is a report
// that hid its own conclusion.
const listCap = 50

// Markdown is the report as it is committed to reports/crosscheck.md.
func (c Crosscheck) Markdown() string {
	var b strings.Builder
	b.WriteString("# The Common Pile crosscheck\n\n")
	b.WriteString(c.headline())
	fmt.Fprintf(&b, "Read on %s, over %s of %s in %s.\n\n",
		c.Generated.UTC().Format("2 January 2006"),
		prose.Count(c.Reading.Rows, "row"), prose.Thousands(c.Reading.Total), c.Reading.Dataset)

	b.WriteString("## What this compares\n\n")
	b.WriteString("The Common Pile is a corpus of openly licensed text and its arXiv collection is a second reading of the question this project asks.\n")
	b.WriteString("Somebody else read arXiv's licence field, decided which papers they were allowed to republish in full, and published the result.\n")
	b.WriteString("Their identifiers carry no version and neither does the licence any bulk surface serves, so both sides here are reading one paper level licence and calling it the paper's.\n")
	b.WriteString("Two readings of the same upstream field agreeing is weak evidence, because a shared source can be wrong the same way twice.\n")
	b.WriteString("The disagreements are the whole of the signal.\n")
	b.WriteString("A paper this corpus calls open that they did not take is not counted as a disagreement at all, because they filtered for their own purposes and a paper missing from their collection is not a claim about its licence.\n\n")
	b.WriteString("Their rows come back in their own order, which is identifier order, so a walk with a limit on it reads the oldest papers they hold rather than a sample of them.\n\n")

	b.WriteString("## The count\n\n")
	b.WriteString("| | Papers |\n")
	b.WriteString("| --- | ---: |\n")
	fmt.Fprintf(&b, "| Rows read | %s |\n", prose.Thousands(c.Reading.Rows))
	fmt.Fprintf(&b, "| Claims read off them | %s |\n", prose.Thousands(c.Claims))
	fmt.Fprintf(&b, "| Held in the metadata plane | %s |\n", prose.Thousands(c.Held))
	fmt.Fprintf(&b, "| Agreed | %s |\n", prose.Thousands(c.Agreed))
	fmt.Fprintf(&b, "| Disagreed | %s |\n", prose.Thousands(len(c.Disagreements)))
	fmt.Fprintf(&b, "| Disagreed in the direction that costs something | %s |\n", prose.Thousands(c.Wider()))
	fmt.Fprintf(&b, "| Held but carrying no licence | %s |\n", prose.Thousands(len(c.Unresolved)))
	fmt.Fprintf(&b, "| Absent from the metadata plane | %s |\n", prose.Thousands(len(c.Absent)))
	if c.Reading.Truncated > 0 {
		fmt.Fprintf(&b, "| Rows the server shortened, and so skipped | %s |\n", prose.Thousands(c.Reading.Truncated))
	}
	if len(c.Reading.Unreadable) > 0 {
		fmt.Fprintf(&b, "| Rows whose licence label we do not map | %s |\n", prose.Thousands(len(c.Reading.Unreadable)))
	}

	b.WriteString("\n## Disagreements\n\n")
	if len(c.Disagreements) == 0 {
		if c.Held == 0 {
			b.WriteString("Nothing to disagree about, because the metadata plane holds none of the papers read here.\n")
		} else {
			b.WriteString("None. Every paper read here that the metadata plane also holds carries the same licence on both sides.\n")
		}
	} else {
		b.WriteString("The first column is what this corpus reads and the second is what they read.\n")
		b.WriteString("A row marked yes is one where their reading permits republishing the English text and ours does not, so either they are republishing text nobody granted them, or this corpus is refusing a paper it could use.\n")
		b.WriteString("Either way the answer is on the abs page, and ax licence resolve is what reads it.\n\n")
		b.WriteString("| Paper | Ours | Ours from | Theirs | Costs something |\n")
		b.WriteString("| --- | --- | --- | --- | --- |\n")
		for i, d := range c.Disagreements {
			if i == listCap {
				fmt.Fprintf(&b, "\nAnd %s more.\n", prose.Count(len(c.Disagreements)-listCap, "disagreement"))
				break
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", d.ID, d.Ours, from(d.From), d.Theirs, yesno(d.Wider()))
		}
	}

	if len(c.Reading.Unreadable) > 0 {
		b.WriteString("\n## Labels we do not map\n\n")
		b.WriteString("These rows were read and skipped. A label this package cannot map would otherwise turn up as a disagreement with every paper carrying it, which would be a bug in the reading rather than a disagreement between the corpora.\n\n")
		b.WriteString("| Paper | Label | Why |\n")
		b.WriteString("| --- | --- | --- |\n")
		for i, u := range c.Reading.Unreadable {
			if i == listCap {
				fmt.Fprintf(&b, "\nAnd %s more.\n", prose.Count(len(c.Reading.Unreadable)-listCap, "row"))
				break
			}
			fmt.Fprintf(&b, "| %s | %s | %s |\n", u.ID, u.Label, u.Why)
		}
	}

	if len(c.Absent) > 0 {
		b.WriteString("\n## Absent from the metadata plane\n\n")
		b.WriteString("Papers they hold that this corpus has no record of.\n")
		b.WriteString("This is a gap in the harvest and not a licence question, and it is here because a harvest that is short is worth knowing about however it is found out.\n\n")
		for i, id := range c.Absent {
			if i == listCap {
				fmt.Fprintf(&b, "\nAnd %s more.\n", prose.Count(len(c.Absent)-listCap, "paper"))
				break
			}
			fmt.Fprintf(&b, "- %s\n", id)
		}
	}

	if len(c.Unresolved) > 0 {
		b.WriteString("\n## Held but carrying no licence\n\n")
		b.WriteString("Papers this corpus has a record of and no licence for, so there was nothing to compare.\n")
		b.WriteString("A record with no licence is not a paper resolved to unknown, it is a paper nobody has looked at.\n\n")
		for i, id := range c.Unresolved {
			if i == listCap {
				fmt.Fprintf(&b, "\nAnd %s more.\n", prose.Count(len(c.Unresolved)-listCap, "paper"))
				break
			}
			fmt.Fprintf(&b, "- %s\n", id)
		}
	}
	return b.String()
}

// Text is the short form, for a terminal.
func (c Crosscheck) Text() string {
	var b strings.Builder
	b.WriteString(c.headline())
	fmt.Fprintf(&b, "  %-28s %8s\n", "rows read", prose.Thousands(c.Reading.Rows))
	fmt.Fprintf(&b, "  %-28s %8s\n", "claims", prose.Thousands(c.Claims))
	fmt.Fprintf(&b, "  %-28s %8s\n", "held here", prose.Thousands(c.Held))
	fmt.Fprintf(&b, "  %-28s %8s\n", "agreed", prose.Thousands(c.Agreed))
	fmt.Fprintf(&b, "  %-28s %8s\n", "disagreed", prose.Thousands(len(c.Disagreements)))
	fmt.Fprintf(&b, "  %-28s %8s\n", "of those, costs something", prose.Thousands(c.Wider()))
	fmt.Fprintf(&b, "  %-28s %8s\n", "absent here", prose.Thousands(len(c.Absent)))
	return b.String()
}

// headline is the one sentence the report exists to produce.
func (c Crosscheck) headline() string {
	switch {
	case c.Claims == 0:
		return "Nothing compared, because the walk read no licence off their collection.\n"
	case c.Held == 0:
		return fmt.Sprintf("Nothing compared, because the metadata plane holds none of the %s read here.\n",
			prose.Count(c.Claims, "paper"))
	case len(c.Disagreements) == 0:
		return fmt.Sprintf("Of %s read and also held here, all agree, and %s are absent from the metadata plane.\n",
			prose.Count(c.Held, "paper"), prose.Thousands(len(c.Absent)))
	}
	return fmt.Sprintf("Of %s read and also held here, %s disagree, %s of them in the direction that costs something, and %s are absent from the metadata plane.\n",
		prose.Count(c.Held, "paper"), prose.Thousands(len(c.Disagreements)),
		prose.Thousands(c.Wider()), prose.Thousands(len(c.Absent)))
}

// from names the surface a licence was harvested from, for a record that has
// one and for a record that does not.
func from(s metadata.Source) string {
	if s == "" {
		return "not recorded"
	}
	return string(s)
}

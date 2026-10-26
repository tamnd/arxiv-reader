// Package gold holds the gold sets of AX11.2: the draw of a set, the gold
// record of one paper, and the score of the extractor against a set.
//
// A gold set is made by people. A model or the extractor can propose marks,
// and a person decides each mark. Only a record that names the person is
// scored.
package gold

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tamnd/arxiv-reader/metadata"
)

// Groups of set X (AX11.2). A group is one stratum of the draw.
const (
	Group1990s = "1991-2000"
	Group2000s = "2001-2010"
	Group2010s = "2011-2020"
	GroupNow   = "2021-2026"
	// GroupForm is the 300 papers drawn by the form of the heads. The form is
	// known only after the source is read, so the draw takes a pool from all
	// years and the census of each paper puts it in manual or custom.
	GroupForm = "form"
)

// Plan is how many papers each group of a set takes.
type Plan struct {
	Groups []GroupSize
	// MinPerCode is the least number of papers for each MSC code at two
	// digits that has at least MinCodeSize papers in the pool.
	MinPerCode  int
	MinCodeSize int
}

// GroupSize is one group and its size.
type GroupSize struct {
	Name string
	Size int
}

// PlanX is the plan of set X, version 1.
var PlanX = Plan{
	Groups: []GroupSize{
		{Group1990s, 100}, {Group2000s, 100}, {Group2010s, 100}, {GroupNow, 400}, {GroupForm, 300},
	},
	MinPerCode:  5,
	MinCodeSize: 1000,
}

// Candidate is one paper that the draw can take.
type Candidate struct {
	ID string
	// Year is the year that the id names.
	Year int
	// Code is the MSC code at two digits, or the primary category when the
	// paper gives no MSC code.
	Code string
}

// Pick is one paper of a drawn set.
type Pick struct {
	ID    string
	Group string
	Code  string
}

// DecadeOf is the group of a year, or "" for a year before 1991.
func DecadeOf(year int) string {
	switch {
	case year >= 1991 && year <= 2000:
		return Group1990s
	case year >= 2001 && year <= 2010:
		return Group2000s
	case year >= 2011 && year <= 2020:
		return Group2010s
	case year >= 2021:
		return GroupNow
	}
	return ""
}

var mscCode = regexp.MustCompile(`\b(\d{2})[A-Z\-xX]`)

// CodeOf is the MSC code at two digits of a record, from the first code in
// its msc_class. A paper with no MSC code gets its primary category.
func CodeOf(r metadata.Record) string {
	if m := mscCode.FindStringSubmatch(r.MSCClass); m != nil {
		return m[1]
	}
	return r.Primary()
}

// Draw draws a set from the pool. The draw is the same for the same pool and
// seed. Inside each group the codes are in proportion to the pool, and each
// code with at least MinCodeSize papers in the pool gets at least MinPerCode
// papers in the group, when the group has them.
func Draw(pool []Candidate, plan Plan, seed string) []Pick {
	order := append([]Candidate(nil), pool...)
	sort.Slice(order, func(i, j int) bool {
		a, b := rank(seed, order[i].ID), rank(seed, order[j].ID)
		if a != b {
			return a < b
		}
		return order[i].ID < order[j].ID
	})
	sizes := map[string]int{}
	for _, c := range pool {
		sizes[c.Code]++
	}
	taken := map[string]bool{}
	var out []Pick
	for _, g := range plan.Groups {
		var in []Candidate
		for _, c := range order {
			if taken[c.ID] {
				continue
			}
			if g.Name == GroupForm || DecadeOf(c.Year) == g.Name {
				in = append(in, c)
			}
		}
		for _, c := range drawGroup(in, g.Size, sizes, plan) {
			taken[c.ID] = true
			out = append(out, Pick{ID: c.ID, Group: g.Name, Code: c.Code})
		}
	}
	return out
}

// drawGroup takes n papers from the candidates of one group, which are in the
// order of the draw already.
func drawGroup(in []Candidate, n int, sizes map[string]int, plan Plan) []Candidate {
	if n >= len(in) {
		return in
	}
	byCode := map[string][]Candidate{}
	total := 0
	for _, c := range in {
		byCode[c.Code] = append(byCode[c.Code], c)
		total++
	}
	codes := make([]string, 0, len(byCode))
	for code := range byCode {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	quota := map[string]int{}
	left := n
	// The floor of each large code first.
	for _, code := range codes {
		if sizes[code] >= plan.MinCodeSize {
			q := min(plan.MinPerCode, len(byCode[code]), left)
			quota[code] = q
			left -= q
		}
	}
	// The rest in proportion, by the largest remainder.
	type share struct {
		code string
		rem  float64
	}
	var shares []share
	given := 0
	for _, code := range codes {
		exact := float64(left) * float64(len(byCode[code])) / float64(total)
		whole := int(exact)
		whole = min(whole, len(byCode[code])-quota[code])
		quota[code] += whole
		given += whole
		shares = append(shares, share{code, exact - float64(int(exact))})
	}
	sort.SliceStable(shares, func(i, j int) bool { return shares[i].rem > shares[j].rem })
	for left-given > 0 {
		moved := false
		for _, s := range shares {
			if left-given == 0 {
				break
			}
			if quota[s.code] < len(byCode[s.code]) {
				quota[s.code]++
				given++
				moved = true
			}
		}
		if !moved {
			break
		}
	}
	var out []Candidate
	for _, c := range in {
		if quota[c.Code] > 0 {
			quota[c.Code]--
			out = append(out, c)
		}
	}
	return out
}

func rank(seed, id string) uint64 {
	h := sha256.Sum256([]byte(seed + "\x00" + id))
	return binary.BigEndian.Uint64(h[:8])
}

// ListPath is the list of the papers of a set version, in tamnd/arxiv.
func ListPath(root, set, version string) string {
	return filepath.Join(root, "manifests", "gold", strings.ToLower(set)+"-"+version+".txt")
}

// WriteList writes a drawn set: a header line and then one line for each
// paper, with the id, the group and the code, separated by tabs.
func WriteList(path, header string, picks []Pick) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", header)
	for _, p := range picks {
		fmt.Fprintf(&b, "%s\t%s\t%s\n", p.ID, p.Group, p.Code)
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// ReadList reads a list that WriteList wrote.
func ReadList(path string) ([]Pick, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Pick
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			return nil, fmt.Errorf("%s:%d: want an id, a group and a code", path, n)
		}
		out = append(out, Pick{ID: f[0], Group: f[1], Code: f[2]})
	}
	return out, sc.Err()
}

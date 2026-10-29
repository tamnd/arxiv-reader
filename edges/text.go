package edges

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/tamnd/arxiv-reader/theorems"
)

// number is a printed number: 3.2, 3.2a or A.
const number = `(?:\d+(?:\.\d+)*[a-z]?|[A-Z](?:\.\d+)*)(?:'+|\b)`

// part is a part marker after a number, as the (ii) of Theorem 2.1(ii).
const part = `(?:\s*\((?:[ivx]{1,5}|[a-h]|\d{1,2})\))?`

var (
	oneNumber = regexp.MustCompile(`(` + number + `)(` + part + `)`)
	rangeSep  = regexp.MustCompile(`--|-|–|—|\\textendash|\bto\b`)
	// citeCmd is a citation: \cite[pre][post]{keys}, and the form
	// \cite{key}*{locator} of amsrefs.
	citeCmd = regexp.MustCompile(`\\(?:cite|citep|citet|citealp|citealt|Cite|cites|parencite|textcite|autocite|footcite|smartcite|citelist)\*?\s*(?:\[([^\]]*)\]\s*)?(?:\[([^\]]*)\]\s*)?\{([^{}]+)\}(?:\s*\*\s*\{([^{}]*)\})?`)
	// beforeOpen is an open bracket of a citation just before a reference, as
	// in [12, Theorem 4.1].
	beforeOpen = regexp.MustCompile(`\[[^\]]{0,30}$`)
	// ofCite is a reference to the result of another paper: Theorem 4.1 of
	// \cite{key}, Theorem 4.1 in [12], Corollary 1.1 of Bucher-Connel
	// \cite{key}, or Lemma 1 in the supplementary material.
	ofCite = regexp.MustCompile(`^(?:\s*(?:,|and|or)\s*(?:~|\s)*[A-Z][a-z]+\.?(?:~|\s)*` + number + part + `)*(?:\s*(?:of|in)\s+(?:Chapter|Section|Part|\\S)(?:~|\s)*[0-9A-Z.]+)?\s*(?:of|in|from)\s*(?:~|\s)*(?:(?:(?:[A-Z][A-Za-z'-]+|de|van|von|der|den|la|le|du)(?:,|\s+and\s+|\s+)?){1,4}(?:~|\s|\()*\\cite|\\cite|\[|the\s+paper|the\s+supplement|\\citet|\\citep|ref\.)`)
	// citeBefore is a citation just before a reference to its result:
	// \cite{key}, Lemma 4, or [22] Theorem C.
	citeBefore = regexp.MustCompile(`(?:\\cite[a-z]*\*?(?:\s*\[[^\]]*\])*\s*\{[^{}]*\}|\[\d{1,3}(?:\s*,\s*\d{1,3})*\])\s*,?(?:~|\s)*$`)
	// commaAfter is the locator after a citation: \cite{key}, Theorem 6.
	commaAfter = regexp.MustCompile(`^\s*,\s*(?:~|\s)*([A-Z][a-z]+\.?(?:~|\s)*` + number + part + `)`)
	// locatorTitle is a locator that names a result: Theorem 4.1, Lemma 2.
	locatorTitle = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z ]{1,30}?)\.?(?:~|\s)*(` + number + `)`)
	// titleBefore is a result named just before a citation: Theorem 4.1 of.
	titleBefore = regexp.MustCompile(`([A-Z][a-z]+\.?(?:~|\s)*` + number + part + `)\s*(?:of|in|from)\s*(?:~|\s)*$`)
)

// defaultTitles are the titles that the text method reads in each paper, in
// addition to the titles of its own items.
var defaultTitles = []string{"Theorem", "Lemma", "Proposition", "Corollary", "Definition", "Remark", "Example", "Conjecture", "Claim", "Assumption", "Hypothesis", "Fact", "Observation", "Problem", "Question", "Notation", "Construction", "Exercise", "Condition", "Property"}

// titles is the alternation of the titles that a text reference can use.
func (r *resolver) titles() []string {
	seen := map[string]bool{}
	var out []string
	addT := func(t string) {
		t = strings.TrimSpace(t)
		n := strings.ToLower(t)
		if t == "" || seen[n] || len(t) > 40 || strings.ContainsAny(t, `\{}$[]()0123456789`) {
			return
		}
		seen[n] = true
		out = append(out, regexp.QuoteMeta(t))
	}
	for _, it := range r.in.Items {
		if !it.Proof {
			addT(it.Title)
		}
	}
	for _, t := range defaultTitles {
		addT(t)
	}
	sort := func(xs []string) {
		for i := 1; i < len(xs); i++ {
			for j := i; j > 0 && len(xs[j]) > len(xs[j-1]); j-- {
				xs[j], xs[j-1] = xs[j-1], xs[j]
			}
		}
	}
	sort(out)
	return out
}

// texts is method 2: a title and a number in the text, resolved by the
// printed numbers of the items.
func (r *resolver) texts() {
	alts := r.titles()
	if len(alts) == 0 {
		return
	}
	one := number + part
	sep := `(?:\s*,\s*(?:and\s+|or\s+)?|\s+and\s+|\s+or\s+|\s*(?:--|-|–|—|\\textendash)\s*|\s+to\s+)`
	re := regexp.MustCompile(`(?i:\b(` + strings.Join(alts, "|") + `)(?:s|es)?\b)\.?(?:~|\s|\\,|\\ )*(` + one + `(?:` + sep + one + `)*)`)
	d := r.in.Doc
	body := d.Text[d.Body:d.End]
	cites := citeCmd.FindAllIndex(body, -1)
	inCite := func(i int) bool {
		for _, s := range cites {
			if i >= s[0] && i < s[1] {
				return true
			}
		}
		return false
	}
	for _, m := range re.FindAllSubmatchIndex(body, -1) {
		off := d.Body + m[0]
		src := r.from(off)
		if src == nil {
			continue
		}
		// A reference to the result of another paper is a citation.
		if inCite(m[0]) || beforeOpen.Match(body[max(0, m[0]-40):m[0]]) || citeBefore.Match(body[max(0, m[0]-80):m[0]]) || ofCite.Match(body[m[1]:min(len(body), m[1]+80)]) {
			continue
		}
		title := string(body[m[2]:m[3]])
		list := string(body[m[4]:m[5]])
		for _, ref := range numbers(list) {
			// The head of an item is not a reference to it.
			if off < src.Inner && ref.n == src.Number {
				continue
			}
			t, flags, conf, ok := r.byNumber(src, title, ref.n, off)
			if !ok {
				r.res.Dangling++
				continue
			}
			t.part = ref.part
			r.link(src, off, d.Body+m[1], t, Text, conf, flags)
		}
	}
}

type numbered struct{ n, part string }

// numbers reads a list of numbers, as "3.1, 3.4 and 3.5", and gives each
// number of a range, as "3.1--3.3".
func numbers(list string) []numbered {
	var out []numbered
	ms := oneNumber.FindAllStringSubmatchIndex(list, -1)
	for i, m := range ms {
		n := list[m[2]:m[3]]
		p := strings.TrimSpace(list[m[4]:m[5]])
		if i > 0 && rangeSep.MatchString(list[ms[i-1][1]:m[0]]) {
			out = append(out, between(out[len(out)-1].n, n)...)
		}
		out = append(out, numbered{n, p})
	}
	return out
}

// between is the numbers strictly between a and b, when the two have the
// same prefix and the last parts are integers: 3.1 and 3.4 give 3.2 and 3.3.
func between(a, b string) []numbered {
	ai, bi := strings.LastIndex(a, "."), strings.LastIndex(b, ".")
	if a[:ai+1] != b[:bi+1] {
		return nil
	}
	x, err1 := strconv.Atoi(a[ai+1:])
	y, err2 := strconv.Atoi(b[bi+1:])
	if err1 != nil || err2 != nil || y <= x || y-x > 20 {
		return nil
	}
	var out []numbered
	for k := x + 1; k < y; k++ {
		out = append(out, numbered{n: a[:ai+1] + strconv.Itoa(k)})
	}
	return out
}

// byNumber finds the item that has the number. The title must agree with the
// kind of the item. When it does not, and an item with another title has the
// number, the edge is medium with the flag title-mismatch (AX6.2). Of two
// items with the number, one in an item that holds the reference comes first,
// as "By Claim 1" in a proof names the claim of that proof and not the claim
// 1 of another proof, and then the nearer one.
func (r *resolver) byNumber(src *Item, title, n string, off int) (target, []string, string, bool) {
	kind := theorems.Unknown
	if r.in.Titles != nil {
		kind = r.in.Titles.Kind(title)
	}
	var same, other *Item
	for i := range r.in.Items {
		it := &r.in.Items[i]
		if it.Proof || it.Number != n {
			continue
		}
		if theorems.Normalize(it.Title) == theorems.Normalize(title) || (kind != theorems.Unknown && it.Kind == kind) {
			if same == nil || r.before(src, it, same, off) {
				same = it
			}
		} else if other == nil || r.before(src, it, other, off) {
			other = it
		}
	}
	switch {
	case same != nil:
		return target{local: same.Local}, nil, High, true
	case other != nil:
		return target{local: other.Local}, []string{FlagTitleMismatch}, Medium, true
	}
	return target{}, nil, "", false
}

// before tells if a reference from src at the offset names a before b: a is
// in an item that holds src and b is not, or the two are alike in that and a
// is nearer.
func (r *resolver) before(src, a, b *Item, off int) bool {
	if sa, sb := r.scoped(src, a), r.scoped(src, b); sa != sb {
		return sa
	}
	return nearer(a, b, off)
}

// scoped tells if the item is in an item that holds src.
func (r *resolver) scoped(src, it *Item) bool {
	p := r.byID[it.Parent]
	return it.Parent != "" && p != nil && r.inside(src, p)
}

// nearer tells if a is nearer to the offset than b.
func nearer(a, b *Item, off int) bool {
	da, db := a.Off-off, b.Off-off
	if da < 0 {
		da = -da
	}
	if db < 0 {
		db = -db
	}
	return da < db
}

// cites is method 4: a citation, with or without a locator. The key is
// resolved by AX5.7 later, so the edge goes to the key.
func (r *resolver) cites() {
	d := r.in.Doc
	body := d.Text[d.Body:d.End]
	for _, m := range citeCmd.FindAllSubmatchIndex(body, -1) {
		off := d.Body + m[0]
		src := r.from(off)
		if src == nil {
			continue
		}
		loc := ""
		switch {
		case m[8] >= 0:
			loc = string(body[m[8]:m[9]])
		case m[4] >= 0:
			loc = string(body[m[4]:m[5]])
		case m[2] >= 0:
			loc = string(body[m[2]:m[3]])
		}
		if loc == "" {
			if c := commaAfter.FindSubmatch(body[m[1]:min(len(body), m[1]+60)]); c != nil {
				loc = string(c[1])
			} else if c := titleBefore.FindSubmatch(body[max(0, m[0]-60):m[0]]); c != nil {
				loc = string(c[1])
			}
		}
		loc = strings.Join(strings.Fields(strings.ReplaceAll(loc, "~", " ")), " ")
		var keys []string
		for _, k := range strings.Split(string(body[m[6]:m[7]]), ",") {
			if k = strings.TrimSpace(k); k != "" {
				keys = append(keys, k)
			}
		}
		file, line := r.where(off)
		for _, k := range keys {
			e := Edge{From: r.id(src.Local), To: r.id("bib:" + k), Method: Cite, Confidence: Medium, Evidence: Evidence{File: file, Line: line}}
			if len(keys) == 1 && locatorTitle.MatchString(loc) {
				// A locator that names a result is a cited result.
				e.Part, e.Predicate, e.Confidence, e.Evidence.Cue = loc, CitesResult, High, "locator"
			} else {
				e.Predicate, e.Evidence.Cue = r.predicate(src, off, d.Body+m[1])
				if e.Predicate == RefersTo {
					e.Predicate = Cites
				}
				if len(keys) == 1 {
					e.Part = loc
				}
			}
			r.add(e)
		}
	}
}

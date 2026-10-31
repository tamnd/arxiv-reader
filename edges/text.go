package edges

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/tamnd/arxiv-reader/theorems"
)

// number is a printed number: 3.2, 3.2a or A.
const number = `(?:\d+(?:\.\d+)*[a-z]?|[A-Z](?:\.\d+)*)\b`

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
	// \cite{key}, Theorem 4.1 in [12], Theorem 4.1 of Chapter 2 of \cite{key},
	// or Lemma 2, part 3 in \cite{key}.
	ofCite = regexp.MustCompile(`^\s*(?:,?\s*(?:(?:of|in)\s+)?(?:Chapter|Section|Part|part|Appendix|Lecture)\s*~?[\w.]+,?\s*)?(?:of|in|from)\s*(?:~|\s)*(?:\\cite|\[|the\s+paper|\\citet|\\citep|ref\.|the\s+supplement)`)
	// listTail is the rest of a list of results before a citation, as in
	// "Assumption 6 (a), Assumption 6 (b) and Assumption 7 in \cite{key}".
	listTail = regexp.MustCompile(`^(?:(?:\s*,\s*(?:and\s+)?|\s+and\s+)(?:the\s+)?[A-Z][a-z]+s?\.?(?:~|\s)*` + number + part + `)+`)
	// ofAuthor is a result of a work named by its author: Proposition 2.10 of
	// Smith (1990), Theorem 3 of Smith [4], or Lemma 2 of Smith et al.
	ofAuthor = regexp.MustCompile(`^\s*(?:of|in)\s+([A-Z][a-z]+)(?:\s+and\s+[A-Z][a-z]+)?(?:\s*(?:\(|\[|\\cite)|\s+et\s+al|'s)`)
	// citeComma is a citation with the locator after it: (\cite{key}, Theorem 3).
	citeComma = regexp.MustCompile(`\\(?:cite|citep|citet|citealp|Cite)\*?\s*(?:\[[^\]]*\]\s*)*\{[^{}]+\}\s*,\s*(?:~|\s)*$`)
	// ofRef is a result numbered in another item: Claim 3 of \ref{t}, or
	// Step 2 in the proof of Theorem~\ref{t}. The ref method reads the item.
	ofRef = regexp.MustCompile(`^\s*(?:of|in)\s+(?:the\s+proof\s+of\s+)?(?:[A-Za-z]+\.?\s*)?(?:~|\s)*\\(?:ref|cref|Cref|autoref)\b`)
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
		if inCite(m[0]) || beforeOpen.Match(body[max(0, m[0]-40):m[0]]) || citeComma.Match(body[max(0, m[0]-120):m[0]]) || r.cited(body[m[1]:min(len(body), m[1]+200)], alts) {
			continue
		}
		title := string(body[m[2]:m[3]])
		list := string(body[m[4]:m[5]])
		for _, ref := range numbers(list) {
			// The head of an item is not a reference to it.
			if off < src.Inner && ref.n == src.Number {
				continue
			}
			t, flags, conf, ok := r.byNumber(title, ref.n, off)
			if !ok {
				r.res.Dangling++
				continue
			}
			t.part = ref.part
			r.link(src, off, d.Body+m[1], t, Text, conf, flags)
		}
	}
}

// cited tells if the text after a reference makes it a result of another
// work or of another item: "of \cite{key}", "of \ref{t}", "of Smith (1990)",
// also after the rest of a list of results.
func (r *resolver) cited(after []byte, titles []string) bool {
	if l := listTail.Find(after); l != nil {
		after = after[len(l):]
	}
	if ofCite.Match(after) || ofRef.Match(after) {
		return true
	}
	m := ofAuthor.FindSubmatch(after)
	if m == nil {
		return false
	}
	// "of Theorem 2 (b)" or "of Section 3 (see ...)" is not an author.
	w := strings.ToLower(string(m[1]))
	switch w {
	case "section", "chapter", "part", "appendix", "step", "case", "the", "this", "our":
		return false
	}
	for _, t := range titles {
		if strings.EqualFold(w, t) {
			return false
		}
	}
	return true
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

// byNumber finds the item that has the number, out of the proofs. The title
// must agree with the kind of the item. A title that does not agree is not
// read: an item of another title with the number was the target in none of
// the five such references of gold set E (AX11.3).
func (r *resolver) byNumber(title, n string, off int) (target, []string, string, bool) {
	kind := theorems.Unknown
	if r.in.Titles != nil {
		kind = r.in.Titles.Kind(title)
	}
	var same *Item
	for i := range r.in.Items {
		it := &r.in.Items[i]
		// A claim or a step inside a proof is numbered in its proof, so
		// its number written in another proof is not of it.
		if it.Proof || it.InProof || it.Number != n {
			continue
		}
		// "Case 2" and "Step 3" name a part of the argument that holds
		// them, not a result. Seen in the gold pilot (set E).
		if it.Kind == "other/case" || (it.Kind == "statement/claim" && r.claimTwice(it)) {
			continue
		}
		if theorems.Normalize(it.Title) == theorems.Normalize(title) || (kind != theorems.Unknown && it.Kind == kind) {
			if same == nil || nearer(it, same, off) {
				same = it
			}
		}
	}
	if same != nil {
		return target{local: same.Local}, nil, High, true
	}
	return target{}, nil, "", false
}

// claimTwice tells if another claim has the number of the claim it. Claims
// are numbered afresh in each argument, so "Claim 1" written once more is not
// known to be it. Seen in the gold pilot (set E).
func (r *resolver) claimTwice(it *Item) bool {
	for i := range r.in.Items {
		o := &r.in.Items[i]
		if o != it && o.Kind == it.Kind && o.Number == it.Number {
			return true
		}
	}
	return false
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

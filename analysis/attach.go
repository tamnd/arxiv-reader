package analysis

import (
	"regexp"
	"strings"

	"github.com/tamnd/arxiv-reader/tex"
	"github.com/tamnd/arxiv-reader/theorems"
)

var (
	headNumber = regexp.MustCompile(`(?i)^(?:the\s+)?([A-Za-z][A-Za-z -]{0,30}?)\s*~?\s*(\d+(?:\.\d+)*[a-z]?|(?-i:[A-Z])(?:\.\d+)*)\b`)
	mainName   = regexp.MustCompile(`(?i)^(?:the\s+)?main\s+(theorem|result|lemma|proposition)s?$`)
	otherProof = regexp.MustCompile(`(?i)\b(second|alternative|another|third|new)\s+proof\b`)
	citeNear   = regexp.MustCompile(`\\cite[a-zA-Z]*\*?\s*(?:\[[^\]]*\]\s*){0,2}\{[^{}]*\}`)
	sentence   = regexp.MustCompile(`[.!?](?:\s|$)`)
)

// attach attaches each proof to what it proves, by the first rule of AX3.6
// that applies.
func (b *Builder) attach(d *tex.Document, items []*item) {
	byLabel := map[string]*item{}
	partLabel := map[string][2]string{}
	for _, it := range items {
		for _, l := range it.rec.Labels {
			if _, ok := byLabel[l]; !ok {
				byLabel[l] = it
			}
		}
		for _, p := range it.rec.Parts {
			if p.Label != "" {
				partLabel[p.Label] = [2]string{it.rec.Local, p.Marker}
			}
		}
	}
	for i, it := range items {
		if !it.proof {
			continue
		}
		r := &it.rec
		target := it.opt
		// Rule 1: a reference to a label in the head.
		if m := refIn.FindStringSubmatch(target); m != nil {
			key := strings.TrimSpace(strings.Split(m[1], ",")[0])
			if t, ok := byLabel[key]; ok && t != it {
				r.Proves, r.Attach, r.Confidence = t.rec.Local, 1, "certain"
				if p, ok := partLabel[key]; ok {
					r.ProvesPart = p[1]
				}
				continue
			}
			if p, ok := partLabel[key]; ok {
				r.Proves, r.ProvesPart, r.Attach, r.Confidence = p[0], p[1], 1, "certain"
				continue
			}
		}
		// Rule 2: a number or a name in the head.
		if t := b.byNumber(items, target); t != nil && t != it {
			r.Proves, r.Attach, r.Confidence = t.rec.Local, 2, "high"
			continue
		}
		prev := previous(items, i)
		// Rule 5: a part of the statement before it.
		if m := partOf.FindStringSubmatch(stripProofOf(target)); m != nil && prev != nil && isStatement(prev) {
			r.Proves, r.ProvesPart, r.Attach, r.Confidence = prev.rec.Local, m[1], 5, "high"
			continue
		}
		near, floats := false, false
		if prev != nil {
			near, floats = adjacent(d, prev, it)
		}
		if near {
			switch {
			case isStatement(prev):
				// Rule 3: right after a statement.
				r.Proves, r.Attach, r.Confidence = prev.rec.Local, 3, "certain"
				if floats {
					r.Confidence = "high"
				}
				continue
			case !prev.proof:
				// Rule 4: right after a definition, a remark or an example.
				r.Proves, r.Attach, r.Confidence = prev.rec.Local, 4, "high"
				r.Flags = append(r.Flags, "proves-non-statement")
				continue
			case prev.proof && otherProof.MatchString(target) && prev.rec.Proves != "":
				// A second proof of the same statement.
				r.Proves, r.Attach, r.Confidence = prev.rec.Proves, 3, "high"
				continue
			}
		}
		// Rule 6: an orphan. AX7.6 proposes a target.
		r.Flags = append(r.Flags, "orphan")
	}
}

func stripProofOf(s string) string {
	s = strings.TrimSpace(s)
	low := strings.ToLower(s)
	for _, p := range []string{"proof of", "proof"} {
		if strings.HasPrefix(low, p) {
			return s[len(p):]
		}
	}
	return s
}

// byNumber resolves "Lemma 3.2", "Theorem A" or "the Main Theorem" in a head
// to an item with that title and number.
func (b *Builder) byNumber(items []*item, head string) *item {
	h := strings.TrimSpace(stripProofOf(head))
	h = strings.TrimPrefix(strings.TrimPrefix(h, "of "), "for ")
	if h == "" {
		return nil
	}
	if m := headNumber.FindStringSubmatch(h); m != nil {
		title, number := strings.TrimSpace(m[1]), m[2]
		kind := b.Titles.Kind(title)
		var any *item
		for _, it := range items {
			if it.proof || it.rec.Number != number {
				continue
			}
			if theorems.Normalize(it.rec.Title) == theorems.Normalize(title) || it.rec.Item == kind {
				return it
			}
			if any == nil {
				any = it
			}
		}
		return any
	}
	if mainName.MatchString(h) {
		var found *item
		for _, it := range items {
			if it.proof {
				continue
			}
			n := strings.ToLower(it.opt)
			if strings.Contains(n, "main") || strings.Contains(strings.ToLower(it.rec.Env), "main") {
				if found != nil {
					return nil
				}
				found = it
			}
		}
		return found
	}
	return nil
}

// previous is the item before items[i] at the same depth, or nil.
func previous(items []*item, i int) *item {
	it := items[i]
	for j := i - 1; j >= 0; j-- {
		p := items[j]
		if p.rec.Origin == "restated" && p.end == p.inner {
			// A restatement is a statement that a proof can follow.
			if p.parent == it.parent {
				return p
			}
			continue
		}
		if p.parent == it.parent {
			return p
		}
	}
	return nil
}

// floatEnv is a float environment. TeX moves a float, so a float between a
// statement and a proof does not separate them.
var floatEnv = regexp.MustCompile(`(?s)\\begin\s*\{(figure|table|wrapfigure|wraptable|algorithm|marginfigure|SCfigure)(\*?)\}.*?\\end\s*\{(?:figure|table|wrapfigure|wraptable|algorithm|marginfigure|SCfigure)\*?\}`)

// adjacent tells if only spacing, labels and end marks are between two items.
// floats is true when a float is between them too.
func adjacent(d *tex.Document, a, b *item) (ok, floats bool) {
	if a.end > b.off {
		return false, false
	}
	gap := d.Text[a.end:b.off]
	if gapOK.Match(gap) {
		return true, false
	}
	if len(gap) > 20000 || !floatEnv.Match(gap) {
		return false, false
	}
	return gapOK.Match(floatEnv.ReplaceAll(gap, nil)), true
}

func isStatement(it *item) bool {
	return strings.HasPrefix(it.rec.Item, "statement") || it.rec.Kind == "statement"
}

// status sets the proof_status of each statement (AX3.6).
func (b *Builder) status(d *tex.Document, items []*item) {
	proved := map[string]bool{}
	byLocal := map[string]*item{}
	first := map[string]*item{}
	for _, it := range items {
		byLocal[it.rec.Local] = it
		if it.restate != "" && it.rec.Origin != "restated" {
			first[it.restate] = it
		}
	}
	for _, it := range items {
		if !it.proof || it.rec.Proves == "" {
			continue
		}
		proved[it.rec.Proves] = true
		// A proof of a restatement proves the first print of the item too.
		if t := byLocal[it.rec.Proves]; t != nil && t.rec.Origin == "restated" {
			if f := first[t.restate]; f != nil {
				proved[f.rec.Local] = true
			}
		}
	}
	// The sections whose title is a proof head, as "Proof of Theorem 1.2".
	elsewhere := map[string]bool{}
	byLabel := map[string]*item{}
	for _, it := range items {
		for _, l := range it.rec.Labels {
			byLabel[l] = it
		}
	}
	for _, h := range theorems.ProofSections(d, b.Titles) {
		if m := refIn.FindStringSubmatch(h.Target); m != nil {
			for k := range strings.SplitSeq(m[1], ",") {
				if t, ok := byLabel[strings.TrimSpace(k)]; ok {
					elsewhere[t.rec.Local] = true
				}
			}
			continue
		}
		if t := b.byNumber(items, h.Target); t != nil {
			elsewhere[t.rec.Local] = true
		}
	}
	for _, it := range items {
		if !isStatement(it) && it.rec.Kind != "problem" {
			continue
		}
		r := &it.rec
		after := textAfter(d, it.end, 2)
		switch {
		case proved[r.Local]:
			r.ProofStatus = "here"
		case r.Origin == "restated":
			r.ProofStatus = "restated"
		case elsewhere[r.Local]:
			r.ProofStatus = "elsewhere"
		case len(r.CitesInHead) > 0 || citeNear.MatchString(after) && startsWithCite(after):
			r.ProofStatus = "cited"
		default:
			if _, ok := b.Phrases.Find("omitted", after); ok {
				r.ProofStatus = "omitted"
			} else if _, ok := b.Phrases.Find("elsewhere", after); ok {
				r.ProofStatus = "elsewhere"
			} else {
				r.ProofStatus = "none"
			}
		}
		if r.Kind == "problem" {
			// A conjecture or a problem has no proof by its nature. The value
			// says if the paper proves it here.
			if r.ProofStatus != "here" {
				r.ProofStatus = "none"
			}
		}
	}
}

// startsWithCite tells if the first sentence after an item cites a source for
// it, as "See [12, Thm 4]" or "This is due to \cite{S}".
func startsWithCite(after string) bool {
	first := after
	if m := sentence.FindStringIndex(after); m != nil {
		first = after[:m[1]]
	}
	low := strings.ToLower(first)
	if !citeNear.MatchString(first) {
		return false
	}
	for _, w := range []string{"see", "due to", "proved in", "proven in", "proof can be found", "for a proof", "for the proof", "cf.", "is from", "follows from"} {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

// textAfter is the text of the next n sentences after an offset.
func textAfter(d *tex.Document, off, n int) string {
	end := min(off+600, d.End)
	if off >= end {
		return ""
	}
	s := string(d.Text[off:end])
	at := 0
	for i := 0; i < n; i++ {
		m := sentence.FindStringIndex(s[at:])
		if m == nil {
			return s
		}
		at += m[1]
	}
	return s[:at]
}

// origin sets the origin of each item (AX3.7).
func (b *Builder) origin(items []*item) {
	byHash := map[string]*item{}
	byRestate := map[string]*item{}
	for _, it := range items {
		if it.restate != "" && it.rec.Origin != "restated" {
			byRestate[it.restate] = it
		}
	}
	for _, it := range items {
		r := &it.rec
		if it.proof {
			continue
		}
		switch {
		case r.Origin == "restated":
			if first, ok := byRestate[it.restate]; ok {
				r.Restates = first.rec.Local
			}
		case b.restatedHead(it.opt):
			r.Origin = "restated"
		case it.normLen >= 40 && byHash[r.Hash] != nil:
			r.Origin = "restated"
			r.Restates = byHash[r.Hash].rec.Local
		case len(r.CitesInHead) > 0 || r.ProofStatus == "cited":
			r.Origin = "cited"
		default:
			if _, ok := b.Phrases.Find("folklore", it.opt); ok {
				r.Origin = "folklore"
			} else {
				r.Origin = "own"
			}
		}
		if r.Origin == "restated" && r.ProofStatus != "" && r.ProofStatus != "here" {
			r.ProofStatus = "restated"
		}
		if it.normLen >= 40 && byHash[r.Hash] == nil {
			byHash[r.Hash] = it
		}
	}
}

func (b *Builder) restatedHead(opt string) bool {
	if opt == "" {
		return false
	}
	_, ok := b.Phrases.Find("restated", opt)
	return ok || regexp.MustCompile(`=\s*(?:Theorem|Lemma|Proposition|Corollary)\s`).MatchString(opt)
}

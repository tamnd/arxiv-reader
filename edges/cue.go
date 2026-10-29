package edges

import (
	"regexp"
	"strings"
)

// The cues of AX6.3. The verb before a reference decides the predicate. Each
// cue is a value of evidence.cue, from this closed list, with none for a
// reference with no cue.
var (
	usesCues    = []string{"by", "using", "use", "uses", "used", "from", "applying", "apply", "applied", "follows from", "in view of", "combining", "combined with", "thanks to", "due to", "according to", "invoking", "via", "because of", "since", "consequence of", "together with", "with the help of", "based on", "relies on", "under the assumptions of", "assumptions of", "hypotheses of", "conditions of"}
	refersCues  = []string{"see", "cf. also", "cf also", "compare", "as in", "recall", "similar to", "analogous to", "analogously to", "as we saw in", "is proved in", "will be proved in", "we prove in", "we show in", "proves", "complete the proof of", "completes the proof of", "completing the proof of", "finish the proof of", "finishes the proof of", "finishing the proof of", "finish to prove", "for proving"}
	generalCues = []string{"generalizes", "generalises", "generalization of", "generalisation of", "extends", "strengthens"}
	specialCues = []string{"special case of", "particular case of", "specializes", "specialises"}
	// afterUses is a verb after a reference that makes it a reason: Lemma 3
	// implies.
	afterUses = regexp.MustCompile(`^[^.;:]{0,12}?\b(implies|gives|shows|yields|ensures|guarantees)\b`)
	// afterForward is a forward reference: Theorem 5 below.
	afterForward = regexp.MustCompile(`^[^.;:]{0,12}?\b(below|later|in the sequel)\b`)
	// afterProved says that the reference is what the proof proves:
	// "\ref{a} and \ref{b} are proven".
	afterProved = regexp.MustCompile(`^(?:\s*(?:,|and)\s*(?:[a-z]+\.?(?:~|\s)*)?\\ref\s*\{[^{}]*\})*\s*(?:is|are)\s+(?:now\s+)?(?:proven|proved|established|complete)\b`)
	// givenBy is a passive "given by" just before a reference, as in "the
	// rate is given by the following theorem".
	givenBy = regexp.MustCompile(`(?i)\b(?:is|are)\s+(?:then\s+)?given\s+by\s+$`)
	cueRe   = buildCues()
	justBy  = regexp.MustCompile(`\b(?:by|from|applying|using)\s+(?:the\s+)?$`)
	// sentenceCut is a candidate end of a sentence; cutAt keeps the ones
	// that do not follow a short word such as "cf." or "Prop.".
	sentenceCut = regexp.MustCompile(`([a-z.]*)[.;:][ \n]`)
	abbrev      = map[string]bool{"cf": true, "prop": true, "props": true, "thm": true, "thms": true, "lem": true, "cor": true, "eq": true, "eqs": true, "sec": true, "secs": true, "def": true, "defn": true, "resp": true, "i.e": true, "e.g": true, "ex": true, "rem": true, "fig": true, "ch": true, "chap": true, "no": true, "nos": true, "vol": true, "p": true, "pp": true, "ref": true, "refs": true, "viz": true, "conj": true, "obs": true, "th": true, "pr": true, "cl": true, "al": true}
)

// CueNone is the cue of a reference with no cue.
const CueNone = "none"

func buildCues() *regexp.Regexp {
	var all []string
	for _, l := range [][]string{usesCues, refersCues, generalCues, specialCues} {
		for _, c := range l {
			all = append(all, regexp.QuoteMeta(c))
		}
	}
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && len(all[j]) > len(all[j-1]); j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}
	return regexp.MustCompile(`\b(` + strings.Join(all, "|") + `)(?:\s|~|$)`)
}

func in(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// predicate decides the predicate of a reference from off to end, from the
// nearest cue before it in the same sentence, or a verb just after it. With
// no cue, a reference in a proof is uses, and any other reference is
// refers-to.
func (r *resolver) predicate(src *Item, off, end int) (string, string) {
	text := r.in.Doc.Text
	after := strings.ToLower(string(text[end:min(len(text), end+40)]))
	from := max(src.Off, off-80)
	before := strings.ToLower(string(text[from:off]))
	ms := sentenceCut.FindAllStringSubmatchIndex(before, -1)
	for k := len(ms) - 1; k >= 0; k-- {
		if m := ms[k]; before[m[1]-2] != '.' || !abbrev[before[m[2]:m[3]]] {
			before = before[m[1]:]
			break
		}
	}
	before = strings.ReplaceAll(before, "~", " ")
	// "Theorem 5 below" names what comes later, and "by the claim below"
	// uses it.
	if afterProved.MatchString(strings.ToLower(string(text[end:min(len(text), end+160)]))) {
		return RefersTo, "proves"
	}
	if m := afterForward.FindStringSubmatch(after); m != nil && !justBy.MatchString(before) {
		return RefersTo, m[1]
	}
	if ms := cueRe.FindAllStringSubmatchIndex(before, -1); ms != nil {
		m := ms[len(ms)-1]
		c := before[m[2]:m[3]]
		switch {
		case in(usesCues, c):
			return Uses, c
		case in(generalCues, c):
			return Generalizes, c
		case in(specialCues, c):
			return Specializes, c
		default:
			return RefersTo, c
		}
	}
	if m := afterUses.FindStringSubmatch(after); m != nil {
		return Uses, m[1]
	}
	if src.Proof || src.InProof {
		return Uses, CueNone
	}
	return RefersTo, CueNone
}

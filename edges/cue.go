package edges

import (
	"regexp"
	"strings"
)

// The cues of AX6.3. The verb before a reference decides the predicate. A
// sentence that closes a proof, as "This proves Lemma 3" or "which
// completes the proof of Theorem 2", names what the proof proves and not a
// use, so its cue is a refers-to cue. Each
// cue is a value of evidence.cue, from this closed list, with none for a
// reference with no cue.
var (
	usesCues    = []string{"by", "using", "use", "uses", "used", "from", "applying", "apply", "applied", "follows from", "in view of", "combining", "combined with", "thanks to", "due to", "according to", "invoking", "via", "because of", "since", "consequence of", "together with", "with the help of", "based on", "relies on", "under the assumptions of", "assumptions of", "hypotheses of", "conditions of"}
	refersCues  = []string{"see", "cf.", "cf", "compare", "as in", "recall", "similar to", "analogous to", "analogously to", "analog of", "analogue of", "replaced by", "as we saw in", "is proved in", "will be proved in", "we prove in", "we show in", "proves", "also proves", "completes the proof of", "concludes the proof of", "finishes the proof of", "complete the proof of", "conclude the proof of", "finish the proof of"}
	generalCues = []string{"generalizes", "generalises", "generalization of", "generalisation of", "extends", "strengthens"}
	specialCues = []string{"special case of", "particular case of", "specializes", "specialises"}
	// afterUses is a verb after a reference that makes it a reason: Lemma 3
	// implies.
	afterUses = regexp.MustCompile(`^[^.;:]{0,12}?\b(implies|gives|shows|yields|ensures|guarantees)\b`)
	// afterForward is a forward reference: Theorem 5 below.
	afterForward = regexp.MustCompile(`^[^.;:]{0,12}?\b(below|later|in the sequel)\b`)
	// afterRefers is a verb after a reference that makes it the subject of a
	// remark and not a step: Lemma 3 can be replaced by, Part (1) of
	// Conjecture 2 is proved.
	afterRefers = regexp.MustCompile(`^[^.;:]{0,12}?\b(can be replaced|is proved)\b`)
	// asFor is "as for" at the start of a sentence, after commands only.
	asFor = regexp.MustCompile(`^(?:\s|[({]|\\[a-z]+\*?(?:\{[^{}]*\}|\[[^\]]*\])*)*as for\s`)
	cueRe = buildCues()
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
	if m := afterForward.FindStringSubmatch(after); m != nil {
		return RefersTo, m[1]
	}
	if m := afterRefers.FindStringSubmatch(after); m != nil {
		return RefersTo, m[1]
	}
	from := max(src.Off, off-80)
	before := strings.ToLower(string(text[from:off]))
	if i := strings.LastIndexAny(before, ".;:"); i >= 0 && i+1 < len(before) && (before[i+1] == ' ' || before[i+1] == '\n') {
		before = before[i+1:]
	}
	before = strings.ReplaceAll(before, "~", " ")
	// "As for" at the start of a sentence names its topic: "As for the
	// other parts of Theorem 2, …". Later in a sentence it is a use: "the
	// proof is exactly as for Theorem 2".
	if asFor.MatchString(before) {
		return RefersTo, "as for"
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

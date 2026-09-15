package vision

import (
	"strconv"
	"strings"
	"unicode"
)

// Question is one page's reading, with everything the rules need to judge it.
//
// The page number is for the message. The text is what came back. The layer is
// what pdftotext could get off the same page, which on this path is usually little
// and occasionally is a cover sheet's worth, and V09 is the rule that spends it.
// Before is the page before this one, which is how the one failure a single page
// cannot show up in gets caught. The prompt is here so a reading can be compared
// against the thing that asked for it.
type Question struct {
	Page   int
	Text   string
	Layer  string
	Before string
	Prompt string
}

// Refusal is one rule's reason for not accepting a reading.
type Refusal struct {
	Rule string
	What string
}

func (r Refusal) String() string { return r.Rule + ": " + r.What }

// Rule is one thing that has to be true of a page's reading.
//
// Numbered in a group of their own, V, and not folded into the audit's seven
// groups. The audit reads a corpus that exists and these decide whether something
// is allowed to become part of one, so an audit rule's answer is a finding somebody
// fixes and a rule here decides whether the page is asked again at a higher
// resolution. The numbers are permanent all the same, because a run that says a
// page failed V05 is a run somebody has to be able to look up.
type Rule struct {
	// ID is the letter and the number, so "V03".
	ID string
	// Says is the rule as a sentence, and Why is the reasoning where the
	// sentence alone does not carry it.
	Says string
	Why  string
	// ask returns the reason this page is refused, or empty when it is fine.
	ask func(Question) string
}

// Rules are the nine, in the order they are asked.
//
// The order is cheapest and most damning first. A page that came back empty has
// nothing for the other eight to read, and a page that came back as the model
// talking about the page is worth saying so about rather than measuring.
//
// Every one of them is here because it happened. That is the standard for adding
// one: a rule guarding against something nobody has seen is a rule that will be
// wrong in a way nobody notices, because it never fires.
var Rules = []Rule{
	{
		ID:   "V01",
		Says: "a page came back with something on it",
		Why:  "A page that came back empty is not the same as a blank page, and this path cannot tell the difference, so both are refused and a person decides. A paper with a genuinely blank page gets -anyway, and that is a sentence in a log rather than a silent hole.",
		ask: func(q Question) string {
			if strings.TrimSpace(q.Text) == "" {
				return "nothing came back for this page"
			}
			return ""
		},
	},
	{
		ID:   "V02",
		Says: "what came back is the page and not the model talking about the page",
		Why:  "The prompt says to write nothing else, and this is what catches the times that was not enough. A reading that opens with a sentence about the reading is either a refusal or a preface, and publishing either one puts the model's voice in the corpus under the paper's name.",
		ask: func(q Question) string {
			first := firstLine(q.Text)
			low := strings.ToLower(first)
			for _, open := range aboutItself {
				if strings.HasPrefix(low, open) {
					return "the reading opens with the model talking about the page rather than the page: " + short(first)
				}
			}
			return ""
		},
	},
	{
		ID:   "V03",
		Says: "nothing is repeated past the point of sense",
		Why:  "A model that loses its place on a page repeats the line it is on until something stops it, and the result reads like a page of the paper for the first two lines. Four copies of one line of twenty characters is not something a typeset page does.",
		ask: func(q Question) string {
			seen := map[string]int{}
			for _, line := range strings.Split(q.Text, "\n") {
				line = strings.TrimSpace(line)
				if len(line) < 20 {
					continue
				}
				seen[line]++
				if seen[line] > 3 {
					return "one line came back " + strconv.Itoa(seen[line]) + " times: " + short(line)
				}
			}
			return ""
		},
	},
	{
		ID:   "V04",
		Says: "the reading is not the prompt read back",
		Why:  "A model that could not see the picture at all sometimes answers the instructions instead, and what comes back is fluent, is about academic papers, and is not this paper.",
		ask: func(q Question) string {
			if q.Prompt == "" {
				return ""
			}
			for _, line := range strings.Split(q.Prompt, "\n") {
				line = strings.TrimSpace(line)
				if len(line) < 40 {
					continue
				}
				if strings.Contains(q.Text, line) {
					return "the reading contains a line of the prompt: " + short(line)
				}
			}
			return ""
		},
	},
	{
		ID:   "V05",
		Says: "one page's reading is not the page before it read twice",
		Why:  "The failure a single page cannot show. A wrong file name, a rasteriser that wrote the same picture twice, a cache keyed on the paper and not the page: all of them end with two identical pages in the corpus and every rule above this one passing on both.",
		ask: func(q Question) string {
			if len(strings.TrimSpace(q.Before)) < 200 {
				return ""
			}
			if strings.TrimSpace(q.Text) == strings.TrimSpace(q.Before) {
				return "this page came back identical to the page before it, so one of the two is the wrong picture"
			}
			return ""
		},
	},
	{
		ID:   "V06",
		Says: "no more text came back than a printed page can hold",
		Why:  "The same ceiling audit rule S08 puts on a file, asked here where it can still be acted on. A page of a journal article holds about four thousand characters and a dense two column page about eight, so twelve thousand is a model that has started writing rather than reading.",
		ask: func(q Question) string {
			if n := dense(q.Text); n > MostPerPage {
				return "the reading holds " + strconv.Itoa(n) + " characters, which is more than a printed page can hold"
			}
			return ""
		},
	},
	{
		ID:   "V07",
		Says: "what came back is characters and not what is left of them",
		Why:  "A reading full of replacement characters is a page that was read through the wrong encoding somewhere between the model and this program, and it is worth catching here because it survives every other rule: the structure is right, the length is right, and every third letter is gone.",
		ask: func(q Question) string {
			bad, all := 0, 0
			for _, r := range q.Text {
				if unicode.IsSpace(r) {
					continue
				}
				all++
				if r == '�' || (!unicode.IsPrint(r) && r != '\t') {
					bad++
				}
			}
			if all >= 100 && float64(bad) > 0.02*float64(all) {
				return "the reading is " + percent(bad, all) + " characters that are not characters, so something between the model and here lost the encoding"
			}
			return ""
		},
	},
	{
		ID:   "V08",
		Says: "every span the reading opens is closed",
		Why:  "An unclosed pair is how one page's failure becomes the whole paper's. A display that opens and does not close swallows everything after it into mathematics, and a fence that opens and does not close swallows the rest of the paper into a code block, so a page that is wrong in this particular way is not a page's worth of damage.",
		ask: func(q Question) string {
			if n := strings.Count(q.Text, "```"); n%2 == 1 {
				return "a code fence opened and nothing closed it"
			}
			if n := countOutsideFences(q.Text, "$$"); n%2 == 1 {
				return "a displayed equation opened with dollars and nothing closed it"
			}
			if b, e := strings.Count(q.Text, `\begin{`), strings.Count(q.Text, `\end{`); b != e {
				return "the LaTeX has " + strconv.Itoa(b) + " begins and " + strconv.Itoa(e) + " ends"
			}
			return ""
		},
	},
	{
		ID:   "V09",
		Says: "the page's own text layer has an answer in the reading",
		Why:  "The only rule here that compares the reading against the page rather than against itself, and so the only one that can catch a fluent invention. A paper on this path has little text layer by definition, but a page that has a sentence of one has a sentence this reading has to account for. Windows of twenty words rather than the whole page, because a running head and a margin stamp are in the layer and the prompt says to leave them out, so a rule about the whole page would be a rule fighting the prompt.",
		ask: func(q Question) string {
			words := strings.Fields(strings.ToLower(q.Layer))
			if len(words) < LayerFloor {
				return ""
			}
			have := map[string]bool{}
			for _, w := range strings.Fields(strings.ToLower(q.Text)) {
				have[bare(w)] = true
			}
			for at := 0; at+LayerFloor <= len(words); at++ {
				window := words[at : at+LayerFloor]
				// The words of the window worth asking about, which is the ones that
				// carry what the page is saying. Every English sentence shares its
				// the, of, and, in and is with every other one, so a rule that
				// counted those would be satisfied by any twenty words of any page,
				// which is the same as not asking.
				said, found := 0, 0
				for _, w := range window {
					w = bare(w)
					if len(w) < 4 || common[w] {
						continue
					}
					said++
					if have[w] {
						found++
					}
				}
				if said >= ContentFloor && found == 0 {
					return "twenty words the page's own text layer has are nowhere in the reading, starting at " + short(strings.Join(window, " "))
				}
			}
			return ""
		},
	},
}

// MostPerPage is the ceiling V06 puts on one page.
//
// Twelve thousand non-space characters, the same number audit rule S08 uses for a
// file, and deliberately the same number. A reading this path accepts becomes a
// file that has to get past S08, so a lower ceiling here would refuse pages the
// corpus would have taken and a higher one would accept pages the audit will fail.
const MostPerPage = 12000

// LayerFloor is how many words V09 needs before it will ask anything.
//
// Twenty, which is a sentence. Under that the layer is a running head and a page
// number, and a rule that fires because the reading left out the running head
// would be a rule fighting the prompt.
const LayerFloor = 20

// ContentFloor is how many words of a window have to carry meaning before V09 will
// judge it.
//
// Five. A window of twenty words of running text has ten or twelve that are not
// stop words, so five is met by ordinary prose and is not met by a window of a
// table of numbers, a list of author initials, or the twenty words of a reference
// entry that are mostly punctuation and dates. Those are exactly the windows where
// a reading legitimately has nothing to match.
const ContentFloor = 5

// common is the words every English sentence has, which V09 does not count.
//
// Short rather than a proper stop list, because anything under four characters is
// already dropped by length and what is left to name is the handful of long words
// that are in every paper regardless of what it says.
var common = map[string]bool{
	"that": true, "this": true, "with": true, "from": true, "have": true,
	"which": true, "there": true, "their": true, "these": true, "those": true,
	"they": true, "then": true, "than": true, "when": true, "where": true,
	"been": true, "were": true, "will": true, "would": true, "could": true,
	"should": true, "such": true, "into": true, "also": true, "only": true,
	"more": true, "most": true, "some": true, "other": true, "both": true,
	"each": true, "over": true, "under": true, "between": true, "because": true,
	"however": true, "therefore": true, "thus": true, "here": true, "does": true,
	"page": true, "about": true, "after": true, "before": true, "while": true,
}

// bare is a word with the punctuation a page prints around it taken off.
//
// The dashes and the curly quotes are written as escapes rather than as
// themselves, because a source file that holds them is a source file where a
// person cannot tell one dash from another.
const around = ".,;:()[]{}$\\\"'`*_-!?\u2013\u2014\u201c\u201d\u2018\u2019"

func bare(w string) string {
	return strings.Trim(strings.ToLower(w), around)
}

// aboutItself is how a reading opens when it is not the page.
//
// Every one of these is a real opening, either a refusal or a preface. Matched on
// the first non-empty line only, because a paper about image models has "I cannot
// see" in its body and this is not a rule about what papers may say.
var aboutItself = []string{
	"i cannot", "i can't", "i am unable", "i'm unable", "i am sorry", "i'm sorry",
	"sorry,", "unfortunately,", "as an ai", "i do not have", "i don't have",
	"here is the", "here's the", "here is a", "here's a",
	"this page contains", "this appears to be", "this image shows", "the image shows",
	"the page appears", "the document appears", "transcription:", "transcription of",
	"sure,", "certainly,", "of course,", "okay,", "note:", "i notice",
}

// Ask puts every rule to a reading and returns what they refused it for.
//
// All of them and not the first, because a page that failed three rules is a page
// worth knowing three things about. A caller asking again at a higher resolution
// needs one refusal to decide and a person reading a log needs all of them.
func Ask(q Question) []Refusal {
	var out []Refusal
	for _, r := range Rules {
		if what := r.ask(q); what != "" {
			out = append(out, Refusal{Rule: r.ID, What: what})
		}
	}
	return out
}

// Find is the rule with this identifier.
func Find(id string) (Rule, bool) {
	for _, r := range Rules {
		if strings.EqualFold(r.ID, id) {
			return r, true
		}
	}
	return Rule{}, false
}

// dense is how many characters a reading holds that are not space.
func dense(s string) int {
	n := 0
	for _, r := range s {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}

// countOutsideFences counts something in the prose and not in the code.
//
// A shell listing full of dollar signs would otherwise make V08 fire on every page
// that has one, and a page of a systems paper is mostly listings.
func countOutsideFences(text, what string) int {
	n, in := 0, false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			in = !in
			continue
		}
		if !in {
			n += strings.Count(line, what)
		}
	}
	return n
}

// firstLine is the first line of a reading that has anything on it.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// short is a piece of text cut to something that fits in a message.
func short(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const most = 60
	if len(s) <= most {
		return s
	}
	return s[:most] + "..."
}

// percent is a share of something, as a whole number with the words after it.
func percent(part, all int) string {
	if all == 0 {
		return "0 per cent"
	}
	return strconv.Itoa(part*100/all) + " per cent"
}

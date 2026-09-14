package refs

import (
	_ "embed"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed locators.yaml
var defaultLocators []byte

// Locator is which result of the cited paper a citation names.
//
// "by Theorem 2 of [7]" gives kind theorem and number 2, and that is the whole
// of what makes a uses edge different from a citation edge. A citation says one
// paper mentioned another and a locator says which specific result was used.
type Locator struct {
	// Kind is the canonical name, so theorem rather than Thm.
	Kind string
	// Number is as the paper printed it, with the brackets an equation number
	// is printed in taken off.
	Number string
	// Via is the pattern that matched, which is the only way to tell a pattern
	// that is earning its place from one that is firing on the wrong prose.
	Via string
}

// Empty is a citation that named no particular result, which is most of them
// outside mathematics.
func (l Locator) Empty() bool { return l.Kind == "" }

func (l Locator) String() string {
	if l.Empty() {
		return ""
	}
	return l.Kind + " " + l.Number
}

// Locators is the pattern set, compiled.
type Locators struct {
	patterns []shape
	// alias maps a kind word as a paper prints it, lowercased, to the name the
	// graph stores it under.
	alias map[string]string
}

type shape struct {
	name   string
	before bool
	re     *regexp.Regexp
}

// file is the shape of locators.yaml, which is also the shape of the locators
// block of the corpus's manifests/graph.yaml.
type file struct {
	Locators struct {
		Number   string              `yaml:"number"`
		Kinds    map[string][]string `yaml:"kinds"`
		Patterns []struct {
			Name  string `yaml:"name"`
			Where string `yaml:"where"`
			Match string `yaml:"match"`
		} `yaml:"patterns"`
	} `yaml:"locators"`
}

// DefaultLocators is the pattern set compiled into the binary.
//
// It panics rather than returning an error, because the file it reads is
// embedded at build time and a broken one is a broken build rather than
// something a run can recover from. The tests are what keep that honest.
func DefaultLocators() *Locators {
	l, err := ParseLocators(defaultLocators)
	if err != nil {
		panic("the embedded locator patterns do not compile: " + err.Error())
	}
	return l
}

// LoadLocators reads a pattern set from the corpus, falling back to the
// embedded one when the corpus has not got its own.
//
// A missing file is not an error. Most corpora will never have one, and the
// point of the file is that a field which writes locators in a shape nothing
// here matches can be taught without a release.
func LoadLocators(path string) (*Locators, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultLocators(), nil
	}
	if err != nil {
		return nil, err
	}
	l, err := ParseLocators(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return l, nil
}

// ParseLocators compiles a pattern set.
func ParseLocators(b []byte) (*Locators, error) {
	var f file
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	spec := f.Locators
	if spec.Number == "" {
		return nil, fmt.Errorf("the pattern set has no number pattern")
	}
	if len(spec.Kinds) == 0 {
		return nil, fmt.Errorf("the pattern set has no kind words")
	}
	if len(spec.Patterns) == 0 {
		return nil, fmt.Errorf("the pattern set has no patterns")
	}
	l := &Locators{alias: map[string]string{}}
	var words []string
	for kind, aliases := range spec.Kinds {
		for _, a := range aliases {
			a = strings.ToLower(a)
			if was, ok := l.alias[a]; ok && was != kind {
				return nil, fmt.Errorf("the kind word %q is listed under both %s and %s", a, was, kind)
			}
			l.alias[a] = kind
			words = append(words, a)
		}
	}
	// Longest first, because Go's alternation takes the first branch that
	// matches rather than the longest, and "theorem" listed before "theorems"
	// would read "Theorems 2 and 3" as a kind of theorem followed by prose that
	// is not a number.
	sort.Slice(words, func(i, j int) bool {
		if len(words[i]) != len(words[j]) {
			return len(words[i]) > len(words[j])
		}
		return words[i] < words[j]
	})
	// Split on whether the word ends in a letter, because the two halves need
	// different edges. A word alias gets a word boundary on both sides, without
	// which "p" for page matches the P of PINNs and the I of the next word
	// becomes a Roman numeral, which is a real locator this really read before
	// the boundary was there. A symbol alias cannot have one: the boundary
	// before a section sign would ask for a letter in front of it, and the
	// boundary after it would refuse "§ 3" for the space.
	var word, symbol []string
	for _, w := range words {
		q := regexp.QuoteMeta(w)
		if isWordEnd(w) {
			word = append(word, q)
		} else {
			symbol = append(symbol, q)
		}
	}
	// The kind words are folded and the number is not. Making the whole pattern
	// case insensitive would turn the single capital letter branch of the number
	// into any letter at all, and then "Theorem a" would be a locator.
	var branches []string
	if len(word) > 0 {
		branches = append(branches, `\b(?i:`+strings.Join(word, "|")+`)\b`)
	}
	if len(symbol) > 0 {
		branches = append(branches, `(?i:`+strings.Join(symbol, "|")+`)`)
	}
	kind := `(?P<kind>` + strings.Join(branches, "|") + `)`
	num := `(?P<number>` + spec.Number + `)`
	for _, p := range spec.Patterns {
		if p.Name == "" {
			return nil, fmt.Errorf("a pattern has no name, and the name is what the manifest records as the reason a locator was read")
		}
		var before bool
		switch p.Where {
		case "before":
			before = true
		case "after":
		default:
			return nil, fmt.Errorf("pattern %s reads %q, and a pattern reads either before or after the citation", p.Name, p.Where)
		}
		src := strings.ReplaceAll(p.Match, "{kind}", kind)
		src = strings.ReplaceAll(src, "{number}", num)
		re, err := regexp.Compile(src)
		if err != nil {
			return nil, fmt.Errorf("pattern %s does not compile: %w", p.Name, err)
		}
		l.patterns = append(l.patterns, shape{name: p.Name, before: before, re: re})
	}
	return l, nil
}

// Patterns is the names of the patterns, in the order they are tried.
func (l *Locators) Patterns() []string {
	out := make([]string, 0, len(l.patterns))
	for _, p := range l.patterns {
		out = append(out, p.name)
	}
	return out
}

// Kinds is the canonical kind names, sorted, which is what ax refs locators
// prints.
func (l *Locators) Kinds() []string {
	seen := map[string]bool{}
	var out []string
	for _, kind := range l.alias {
		if !seen[kind] {
			seen[kind] = true
			out = append(out, kind)
		}
	}
	sort.Strings(out)
	return out
}

// Find reads the locator a citation carries, given the text on either side of
// it.
//
// The first pattern that matches wins and the rest are not tried. The order in
// the file is the order of certainty: the shapes that read inside the citation's
// own brackets cannot misfire and come first, and the shapes that read the
// sentence around it can and come last.
func (l *Locators) Find(before, after string) Locator {
	before, after = normalise(before), normalise(after)
	for _, p := range l.patterns {
		s := after
		if p.before {
			s = before
		}
		m := p.re.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		kind := l.alias[strings.ToLower(m[p.re.SubexpIndex("kind")])]
		if kind == "" {
			continue
		}
		return Locator{Kind: kind, Number: cleanNumber(m[p.re.SubexpIndex("number")]), Via: p.name}
	}
	return Locator{}
}

// normalise is the form the patterns are run over.
//
// The non-breaking space goes, because a paper writes Theorem~2 and the tie is
// what comes out the other side, and a pattern written with a plain space would
// miss every locator in every paper that uses the tie, which is every paper with
// a house style.
func normalise(s string) string {
	return strings.NewReplacer("\u00a0", " ", "\u2009", " ", "\u202f", " ").Replace(s)
}

// cleanNumber takes off what a style printed around the number rather than as
// part of it.
func cleanNumber(s string) string {
	return strings.Trim(s, "().,")
}

// isWordEnd is whether an alias both starts and ends in a character a word
// boundary means anything next to.
func isWordEnd(s string) bool {
	r := []rune(s)
	return len(r) > 0 && wordish(r[0]) && wordish(r[len(r)-1])
}

func wordish(r rune) bool {
	return r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

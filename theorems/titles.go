// Package theorems resolves the theorem-like environments of a paper.
//
// It has four parts. The declaration resolver reads each form of AX3.2 and
// gives each environment its title, its counter and its numbering. The class
// table gives the environments that a document class or a package defines.
// The title map of AX3.3 gives the item kind of a title. The head finder of
// AX3.4 finds the items that an author wrote by hand, with no environment.
//
// The package is a public API. The importers of math2 for other LaTeX sources
// use it too.
package theorems

import (
	_ "embed"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed data/environments.yaml
var defaultEnvironments []byte

//go:embed data/classes.yaml
var defaultClasses []byte

//go:embed data/proofs.yaml
var defaultProofs []byte

//go:embed data/prose.yaml
var defaultProse []byte

// Manifest returns the first version of one of the four manifests.
//
// The corpus keeps its own copy under manifests/. A corpus copy is the one in
// force, and this copy is the default for a corpus that has none.
func Manifest(name string) ([]byte, bool) {
	switch name {
	case "environments.yaml":
		return defaultEnvironments, true
	case "classes.yaml":
		return defaultClasses, true
	case "proofs.yaml":
		return defaultProofs, true
	case "prose.yaml":
		return defaultProse, true
	}
	return nil, false
}

// ManifestNames is the names of the four manifests.
var ManifestNames = []string{"environments.yaml", "classes.yaml", "proofs.yaml", "prose.yaml"}

// Titles maps normalized titles to item kinds.
type Titles struct {
	kinds map[string]string
}

// ParseTitles reads manifests/environments.yaml.
func ParseTitles(b []byte) (*Titles, error) {
	var raw map[string][]string
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("theorems: environments.yaml: %w", err)
	}
	t := &Titles{kinds: map[string]string{}}
	kinds := make([]string, 0, len(raw))
	for k := range raw {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		for _, title := range raw[kind] {
			n := Normalize(title)
			if prev, ok := t.kinds[n]; ok && prev != kind {
				return nil, fmt.Errorf("theorems: environments.yaml gives the title %q to %s and to %s", title, prev, kind)
			}
			t.kinds[n] = kind
		}
	}
	return t, nil
}

// DefaultTitles is the title map of the first version of the manifest.
func DefaultTitles() *Titles {
	t, err := ParseTitles(defaultEnvironments)
	if err != nil {
		panic(err)
	}
	return t
}

// Unknown is the item kind of a title that the map does not know.
const Unknown = "unknown"

// Kind gives the item kind of a title, or Unknown.
//
// It tries the whole title, then the title without a leading qualifier such
// as "main" or "key", then the first word, then the last word. So "Theorem A"
// and "Main Theorem" and "Technical Lemma" all match. A hyphen or a slash
// splits words too.
func (t *Titles) Kind(title string) string {
	n := Normalize(title)
	if n == "" {
		return Unknown
	}
	if k, ok := t.kinds[n]; ok {
		return k
	}
	// "Proposition-definition" is the words "proposition" and "definition".
	words := strings.FieldsFunc(n, func(r rune) bool { return r == ' ' || r == '-' || r == '/' })
	if len(words) == 0 {
		return Unknown
	}
	for _, try := range []string{strings.Join(words[1:], " "), words[0], words[len(words)-1]} {
		if k, ok := t.kinds[try]; ok && try != "" {
			return k
		}
	}
	if len(words) > 1 {
		if k, ok := t.kinds[words[0]+" "+words[1]]; ok {
			return k
		}
	}
	return Unknown
}

// Known tells if the title is in the map.
func (t *Titles) Known(title string) bool { return t.Kind(title) != Unknown }

// Words is each title of the map, longest first, for the head finder.
func (t *Titles) Words() []string {
	out := make([]string, 0, len(t.kinds))
	for k := range t.kinds {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i] < out[j]
	})
	return out
}

var (
	accentCmd = regexp.MustCompile(`\\([\x60'^"~=.Hcuvk])\s*\{?\s*([a-zA-Z])\}?`)
	texCmd    = regexp.MustCompile(`\\[a-zA-Z@]+\*?`)
)

var accents = map[string]map[byte]string{
	"'":  {'a': "á", 'e': "é", 'i': "í", 'o': "ó", 'u': "ú", 'y': "ý", 'c': "ć", 'n': "ń", 's': "ś", 'z': "ź", 'E': "É", 'A': "Á"},
	"`":  {'a': "à", 'e': "è", 'i': "ì", 'o': "ò", 'u': "ù", 'E': "È", 'A': "À"},
	"^":  {'a': "â", 'e': "ê", 'i': "î", 'o': "ô", 'u': "û"},
	"\"": {'a': "ä", 'e': "ë", 'i': "ï", 'o': "ö", 'u': "ü", 'A': "Ä", 'O': "Ö", 'U': "Ü"},
	"~":  {'a': "ã", 'n': "ñ", 'o': "õ"},
	"c":  {'c': "ç", 'C': "Ç"},
	"v":  {'c': "č", 's': "š", 'z': "ž", 'r': "ř", 'e': "ě"},
}

// Normalize is the form of a title that the map compares.
//
// It removes TeX commands and braces, turns the TeX accents into letters,
// folds the case, removes a trailing * and a trailing plural s, and collapses
// spaces. Numbers and letters after the title are not removed here. The head
// finder removes them.
func Normalize(title string) string {
	s := accentCmd.ReplaceAllStringFunc(title, func(m string) string {
		sub := accentCmd.FindStringSubmatch(m)
		if r, ok := accents[sub[1]][sub[2][0]]; ok {
			return r
		}
		return sub[2]
	})
	s = texCmd.ReplaceAllString(s, " ")
	s = strings.NewReplacer("{", "", "}", "", "~", " ", "\\", " ", ".", " ", ":", " ", " ", " ").Replace(s)
	s = strings.ToLower(strings.Join(strings.Fields(s), " "))
	s = strings.TrimSuffix(s, "*")
	s = strings.TrimSpace(s)
	if len(s) > 3 && strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss") {
		s = s[:len(s)-1]
	}
	return s
}

// KindOf maps an item kind of AX1.3 to the kind and the subkind of the object
// record of 2166-06.
func KindOf(item string) (kind, subkind string) {
	head, sub, _ := strings.Cut(item, "/")
	switch head {
	case "statement":
		return "statement", sub
	case "definition":
		return "definition", sub
	case "example":
		if sub == "" {
			sub = "example"
		}
		return "remark", sub
	case "counterexample":
		return "remark", "counterexample"
	case "conjecture":
		return "problem", "conjecture"
	case "problem":
		return "problem", "problem"
	case "exercise":
		return "exercise", ""
	case "context":
		return "remark", sub
	case "remark":
		return "remark", ""
	case "other":
		return "remark", sub
	case "proof":
		return "proof", ""
	}
	return "remark", Unknown
}

// Phrases is the phrase lists of manifests/proofs.yaml: omitted, folklore,
// elsewhere and restated.
type Phrases map[string][]string

// ParsePhrases reads manifests/proofs.yaml.
func ParsePhrases(b []byte) (Phrases, error) {
	var p Phrases
	if err := yaml.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("theorems: proofs.yaml: %w", err)
	}
	for k, list := range p {
		for i, s := range list {
			list[i] = strings.ToLower(s)
		}
		p[k] = list
	}
	return p, nil
}

// DefaultPhrases is the phrase lists of the first version of the manifest.
func DefaultPhrases() Phrases {
	p, err := ParsePhrases(defaultProofs)
	if err != nil {
		panic(err)
	}
	return p
}

// Find gives the first phrase of a list that the text holds, in lower case.
func (p Phrases) Find(list, text string) (string, bool) {
	t := strings.ToLower(text)
	for _, s := range p[list] {
		if strings.Contains(t, s) {
			return s, true
		}
	}
	return "", false
}

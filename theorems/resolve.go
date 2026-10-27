package theorems

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/tamnd/arxiv-reader/tex"
	"gopkg.in/yaml.v3"
)

// Declaration is one theorem-like environment of a paper.
type Declaration struct {
	Env   string `json:"env"`
	Title string `json:"title"`
	// Counter is the counter that the environment steps. Shared is the
	// environment whose counter it shares, and Within is the counter that
	// resets it.
	Counter  string `json:"counter,omitempty"`
	Shared   string `json:"shared,omitempty"`
	Within   string `json:"within,omitempty"`
	Numbered bool   `json:"numbered"`
	Style    string `json:"style,omitempty"`
	// Item is the item kind of the title.
	Item string `json:"item"`
	// Proof is true for a proof environment.
	Proof bool `json:"proof,omitempty"`
	// From says where the declaration is: paper, local:<file>, class:<name>,
	// package:<name>, wrapper, or used for an environment that is used and
	// that the front end found no declaration for.
	From string `json:"from"`
	// Form is the command of the declaration, for the census patterns.
	Form string `json:"form,omitempty"`
	// Inner is the environment that a wrapper opens.
	Inner string `json:"inner,omitempty"`
	// TitleArg is true when each use gives the title as the first argument,
	// as \begin{thm}{Claim}. Title and Item are then empty, and TitleOf reads
	// them from the use.
	TitleArg bool `json:"title_arg,omitempty"`
	// TitleOpt is true when the title argument is optional, and TitleDefault
	// is the title of a use that does not give it.
	TitleOpt     bool   `json:"title_opt,omitempty"`
	TitleDefault string `json:"title_default,omitempty"`
	// Off is the offset of the declaration in the expanded text, or -1.
	Off int `json:"-"`
}

// Set is the theorem-like environments of a paper.
type Set struct {
	Envs  map[string]*Declaration
	Order []string
	// Aliases maps the counters of \newaliascnt to the counters they alias.
	Aliases map[string]string
	// NumberWithin is each \numberwithin{counter}{within} of the paper.
	NumberWithin map[string]string
}

// Get is the declaration of an environment, or nil.
func (s *Set) Get(env string) *Declaration { return s.Envs[env] }

// List is the declarations in the order of the paper.
func (s *Set) List() []*Declaration {
	out := make([]*Declaration, 0, len(s.Order))
	for _, e := range s.Order {
		out = append(out, s.Envs[e])
	}
	return out
}

func (s *Set) add(d *Declaration, replace bool) {
	if _, ok := s.Envs[d.Env]; ok && !replace {
		return
	}
	if _, ok := s.Envs[d.Env]; !ok {
		s.Order = append(s.Order, d.Env)
	}
	if d.Item == "proof" {
		// A class can declare proof with \newtheorem or \spnewtheorem. It
		// still writes proofs, and LaTeXML renders them as proofs.
		d.Proof = true
	}
	s.Envs[d.Env] = d
}

// Classes is the table of the environments that classes and packages define.
type Classes struct {
	Classes  map[string][]classEnv `yaml:"classes"`
	Packages map[string][]classEnv `yaml:"packages"`
	Proofs   []string              `yaml:"proofs"`
}

type classEnv struct {
	Env      string `yaml:"env"`
	Title    string `yaml:"title"`
	Numbered *bool  `yaml:"numbered"`
	Proof    bool   `yaml:"proof"`
	Shared   string `yaml:"shared"`
}

// ParseClasses reads manifests/classes.yaml.
func ParseClasses(b []byte) (*Classes, error) {
	var c Classes
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("theorems: classes.yaml: %w", err)
	}
	return &c, nil
}

// DefaultClasses is the class table of the first version of the manifest.
func DefaultClasses() *Classes {
	c, err := ParseClasses(defaultClasses)
	if err != nil {
		panic(err)
	}
	return c
}

// IsProofName tells if an environment name is a proof in any paper.
func (c *Classes) IsProofName(env string) bool {
	for _, p := range c.Proofs {
		if p == env {
			return true
		}
	}
	return false
}

// Resolver resolves the declarations of a paper.
type Resolver struct {
	Titles  *Titles
	Classes *Classes
}

// NewResolver is a resolver with the default manifests.
func NewResolver() *Resolver {
	return &Resolver{Titles: DefaultTitles(), Classes: DefaultClasses()}
}

var (
	declCmd   = regexp.MustCompile(`\\(newtheorem|renewtheorem|spnewtheorem|declaretheorem|newproof|newdefinition|newremark|newaliascnt|theoremstyle|numberwithin|counterwithin|newenvironment|renewenvironment|newmdtheoremenv|newtcbtheorem|NewTheorem)\b`)
	stepCount = regexp.MustCompile(`\\refstepcounter\s*\{([^}]+)\}`)
	innerEnv  = regexp.MustCompile(`\\begin\s*\{([^}]+)\}`)
)

// Resolve gives the theorem-like environments of a document.
//
// The order of precedence is: the paper, then its local style files, then the
// class, then the packages, then the names that are a proof in any paper.
// Wrappers are resolved last, because a wrapper names an environment that one
// of the others declares.
func (r *Resolver) Resolve(d *tex.Document) *Set {
	s := &Set{Envs: map[string]*Declaration{}, Aliases: map[string]string{}, NumberWithin: map[string]string{}}
	for _, name := range sortedNames(d.Local) {
		r.scan(s, d.Local[name], "local:"+name, -1)
	}
	r.scan(s, d.Text, "paper", 0)
	r.macroDecls(s, d)
	if envs, ok := r.Classes.Classes[d.Class]; ok {
		for _, e := range envs {
			s.add(r.fromClass(e, "class:"+d.Class), false)
		}
	}
	for _, p := range d.Packages {
		for _, e := range r.Classes.Packages[p] {
			s.add(r.fromClass(e, "package:"+p), false)
		}
	}
	for i := 0; i < 3; i++ {
		r.wrappers(s, d)
	}
	r.usedProofs(s, d)
	for _, d := range s.Envs {
		d.Counter = s.counterOf(d)
	}
	return s
}

// usedProofs adds an entry for each environment that the body uses, that has
// no declaration, and whose name is a proof name. A class can define proof
// with \def\proof, which the resolver does not read, and a paper writes its
// proofs with it.
func (r *Resolver) usedProofs(s *Set, d *tex.Document) {
	for _, n := range d.Tree.Nodes {
		if _, ok := s.Envs[n.Name]; ok || n.Kind != "env" || !r.Classes.IsProofName(n.Name) {
			continue
		}
		s.add(&Declaration{Env: n.Name, Title: "Proof", Proof: true, Item: "proof", From: "used", Off: -1}, false)
	}
}

func (r *Resolver) fromClass(e classEnv, from string) *Declaration {
	numbered := e.Numbered == nil || *e.Numbered
	if e.Proof {
		numbered = false
	}
	d := &Declaration{Env: e.Env, Title: e.Title, Numbered: numbered, Proof: e.Proof, Shared: e.Shared, From: from, Off: -1}
	d.Item = r.item(d)
	return d
}

func (r *Resolver) item(d *Declaration) string {
	if d.Proof {
		return "proof"
	}
	return r.Titles.Kind(d.Title)
}

// counterOf follows shared environments and aliases to the counter that an
// environment steps.
func (s *Set) counterOf(d *Declaration) string {
	if !d.Numbered {
		return ""
	}
	seen := map[string]bool{}
	c := d.Env
	shared := d.Shared
	for shared != "" && !seen[shared] {
		seen[shared] = true
		if a, ok := s.Aliases[shared]; ok {
			c = a
			break
		}
		next, ok := s.Envs[shared]
		if !ok || next == d {
			c = shared
			break
		}
		c = next.Env
		shared = next.Shared
	}
	if a, ok := s.Aliases[c]; ok {
		c = a
	}
	return c
}

// scan reads the declarations of one text. base is the offset of the text in
// the expanded source, or -1 for a style file.
func (r *Resolver) scan(s *Set, text []byte, from string, base int) {
	style := "plain"
	for _, m := range declCmd.FindAllSubmatchIndex(text, -1) {
		cmd := string(text[m[2]:m[3]])
		at := m[1]
		off := -1
		if base >= 0 {
			off = base + m[0]
		}
		switch cmd {
		case "theoremstyle":
			if a, _, ok := tex.ReadArgs(text, at, "m"); ok {
				style = strings.TrimSpace(a.Values[0])
			}
		case "newaliascnt":
			if a, _, ok := tex.ReadArgs(text, at, "mm"); ok {
				s.Aliases[strings.TrimSpace(a.Values[0])] = strings.TrimSpace(a.Values[1])
			}
		case "numberwithin", "counterwithin":
			if a, _, ok := tex.ReadArgs(text, at, "*omm"); ok {
				s.NumberWithin[strings.TrimSpace(a.Values[1])] = strings.TrimSpace(a.Values[2])
			}
		case "newtheorem", "renewtheorem", "spnewtheorem", "newdefinition", "newremark", "NewTheorem":
			d, ok := newTheorem(text, at)
			if !ok {
				continue
			}
			d.Style, d.From, d.Form, d.Off = style, from, cmd, off
			switch cmd {
			case "newdefinition":
				d.Style = "definition"
			case "newremark":
				d.Style = "remark"
			}
			d.Item = r.item(d)
			s.add(d, true)
		case "newproof":
			a, _, ok := tex.ReadArgs(text, at, "mom")
			if !ok {
				continue
			}
			d := &Declaration{Env: strings.TrimSpace(a.Values[0]), Title: strings.TrimSpace(a.Values[2]), Proof: true, From: from, Form: cmd, Off: off}
			d.Item = r.item(d)
			s.add(d, true)
		case "declaretheorem":
			d, ok := declareTheorem(text, at)
			if !ok {
				continue
			}
			d.From, d.Form, d.Off = from, cmd, off
			if d.Style == "" {
				d.Style = style
			}
			d.Item = r.item(d)
			s.add(d, true)
		case "newmdtheoremenv":
			// \newmdtheoremenv[options]{env}[shared]{Title}[within], from mdframed.
			if _, end, ok := tex.ReadArgs(text, at, "o"); ok {
				d, ok := newTheorem(text, end)
				if !ok {
					continue
				}
				d.Style, d.From, d.Form, d.Off = style, from, cmd, off
				d.Item = r.item(d)
				s.add(d, true)
			}
		case "newtcbtheorem":
			// \newtcbtheorem[init]{env}{Title}{options}{prefix}, from tcolorbox.
			a, _, ok := tex.ReadArgs(text, at, "ommmm")
			if !ok {
				continue
			}
			d := &Declaration{Env: strings.TrimSpace(a.Values[1]), Title: strings.TrimSpace(a.Values[2]), Numbered: true, Style: style, From: from, Form: cmd, Off: off}
			d.Item = r.item(d)
			s.add(d, true)
		}
	}
}

// newTheorem reads the arguments of \newtheorem and of the forms with the same
// arguments: \newtheorem{env}[shared]{Title} or \newtheorem{env}{Title}[within],
// each with an optional *.
func newTheorem(text []byte, at int) (*Declaration, bool) {
	a, end, ok := tex.ReadArgs(text, at, "*m")
	if !ok {
		return nil, false
	}
	d := &Declaration{Env: strings.TrimSpace(a.Values[0]), Numbered: !a.Star}
	b, _, ok := tex.ReadArgs(text, end, "omo")
	if !ok {
		return nil, false
	}
	d.Shared = strings.TrimSpace(b.Values[0])
	d.Title = strings.TrimSpace(b.Values[1])
	d.Within = strings.TrimSpace(b.Values[2])
	if d.Env == "" || strings.ContainsAny(d.Env, "#\\") {
		return nil, false
	}
	return d, true
}

// declareTheorem reads \declaretheorem[keys]{env} and \declaretheorem{env}[keys]
// of thmtools.
func declareTheorem(text []byte, at int) (*Declaration, bool) {
	a, _, ok := tex.ReadArgs(text, at, "omo")
	if !ok {
		return nil, false
	}
	env := strings.TrimSpace(a.Values[1])
	if env == "" || strings.ContainsAny(env, "#\\") {
		return nil, false
	}
	d := &Declaration{Env: env, Numbered: true}
	keys := a.Values[0] + "," + a.Values[2]
	for _, kv := range splitKeys(keys) {
		k, v, _ := strings.Cut(kv, "=")
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), "{}")
		switch k {
		case "name", "title", "heading":
			d.Title = v
		case "style":
			d.Style = v
		case "numberwithin", "within", "parent":
			d.Within = v
		case "sibling", "numberlike", "sharenumber", "sharecounter":
			d.Shared = v
		case "numbered":
			d.Numbered = v != "no" && v != "false"
		}
	}
	if d.Title == "" {
		d.Title = strings.ToUpper(env[:1]) + env[1:]
	}
	return d, true
}

// splitKeys splits a key list on the commas that are not inside braces.
func splitKeys(s string) []string {
	var out []string
	depth, from := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[from:i])
				from = i + 1
			}
		}
	}
	out = append(out, s[from:])
	var kept []string
	for _, k := range out {
		if strings.TrimSpace(k) != "" {
			kept = append(kept, k)
		}
	}
	return kept
}

// macroDecls finds declarations made by the paper's own macros.
//
// A paper can write \newcommand{\thm}[2]{\newtheorem{#1}{#2}} and then
// \thm{lem}{Lemma}. Such a declaration is the usual cause of a contiguity gap
// (AX4.4), so each call of a macro whose body declares a theorem is expanded
// and read. The calls in the local .sty and .cls files are read too, because a
// class such as siamart declares its theorems with \newsiamthm.
func (r *Resolver) macroDecls(s *Set, d *tex.Document) {
	r.macroCalls(s, d, d.Text, "paper", true)
	for _, name := range sortedNames(d.Local) {
		r.macroCalls(s, d, d.Local[name], "local:"+name, false)
	}
}

// macroCalls expands each call in text of a macro that declares a theorem. The
// offset of a declaration is the offset of its call only when text is the
// paper.
func (r *Resolver) macroCalls(s *Set, d *tex.Document, text []byte, from string, paper bool) {
	for _, name := range sortedNames(declMacros(d.Macros)) {
		m := d.Macros.Commands[name]
		call := regexp.MustCompile(regexp.QuoteMeta(name) + `\b`)
		for _, loc := range call.FindAllIndex(text, -1) {
			if before(text, loc[0], "newcommand") || before(text, loc[0], "def") {
				continue
			}
			end := loc[1]
			for k := 0; k < m.Args; k++ {
				_, e, ok := tex.ReadArgs(text, end, "m")
				if !ok {
					break
				}
				end = e
			}
			expanded := d.Macros.Expand(string(text[loc[0]:end]))
			sub := &Set{Envs: map[string]*Declaration{}, Aliases: s.Aliases, NumberWithin: s.NumberWithin}
			r.scan(sub, []byte(expanded), from, -1)
			for _, e := range sub.List() {
				e.Off, e.Form = -1, "macro:"+name
				if paper {
					e.Off = loc[0]
				}
				s.add(e, false)
			}
		}
	}
}

// declMacros is the set of the macros with arguments whose body declares a theorem.
func declMacros(ms *tex.Macros) map[string][]byte {
	out := map[string][]byte{}
	for name, m := range ms.Commands {
		if m.Args > 0 && declCmd.MatchString(m.Body) {
			out[name] = nil
		}
	}
	return out
}

// before tells if the text just before off ends with word, with spaces,
// braces and a backslash allowed between them.
func before(text []byte, off int, word string) bool {
	i := off
	for i > 0 && (text[i-1] == ' ' || text[i-1] == '{' || text[i-1] == '*') {
		i--
	}
	return i >= len(word) && string(text[i-len(word):i]) == word
}

// wrappers resolves the environments that wrap another one.
//
// \newenvironment{proofof}[1]{\begin{proof}[Proof of #1]}{\end{proof}} makes
// proofof a proof. An environment whose begin code writes a head by hand, such
// as {\noindent\textbf{Theorem.}\it}, is theorem-like with that title. When the
// begin code steps a counter with \refstepcounter, it is numbered.
func (r *Resolver) wrappers(s *Set, d *tex.Document) {
	for _, name := range d.Macros.EnvOrder {
		e := d.Macros.Envs[name]
		if _, ok := s.Envs[name]; ok {
			continue
		}
		if m := innerEnv.FindStringSubmatch(e.Begin); m != nil {
			inner := strings.TrimSpace(m[1])
			in, ok := s.Envs[inner]
			if !ok && r.Classes.IsProofName(inner) {
				in = &Declaration{Env: inner, Title: "Proof", Proof: true, Item: "proof"}
				ok = true
			}
			if ok {
				w := *in
				w.Env, w.From, w.Form, w.Inner, w.Off = name, "wrapper", "newenvironment", inner, e.Off
				if w.Shared == "" && w.Numbered {
					w.Shared = inner
				}
				s.add(&w, false)
			}
			continue
		}
		if code, ok := titledBegin(d.Macros, e); ok {
			w := &Declaration{Env: name, TitleArg: true, Item: Unknown, From: "wrapper", Form: "titled", Off: e.Off}
			w.TitleOpt, w.TitleDefault = e.HasDefault, e.Default
			if r.Classes.IsProofName(name) {
				// A paper that defines proof again with a title argument
				// still writes proofs with it.
				w.Proof, w.Item = true, "proof"
			}
			if m := stepCount.FindStringSubmatch(code); m != nil {
				w.Numbered = true
				w.Counter = strings.TrimSpace(m[1])
				s.Aliases[name] = w.Counter
			}
			s.add(w, false)
			continue
		}
		if h, ok := FirstHead(e.Begin, r.Titles); ok {
			w := &Declaration{Env: name, Title: h, From: "wrapper", Form: "newenvironment", Off: e.Off}
			w.Item = r.Titles.Kind(h)
			if w.Item == "proof" {
				w.Proof = true
			}
			if m := stepCount.FindStringSubmatch(e.Begin); m != nil && !w.Proof {
				w.Numbered = true
				w.Shared = ""
				w.Counter = strings.TrimSpace(m[1])
				s.Aliases[name] = w.Counter
			}
			s.add(w, false)
		}
	}
}

// titleHead finds #1 inside a bold, italic or small caps group or argument.
var titleHead = regexp.MustCompile(`\{\s*\\(?:bf|bfseries|sc|scshape|it|itshape|sl)\b[^{}]*(?:\{[^{}]*\}[^{}]*)*#1|\\(?:textbf|textsc|textit|emph)\s*\{[^{}]*(?:\{[^{}]*\}[^{}]*)*#1`)

// titledBegin tells if an environment prints a head whose title is its first
// argument. The begin code can do it itself, or it can call one command of
// the paper that takes the argument, as {\claim} with \def\claim#1{...{\bf
// \arabic{thm}. #1.}}. It returns the code that writes the head.
func titledBegin(m *tex.Macros, e *tex.Env) (string, bool) {
	code := strings.TrimSpace(e.Begin)
	if e.Args >= 1 && titleHead.MatchString(code) {
		return code, true
	}
	if e.Args > 0 || !strings.HasPrefix(code, `\`) {
		return "", false
	}
	if c, ok := m.Commands[code]; ok && c.Args >= 1 && titleHead.MatchString(c.Body) {
		return c.Body, true
	}
	return "", false
}

// TitleOf reads the title of one use of a titled environment, from the
// argument at inner. It returns the title, its item kind and the offset after
// the argument. For an environment that is not titled, it returns the title of
// the declaration and inner.
func (d *Declaration) TitleOf(text []byte, inner int, t *Titles) (title, item string, after int) {
	if !d.TitleArg {
		return d.Title, d.Item, inner
	}
	spec := "m"
	if d.TitleOpt {
		spec = "o"
	}
	a, end, ok := tex.ReadArgs(text, inner, spec)
	if !ok {
		if d.Proof {
			return "", "proof", inner
		}
		return "", Unknown, inner
	}
	title = a.Values[0]
	if d.TitleOpt && !a.Has[0] {
		title = d.TitleDefault
	}
	title = strings.TrimSpace(strings.TrimRight(stripFonts(title), ".: "))
	if d.Proof {
		return title, "proof", end
	}
	return title, t.Kind(title), end
}

// Used adds an entry for each environment that the body uses, that has no
// declaration, and whose name is a title or a proof name. Such an environment
// is declared in a way that the resolver does not read, and it must still be
// counted.
func (r *Resolver) Used(s *Set, d *tex.Document) {
	for _, n := range d.Tree.Nodes {
		if n.Kind != "env" {
			continue
		}
		if _, ok := s.Envs[n.Name]; ok {
			continue
		}
		name := strings.TrimSuffix(n.Name, "*")
		switch {
		case r.Classes.IsProofName(n.Name):
			s.add(&Declaration{Env: n.Name, Title: "Proof", Proof: true, Item: "proof", From: "used", Off: -1}, false)
		case r.Titles.Known(name) && !nonItemEnvs[name]:
			dec := &Declaration{Env: n.Name, Title: name, Numbered: !strings.HasSuffix(n.Name, "*"), From: "used", Off: -1}
			dec.Item = r.Titles.Kind(name)
			if dec.Item == "proof" {
				dec.Proof, dec.Numbered = true, false
			}
			s.add(dec, false)
		}
	}
	for _, d := range s.Envs {
		d.Counter = s.counterOf(d)
	}
}

// nonItemEnvs are environment names that are titles in the map but that the
// standard packages use for something that is not an item.
var nonItemEnvs = map[string]bool{
	"case": true, "cases": true, "step": true, "part": true, "algorithm": true,
	"note": true, "comment": true, "solution": false, "conditions": true,
	"property": false, "def": true, "setting": true, "setup": true,
}

func sortedNames(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

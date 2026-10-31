package theorems

import (
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
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
	Counter string `json:"counter,omitempty"`
	Shared  string `json:"shared,omitempty"`
	Within  string `json:"within,omitempty"`
	// EmptyWithin is true for \newtheorem{env}{Title}[], whose number
	// prints with a 0 before it, as "01".
	EmptyWithin bool   `json:"empty_within,omitempty"`
	Numbered    bool   `json:"numbered"`
	Style       string `json:"style,omitempty"`
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
	// NumberArg is the argument of a wrapper use that prints the number, as
	// #1 of \newenvironment{customthm}[1]{\renewcommand\theinner{#1}\inner},
	// or 0.
	NumberArg int `json:"number_arg,omitempty"`
	// TitleArg is true when each use gives the title as the first argument,
	// as \begin{thm}{Claim}. Title and Item are then empty, and TitleOf reads
	// them from the use.
	TitleArg bool `json:"title_arg,omitempty"`
	// TitleOpt is true when the title argument is optional, and TitleDefault
	// is the title of a use that does not give it.
	TitleOpt     bool   `json:"title_opt,omitempty"`
	TitleDefault string `json:"title_default,omitempty"`
	// TitlePrefix is the title that the head writes right before the
	// argument, as "Theorem#1", where the argument only adds to it.
	TitlePrefix string `json:"title_prefix,omitempty"`
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
	// Counters is the counter that each theorem-like environment of a class
	// steps, whatever the declaration says, for a class that defines
	// \newtheorem again.
	Counters map[string]string `yaml:"counters"`
}

type classEnv struct {
	Env      string `yaml:"env"`
	Title    string `yaml:"title"`
	Numbered *bool  `yaml:"numbered"`
	Proof    bool   `yaml:"proof"`
	Shared   string `yaml:"shared"`
	Within   string `yaml:"within"`
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
	declCmd   = regexp.MustCompile(`\\(newtheoremrm|newtheorem|renewtheorem|spnewtheorem|declaretheorem|newproof|newdefinition|newremark|newaliascnt|theoremstyle|numberwithin|counterwithin|newenvironment|renewenvironment|newmdtheoremenv|newtcbtheorem|NewTheorem)\b`)
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
		r.scan(s, tex.LocalText(d, name), "local:"+name, -1)
	}
	r.scan(s, d.Text, "paper", 0)
	r.macroDecls(s, d)
	if cls := r.tableClass(d); cls != "" {
		for _, e := range r.Classes.Classes[cls] {
			s.add(r.fromClass(e, "class:"+cls), false)
		}
	}
	for _, p := range d.Packages {
		// An entry such as ntheorem[standard] is for the package with the option.
		names := []string{p}
		for _, o := range d.PackageOptions[p] {
			names = append(names, p+"["+o+"]")
		}
		for _, n := range names {
			for _, e := range r.Classes.Packages[n] {
				s.add(r.fromClass(e, "package:"+n), false)
			}
		}
	}
	for i := 0; i < 3; i++ {
		r.wrappers(s, d)
	}
	letEnvs(s, d)
	r.usedProofs(s, d)
	r.macroTitles(s, d)
	r.springerOptions(s, d)
	for _, d := range s.Envs {
		d.Counter = s.counterOf(d)
	}
	if c := r.Classes.Counters[r.tableClass(d)]; c != "" {
		for _, e := range s.Envs {
			if e.Numbered && !e.Proof && (e.From == "paper" || strings.HasPrefix(e.From, "class:")) {
				e.Counter = c
			}
		}
	}
	return s
}

// springerClasses are the classes of Springer that read the options
// envcountsame, envcountsect and envcountchap.
var springerClasses = map[string]bool{"llncs": true, "svjour": true, "svjour3": true, "svmult": true, "svmono": true, "svproc": true}

// springerOptions applies the options of a Springer class to its environments
// and to each \spnewtheorem that names no counter. envcountsame makes them
// share the counter of theorem, and envcountsect and envcountchap number them
// within the section or the chapter. Seen in the gold pilot (set X).
func (r *Resolver) springerOptions(s *Set, d *tex.Document) {
	if !springerClasses[r.tableClass(d)] {
		return
	}
	same, within := false, ""
	for _, o := range d.Options {
		switch o {
		case "envcountsame":
			same = true
		case "envcountsect":
			within = "section"
		case "envcountchap":
			within = "chapter"
		}
	}
	if !same && within == "" {
		return
	}
	for _, e := range s.Envs {
		if !e.Numbered || e.Proof || e.Shared != "" || e.Within != "" || !(strings.HasPrefix(e.From, "class:") || e.Form == "spnewtheorem") {
			continue
		}
		if same && e.Env != "theorem" {
			e.Shared = "theorem"
		} else {
			e.Within = within
		}
	}
}

var protectCmd = regexp.MustCompile(`\\protect\b\s*`)

// macroTitles expands a title written as a macro. LyX writes
// \newtheorem{thm}{\protect\theoremname} and defines \theoremname with
// \providecommand, so the title is the macro's body. Seen in the gold pilot
// (set E).
func (r *Resolver) macroTitles(s *Set, d *tex.Document) {
	for _, e := range s.Envs {
		if e.Proof || !strings.Contains(e.Title, `\`) {
			continue
		}
		t := protectCmd.ReplaceAllString(e.Title, "")
		t = strings.TrimSpace(strings.Trim(d.Macros.Expand(t), "{} "))
		if t == e.Title || strings.Contains(t, `\`) {
			continue
		}
		if item := r.Titles.Kind(t); item != Unknown && item != "proof" {
			e.Title, e.Item = t, item
		}
	}
}

var loadClassCmd = regexp.MustCompile(`\\LoadClass(?:WithOptions)?\s*(?:\[[^\]]*\])?\s*\{([^}]*)\}`)

// tableClass gives the class of the table that the document uses. A local
// class such as l4dc2026.cls is often a few lines that load a class of the
// table with \LoadClass, so the resolver follows \LoadClass in the local
// class files. It gives "" when no class of the chain is in the table.
func (r *Resolver) tableClass(d *tex.Document) string {
	c := d.Class
	for range 4 {
		if _, ok := r.Classes.Classes[c]; ok {
			return c
		}
		var m [][]byte
		for _, name := range sortedNames(d.Local) {
			if path.Base(name) == c+".cls" {
				m = loadClassCmd.FindSubmatch(d.Local[name])
				break
			}
		}
		if m == nil {
			return ""
		}
		c = strings.TrimSpace(string(m[1]))
	}
	return ""
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
	d := &Declaration{Env: e.Env, Title: e.Title, Numbered: numbered, Proof: e.Proof, Shared: e.Shared, Within: e.Within, From: from, Off: -1}
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
		case "newtheorem", "newtheoremrm", "renewtheorem", "spnewtheorem", "newdefinition", "newremark", "NewTheorem":
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
	d.EmptyWithin = b.Has[2] && d.Within == "" && d.Shared == ""
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
	lets := declLets(d)
	r.macroCalls(s, d, d.Text, "paper", true, lets)
	for _, name := range sortedNames(d.Local) {
		r.macroCalls(s, d, d.Local[name], "local:"+name, false, lets)
	}
}

// letEnv is \let\claim\claimx, which makes an environment the same as
// another one when \let\endclaim\endclaimx goes with it.
var letEnv = regexp.MustCompile(`\\let\s*\\([A-Za-z]+)\s*=?\s*\\([A-Za-z]+)`)

// letEnvs gives the declaration of Y to each environment X of \let\X\Y and
// \let\endX\endY. Seen in the gold pilot (set X).
func letEnvs(s *Set, d *tex.Document) {
	lets := map[string]string{}
	for _, m := range letEnv.FindAllSubmatch(d.Text, -1) {
		lets[string(m[1])] = string(m[2])
	}
	for _, x := range slices.Sorted(maps.Keys(lets)) {
		y := lets[x]
		from := s.Envs[y]
		if from == nil || x == y || lets["end"+x] != "end"+y {
			continue
		}
		nd := *from
		nd.Env = x
		s.add(&nd, true)
	}
}

// letDecl is \let\@defthm\newtheorem: a name for a command that declares.
var letDecl = regexp.MustCompile(`\\let\s*(\\[A-Za-z@]+)\s*=?\s*\\(newtheoremrm|newtheorem|declaretheorem|spnewtheorem)\b`)

// declLets is each name that \let makes for a command that declares, as
// \let\@defthm\newtheorem with \def\defthm#1#2#3{\@defthm{#1}{#2}}. The first
// \let of a name holds. Seen in the gold pilot (set X).
func declLets(d *tex.Document) map[string]string {
	out := map[string]string{}
	for _, m := range letDecl.FindAllSubmatch(d.Text, -1) {
		if _, ok := out[string(m[1])]; !ok {
			out[string(m[1])] = `\` + string(m[2])
		}
	}
	return out
}

// macroCalls expands each call in text of a macro that declares a theorem. The
// offset of a declaration is the offset of its call only when text is the
// paper.
func (r *Resolver) macroCalls(s *Set, d *tex.Document, text []byte, from string, paper bool, lets map[string]string) {
	// The expansion stops at a command that declares, as \newtheoremrm,
	// which tac.cls defines. Seen in the gold pilot (set X).
	keep := tex.NewMacros()
	for name, c := range d.Macros.Commands {
		if m := declCmd.FindStringIndex(name); m == nil || m[0] != 0 || m[1] != len(name) {
			keep.Commands[name] = c
		}
	}
	for _, name := range sortedNames(declMacros(d.Macros, lets)) {
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
			expanded := keep.Expand(string(text[loc[0]:end]))
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
func declMacros(ms *tex.Macros, lets map[string]string) map[string][]byte {
	out := map[string][]byte{}
	for name, m := range ms.Commands {
		if m.Args > 0 && (declCmd.MatchString(m.Body) || slices.ContainsFunc(slices.Sorted(maps.Keys(lets)), func(l string) bool { return strings.Contains(m.Body, l) })) {
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
		inner := ""
		if m := innerEnv.FindStringSubmatch(e.Begin); m != nil {
			inner = strings.TrimSpace(m[1])
		} else {
			inner = innerCommand(s, e.Begin, e.End)
		}
		if inner != "" {
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
				if m := theArg.FindStringSubmatch(e.Begin); m != nil && m[1] == inner {
					w.NumberArg = int(m[2][0] - '0')
				}
				s.add(&w, false)
			}
			continue
		}
		if code, ok := titledBegin(d.Macros, e); ok {
			w := &Declaration{Env: name, TitleArg: true, Item: Unknown, From: "wrapper", Form: "titled", Off: e.Off}
			w.TitleOpt, w.TitleDefault = e.HasDefault, e.Default
			if m := titlePrefix.FindStringSubmatch(code); m != nil && r.Titles.Known(m[1]) {
				// {\bf \theitem. Theorem#1.} with \begin{theo}{\label{a}}:
				// the title is Theorem. Seen in the gold pilot (set X).
				w.TitlePrefix = m[1]
			}
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

var cmdCall = regexp.MustCompile(`\\([A-Za-z@]+)`)

// theArg is \renewcommand\theinner{#1} in the begin code of a wrapper.
var theArg = regexp.MustCompile(`\\(?:renewcommand\*?|def|gdef)\s*\{?\s*\\the([A-Za-z@]+)\s*\}?\s*\{\s*#([1-9])\s*\}`)

// innerCommand gives the environment that a wrapper opens with its command
// and closes with \end<name>, as in the pattern
//
//	\newenvironment{customthm}[1]
//	  {\renewcommand\theinnercustomthm{#1}\innercustomthm}
//	  {\endinnercustomthm}
//
// It gives "" when the begin code calls no command of a declared environment.
func innerCommand(s *Set, begin, end string) string {
	for _, m := range cmdCall.FindAllStringSubmatch(begin, -1) {
		name := m[1]
		if _, ok := s.Envs[name]; !ok {
			continue
		}
		if ends := cmdCall.FindAllStringSubmatch(end, -1); slices.ContainsFunc(ends, func(c []string) bool { return c[1] == "end"+name }) {
			return name
		}
	}
	return ""
}

// titleHead finds #1 inside a bold, italic or small caps group or argument.
var titleHead = regexp.MustCompile(`\{\s*\\(?:bf|bfseries|sc|scshape|it|itshape|sl)\b[^{}]*(?:\{[^{}]*\}[^{}]*)*#1|\\(?:textbf|textsc|textit|emph)\s*\{[^{}]*(?:\{[^{}]*\}[^{}]*)*#1`)

// titlePrefix is a title right before the argument in the head.
var titlePrefix = regexp.MustCompile(`([A-Za-z]{3,})#1`)

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
	if d.TitlePrefix != "" {
		return d.TitlePrefix, t.Kind(d.TitlePrefix), end
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

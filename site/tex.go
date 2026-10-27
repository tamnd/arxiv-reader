package site

import (
	"html"
	"regexp"
	"strings"
)

// Ref is where a \ref of an item goes: the anchor of the item and the text
// that the paper prints for it.
type Ref struct {
	Href string
	Text string // the number, as 2.3
	Kind string // the title of the item, as Lemma, for \cref
}

// texHTML is the converter of the body of one item. The body is the LaTeX of
// the paper, with the macros of the paper expanded. The converter writes the
// text as HTML and each formula as a span or a div with the TeX in it, which
// KaTeX renders in the browser. It knows the common commands of the text and
// drops the others, but it keeps their arguments, so no word of the paper is
// lost.
type texHTML struct {
	src  string
	refs func(label string) (Ref, bool)
	out  strings.Builder
	// open is the stack of what the groups and the environments closed:
	// the HTML to write at the end of each.
	open []frame
	para bool // a paragraph is open
}

type frame struct {
	env   string // the environment, or "" for a brace group
	close string // the HTML that closes it
}

// mathEnvs are the display environments of LaTeX and what KaTeX gets for
// each. KaTeX knows aligned and gathered inside display math, so each form of
// align and gather becomes one of those.
var mathEnvs = map[string]string{
	"equation": "", "equation*": "", "displaymath": "", "math": "",
	"align": "aligned", "align*": "aligned", "flalign": "aligned", "flalign*": "aligned",
	"eqnarray": "aligned", "eqnarray*": "aligned", "alignat": "alignedat", "alignat*": "alignedat",
	"xalignat": "alignedat", "xalignat*": "alignedat",
	"gather": "gathered", "gather*": "gathered", "multline": "gathered", "multline*": "gathered",
	"dmath": "", "dmath*": "", "subequations": "",
}

// omittedEnvs are the environments that a browser cannot show from their TeX:
// pictures, diagrams and tables. The page says where they are.
var omittedEnvs = map[string]string{
	"tikzpicture": "a picture", "tikzcd": "a diagram", "xy": "a diagram", "xymatrix": "a diagram",
	"picture": "a picture", "figure": "a figure", "figure*": "a figure", "table": "a table", "table*": "a table",
	"tabular": "a table", "tabular*": "a table", "tabularx": "a table", "longtable": "a table",
	"algorithm": "an algorithm", "algorithmic": "an algorithm", "lstlisting": "a listing", "verbatim": "a listing",
	"pspicture": "a picture", "asy": "a picture", "comment": "",
}

// dropped are the commands that print no text, with the number of their
// arguments.
var dropped = map[string]int{
	`\label`: 1, `\vspace`: 1, `\vspace*`: 1, `\hspace`: 1, `\hspace*`: 1, `\index`: 1, `\addcontentsline`: 3,
	`\noindent`: 0, `\indent`: 0, `\medskip`: 0, `\smallskip`: 0, `\bigskip`: 0, `\par`: 0, `\hfill`: 0, `\centering`: 0,
	`\newline`: 0, `\linebreak`: 0, `\pagebreak`: 0, `\newpage`: 0, `\clearpage`: 0, `\nopagebreak`: 0, `\leavevmode`: 0,
	`\vskip`: 0, `\hskip`: 0, `\vfill`: 0, `\qed`: 0, `\qedhere`: 0, `\relax`: 0, `\protect`: 0, `\nonumber`: 0, `\notag`: 0,
	`\phantomsection`: 0, `\normalfont`: 0, `\upshape`: 0, `\rmfamily`: 0, `\small`: 0, `\footnotesize`: 0, `\large`: 0,
	`\Large`: 0, `\normalsize`: 0, `\scriptsize`: 0, `\hline`: 0, `\allowbreak`: 0, `\ignorespaces`: 0, `\unskip`: 0,
	`\setlength`: 2, `\setcounter`: 2, `\addtocounter`: 2, `\refstepcounter`: 1, `\stepcounter`: 1,
}

// styles are the commands of the text that take one argument and set its style.
var styles = map[string][2]string{
	`\emph`: {"<em>", "</em>"}, `\textit`: {"<em>", "</em>"}, `\textsl`: {"<em>", "</em>"},
	`\textbf`: {"<strong>", "</strong>"}, `\texttt`: {"<code>", "</code>"}, `\textsc`: {`<span class="sc">`, "</span>"},
	`\underline`: {"<u>", "</u>"}, `\uline`: {"<u>", "</u>"}, `\textsf`: {`<span class="sf">`, "</span>"},
	`\footnote`: {`<span class="fn">(`, ")</span>"}, `\mbox`: {"", ""}, `\text`: {"", ""}, `\textrm`: {"", ""},
	`\textup`: {"", ""}, `\textnormal`: {"", ""}, `\hbox`: {"", ""}, `\textmd`: {"", ""}, `\thanks`: {`<span class="fn">(`, ")</span>"},
}

// switches are the commands that set the style up to the end of the group.
var switches = map[string][2]string{
	`\em`: {"<em>", "</em>"}, `\it`: {"<em>", "</em>"}, `\itshape`: {"<em>", "</em>"}, `\sl`: {"<em>", "</em>"},
	`\slshape`: {"<em>", "</em>"}, `\bf`: {"<strong>", "</strong>"}, `\bfseries`: {"<strong>", "</strong>"},
	`\tt`: {"<code>", "</code>"}, `\ttfamily`: {"<code>", "</code>"}, `\sc`: {`<span class="sc">`, "</span>"},
	`\scshape`: {`<span class="sc">`, "</span>"},
}

// symbols are the commands of the text that print one character.
var symbols = map[string]string{
	`\%`: "%", `\&`: "&", `\#`: "#", `\_`: "_", `\$`: "$", `\{`: "{", `\}`: "}", `\ `: " ", `\,`: " ",
	`\;`: " ", `\:`: " ", `\!`: "", `\/`: "", `\-`: "", `\@`: "", `\ldots`: "…", `\dots`: "…", `\textellipsis`: "…",
	`\S`: "§", `\P`: "¶", `\dag`: "†", `\ddag`: "‡", `\copyright`: "©", `\textendash`: "–", `\textemdash`: "—",
	`\quad`: " ", `\qquad`: "  ", `\enspace`: " ", `\thinspace`: " ", `\textbackslash`: `\`, `\LaTeX`: "LaTeX",
	`\TeX`: "TeX", `\ss`: "ß", `\o`: "ø", `\O`: "Ø", `\ae`: "æ", `\AE`: "Æ", `\oe`: "œ", `\OE`: "Œ", `\aa`: "å",
	`\AA`: "Å", `\l`: "ł", `\L`: "Ł", `\i`: "ı", `\j`: "ȷ", `\textquoteright`: "’", `\textquoteleft`: "‘",
	`\cdots`: "⋯", `\textbullet`: "•", `\textdegree`: "°", `\checkmark`: "✓", `\today`: "",
}

// accents are the accent commands and the combining character of each.
var accents = map[byte]string{
	'\'': "́", '`': "̀", '^': "̂", '"': "̈", '~': "̃", '=': "̄", '.': "̇",
	'u': "̆", 'v': "̌", 'H': "̋", 'c': "̧", 'k': "̨", 'r': "̊", 'b': "̱", 'd': "̣",
}

var refCommands = map[string]bool{
	`\ref`: true, `\eqref`: true, `\cref`: true, `\Cref`: true, `\autoref`: true, `\Autoref`: true,
	`\pageref`: true, `\vref`: true, `\Vref`: true, `\nameref`: true, `\thref`: true, `\cpageref`: true, `\labelcref`: true,
}

var citeCommands = regexp.MustCompile(`^\\(cite|citep|citet|citealp|citealt|citeauthor|citeyear|Cite|Citep|Citet|parencite|textcite|autocite|footcite|nocite|cites)\*?$`)

var mathLabel = regexp.MustCompile(`\\(label|tag\*?)\s*\{[^{}]*\}|\\(nonumber|notag)\b`)

var safeURL = regexp.MustCompile(`^(https?://|mailto:)`)

// TeXToHTML converts the body of an item to HTML. refs resolves the labels of
// \ref, and it can be nil.
func TeXToHTML(src string, refs func(string) (Ref, bool)) string {
	c := &texHTML{src: src, refs: refs}
	c.run(0, len(src))
	c.closeAll()
	return strings.TrimSpace(c.out.String())
}

func (c *texHTML) startPara() {
	if !c.para {
		c.out.WriteString("<p>")
		c.para = true
	}
}

func (c *texHTML) endPara() {
	if c.para {
		c.out.WriteString("</p>")
		c.para = false
	}
}

// block writes HTML that cannot be inside a paragraph.
func (c *texHTML) block(s string) {
	c.endPara()
	c.out.WriteString(s)
}

func (c *texHTML) text(s string) {
	if strings.TrimSpace(s) == "" && !c.para {
		return
	}
	c.startPara()
	c.out.WriteString(html.EscapeString(s))
}

func (c *texHTML) closeAll() {
	for len(c.open) > 0 {
		c.pop()
	}
	c.endPara()
}

func (c *texHTML) pop() {
	f := c.open[len(c.open)-1]
	c.open = c.open[:len(c.open)-1]
	if strings.HasPrefix(f.close, "</li>") || strings.HasPrefix(f.close, "</div>") {
		c.block(f.close)
		return
	}
	c.out.WriteString(f.close)
}

// group returns the inside of the brace group at i and the offset after it,
// or ok false when no group starts at i, after spaces.
func group(s string, i int) (inner string, end int, ok bool) {
	j := i
	for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n') {
		j++
	}
	if j >= len(s) || s[j] != '{' {
		return "", i, false
	}
	depth := 0
	for k := j; k < len(s); k++ {
		switch s[k] {
		case '\\':
			k++
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[j+1 : k], k + 1, true
			}
		}
	}
	return "", i, false
}

// optional returns the inside of the [ ] group at i, after spaces.
func optional(s string, i int) (inner string, end int, ok bool) {
	j := i
	for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
		j++
	}
	if j >= len(s) || s[j] != '[' {
		return "", i, false
	}
	depth := 0
	for k := j + 1; k < len(s); k++ {
		switch s[k] {
		case '\\':
			k++
		case '{':
			depth++
		case '}':
			depth--
		case ']':
			if depth == 0 {
				return s[j+1 : k], k + 1, true
			}
		}
	}
	return "", i, false
}

// command returns the command at i, which is a backslash: a name of letters
// with an optional star, or one symbol.
func command(s string, i int) string {
	j := i + 1
	for j < len(s) && (s[j] >= 'a' && s[j] <= 'z' || s[j] >= 'A' && s[j] <= 'Z' || s[j] == '@') {
		j++
	}
	if j == i+1 {
		if j < len(s) {
			return s[i : j+1]
		}
		return s[i:j]
	}
	if j < len(s) && s[j] == '*' {
		j++
	}
	return s[i:j]
}

// mathEnd returns the offset of the end of the inline or display math that
// starts at i with the delimiter, and the offset after the closing delimiter.
func mathEnd(s string, from int, close string) (int, int, bool) {
	for k := from; k < len(s); k++ {
		if s[k] == '\\' && close != `\]` && close != `\)` {
			k++
			continue
		}
		if strings.HasPrefix(s[k:], close) {
			if close == "$" && k > 0 && s[k-1] == '\\' {
				continue
			}
			return k, k + len(close), true
		}
		if s[k] == '\\' {
			k++
		}
	}
	return 0, 0, false
}

// envEnd returns the offsets of the \end{env} that closes the environment
// whose content starts at from.
func envEnd(s string, from int, env string) (int, int, bool) {
	begin, end := `\begin{`+env+`}`, `\end{`+env+`}`
	depth := 1
	for k := from; k < len(s); k++ {
		switch {
		case strings.HasPrefix(s[k:], begin):
			depth++
			k += len(begin) - 1
		case strings.HasPrefix(s[k:], end):
			depth--
			if depth == 0 {
				return k, k + len(end), true
			}
			k += len(end) - 1
		}
	}
	return 0, 0, false
}

func (c *texHTML) math(tex string, display bool) {
	tex = strings.TrimSpace(mathLabel.ReplaceAllString(tex, ""))
	if tex == "" {
		return
	}
	if display {
		c.block(`<div class="math display">` + html.EscapeString(tex) + `</div>`)
		return
	}
	c.startPara()
	c.out.WriteString(`<span class="math">` + html.EscapeString(tex) + `</span>`)
}

// run converts s[from:to].
func (c *texHTML) run(from, to int) {
	s := c.src
	start := from
	flush := func(i int) {
		if i > start {
			c.prose(s[start:i])
		}
	}
	for i := from; i < to; {
		ch := s[i]
		switch {
		case ch == '$':
			if strings.HasPrefix(s[i:], "$$") {
				if a, b, ok := mathEnd(s[:to], i+2, "$$"); ok {
					flush(i)
					c.math(s[i+2:a], true)
					i, start = b, b
					continue
				}
			} else if a, b, ok := mathEnd(s[:to], i+1, "$"); ok {
				flush(i)
				c.math(s[i+1:a], false)
				i, start = b, b
				continue
			}
			i++
		case ch == '\\' && i+1 < to && (s[i+1] == '[' || s[i+1] == '('):
			close := `\]`
			if s[i+1] == '(' {
				close = `\)`
			}
			if a, b, ok := mathEnd(s[:to], i+2, close); ok {
				flush(i)
				c.math(s[i+2:a], close == `\]`)
				i, start = b, b
				continue
			}
			i += 2
		case ch == '\\':
			flush(i)
			i = c.control(i, to)
			start = i
		case ch == '{':
			flush(i)
			c.open = append(c.open, frame{})
			i++
			start = i
		case ch == '}':
			flush(i)
			// Close the switches of the group, then the group.
			for len(c.open) > 0 {
				f := c.open[len(c.open)-1]
				if f.env == "" && f.close == "" {
					c.open = c.open[:len(c.open)-1]
					break
				}
				if f.env != "" && f.env != "\x00switch" {
					break
				}
				c.pop()
			}
			i++
			start = i
		case ch == '\n' && i+1 < to && blankLine(s, i, to):
			flush(i)
			c.endPara()
			for i < to && (s[i] == '\n' || s[i] == ' ' || s[i] == '\t' || s[i] == '\r') {
				i++
			}
			start = i
		default:
			i++
		}
	}
	flush(to)
}

// blankLine tells if a line with only spaces follows the newline at i.
func blankLine(s string, i, to int) bool {
	for k := i + 1; k < to; k++ {
		switch s[k] {
		case ' ', '\t', '\r':
		case '\n':
			return true
		default:
			return false
		}
	}
	return false
}

var dashes = strings.NewReplacer("---", "—", "--", "–", "``", "“", "''", "”", "`", "‘", "'", "’", "~", " ", "\n", " ", "\t", " ")

// prose writes plain text.
func (c *texHTML) prose(s string) {
	s = dashes.Replace(s)
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	c.text(s)
}

// control handles the command at i and returns the offset after it.
func (c *texHTML) control(i, to int) int {
	s := c.src[:to]
	name := command(s, i)
	end := i + len(name)
	switch {
	case name == `\\`:
		// A line break, with an optional length.
		if _, e, ok := optional(s, end); ok {
			end = e
		}
		if c.para {
			c.out.WriteString("<br>")
		}
		return end
	case name == `\begin`:
		return c.begin(i, end, to)
	case name == `\end`:
		env, e, _ := group(s, end)
		for k := len(c.open) - 1; k >= 0; k-- {
			if c.open[k].env == env {
				for len(c.open) > k {
					c.pop()
				}
				break
			}
		}
		return e
	case name == `\item`:
		c.item(&end, s)
		return skipSpace(s, end)
	case len(name) == 2 && accents[name[1]] != "" && !(name[1] >= 'a' && name[1] <= 'z' || name[1] >= 'A' && name[1] <= 'Z'):
		return c.accent(name[1], end, s)
	case len(name) == 2 && (name[1] == 'c' || name[1] == 'v' || name[1] == 'u' || name[1] == 'H' || name[1] == 'k' || name[1] == 'r' || name[1] == 'b' || name[1] == 'd') && end < len(s) && (s[end] == '{' || s[end] == ' '):
		return c.accent(name[1], end, s)
	case symbols[name] != "" || name == `\today` || name == `\!` || name == `\/` || name == `\-` || name == `\@`:
		c.text(symbols[name])
		return end
	case refCommands[name]:
		arg, e, ok := group(s, end)
		if !ok {
			return end
		}
		c.ref(name, arg)
		return e
	case citeCommands.MatchString(name):
		var notes []string
		for {
			o, e, ok := optional(s, end)
			if !ok {
				break
			}
			notes, end = append(notes, o), e
		}
		keys, e, ok := group(s, end)
		if !ok {
			return end
		}
		if name == `\nocite` {
			return e
		}
		t := "[" + html.EscapeString(strings.Join(strings.FieldsFunc(keys, func(r rune) bool { return r == ',' || r == ' ' }), ", "))
		for _, n := range notes {
			if strings.TrimSpace(n) != "" {
				t += ", " + c.inner(n)
			}
		}
		c.startPara()
		c.out.WriteString(`<span class="cite">` + t + "]</span>")
		return e
	case name == `\url`:
		arg, e, ok := group(s, end)
		if ok {
			c.link(arg, arg)
		}
		return e
	case name == `\href`:
		u, e, ok := group(s, end)
		t, e2, ok2 := group(s, e)
		if ok && ok2 {
			c.link(u, t)
			return e2
		}
		return e
	case styles[name] != [2]string{}:
		arg, e, ok := group(s, end)
		if !ok {
			return end
		}
		st := styles[name]
		c.startPara()
		c.out.WriteString(st[0])
		c.open = append(c.open, frame{env: "\x00style", close: st[1]})
		sub := &texHTML{src: arg, refs: c.refs, para: true}
		sub.run(0, len(arg))
		for len(sub.open) > 0 {
			sub.pop()
		}
		c.out.WriteString(strings.TrimSuffix(sub.out.String(), "</p>"))
		c.open = c.open[:len(c.open)-1]
		c.out.WriteString(st[1])
		return e
	case switches[name] != [2]string{}:
		st := switches[name]
		c.startPara()
		c.out.WriteString(st[0])
		c.open = append(c.open, frame{env: "\x00switch", close: st[1]})
		return skipSpace(s, end)
	}
	if n, ok := dropped[name]; ok {
		if _, e, ok := optional(s, end); ok {
			end = e
		}
		for k := 0; k < n; k++ {
			_, e, ok := group(s, end)
			if !ok {
				break
			}
			end = e
		}
		return end
	}
	// An unknown command: drop the name, and keep its arguments as text.
	if _, e, ok := optional(s, end); ok {
		end = e
	}
	if len(name) > 1 && name[len(name)-1] >= 'a' && name[len(name)-1] <= 'z' {
		for end < len(s) && s[end] == ' ' {
			end++
		}
	}
	return end
}

// skipSpace returns the offset after the spaces at i, as TeX skips the spaces
// after a command.
func skipSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n') {
		i++
	}
	return i
}

func (c *texHTML) accent(a byte, end int, s string) int {
	var base string
	if arg, e, ok := group(s, end); ok {
		base, end = arg, e
	} else {
		for end < len(s) && s[end] == ' ' {
			end++
		}
		if end < len(s) {
			if s[end] == '\\' {
				name := command(s, end)
				base, end = symbols[name], end+len(name)
			} else {
				base, end = s[end:end+1], end+1
			}
		}
	}
	if base == `\i` {
		base = "i"
	}
	c.text(base + accents[a])
	return end
}

// inner converts a short text inside the text, as the note of a citation,
// and returns its HTML with no paragraph.
func (c *texHTML) inner(tex string) string {
	sub := &texHTML{src: tex, refs: c.refs, para: true}
	sub.run(0, len(tex))
	sub.closeAll()
	return strings.TrimSuffix(sub.out.String(), "</p>")
}

func (c *texHTML) link(u, t string) {
	u = strings.TrimSpace(u)
	c.startPara()
	if !safeURL.MatchString(u) {
		c.out.WriteString(html.EscapeString(t))
		return
	}
	c.out.WriteString(`<a href="` + html.EscapeString(u) + `" rel="nofollow">` + html.EscapeString(t) + `</a>`)
}

func (c *texHTML) ref(name, arg string) {
	c.startPara()
	labels := strings.Split(arg, ",")
	for k, l := range labels {
		l = strings.TrimSpace(l)
		if k > 0 {
			c.out.WriteString(", ")
		}
		var r Ref
		ok := false
		if c.refs != nil {
			r, ok = c.refs(l)
		}
		if !ok {
			t := "?"
			if name == `\eqref` {
				t = "(?)"
			}
			c.out.WriteString(`<span class="ref missing" title="` + html.EscapeString(l) + `">` + t + `</span>`)
			continue
		}
		t := r.Text
		switch name {
		case `\eqref`:
			t = "(" + t + ")"
		case `\cref`, `\Cref`, `\autoref`, `\Autoref`, `\vref`, `\Vref`:
			if r.Kind != "" {
				t = r.Kind + " " + t
			}
		}
		c.out.WriteString(`<a class="ref" href="` + html.EscapeString(r.Href) + `">` + html.EscapeString(t) + `</a>`)
	}
}

func (c *texHTML) item(end *int, s string) {
	label, e, ok := optional(s, *end)
	if ok {
		*end = e
	}
	// Close the item before this one.
	for k := len(c.open) - 1; k >= 0; k-- {
		if c.open[k].env == "\x00item" {
			for len(c.open) > k {
				c.pop()
			}
			break
		}
		if c.open[k].env == "itemize" || c.open[k].env == "enumerate" || c.open[k].env == "description" {
			break
		}
	}
	c.block("<li>")
	c.open = append(c.open, frame{env: "\x00item", close: "</li>"})
	if ok {
		sub := &texHTML{src: label, refs: c.refs, para: true}
		sub.run(0, len(label))
		sub.closeAll()
		c.startPara()
		c.out.WriteString(`<span class="label">` + strings.TrimSuffix(sub.out.String(), "</p>") + `</span> `)
	}
}

// begin handles \begin{env} at i.
func (c *texHTML) begin(i, end, to int) int {
	s := c.src[:to]
	env, e, ok := group(s, end)
	if !ok {
		return end
	}
	if inner, ok := mathEnvs[env]; ok {
		a, b, ok := envEnd(s, e, env)
		if !ok {
			return e
		}
		body := s[e:a]
		if inner == "alignedat" {
			if _, e2, ok := group(body, 0); ok {
				n, _, _ := group(body, 0)
				body = `\begin{alignedat}{` + n + `}` + body[e2:] + `\end{alignedat}`
			}
		} else if inner != "" {
			if env == "eqnarray" || env == "eqnarray*" {
				body = strings.ReplaceAll(body, "&=&", "&=")
			}
			body = `\begin{` + inner + `}` + body + `\end{` + inner + `}`
		}
		c.math(body, true)
		return b
	}
	if what, ok := omittedEnvs[env]; ok {
		_, b, ok := envEnd(s, e, env)
		if !ok {
			return e
		}
		if what != "" {
			c.block(`<div class="omitted">The paper has ` + what + ` here, which this page does not show.</div>`)
		}
		return b
	}
	switch env {
	case "itemize", "enumerate", "description":
		if _, e2, ok := optional(s, e); ok {
			e = e2
		}
		tag := map[string]string{"itemize": "ul", "enumerate": "ol", "description": "ul class=\"desc\""}[env]
		c.block("<" + tag + ">")
		c.open = append(c.open, frame{env: env, close: "</" + strings.Fields(tag)[0] + ">"})
		return e
	case "center", "flushleft", "flushright", "quote", "quotation", "minipage", "small", "footnotesize":
		if _, e2, ok := optional(s, e); ok {
			e = e2
		}
		if env == "minipage" {
			if _, e2, ok := group(s, e); ok {
				e = e2
			}
		}
		c.block(`<div class="` + env + `">`)
		c.open = append(c.open, frame{env: env, close: "</div>"})
		return e
	}
	// Another environment, as a theorem inside a proof: a block with its head.
	head := strings.TrimSuffix(env, "*")
	if len(head) > 0 {
		head = strings.ToUpper(head[:1]) + head[1:]
	}
	name, e2, ok := optional(s, e)
	if ok {
		e = e2
	}
	c.block(`<div class="inner"><span class="inner-head">` + html.EscapeString(head))
	if ok {
		sub := &texHTML{src: name, refs: c.refs, para: true}
		sub.run(0, len(name))
		sub.closeAll()
		c.out.WriteString(" (" + strings.TrimSuffix(sub.out.String(), "</p>") + ")")
	}
	c.out.WriteString(".</span> ")
	c.open = append(c.open, frame{env: env, close: "</div>"})
	return e
}

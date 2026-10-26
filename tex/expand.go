package tex

import (
	"bytes"
	"path"
	"regexp"
	"sort"
	"strings"
)

// segment says which file and line one stretch of the expanded text comes from.
type segment struct {
	off  int
	file string
	line int
}

// Pos is a place in a file of the submission.
type Pos struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

// maxDepth is the deepest chain of inclusions that the front end follows.
const maxDepth = 16

// maxExpanded is the largest expanded text that the front end makes, in bytes.
const maxExpanded = 64 << 20

// includeCmd finds the commands that read another file in place.
//
// The forms are \input{x}, \input x, \include{x}, \subfile{x}, and the two
// argument forms of the import package. The second group is the directory of
// \import and \subimport.
var includeCmd = regexp.MustCompile(`\\(input|include|subfile|import|subimport|inputfrom|subinputfrom|includefrom|subincludefrom)\b(?:\s*\{([^{}]*)\})?\s*(?:\{([^{}]*)\}|([A-Za-z0-9_./-]+))`)

// expander joins the files of a submission into one text.
type expander struct {
	files   map[string][]byte
	out     bytes.Buffer
	segs    []segment
	seen    map[string]bool
	missing []string
	read    []string
	tooBig  bool
}

// lineAt is the line number of offset i in b, from 1.
func lineAt(b []byte, i int) int {
	return bytes.Count(b[:i], []byte("\n")) + 1
}

func (e *expander) mark(file string, line int) {
	e.segs = append(e.segs, segment{off: e.out.Len(), file: file, line: line})
}

// expand writes one file into the output, with its inclusions in their place.
func (e *expander) expand(name string, depth int) {
	b := e.files[name]
	e.read = append(e.read, name)
	e.mark(name, 1)
	at := 0
	for _, m := range includeCmd.FindAllSubmatchIndex(b, -1) {
		if e.out.Len() > maxExpanded {
			e.tooBig = true
			return
		}
		cmd := string(b[m[2]:m[3]])
		dir := ""
		if m[4] >= 0 {
			dir = strings.TrimSpace(string(b[m[4]:m[5]]))
		}
		var arg string
		switch {
		case m[6] >= 0:
			arg = string(b[m[6]:m[7]])
		case m[8] >= 0:
			arg = string(b[m[8]:m[9]])
		}
		arg = strings.TrimSpace(arg)
		twoArg := strings.Contains(cmd, "import") || strings.HasSuffix(cmd, "from")
		if twoArg && m[4] < 0 {
			// \import needs a directory and a file. One argument is not this form.
			continue
		}
		if !twoArg && m[4] >= 0 {
			// \input{a}{b}: the second group is not an argument of \input.
			// Read the first group as the file and leave the rest as text.
			arg, dir = dir, ""
		}
		if arg == "" {
			continue
		}
		target, ok := e.resolve(name, dir, arg)
		if !ok {
			if depth < maxDepth && !strings.HasSuffix(arg, ".sty") && !strings.HasSuffix(arg, ".cls") {
				e.missing = append(e.missing, arg)
			}
			continue
		}
		if e.seen[target] || depth >= maxDepth {
			continue
		}
		e.out.Write(b[at:m[0]])
		e.seen[target] = true
		e.out.WriteString("\n")
		e.expand(target, depth+1)
		e.out.WriteString("\n")
		delete(e.seen, target)
		at = m[1]
		e.mark(name, lineAt(b, at))
	}
	e.out.Write(b[at:])
}

// resolve finds the file that an inclusion names.
//
// TeX looks for the name as written and then with .tex added. It looks in the
// directory of the main file, and a careful author also writes paths from the
// directory of the including file. Both are tried.
func (e *expander) resolve(from, dir, arg string) (string, bool) {
	arg = strings.Trim(arg, `"`)
	var bases []string
	if dir != "" {
		bases = append(bases, path.Join(path.Dir(from), dir), dir)
	} else {
		bases = append(bases, "", path.Dir(from))
	}
	for _, base := range bases {
		for _, name := range []string{arg, arg + ".tex", arg + ".ltx"} {
			p := path.Clean(path.Join(base, name))
			if _, ok := e.files[p]; ok && isTeXFile(p) {
				return p, true
			}
		}
	}
	return "", false
}

func isTeXFile(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".tex", ".ltx", ".txt", "":
		return true
	}
	return false
}

// pos maps an offset in the expanded text to a file and a line.
func pos(segs []segment, text []byte, off int) Pos {
	i := sort.Search(len(segs), func(i int) bool { return segs[i].off > off }) - 1
	if i < 0 {
		return Pos{}
	}
	s := segs[i]
	if off > len(text) {
		off = len(text)
	}
	return Pos{File: s.file, Line: s.line + bytes.Count(text[s.off:off], []byte("\n"))}
}

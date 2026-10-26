package tex

import (
	"bytes"
	"regexp"
	"strings"
)

// verbatimEnvs are the environments whose body TeX does not read as TeX.
//
// No item is found inside them. The body is blanked, and the begin and end
// lines stay, so that the tree still shows the environment.
var verbatimEnvs = map[string]bool{
	"verbatim": true, "verbatim*": true, "Verbatim": true, "Verbatim*": true,
	"BVerbatim": true, "LVerbatim": true, "lstlisting": true, "minted": true,
	"filecontents": true, "filecontents*": true, "code": true, "spverbatim": true,
	"comment": true,
}

// verbCommands are the inline forms that take a delimiter.
var verbCommands = []string{`\verb*`, `\verb`, `\lstinline`, `\mintinline`}

// blank replaces each byte of b[from:to] with a space, but keeps newlines.
//
// The length of the text and the place of each line do not change. So an
// offset into the cleaned text is an offset into the file, and a line number
// stays correct.
func blank(b []byte, from, to int) {
	if from < 0 {
		from = 0
	}
	if to > len(b) {
		to = len(b)
	}
	for i := from; i < to; i++ {
		if b[i] != '\n' && b[i] != '\r' {
			b[i] = ' '
		}
	}
}

// escaped tells if the byte at i has an odd number of backslashes before it.
func escaped(b []byte, i int) bool {
	n := 0
	for j := i - 1; j >= 0 && b[j] == '\\'; j-- {
		n++
	}
	return n%2 == 1
}

// Clean removes from one file what does not print.
//
// It removes comments, the bodies of verbatim environments, inline verbatim,
// the bodies of the comment environments in excluded, and all text after
// \endinput. It does these in one pass from left to right, because each one
// can hide the others: a % in a verbatim body is not a comment, and a
// \begin{verbatim} after a % is not verbatim.
//
// The result has the same length as the input. Only spaces replace text.
func Clean(src []byte, excluded map[string]bool) []byte {
	b := append([]byte(nil), src...)
	for i := 0; i < len(b); i++ {
		switch b[i] {
		case '%':
			if escaped(b, i) {
				continue
			}
			j := bytes.IndexByte(b[i:], '\n')
			if j < 0 {
				j = len(b) - i
			}
			blank(b, i, i+j)
			i += j
		case '\\':
			if escaped(b, i) {
				continue
			}
			rest := b[i:]
			if bytes.HasPrefix(rest, []byte(`\endinput`)) && !letterAt(b, i+len(`\endinput`)) {
				blank(b, i, len(b))
				return b
			}
			if name, end, ok := beginAt(b, i); ok && (verbatimEnvs[name] || excluded[name]) {
				closer := []byte(`\end{` + name + `}`)
				k := bytes.Index(b[end:], closer)
				if k < 0 {
					blank(b, end, len(b))
					return b
				}
				blank(b, end, end+k)
				i = end + k + len(closer) - 1
				continue
			}
			if n := verbAt(b, i); n > 0 {
				i = n - 1
			}
		}
	}
	return b
}

// letterAt tells if b[i] is a letter, which would make it part of a command name.
func letterAt(b []byte, i int) bool {
	return i < len(b) && isLetter(b[i])
}

func isLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '@'
}

// beginAt reads \begin{name} at i and returns the name and the offset after it.
func beginAt(b []byte, i int) (string, int, bool) {
	const begin = `\begin`
	if !bytes.HasPrefix(b[i:], []byte(begin)) || letterAt(b, i+len(begin)) {
		return "", 0, false
	}
	j := skipSpace(b, i+len(begin))
	if j >= len(b) || b[j] != '{' {
		return "", 0, false
	}
	k := bytes.IndexByte(b[j:], '}')
	if k < 0 || k > 64 {
		return "", 0, false
	}
	return strings.TrimSpace(string(b[j+1 : j+k])), j + k + 1, true
}

func skipSpace(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	return i
}

// verbAt blanks one inline verbatim at i and returns the offset after it, or 0.
func verbAt(b []byte, i int) int {
	for _, cmd := range verbCommands {
		if !bytes.HasPrefix(b[i:], []byte(cmd)) || letterAt(b, i+len(cmd)) {
			continue
		}
		j := i + len(cmd)
		if cmd == `\mintinline` {
			// \mintinline{lang}{code} or \mintinline{lang}|code|.
			if j < len(b) && b[j] == '{' {
				if k := bytes.IndexByte(b[j:], '}'); k > 0 {
					j += k + 1
				}
			}
		}
		if cmd == `\lstinline` && j < len(b) && b[j] == '[' {
			if k := bytes.IndexByte(b[j:], ']'); k > 0 {
				j += k + 1
			}
		}
		if j >= len(b) || b[j] == '\n' || b[j] == ' ' && cmd == `\verb` {
			return 0
		}
		delim := b[j]
		if delim == '{' {
			delim = '}'
		}
		k := bytes.IndexByte(b[j+1:], delim)
		nl := bytes.IndexByte(b[j+1:], '\n')
		if k < 0 || (nl >= 0 && nl < k) {
			return 0
		}
		blank(b, j+1, j+1+k)
		return j + 2 + k
	}
	return 0
}

// excludeDecl finds the names that \excludecomment declares as comments.
var excludeDecl = regexp.MustCompile(`\\excludecomment\s*\{([^}]+)\}`)

// Excluded reads the comment environments that a paper declares.
//
// It reads the raw files, because the declaration can be in any of them and
// the cleaning of each file needs the full list first.
func Excluded(files ...[]byte) map[string]bool {
	out := map[string]bool{}
	for _, f := range files {
		for _, m := range excludeDecl.FindAllSubmatch(f, -1) {
			for name := range strings.SplitSeq(string(m[1]), ",") {
				if name = strings.TrimSpace(name); name != "" {
					out[name] = true
				}
			}
		}
	}
	return out
}

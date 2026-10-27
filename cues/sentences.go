package cues

import (
	"bytes"
	"strings"

	"github.com/tamnd/arxiv-reader/tex"
)

// abbreviations end with a period that does not end a sentence.
var abbreviations = map[string]bool{
	"e.g": true, "i.e": true, "cf": true, "resp": true, "etc": true, "al": true, "viz": true, "vs": true,
	"thm": true, "prop": true, "lem": true, "cor": true, "def": true, "rem": true, "ex": true, "eq": true,
	"eqs": true, "sec": true, "secs": true, "fig": true, "figs": true, "ch": true, "chap": true, "app": true,
	"no": true, "nos": true, "ref": true, "refs": true, "p": true, "pp": true, "vol": true, "ed": true,
	"eds": true, "dr": true, "prof": true, "mr": true, "st": true, "approx": true, "const": true, "conj": true,
}

// sentences splits text[from:to] into sentences. A masked byte, a blank line
// and \par end a sentence. A period, a question mark or an exclamation mark
// ends a sentence when a space follows it, when it is not in inline math, and
// when the word before it is not an abbreviation or a single letter.
func sentences(text []byte, from, to int, mask []bool) []tex.Region {
	var out []tex.Region
	start := -1
	math := false
	end := func(at int) {
		if start >= 0 {
			s, e := start, at
			for s < e && isSpace(text[s]) {
				s++
			}
			for e > s && isSpace(text[e-1]) {
				e--
			}
			if e > s && bytes.ContainsFunc(text[s:e], isLetter) {
				out = append(out, tex.Region{From: s, To: e})
			}
		}
		start, math = -1, false
	}
	for i := from; i < to; i++ {
		if mask[i-from] {
			end(i)
			continue
		}
		ch := text[i]
		if start < 0 {
			if isSpace(ch) {
				continue
			}
			start = i
		}
		switch {
		case ch == '\\' && i+1 < to:
			if bytes.HasPrefix(text[i:to], []byte(`\par`)) && (i+4 == to || !isASCIILetter(text[i+4])) {
				end(i)
				i += 3
				continue
			}
			i++ // the next byte is escaped
		case ch == '$':
			math = !math
		case ch == '\n':
			if j := i + 1; j < to && blankLine(text[j:to]) {
				end(i)
			}
		case (ch == '.' || ch == '?' || ch == '!') && !math:
			if i+1 < to && !isSpace(text[i+1]) {
				continue
			}
			if ch == '.' && abbreviated(text[start:i]) {
				continue
			}
			end(i + 1)
		}
	}
	end(to)
	return out
}

// blankLine tells if b starts with a line of spaces only.
func blankLine(b []byte) bool {
	for _, c := range b {
		switch c {
		case '\n':
			return true
		case ' ', '\t', '\r':
		default:
			return false
		}
	}
	return false
}

// abbreviated tells if the text ends with an abbreviation or with one letter.
func abbreviated(b []byte) bool {
	i := len(b)
	for i > 0 && (isASCIILetter(b[i-1]) || b[i-1] == '.') {
		i--
	}
	w := strings.ToLower(string(b[i:]))
	return len(w) == 1 || abbreviations[w]
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func isASCIILetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func isLetter(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= 0x80 }

// Package bib reads the bibliography of a paper from its TeX source, for the
// cited results of AX5.7 step 1.
//
// A citation in the text names a key, and the edges of AX06 point to
// <paper>#bib:<key>. This package gives each key the identifiers that the entry
// prints: the arXiv id, the DOI and the year. The importer of math2 uses them
// to find the cited paper in the corpus. An entry gives no text here, so the
// records are the same for each licence class (rule D06).
//
// It reads three forms: \bibitem in a thebibliography environment, in a .tex
// or a .bbl file; the \entry blocks of a biblatex .bbl file; and the @entries
// of a .bib file. A .bib file often has more entries than the paper cites, so
// its entries only add to the keys of the other forms, or stand alone when the
// source has no other form.
package bib

import (
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/tamnd/arxiv-reader/source"
)

// Entry is one entry of the bibliography.
type Entry struct {
	Key   string `json:"key"`
	ArXiv string `json:"arxiv,omitempty"`
	DOI   string `json:"doi,omitempty"`
	Year  int    `json:"year,omitempty"`
	// From is the form that gave the entry: bibitem, biblatex or bibtex.
	From string `json:"from"`
}

// Record is one entry of a paper, as the batch writes it to analysis/bib/.
type Record struct {
	Paper   string `json:"paper"`
	Version int    `json:"version"`
	Entry
}

// FromSource gives the entries of the files of one e-print, in the order of
// their keys.
func FromSource(files []source.File) []Entry {
	byKey := map[string]*Entry{}
	var bibtex []Entry
	for _, f := range files {
		switch strings.ToLower(path.Ext(f.Name)) {
		case ".tex", ".ltx", ".latex", ".bbl", "":
			for _, e := range append(bibitems(f.Data), biblatex(f.Data)...) {
				if old, ok := byKey[e.Key]; ok {
					fill(old, e)
					continue
				}
				byKey[e.Key] = &e
			}
		case ".bib":
			bibtex = append(bibtex, bibtexEntries(f.Data)...)
		}
	}
	alone := len(byKey) == 0
	for _, e := range bibtex {
		if old, ok := byKey[e.Key]; ok {
			fill(old, e)
		} else if alone {
			byKey[e.Key] = &e
		}
	}
	out := make([]Entry, 0, len(byKey))
	for _, e := range byKey {
		out = append(out, *e)
	}
	slices.SortFunc(out, func(a, b Entry) int { return strings.Compare(a.Key, b.Key) })
	return out
}

// fill sets the empty fields of e from f.
func fill(e *Entry, f Entry) {
	if e.ArXiv == "" {
		e.ArXiv = f.ArXiv
	}
	if e.DOI == "" {
		e.DOI = f.DOI
	}
	if e.Year == 0 {
		e.Year = f.Year
	}
}

var (
	bibitemAt  = regexp.MustCompile(`\\bibitem\b`)
	endBib     = regexp.MustCompile(`\\end\s*\{thebibliography\}`)
	newID      = regexp.MustCompile(`(?i)(?:arxiv\}?[\s:./{]*(?:abs/|pdf/)?|eprint\}?[\s:{=]*|arxiv\.org/(?:abs|pdf)/)\s*(\d{4}\.\d{4,5})(?:v\d+)?`)
	oldID      = regexp.MustCompile(`\b((?:math|math-ph|hep-th|hep-ph|hep-ex|hep-lat|gr-qc|quant-ph|alg-geom|dg-ga|funct-an|q-alg|cond-mat|nlin|physics|cs|chao-dyn|solv-int|patt-sol|adap-org|cmp-lg|comp-gas|mtrl-th|supr-con|chem-ph|atom-ph|astro-ph|nucl-th|nucl-ex|acc-phys|ao-sci|bayes-an|plasm-ph)(?:\.[A-Z]{2})?/\d{7})(?:v\d+)?\b`)
	doiIn      = regexp.MustCompile(`(?i)\b(10\.\d{4,9}/[^\s{}"<>,\\]+(?:\\_[^\s{}"<>,\\]+)*)`)
	yearIn     = regexp.MustCompile(`\b(1[89]\d\d|20[0-4]\d)\b`)
	entryAt    = regexp.MustCompile(`\\entry\s*\{([^{}]+)\}`)
	endEntry   = regexp.MustCompile(`\\endentry\b`)
	fieldIn    = regexp.MustCompile(`\\field\s*\{(eprint|doi|year|date)\}\s*\{([^{}]*)\}`)
	verbIn     = regexp.MustCompile(`\\verb\s*\{(eprint|doi|url)\}\s*\\verb\s+([^\n]*)`)
	bibtexAt   = regexp.MustCompile(`@([a-zA-Z]+)\s*[{(]\s*([^,\s{}]+)\s*,`)
	bibtexYear = regexp.MustCompile(`(?i)\byear\s*=\s*[{"]?\s*(\d{4})`)
)

// bibitems reads the \bibitem entries of a file.
func bibitems(b []byte) []Entry {
	s := uncomment(string(b))
	at := bibitemAt.FindAllStringIndex(s, -1)
	var out []Entry
	for i, m := range at {
		end := len(s)
		if i+1 < len(at) {
			end = at[i+1][0]
		}
		if e := endBib.FindStringIndex(s[m[1]:end]); e != nil {
			end = m[1] + e[0]
		}
		rest := s[m[1]:end]
		rest = skipSpace(rest)
		if strings.HasPrefix(rest, "[") {
			rest = skipSpace(rest[group(rest, '[', ']'):])
		}
		if !strings.HasPrefix(rest, "{") {
			continue
		}
		n := group(rest, '{', '}')
		key := strings.TrimSpace(rest[1 : n-1])
		if key == "" || strings.ContainsAny(key, "{}\\") {
			continue
		}
		e := idsOf(rest[n:])
		e.Key, e.From = key, "bibitem"
		out = append(out, e)
	}
	return out
}

// biblatex reads the \entry blocks of a biblatex .bbl file.
func biblatex(b []byte) []Entry {
	s := string(b)
	var out []Entry
	for _, m := range entryAt.FindAllStringSubmatchIndex(s, -1) {
		body := s[m[1]:]
		if e := endEntry.FindStringIndex(body); e != nil {
			body = body[:e[0]]
		}
		e := Entry{Key: strings.TrimSpace(s[m[2]:m[3]]), From: "biblatex"}
		var text []string
		for _, f := range fieldIn.FindAllStringSubmatch(body, -1) {
			switch f[1] {
			case "year", "date":
				if y := yearIn.FindString(f[2]); y != "" && e.Year == 0 {
					e.Year, _ = strconv.Atoi(y)
				}
			case "eprint":
				text = append(text, "eprint "+f[2])
			case "doi":
				text = append(text, f[2])
			}
		}
		for _, f := range verbIn.FindAllStringSubmatch(body, -1) {
			if f[1] == "eprint" {
				text = append(text, "eprint "+f[2])
			} else {
				text = append(text, f[2])
			}
		}
		ids := idsOf(strings.Join(text, " "))
		e.ArXiv, e.DOI = ids.ArXiv, ids.DOI
		out = append(out, e)
	}
	return out
}

// bibtexEntries reads the entries of a .bib file.
func bibtexEntries(b []byte) []Entry {
	s := string(b)
	var out []Entry
	for _, m := range bibtexAt.FindAllStringSubmatchIndex(s, -1) {
		kind := strings.ToLower(s[m[2]:m[3]])
		if kind == "string" || kind == "comment" || kind == "preamble" {
			continue
		}
		open := m[3]
		for open < len(s) && s[open] != '{' && s[open] != '(' {
			open++
		}
		end := len(s)
		if open < len(s) && s[open] == '{' {
			end = open + group(s[open:], '{', '}')
		}
		body := s[m[1]:end]
		e := idsOf(body)
		e.Key, e.From, e.Year = strings.TrimSpace(s[m[4]:m[5]]), "bibtex", 0
		if y := bibtexYear.FindStringSubmatch(body); y != nil {
			e.Year, _ = strconv.Atoi(y[1])
		}
		out = append(out, e)
	}
	return out
}

// idsOf finds the arXiv id, the DOI and the year in the text of an entry.
func idsOf(text string) Entry {
	var e Entry
	if m := newID.FindStringSubmatch(text); m != nil {
		e.ArXiv = m[1]
	} else if m := oldID.FindStringSubmatch(text); m != nil {
		e.ArXiv = m[1]
	}
	if m := doiIn.FindStringSubmatch(text); m != nil {
		d := strings.ReplaceAll(m[1], `\_`, "_")
		e.DOI = strings.ToLower(strings.TrimRight(d, ".;:)]"))
	}
	e.Year = yearOf(newID.ReplaceAllString(oldID.ReplaceAllString(doiIn.ReplaceAllString(text, " "), " "), " "))
	return e
}

var yearInParens = regexp.MustCompile(`\(([^()]*[\s.,])?(1[89]\d\d|20[0-4]\d)\)`)

// yearOf gives the year of the text of an entry with no identifiers: the
// first year in parentheses, as in "41 (1990)", or else the last year that is
// not in a range of pages, as the 1935 of "1932--1935".
func yearOf(text string) int {
	if m := yearInParens.FindStringSubmatch(text); m != nil {
		y, _ := strconv.Atoi(m[2])
		return y
	}
	y := 0
	for _, m := range yearIn.FindAllStringIndex(text, -1) {
		if m[0] > 0 && text[m[0]-1] == '-' || m[1] < len(text) && text[m[1]] == '-' {
			continue
		}
		y, _ = strconv.Atoi(text[m[0]:m[1]])
	}
	return y
}

// group gives the length of the group that s starts with, from the open
// bracket to its close bracket, with nested groups and escaped brackets. An
// unclosed group gives the length of s.
func group(s string, open, close byte) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(s)
}

// uncomment removes the TeX comments: each % that no backslash escapes, to
// the end of its line.
func uncomment(s string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(s, "\n") {
		for i := 0; i < len(line); i++ {
			if line[i] == '\\' {
				i++
				continue
			}
			if line[i] == '%' {
				line = line[:i]
				break
			}
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func skipSpace(s string) string { return strings.TrimLeft(s, " \t\r\n") }

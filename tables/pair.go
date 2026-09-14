// Package tables reads the two files a table is kept as and says whether they
// agree.
//
// A table is written twice on purpose. The Markdown is what a reader sees and
// what a translator works on, and it is lossy: it cannot express a multi row
// span, a multi column header or a rule drawn under part of a row, and academic
// tables use all three constantly. The markup is what the loss is measured
// against.
//
// This package is the measurement, and it is a package rather than a function
// inside ax tables so that the command and audit rules F11 and F12 read the
// same code. A rule and the command it checks that disagree about what agreeing
// means is worse than having neither.
package tables

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Problem is what is wrong with one pair of files.
type Problem struct {
	// Rule is F11 for a table that is not kept twice and F12 for two files that
	// do not say the same thing.
	Rule string
	// What is the problem, as a phrase that reads after the table's name.
	What string
}

func (p Problem) String() string { return p.Rule + ": " + p.What }

// Names is every table in a directory, by the stem of its files.
func Names(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		if ext != ".md" && ext != ".tex" {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ext)
		if !seen[stem] {
			seen[stem] = true
			out = append(out, stem)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Agree runs both rules over one table and gives back what is wrong, or nil
// when nothing is.
//
// F11 first, because a pair with one half missing has nothing to compare, and
// then F12 over the row count, the column count and every numeric cell. The
// numbers are the part that matters: a table is where a paper's measured
// results live, and a representation that quietly reformats one of them is
// worse than no second representation at all.
func Agree(dir, name string) (*Problem, error) {
	md, mderr := os.ReadFile(filepath.Join(dir, name+".md"))
	tx, txerr := os.ReadFile(filepath.Join(dir, name+".tex"))
	switch {
	case mderr != nil:
		return &Problem{Rule: "F11", What: "has no " + name + ".md, and a table is kept twice"}, nil
	case txerr != nil:
		return &Problem{Rule: "F11", What: "has no " + name + ".tex, and a table is kept twice"}, nil
	}
	mdRows, txRows := mdCells(string(md)), texCells(string(tx))
	if len(mdRows) != len(txRows) {
		return f12("the Markdown has %d rows and the markup has %d", len(mdRows), len(txRows)), nil
	}
	mdCols := 0
	for _, r := range mdRows {
		if len(r) > mdCols {
			mdCols = len(r)
		}
	}
	if n := texCols(string(tx)); n != mdCols {
		return f12("the Markdown has %d columns and the markup has %d", mdCols, n), nil
	}
	a, b := numbers(mdRows), numbers(txRows)
	if len(a) != len(b) {
		return f12("the Markdown holds %d numbers and the markup holds %d", len(a), len(b)), nil
	}
	for i := range a {
		if a[i] != b[i] {
			return f12("number %d is %s in the Markdown and %s in the markup", i+1, a[i], b[i]), nil
		}
	}
	return nil, nil
}

func f12(what string, args ...any) *Problem {
	return &Problem{Rule: "F12", What: fmt.Sprintf(what, args...)}
}

// Count is how many tables a body holds.
//
// A run of consecutive lines opening with a pipe is one table, which is the
// shape the emitter writes and is the only shape in a body, because the same
// emitter writes the section and the .md file. It is here rather than in the
// audit so that the thing that counts a paper's tables and the thing that
// writes them are reading the same definition.
func Count(body string) int {
	n, in := 0, false
	for _, line := range strings.Split(body, "\n") {
		row := strings.HasPrefix(strings.TrimSpace(line), "|")
		if row && !in {
			n++
		}
		in = row
	}
	return n
}

// mdCells is the cells of a Markdown table, row by row.
//
// The rule row is not a row of the table, and neither is the empty header the
// emitter puts in front of a table whose first row was not one. Both are
// artefacts of Markdown needing a header, and counting them would make the two
// representations disagree about a table they agree about.
func mdCells(s string) [][]string {
	var out [][]string
	for i, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := mdSplit(line)
		if i == 1 && isRule(cells) {
			continue
		}
		if i == 0 && blank(cells) {
			continue
		}
		out = append(out, cells)
	}
	return out
}

// mdSplit is one Markdown row's cells, cut at the pipes that separate cells and
// not at the ones inside them.
//
// A pipe in a cell is written \|, because an unescaped one would end the cell,
// and mathematics uses the pipe for absolute value constantly. KAN's table 4
// has one in a formula and splitting on it read a six column table as eight.
// The escape comes off on the way out, since the markup file has the pipe
// itself and the two are compared cell by cell.
func mdSplit(line string) []string {
	var cells []string
	var b strings.Builder
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '\\' && i+1 < len(line) && line[i+1] == '|':
			b.WriteByte('|')
			i++
		case line[i] == '|':
			cells = append(cells, strings.TrimSpace(b.String()))
			b.Reset()
		default:
			b.WriteByte(line[i])
		}
	}
	cells = append(cells, strings.TrimSpace(b.String()))
	// A row opens and closes with a pipe, so the first piece and the last one
	// are what lies outside the table rather than cells of it.
	if len(cells) >= 2 {
		cells = cells[1 : len(cells)-1]
	}
	return cells
}

func isRule(cells []string) bool {
	for _, c := range cells {
		if c == "" || strings.Trim(c, ":-") != "" {
			return false
		}
	}
	return len(cells) > 0
}

func blank(cells []string) bool {
	for _, c := range cells {
		if c != "" {
			return false
		}
	}
	return true
}

// texCells is the cells of a tabular, row by row.
//
// Comments go first, because the header the writer puts on says which table it
// is and which paper it came from, and a paper number read as a measurement
// would fail rule F12 on every table in the corpus.
func texCells(s string) [][]string {
	body := s
	if i := strings.Index(body, "}\n"); i >= 0 && strings.Contains(body[:i], "\\begin{tabular}") {
		body = body[i+2:]
	}
	if i := strings.Index(body, "\\end{tabular}"); i >= 0 {
		body = body[:i]
	}
	var out [][]string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "%") || line == "\\hline" {
			continue
		}
		line = strings.TrimSuffix(line, "\\\\")
		cells := strings.Split(line, "&")
		for j := range cells {
			cells[j] = plainTeX(strings.TrimSpace(cells[j]))
		}
		out = append(out, cells)
	}
	return out
}

// plainTeX strips the markup off a cell and leaves what it says.
//
// The counted arguments of multicolumn and multirow go with it. Those are the
// shape of the table and not a measurement in it, and a span of 2 read as a
// number is the difference between the two files agreeing and not.
//
// Mathematics is left exactly as it stands, because the emitter leaves it
// exactly as it stands: the formula in the markup file is the same bytes as the
// formula in the Markdown file, so the two only match when neither side is
// touched. Stripping it is also wrong on its own terms. A \frac{1}{2} with the
// macro and the braces taken out reads as the number twelve, which is not a
// number anywhere in the paper.
func plainTeX(s string) string {
	for _, m := range []string{"multicolumn", "multirow"} {
		for {
			i := strings.Index(s, "\\"+m+"{")
			if i < 0 {
				break
			}
			rest := s[i+len(m)+2:]
			// Two braced arguments, then the text in the third.
			for n := 0; n < 2; n++ {
				j := strings.IndexByte(rest, '}')
				if j < 0 {
					return s
				}
				rest = rest[j+1:]
				if !strings.HasPrefix(rest, "{") {
					return s
				}
				rest = rest[1:]
			}
			s = s[:i] + rest
		}
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '$' {
			if j := strings.IndexByte(s[i+1:], '$'); j >= 0 {
				b.WriteString(s[i : i+1+j+1])
				i += 1 + j + 1
				continue
			}
		}
		if s[i] == '\\' && i+1 < len(s) {
			// A backslash before a letter starts a macro and the whole name
			// goes. A backslash before anything else is protecting that
			// character, which is the ampersand and the per cent sign a cell
			// is full of, so the character stays and the backslash does not.
			if !letter(s[i+1]) {
				b.WriteByte(s[i+1])
				i += 2
				continue
			}
			j := i + 1
			for j < len(s) && letter(s[j]) {
				j++
			}
			i = j
			continue
		}
		if s[i] == '{' || s[i] == '}' {
			i++
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func letter(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }

// texCols is the number of columns a tabular covers, read out of its
// specification.
func texCols(s string) int {
	n := 0
	if i := strings.Index(s, "\\begin{tabular}{"); i >= 0 {
		rest := s[i+len("\\begin{tabular}{"):]
		if j := strings.IndexByte(rest, '}'); j >= 0 {
			for _, r := range rest[:j] {
				if r == 'l' || r == 'c' || r == 'r' {
					n++
				}
			}
		}
	}
	return n
}

// numbers is every number in a set of cells, in order.
//
// A number is a run of digits with an optional decimal point, which is what a
// results table is full of. Read cell by cell rather than off the whole file,
// because the two files lay the same table out differently by design and the
// question the rule asks is whether the same measurements are in both.
func numbers(rows [][]string) []string {
	var out []string
	for _, r := range rows {
		for _, c := range r {
			for i := 0; i < len(c); {
				if !digit(c[i]) {
					i++
					continue
				}
				j := i
				for j < len(c) && (digit(c[j]) || c[j] == '.') {
					j++
				}
				out = append(out, strings.TrimRight(c[i:j], "."))
				i = j
			}
		}
	}
	return out
}

func digit(b byte) bool { return b >= '0' && b <= '9' }

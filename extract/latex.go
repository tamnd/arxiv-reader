package extract

import (
	"fmt"
	"strings"
)

// Tabular writes a table as LaTeX.
//
// This is the second of the two representations a table is kept in, and it
// exists because Markdown cannot express a multi row span, a multi column
// header or a rule drawn under part of a row, and academic tables use all three
// constantly. The Markdown is what a reader sees and it is lossy on purpose;
// this is what the loss is measured against.
//
// On the render path it is a reconstruction rather than the author's bytes. The
// rendering is LaTeXML's reading of the original tabular and it carries the
// spans, the alignment and the rules, so what comes out is the same table and
// is not the same source. The file says so in its first line, and the source
// path replaces it with the real thing when a paper goes down that route.
func Tabular(rows []Row) string {
	width := tableWidth(rows)
	if width == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\\begin{tabular}{%s}\n", columns(rows, width))
	for _, r := range rows {
		if r.Above {
			b.WriteString("\\hline\n")
		}
		b.WriteString(latexRow(r))
		b.WriteString(" \\\\\n")
		if r.Below {
			b.WriteString("\\hline\n")
		}
	}
	b.WriteString("\\end{tabular}\n")
	return b.String()
}

// tableWidth is the number of columns, which is the widest row.
//
// The widest and not the first, because a table whose first row is a single
// spanning title would otherwise be read as having one column.
func tableWidth(rows []Row) int {
	width := 0
	for _, r := range rows {
		n := 0
		for _, c := range r.Cells {
			n += max(c.Span, 1)
		}
		width = max(width, n)
	}
	return width
}

// columns is the column specification.
//
// Built a column at a time rather than copied off one row, because no single
// row of a real table states the alignment of every column. The first row that
// says anything about a column wins it, and a cell that spans is asked second,
// because a header reading Accuracy centred across three columns says less
// about those three than the cells sitting in them do.
//
// A column nothing said anything about is left aligned, which is what a tabular
// does with an l and what LaTeX does when the author wrote nothing either.
func columns(rows []Row, width int) string {
	out := make([]byte, width)
	fill(out, rows, false)
	fill(out, rows, true)
	for i, b := range out {
		if b == 0 {
			out[i] = 'l'
		}
	}
	return string(out)
}

// fill takes the alignment of every column no earlier row has claimed, from the
// cells that span or from the cells that do not.
func fill(out []byte, rows []Row, spanning bool) {
	for _, r := range rows {
		col := 0
		for _, c := range r.Cells {
			n := max(c.Span, 1)
			if c.Align != "" && n > 1 == spanning {
				for i := col; i < col+n && i < len(out); i++ {
					if out[i] == 0 {
						out[i] = align(c.Align)
					}
				}
			}
			col += n
		}
	}
}

func align(s string) byte {
	switch s {
	case "center":
		return 'c'
	case "right":
		return 'r'
	}
	return 'l'
}

func latexRow(r Row) string {
	cells := make([]string, 0, len(r.Cells))
	for _, c := range r.Cells {
		cells = append(cells, latexCell(c))
	}
	return strings.Join(cells, " & ")
}

// latexCell wraps one cell in whatever it needs to keep its shape.
//
// multirow before multicolumn when a cell has both, because that is the nesting
// LaTeX accepts: the column span is the outer box and the row span sits inside
// it. A cell with neither is written bare, which is almost all of them.
func latexCell(c Cell) string {
	s := InlineTeX(c.Text)
	if c.Header {
		s = "\\textbf{" + s + "}"
	}
	if c.Down > 1 {
		s = fmt.Sprintf("\\multirow{%d}{*}{%s}", c.Down, s)
	}
	if c.Span > 1 {
		s = fmt.Sprintf("\\multicolumn{%d}{%c}{%s}", c.Span, align(c.Align), s)
	}
	return s
}

// InlineTeX turns the Markdown this package writes back into LaTeX.
//
// It handles what the cell emitter actually produces and nothing else, which is
// mathematics, bold, italic, code and a link. A general Markdown to LaTeX
// converter is a different and much larger thing, and a table cell that needs
// one is a table cell that should be read on the render path as prose.
//
// Mathematics passes through untouched and is not escaped. A dollar sign in a
// cell is either the opening of a formula or the currency symbol the author
// escaped already, and a converter that escapes inside $...$ turns every
// subscript in the table into a literal underscore.
func InlineTeX(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		switch {
		case s[i] == '\\' && i+1 < len(s):
			// The Markdown emitter escapes a pipe. Nothing else it escapes
			// means anything different in LaTeX, so the backslash is dropped
			// and the character it protected is written out.
			if s[i+1] == '|' {
				b.WriteString("|")
			} else {
				b.WriteByte(s[i])
				b.WriteByte(s[i+1])
			}
			i += 2
		case s[i] == '$':
			j := strings.IndexByte(s[i+1:], '$')
			if j < 0 {
				b.WriteString(texEscape(s[i:]))
				i = len(s)
				continue
			}
			b.WriteString(s[i : i+1+j+1])
			i += 1 + j + 1
		case strings.HasPrefix(s[i:], "**"):
			if j := strings.Index(s[i+2:], "**"); j >= 0 {
				fmt.Fprintf(&b, "\\textbf{%s}", InlineTeX(s[i+2:i+2+j]))
				i += 2 + j + 2
				continue
			}
			b.WriteString(texEscape(s[i : i+2]))
			i += 2
		case s[i] == '*':
			if j := strings.IndexByte(s[i+1:], '*'); j >= 0 {
				fmt.Fprintf(&b, "\\textit{%s}", InlineTeX(s[i+1:i+1+j]))
				i += 1 + j + 1
				continue
			}
			b.WriteString(texEscape(s[i : i+1]))
			i++
		case s[i] == '`':
			if j := strings.IndexByte(s[i+1:], '`'); j >= 0 {
				fmt.Fprintf(&b, "\\texttt{%s}", texEscape(s[i+1:i+1+j]))
				i += 1 + j + 1
				continue
			}
			b.WriteString(texEscape(s[i : i+1]))
			i++
		case s[i] == '[':
			// A link becomes its text. The target is a local identifier this
			// corpus made up and a tabular in a corpus is not the place to
			// resolve it, so the reader gets the words and loses the jump.
			if text, rest, ok := linkText(s[i:]); ok {
				b.WriteString(InlineTeX(text))
				i = len(s) - len(rest)
				continue
			}
			b.WriteString(texEscape(s[i : i+1]))
			i++
		default:
			j := strings.IndexAny(s[i:], "\\$*`[")
			if j < 0 {
				b.WriteString(texEscape(s[i:]))
				i = len(s)
				continue
			}
			if j == 0 {
				j = 1
			}
			b.WriteString(texEscape(s[i : i+j]))
			i += j
		}
	}
	return b.String()
}

// linkText reads [text](target) and gives back the text and what follows it.
func linkText(s string) (text, rest string, ok bool) {
	shut := strings.IndexByte(s, ']')
	if shut < 0 || shut+1 >= len(s) || s[shut+1] != '(' {
		return "", "", false
	}
	end := strings.IndexByte(s[shut+1:], ')')
	if end < 0 {
		return "", "", false
	}
	return s[1:shut], s[shut+1+end+1:], true
}

// texEscape protects the characters LaTeX reads as instructions.
//
// A number passes through untouched, which is the property rule F12 checks: the
// two representations of a table have to agree on every numeric cell, and a
// converter that turned 99.5% into 99.5\% and then back would not be able to
// prove it.
func texEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		// The dollar is in here and mathematics still passes through
		// untouched, because a formula is written out by the branch above
		// rather than by this function. A dollar that reaches here is one
		// that opened nothing, so it is a currency symbol.
		case '&', '%', '#', '_', '{', '}', '$':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '~':
			b.WriteString("\\textasciitilde{}")
		case '^':
			b.WriteString("\\textasciicircum{}")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

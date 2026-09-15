// Package prose holds the small number to English helpers the reports share.
//
// They live here rather than once per report because a corpus that prints
// 3163381 in one report and 3,163,381 in the next looks like two projects, and
// because the rounding in Percent is a decision rather than a detail.
package prose

import (
	"fmt"
	"strconv"
	"strings"
)

// Thousands groups a count.
//
// The numbers these reports exist to compare are seven digits long and
// unreadable side by side without it.
func Thousands(n int) string {
	s := strconv.Itoa(n)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return sign + b.String()
}

// Percent prints a fraction, and n/a for the negative value the reports use to
// mean there was no denominator.
//
// One decimal place, because the second one is noise at three million and the
// reports that use this are read for their order of magnitude.
func Percent(f float64) string {
	if f < 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", f*100)
}

// Count is a grouped number and its noun, pluralised.
func Count(n int, what string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", what)
	}
	return fmt.Sprintf("%s %ss", Thousands(n), what)
}

// Bytes is a file size in the units a person reads sizes in.
//
// Powers of 1024 and labelled KB, MB and GB, which is what every file manager
// on every desktop prints and is what the size cap in the spec is written in.
// The decimal units are more correct and nobody outside a disk vendor uses them.
//
// An int64 because the two numbers this has to print are a 500 KB figure and a
// 20 GB checkout, and the second one is a whole repository rather than a file.
func Bytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return Count(int(n), "byte")
}

// Clip shortens a line to fit a report, ending it at a word.
//
// Counted in runes and not in bytes, because the lines this shortens are
// author names and paper titles and half of them are not ASCII. Cut back to the
// last space so the tail is a word rather than the front of one, unless there
// is no space to cut back to, which is a line with no English in it and is
// better shown cut mid word than not shown at all.
func Clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	out := string(r[:n])
	if i := strings.LastIndexByte(out, ' '); i > n/2 {
		out = out[:i]
	}
	return strings.TrimRight(out, " ,.;:") + "..."
}

// Share is a part over a whole, and -1 when the whole is zero.
//
// Zero over zero is not zero percent, it is a question nobody asked, and a
// report that prints 0.0% for it says the corpus holds none of something when
// what it holds is nothing at all.
func Share(part, whole int) float64 {
	if whole <= 0 {
		return -1
	}
	return float64(part) / float64(whole)
}

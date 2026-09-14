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
// Powers of 1024 and labelled KB and MB, which is what every file manager on
// every desktop prints and is what the size cap in the spec is written in. The
// decimal units are more correct and nobody outside a disk vendor uses them.
func Bytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return Count(n, "byte")
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

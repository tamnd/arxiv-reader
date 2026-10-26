// Package fingerprint computes the fingerprints of AX3.8.
//
// The fingerprints are computed from the normalized text of an item: the body
// with the paper's macros expanded, Unicode in NFC, labels and references
// replaced by placeholders, and white space collapsed. For a version whose
// licence does not allow text, the fingerprints are the only trace of the text,
// so they must not let a reader get the text back. A SHA-256 hash and 128
// MinHash values of 16 bits each do not.
package fingerprint

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"hash/fnv"
	"math"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Expander expands the macros of a paper. The tex package gives one.
type Expander interface {
	Expand(string) string
}

var (
	labelCmd = regexp.MustCompile(`\\label\s*\{[^{}]*\}`)
	refCmd   = regexp.MustCompile(`\\(?:ref|eqref|cref|Cref|autoref|pageref|nameref|vref|cpageref|hyperref)\*?\s*(?:\[[^\]]*\])?\s*\{[^{}]*\}`)
	citeCmd  = regexp.MustCompile(`\\(?:cite[a-zA-Z]*|nocite)\*?\s*(?:\[[^\]]*\]\s*){0,2}\{[^{}]*\}`)
	spacing  = regexp.MustCompile(`\\(?:,|;|:|!|quad|qquad|enspace|thinspace|medspace|thickspace|hfill|hspace\*?\s*\{[^{}]*\}|vspace\*?\s*\{[^{}]*\}|noindent|medskip|smallskip|bigskip|par\b|newline|\\)|~`)
	blank    = regexp.MustCompile(`\s+`)
)

// Normalize is the normalized text of an item body.
func Normalize(body string, m Expander) string {
	s := body
	if m != nil {
		s = m.Expand(s)
	}
	s = norm.NFC.String(s)
	s = labelCmd.ReplaceAllString(s, " ")
	s = refCmd.ReplaceAllString(s, " \\REF ")
	s = citeCmd.ReplaceAllString(s, " \\CITE ")
	s = spacing.ReplaceAllString(s, " ")
	s = blank.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// Hash is the SHA-256 of the normalized text, as "sha256:<hex>".
func Hash(normalized string) string {
	h := sha256.Sum256([]byte(normalized))
	return "sha256:" + hex.EncodeToString(h[:])
}

// Size is the count of MinHash values.
const Size = 128

// Shingle is the length of the character shingles.
const Shingle = 5

// MinHash is the MinHash of the character 5-grams of the normalized text.
//
// Each of the 128 values keeps its low 16 bits (b-bit MinHash), and the result
// is the 256 bytes in base64. A text shorter than one shingle is one shingle.
func MinHash(normalized string) string {
	sig := Signature(normalized)
	b := make([]byte, 2*Size)
	for i, v := range sig {
		binary.BigEndian.PutUint16(b[2*i:], v)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// Signature is the 128 MinHash values of the text.
func Signature(normalized string) [Size]uint16 {
	var mins [Size]uint64
	for i := range mins {
		mins[i] = math.MaxUint64
	}
	r := []rune(normalized)
	n := len(r) - Shingle + 1
	if n < 1 {
		n = 1
	}
	for i := 0; i < n; i++ {
		end := min(i+Shingle, len(r))
		f := fnv.New64a()
		f.Write([]byte(string(r[i:end])))
		x := f.Sum64()
		for k := range mins {
			if v := mix(x, uint64(k)); v < mins[k] {
				mins[k] = v
			}
		}
	}
	var out [Size]uint16
	for k, v := range mins {
		out[k] = uint16(v)
	}
	return out
}

// mix is the k-th hash function, from one base hash.
func mix(x, k uint64) uint64 {
	x ^= (k + 1) * 0x9e3779b97f4a7c15
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return x
}

// Similarity estimates the Jaccard similarity of two texts from their
// MinHash strings. It corrects for the chance agreement of 16-bit values.
func Similarity(a, b string) float64 {
	x, err1 := base64.StdEncoding.DecodeString(a)
	y, err2 := base64.StdEncoding.DecodeString(b)
	if err1 != nil || err2 != nil || len(x) != len(y) || len(x) == 0 {
		return 0
	}
	same := 0
	for i := 0; i+1 < len(x); i += 2 {
		if x[i] == y[i] && x[i+1] == y[i+1] {
			same++
		}
	}
	p := float64(same) / float64(len(x)/2)
	const c = 1.0 / 65536
	j := (p - c) / (1 - c)
	if j < 0 {
		return 0
	}
	return j
}

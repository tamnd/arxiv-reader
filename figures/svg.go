package figures

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Scalable is an SVG rewritten to scale by its viewBox and never by a transform.
//
// 09-publish.md section 4 asks for this and names the reason: SVG is a core media
// type in EPUB and is safe everywhere with one known trap, which is that scaling
// a picture with a transform on a group breaks on some Kobo devices. A drawing
// that scales by its viewBox is the same drawing and it works on the reader
// somebody actually owns.
//
// So the root loses its width and height, which are what pin a picture to one
// size, and keeps a viewBox, which says what the drawing contains and lets the
// page decide how large to show it. A scaling transform on the top level group
// is folded into the viewBox rather than left where it is, because the two say
// the same thing and only one of them is safe.
//
// The compiled size is not lost by this. A viewBox is a coordinate system rather
// than a measurement, and readSVG already falls back to it for the pixel size of
// a picture that states none, which is what the manifest records.
func Scalable(b []byte) ([]byte, error) {
	at := svgRoot.FindIndex(b)
	if at == nil {
		return nil, errors.New("figures: the svg has no root element in its first kilobyte")
	}
	// An SVG may open with an XML declaration, a doctype or a comment, and all of
	// that is kept exactly as it was.
	head, root, rest := b[:at[0]], b[at[0]:at[1]], b[at[1]:]
	box, err := frame(string(root))
	if err != nil {
		return nil, err
	}
	if m := topGroup.FindSubmatchIndex(rest); m != nil {
		by, fold, err := transform(string(rest[m[4]:m[5]]))
		if err != nil {
			return nil, err
		}
		if fold {
			box = by.invert(box)
			// The whole attribute goes and not the scale out of it, because what
			// is folded into the viewBox is the whole of what the transform did.
			rest = append(append([]byte{}, rest[:m[2]]...), rest[m[3]:]...)
		}
	}
	out := append([]byte{}, head...)
	out = append(out, reroot(root, box)...)
	return append(out, rest...), nil
}

// box is a viewBox, in the order it is written.
type box struct{ x, y, w, h float64 }

func (v box) String() string {
	return strings.Join([]string{num(v.x), num(v.y), num(v.w), num(v.h)}, " ")
}

// num prints a coordinate without a trailing run of zeroes, because a viewBox
// that reads 0 0 288 216 is one a person can check against the picture and one
// that reads 0 0 288.00000000000006 216 is not.
func num(f float64) string { return strconv.FormatFloat(round(f), 'f', -1, 64) }

// round takes a coordinate to six decimal places, which is finer than any
// display and coarse enough to take the floating point dust off a division.
func round(f float64) float64 {
	s := strconv.FormatFloat(f, 'f', 6, 64)
	out, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return f
	}
	return out
}

// frame is the viewBox the root already carries, or the one its size implies.
//
// A picture with neither is one this cannot make scalable, because dropping a
// width and a height from a document that has no coordinate system leaves a
// drawing with no size at all.
func frame(root string) (box, error) {
	var w, h float64
	for _, m := range svgAttr.FindAllStringSubmatch(root, -1) {
		switch m[1] {
		case "viewBox":
			f := fields(m[2])
			if len(f) != 4 {
				return box{}, fmt.Errorf("figures: the viewBox reads %q and a viewBox is four numbers", m[2])
			}
			return box{f[0], f[1], f[2], f[3]}, nil
		case "width":
			w = userUnits(m[2])
		case "height":
			h = userUnits(m[2])
		}
	}
	if w <= 0 || h <= 0 {
		return box{}, errors.New("figures: the svg has no viewBox and states no size, so there is nothing to scale it by")
	}
	return box{0, 0, w, h}, nil
}

// userUnits is a length attribute in the coordinate system a viewBox is written
// in, which CSS calls pixels at ninety six to the inch.
//
// svgLength answers the same question in whole units, because the rules that
// read it are about how large a picture is and a fraction of a pixel is not a
// size. Here the number is going into a coordinate system, where a hundred
// points coming out as a hundred and thirty three rather than a hundred and
// thirty three and a third is a drawing with the wrong shape.
func userUnits(s string) float64 {
	m := svgUnits.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	n, err := strconv.ParseFloat(m[1], 64)
	if err != nil || n <= 0 {
		return 0
	}
	switch m[2] {
	case "in":
		return n * 96
	case "cm":
		return n / 2.54 * 96
	case "mm":
		return n / 25.4 * 96
	case "pt":
		return n / 72 * 96
	case "pc":
		return n / 6 * 96
	}
	return n
}

// reroot writes the root element back with the viewBox it should carry and
// without the width and height that pinned it to one size.
func reroot(root []byte, v box) []byte {
	out := svgAttr.ReplaceAllStringFunc(string(root), func(s string) string {
		switch {
		case strings.HasPrefix(s, "viewBox"):
			return `viewBox="` + v.String() + `"`
		default:
			return ""
		}
	})
	if !strings.Contains(out, "viewBox=") {
		out = strings.Replace(out, "<svg", `<svg viewBox="`+v.String()+`"`, 1)
	}
	// Taking two attributes out of the middle leaves the spaces they sat
	// between, and an element with a run of them in it is a diff nobody can read.
	// The run first and then the one before the bracket, because collapsing two
	// spaces to one still leaves one in front of it.
	out = gaps.ReplaceAllString(out, " ")
	return []byte(strings.Replace(out, " >", ">", 1))
}

var (
	// topGroup is the first group in the document and its transform, which is
	// where dvisvgm puts the placement of the whole drawing.
	topGroup = regexp.MustCompile(`(?s)\A\s*(?:<!--.*?-->\s*)*<g\b[^>]*?(\s+transform="([^"]*)")`)
	gaps     = regexp.MustCompile(`\s{2,}`)
	// each is one operation in a transform list.
	each = regexp.MustCompile(`([a-zA-Z]+)\s*\(([^)]*)\)`)
)

// affine is the part of a transform this can undo, which is a uniform scale and
// a translation and nothing else.
type affine struct{ s, x, y float64 }

// invert is the viewBox that shows the same picture once the transform is gone.
//
// The group draws at s*p + t and the viewBox frames that. Take the transform
// away and the drawing is at p, so the frame has to come back through the same
// map, which is what this is.
func (a affine) invert(v box) box {
	return box{(v.x - a.x) / a.s, (v.y - a.y) / a.s, v.w / a.s, v.h / a.s}
}

// transform reads a transform list and says whether it can be folded away.
//
// Only translate and a uniform scale, because those two are what a picture
// compiled from TikZ carries and they are the two that fold into a viewBox
// exactly. A rotation or a skew changes the shape of the frame rather than its
// size, and folding one would mean writing a viewBox that is not a rectangle.
//
// A transform that is already the identity is left alone rather than removed,
// because rewriting an attribute that was doing nothing is a diff that says
// something happened when nothing did.
func transform(list string) (affine, bool, error) {
	at := affine{s: 1}
	ops := each.FindAllStringSubmatch(list, -1)
	if len(ops) == 0 {
		return at, false, nil
	}
	for _, op := range ops {
		f := fields(op[2])
		switch op[1] {
		case "translate":
			if len(f) == 1 {
				f = append(f, 0)
			}
			if len(f) != 2 {
				return at, false, fmt.Errorf("figures: the transform reads %q and a translate is one number or two", list)
			}
			at.x, at.y = at.x+at.s*f[0], at.y+at.s*f[1]
		case "scale":
			if len(f) == 2 && f[0] != f[1] {
				return at, false, fmt.Errorf("figures: the transform scales by %v across and %v down, and a viewBox scales both the same", f[0], f[1])
			}
			if len(f) == 0 || f[0] == 0 {
				return at, false, fmt.Errorf("figures: the transform reads %q and a scale is a number that is not nought", list)
			}
			at.s *= f[0]
		default:
			return at, false, fmt.Errorf("figures: the transform reads %q, and only a translate and a scale fold into a viewBox", list)
		}
	}
	if at.s == 1 && at.x == 0 && at.y == 0 {
		return at, false, nil
	}
	return at, true, nil
}

// fields reads the numbers out of an attribute written with spaces or commas or
// both, which all three occur.
func fields(s string) []float64 {
	var out []float64
	for _, f := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t' || r == '\n' || r == '\r'
	}) {
		n, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

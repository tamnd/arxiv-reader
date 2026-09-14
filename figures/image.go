// Package figures decides which of a paper's pictures may be committed.
//
// A cc-by article licenses what the authors own. It does not license a figure
// the authors reproduced from somebody else's paper with permission, and papers
// in every field do that constantly: a reprinted benchmark plot, a photograph
// out of a telescope archive, a diagram credited to a textbook. There is no
// metadata for it and there never will be, so the decision is made from the
// signals that are actually in the source.
//
// Nothing here downloads anything and nothing here writes anything. It reads a
// file that is already in hand and says whether it may be published, which is
// the order the rules have to run in: rule F09 in the quality spec fails a
// build where a suspected figure's bytes are committed, so the check has to
// exist before the first figure does.
//
// The file headers are read by hand rather than by decoding the image. A figure
// is judged on its size and its shape, image/png and image/jpeg decode the whole
// raster to tell us that, and a corpus that has to decode three million pictures
// to find out how big they are is a corpus that never finishes.
package figures

import (
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Format is the kind of file a figure is.
type Format string

const (
	FormatPNG  Format = "png"
	FormatJPEG Format = "jpeg"
	FormatGIF  Format = "gif"
	FormatSVG  Format = "svg"
)

// Image is what a figure file says about itself.
//
// Both sizes are here because they answer different questions. The pixel size
// is what a screen shows and it is what rule F02 is about. The physical size is
// what a page shows and it is what rule F06 is about, and a file does not always
// state one.
type Image struct {
	Format Format
	// Width and Height are pixels, or user units for an SVG.
	Width, Height int
	// WidthIn and HeightIn are inches, and they are zero when the file states
	// no physical size at all.
	WidthIn, HeightIn float64
	// Stated is where the physical size was read from, so pHYs for a PNG, JFIF
	// or Exif for a JPEG, and the width and height attributes for an SVG. It is
	// empty when the file stated none.
	Stated string
	// Bytes is the size of the file, which is what rule F03 is about.
	Bytes int
}

// ErrUnknownFormat is a file this package has no reader for.
//
// Worth its own error. The render path serves PNG, JPEG and SVG and the source
// path adds EPS and PDF, so a format landing here in M3 is a rendering doing
// something new rather than a file that is broken.
var ErrUnknownFormat = errors.New("figures: not a picture this reads")

// Read parses a figure's header.
//
// It reads the header and not the picture, so it is cheap and so it is honest
// about what it does not know: a file that states no resolution comes back with
// zero inches rather than with a guess.
func Read(b []byte) (Image, error) {
	switch {
	case len(b) >= 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n":
		return readPNG(b)
	case len(b) >= 2 && b[0] == 0xff && b[1] == 0xd8:
		return readJPEG(b)
	case len(b) >= 6 && (string(b[:6]) == "GIF87a" || string(b[:6]) == "GIF89a"):
		return readGIF(b)
	case looksSVG(b):
		return readSVG(b)
	}
	return Image{}, ErrUnknownFormat
}

// readPNG walks the chunks for IHDR and pHYs.
//
// IHDR is always the first chunk and pHYs may be anywhere before IDAT, so the
// walk stops at the image data: everything this needs is in front of it, and
// reading past it means reading the whole picture.
func readPNG(b []byte) (Image, error) {
	im := Image{Format: FormatPNG, Bytes: len(b)}
	for i := 8; i+8 <= len(b); {
		n := int(binary.BigEndian.Uint32(b[i:]))
		kind := string(b[i+4 : i+8])
		body := i + 8
		if n < 0 || body+n > len(b) {
			return im, fmt.Errorf("figures: the png chunk %q runs past the end of the file", kind)
		}
		switch kind {
		case "IHDR":
			if n < 8 {
				return im, errors.New("figures: the png header is too short to hold a size")
			}
			im.Width = int(binary.BigEndian.Uint32(b[body:]))
			im.Height = int(binary.BigEndian.Uint32(b[body+4:]))
		case "pHYs":
			if n < 9 {
				break
			}
			x := binary.BigEndian.Uint32(b[body:])
			y := binary.BigEndian.Uint32(b[body+4:])
			// Unit 1 is the metre and unit 0 means the two numbers are an
			// aspect ratio and not a resolution, which says nothing about how
			// big the picture is on a page.
			if b[body+8] == 1 && x > 0 && y > 0 {
				im.WidthIn = float64(im.Width) / (float64(x) * 0.0254)
				im.HeightIn = float64(im.Height) / (float64(y) * 0.0254)
				im.Stated = "pHYs"
			}
		case "IDAT":
			return im, done(im)
		}
		i = body + n + 4
	}
	return im, done(im)
}

// readJPEG walks the markers for a start of frame and a JFIF density.
func readJPEG(b []byte) (Image, error) {
	im := Image{Format: FormatJPEG, Bytes: len(b)}
	var dpiX, dpiY float64
	for i := 2; i+4 <= len(b); {
		if b[i] != 0xff {
			return im, errors.New("figures: the jpeg markers do not line up")
		}
		marker := b[i+1]
		// A standalone marker carries no length, and the only ones that can
		// appear here are the restart markers and the fill byte.
		if marker == 0xff {
			i++
			continue
		}
		if marker == 0xd8 || marker == 0x01 || (marker >= 0xd0 && marker <= 0xd7) {
			i += 2
			continue
		}
		n := int(binary.BigEndian.Uint16(b[i+2:]))
		body, end := i+4, i+2+n
		if n < 2 || end > len(b) {
			return im, errors.New("figures: a jpeg segment runs past the end of the file")
		}
		switch {
		case marker == 0xe0 && end-body >= 12 && string(b[body:body+5]) == "JFIF\x00":
			unit := b[body+7]
			x := float64(binary.BigEndian.Uint16(b[body+8:]))
			y := float64(binary.BigEndian.Uint16(b[body+10:]))
			// Unit 0 is an aspect ratio, unit 1 is dots per inch and unit 2 is
			// dots per centimetre.
			switch {
			case unit == 1 && x > 0 && y > 0:
				dpiX, dpiY, im.Stated = x, y, "JFIF"
			case unit == 2 && x > 0 && y > 0:
				dpiX, dpiY, im.Stated = x*2.54, y*2.54, "JFIF"
			}
		case isSOF(marker) && end-body >= 5:
			im.Height = int(binary.BigEndian.Uint16(b[body+1:]))
			im.Width = int(binary.BigEndian.Uint16(b[body+3:]))
			// The frame header is the last thing needed and the scan comes
			// after it, so stop rather than walk the entropy coded data.
			if dpiX > 0 {
				im.WidthIn = float64(im.Width) / dpiX
				im.HeightIn = float64(im.Height) / dpiY
			}
			return im, done(im)
		case marker == 0xda:
			return im, done(im)
		}
		i = end
	}
	return im, done(im)
}

// isSOF says a marker starts a frame.
//
// Every 0xc0 through 0xcf is a start of frame except three that are not: 0xc4
// is a huffman table, 0xc8 is reserved and 0xcc is an arithmetic coding table.
func isSOF(m byte) bool {
	return m >= 0xc0 && m <= 0xcf && m != 0xc4 && m != 0xc8 && m != 0xcc
}

// readGIF takes the size off the logical screen descriptor.
//
// A GIF states no resolution at all, so it is never judged by rule F06, and
// that is the format saying so rather than this package deciding it.
func readGIF(b []byte) (Image, error) {
	if len(b) < 10 {
		return Image{}, errors.New("figures: the gif header is too short to hold a size")
	}
	im := Image{
		Format: FormatGIF,
		Width:  int(binary.LittleEndian.Uint16(b[6:])),
		Height: int(binary.LittleEndian.Uint16(b[8:])),
		Bytes:  len(b),
	}
	return im, done(im)
}

// looksSVG sniffs for the root element rather than for a signature, because SVG
// has none and a file can open with an XML declaration, a doctype, a comment or
// a byte order mark before it gets to the picture.
func looksSVG(b []byte) bool {
	head := b
	if len(head) > 1024 {
		head = head[:1024]
	}
	return strings.Contains(string(head), "<svg")
}

var (
	svgRoot  = regexp.MustCompile(`(?s)<svg\b[^>]*>`)
	svgAttr  = regexp.MustCompile(`(\bwidth|\bheight|\bviewBox)\s*=\s*"([^"]*)"`)
	svgUnits = regexp.MustCompile(`^\s*(-?[0-9.]+)\s*(in|cm|mm|pt|pc|px|)\s*$`)
)

// readSVG takes the size off the root element.
//
// An SVG is the good case: it is vector, it scales, and when it states its size
// in inches or points it is stating the size the author meant it to print at,
// which is exactly what rule F06 asks about.
func readSVG(b []byte) (Image, error) {
	root := svgRoot.Find(b)
	if root == nil {
		return Image{}, errors.New("figures: the svg has no root element in its first kilobyte")
	}
	im := Image{Format: FormatSVG, Bytes: len(b)}
	var vb string
	for _, m := range svgAttr.FindAllStringSubmatch(string(root), -1) {
		switch m[1] {
		case "width":
			im.Width, im.WidthIn = svgLength(m[2])
		case "height":
			im.Height, im.HeightIn = svgLength(m[2])
		case "viewBox":
			vb = m[2]
		}
	}
	if im.WidthIn > 0 && im.HeightIn > 0 {
		im.Stated = "svg"
	}
	// The viewBox is the fallback for the pixel size and never for the physical
	// one. It is a coordinate system and not a measurement, so an author who
	// gave only a viewBox has said what the drawing contains and has said
	// nothing about how large it prints.
	if (im.Width == 0 || im.Height == 0) && vb != "" {
		if w, h, ok := viewBox(vb); ok {
			if im.Width == 0 {
				im.Width = w
			}
			if im.Height == 0 {
				im.Height = h
			}
		}
	}
	return im, done(im)
}

// svgLength reads one length attribute and gives back its size in user units
// and in inches, with zero inches when the attribute carried no unit.
//
// A bare number is user units, which CSS calls pixels at ninety six to the inch,
// and that is a coordinate rather than a measurement. Treating it as a physical
// size would make every plain SVG claim a resolution its author never stated.
func svgLength(s string) (int, float64) {
	m := svgUnits.FindStringSubmatch(s)
	if m == nil {
		return 0, 0
	}
	n, err := strconv.ParseFloat(m[1], 64)
	if err != nil || n <= 0 {
		return 0, 0
	}
	switch m[2] {
	case "in":
		return int(n * 96), n
	case "cm":
		return int(n / 2.54 * 96), n / 2.54
	case "mm":
		return int(n / 25.4 * 96), n / 25.4
	case "pt":
		return int(n / 72 * 96), n / 72
	case "pc":
		return int(n / 6 * 96), n / 6
	}
	return int(n), 0
}

func viewBox(s string) (int, int, bool) {
	f := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' })
	if len(f) != 4 {
		return 0, 0, false
	}
	w, err1 := strconv.ParseFloat(f[2], 64)
	h, err2 := strconv.ParseFloat(f[3], 64)
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return int(w), int(h), true
}

// done is the one check every reader ends with, because a picture with no size
// is a file this package cannot judge and every rule below is about a size.
func done(im Image) error {
	if im.Width <= 0 || im.Height <= 0 {
		return fmt.Errorf("figures: the %s states no size", im.Format)
	}
	return nil
}

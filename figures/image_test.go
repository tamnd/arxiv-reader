package figures

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// png builds a header rather than a picture, because the header is all this
// package reads and a real encoder would put an IDAT in front of the pHYs on
// some inputs and hide the bug this is here to catch.
func png(w, h int, ppmX, ppmY uint32, unit byte) []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	chunk := func(kind string, body []byte) {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(body)))
		b.Write(n[:])
		b.WriteString(kind)
		b.Write(body)
		b.WriteString("crc0")
	}
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr, uint32(w))
	binary.BigEndian.PutUint32(ihdr[4:], uint32(h))
	chunk("IHDR", ihdr)
	if ppmX > 0 {
		phys := make([]byte, 9)
		binary.BigEndian.PutUint32(phys, ppmX)
		binary.BigEndian.PutUint32(phys[4:], ppmY)
		phys[8] = unit
		chunk("pHYs", phys)
	}
	chunk("IDAT", []byte("data"))
	return b.Bytes()
}

// dpi is pixels per metre for a resolution in dots per inch, which is the unit
// a PNG states and not the unit anybody thinks in.
func dpi(n float64) uint32 { return uint32(n / 0.0254) }

func jpeg(w, h int, unit byte, x, y uint16) []byte {
	var b bytes.Buffer
	b.WriteString("\xff\xd8")
	seg := func(marker byte, body []byte) {
		b.WriteByte(0xff)
		b.WriteByte(marker)
		var n [2]byte
		binary.BigEndian.PutUint16(n[:], uint16(len(body)+2))
		b.Write(n[:])
		b.Write(body)
	}
	jfif := make([]byte, 14)
	copy(jfif, "JFIF\x00")
	jfif[5], jfif[6] = 1, 2
	jfif[7] = unit
	binary.BigEndian.PutUint16(jfif[8:], x)
	binary.BigEndian.PutUint16(jfif[10:], y)
	seg(0xe0, jfif)
	sof := make([]byte, 6)
	sof[0] = 8
	binary.BigEndian.PutUint16(sof[1:], uint16(h))
	binary.BigEndian.PutUint16(sof[3:], uint16(w))
	seg(0xc0, sof)
	return b.Bytes()
}

func gif(w, h int) []byte {
	b := make([]byte, 13)
	copy(b, "GIF89a")
	binary.LittleEndian.PutUint16(b[6:], uint16(w))
	binary.LittleEndian.PutUint16(b[8:], uint16(h))
	return b
}

func TestAPNGStatesItsSizeAndSometimesItsResolution(t *testing.T) {
	im, err := Read(png(1200, 800, dpi(300), dpi(300), 1))
	if err != nil {
		t.Fatal(err)
	}
	if im.Format != FormatPNG || im.Width != 1200 || im.Height != 800 {
		t.Fatalf("read %+v", im)
	}
	if im.Stated != "pHYs" {
		t.Fatalf("the resolution came from %q, want pHYs", im.Stated)
	}
	if got := im.WidthIn; got < 3.99 || got > 4.01 {
		t.Errorf("the picture is %.3f inches wide, want 4", got)
	}
}

// Unit zero means the two numbers are an aspect ratio, so the file has stated
// how square its pixels are and has said nothing about how big it prints.
func TestAPNGWithAnAspectRatioHasNoPhysicalSize(t *testing.T) {
	im, err := Read(png(1200, 800, 3, 3, 0))
	if err != nil {
		t.Fatal(err)
	}
	if im.Measured() {
		t.Fatalf("an aspect ratio was read as a resolution: %+v", im)
	}
	if im.PageFraction() != 0 {
		t.Errorf("page fraction is %v, want 0", im.PageFraction())
	}
}

func TestAPNGWithNoResolutionChunkHasNoPhysicalSize(t *testing.T) {
	im, err := Read(png(1200, 800, 0, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if im.Measured() || im.Width != 1200 {
		t.Fatalf("read %+v", im)
	}
}

func TestAJPEGReadsItsDensityInBothUnits(t *testing.T) {
	inch, err := Read(jpeg(600, 900, 1, 150, 150))
	if err != nil {
		t.Fatal(err)
	}
	if got := inch.WidthIn; got < 3.99 || got > 4.01 {
		t.Errorf("at 150 dpi 600 pixels is %.3f inches, want 4", got)
	}
	cm, err := Read(jpeg(600, 900, 2, 59, 59))
	if err != nil {
		t.Fatal(err)
	}
	// 59 dots per centimetre is about 150 to the inch, so the two files are
	// the same picture stated two ways.
	if got := cm.WidthIn; got < 3.9 || got > 4.1 {
		t.Errorf("at 59 dpcm 600 pixels is %.3f inches, want about 4", got)
	}
	none, err := Read(jpeg(600, 900, 0, 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	if none.Measured() {
		t.Fatalf("an aspect ratio was read as a density: %+v", none)
	}
}

func TestAGIFStatesASizeAndNeverAResolution(t *testing.T) {
	im, err := Read(gif(320, 240))
	if err != nil {
		t.Fatal(err)
	}
	if im.Format != FormatGIF || im.Width != 320 || im.Height != 240 {
		t.Fatalf("read %+v", im)
	}
	if im.Measured() {
		t.Fatal("a gif claimed a resolution, and the format has nowhere to put one")
	}
}

func TestAnSVGReadsItsUnits(t *testing.T) {
	for _, tc := range []struct {
		name     string
		doc      string
		w, h     int
		in       float64
		measured bool
	}{
		{
			name:     "inches",
			doc:      `<svg xmlns="http://www.w3.org/2000/svg" width="4in" height="3in"></svg>`,
			w:        384,
			h:        288,
			in:       4,
			measured: true,
		},
		{
			name:     "points",
			doc:      `<svg width="288pt" height="216pt" viewBox="0 0 288 216"></svg>`,
			w:        384,
			h:        288,
			in:       4,
			measured: true,
		},
		{
			// A bare number is a user unit, which is a coordinate and not a
			// measurement, so the drawing has a pixel size and no printed one.
			name: "user units",
			doc:  `<svg width="384" height="288"></svg>`,
			w:    384,
			h:    288,
		},
		{
			// The viewBox is the fallback for the pixel size and never for the
			// physical one, for the same reason.
			name: "only a viewBox",
			doc:  `<?xml version="1.0"?>` + "\n" + `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 384 288"></svg>`,
			w:    384,
			h:    288,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			im, err := Read([]byte(tc.doc))
			if err != nil {
				t.Fatal(err)
			}
			if im.Format != FormatSVG || im.Width != tc.w || im.Height != tc.h {
				t.Fatalf("read %+v, want %d by %d", im, tc.w, tc.h)
			}
			if im.Measured() != tc.measured {
				t.Fatalf("measured is %v, want %v, from %+v", im.Measured(), tc.measured, im)
			}
			if tc.measured && (im.WidthIn < tc.in-0.01 || im.WidthIn > tc.in+0.01) {
				t.Errorf("the drawing is %.3f inches wide, want %v", im.WidthIn, tc.in)
			}
		})
	}
}

func TestAFileThisCannotReadSaysSo(t *testing.T) {
	for name, b := range map[string][]byte{
		"nothing":    nil,
		"prose":      []byte("this is not a picture"),
		"a pdf":      []byte("%PDF-1.5\n"),
		"postscript": []byte("%!PS-Adobe-3.0 EPSF-3.0\n"),
	} {
		if _, err := Read(b); err != ErrUnknownFormat {
			t.Errorf("%s came back with %v, want ErrUnknownFormat", name, err)
		}
	}
}

// A header that says nothing about how big the picture is is a file no rule
// below can be applied to, so it is an error and not a zero.
func TestAPictureWithNoSizeIsAnError(t *testing.T) {
	for name, b := range map[string][]byte{
		"a png with a zero width": png(0, 800, 0, 0, 0),
		"an svg with no size":     []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`),
		"a truncated gif":         []byte("GIF89a"),
	} {
		if _, err := Read(b); err == nil {
			t.Errorf("%s was read without complaint", name)
		}
	}
}

func TestATruncatedPNGChunkIsRefused(t *testing.T) {
	b := png(1200, 800, 0, 0, 0)
	if _, err := Read(b[:20]); err == nil {
		t.Fatal("a chunk running past the end of the file was read anyway")
	}
}

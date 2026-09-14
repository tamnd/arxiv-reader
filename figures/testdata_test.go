package figures

import (
	"os"
	"path/filepath"
	"testing"
)

// The fixtures are real files and the tests above build headers, and both are
// worth having. A header built in a test says exactly what is being read. A
// file an encoder wrote says whether what real encoders emit is what this
// reads, and the pHYs chunk is the one where those two came apart: image/png
// does not write one at all, so a fixture with a resolution in it had to be
// made on purpose and this is what proves it came out right.
func TestTheFixturesReadBackAsTheyWereMade(t *testing.T) {
	for _, tc := range []struct {
		file     string
		format   Format
		w, h     int
		measured bool
		commit   bool
		rule     string
	}{
		{file: "plot.png", format: FormatPNG, w: 1200, h: 900, measured: true, commit: true},
		{file: "drawing.svg", format: FormatSVG, w: 384, h: 288, measured: true, commit: true},
		{file: "page.png", format: FormatPNG, w: 1275, h: 1650, measured: true, rule: "F06"},
		{file: "spacer.png", format: FormatPNG, w: 20, h: 8, rule: "F02"},
		// A tall figure with no stated resolution is the picture a guessed one
		// would throw away, so it is here to stay committed.
		{file: "tall.png", format: FormatPNG, w: 1600, h: 2200, commit: true},
	} {
		t.Run(tc.file, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			im, err := Read(b)
			if err != nil {
				t.Fatal(err)
			}
			if im.Format != tc.format || im.Width != tc.w || im.Height != tc.h {
				t.Fatalf("read %+v", im)
			}
			if im.Measured() != tc.measured {
				t.Fatalf("measured is %v, want %v, from %+v", im.Measured(), tc.measured, im)
			}
			d := Decide("", im)
			if d.Commit != tc.commit || d.Rule != tc.rule {
				t.Fatalf("commit %v by %q, want %v by %q: %s", d.Commit, d.Rule, tc.commit, tc.rule, d.Why)
			}
		})
	}
}

package scope

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOf(t *testing.T) {
	cases := []struct {
		cats      []string
		in, cross bool
	}{
		{[]string{"math.NT"}, true, false},
		{[]string{"cs.LG", "math.OC"}, true, true},
		{[]string{"math-ph", "hep-th"}, true, false},
		{[]string{"hep-th", "math-ph"}, true, true},
		{[]string{"alg-geom"}, true, false},
		{[]string{"cs.LO", "cs.DM"}, false, false},
		{[]string{"mathph"}, false, false},
	}
	for _, c := range cases {
		in, cross := Of(c.cats)
		if in != c.in || cross != c.cross {
			t.Errorf("%v: in %v cross %v, want %v %v", c.cats, in, cross, c.in, c.cross)
		}
	}
}

func TestWriteRead(t *testing.T) {
	path := Path(t.TempDir(), Math)
	l := List{Reason: Math, IDs: []string{"2312.00752", "alg-geom/9201001", "0809.0076"}, Cross: 1, Date: "2026-10-27"}
	if err := l.Write(path); err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "math.txt" {
		t.Errorf("path %s", path)
	}
	b, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(b), "# scope-math: 3 papers, 2 with a primary category in scope and 1 by a cross-list only.") {
		t.Errorf("header %q", strings.SplitN(string(b), "\n", 2)[0])
	}
	ids, err := Read(path)
	if err != nil || len(ids) != 3 || !ids["alg-geom/9201001"] {
		t.Errorf("read %v %v", ids, err)
	}
}

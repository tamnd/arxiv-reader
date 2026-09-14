package prose

import "testing"

func TestThousands(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{
		{0, "0"},
		{7, "7"},
		{999, "999"},
		{1000, "1,000"},
		{16248, "16,248"},
		{3163381, "3,163,381"},
		{-16244, "-16,244"},
		{-999, "-999"},
	} {
		if got := Thousands(tc.n); got != tc.want {
			t.Errorf("Thousands(%d) is %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestPercent(t *testing.T) {
	for _, tc := range []struct {
		f    float64
		want string
	}{
		{1, "100.0%"},
		{0.5, "50.0%"},
		{0, "0.0%"},
		{-1, "n/a"},
	} {
		if got := Percent(tc.f); got != tc.want {
			t.Errorf("Percent(%v) is %q, want %q", tc.f, got, tc.want)
		}
	}
}

func TestBytes(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{
		{0, "0 bytes"},
		{1, "1 byte"},
		{999, "999 bytes"},
		{1024, "1 KB"},
		// The size cap in the spec is five hundred kilobytes, so it has to
		// print as that and not as 512000 or as 0.5 MB.
		{500 << 10, "500 KB"},
		{(500 << 10) + 1, "500 KB"},
		{1 << 20, "1.0 MB"},
		{3 << 20, "3.0 MB"},
	} {
		if got := Bytes(tc.n); got != tc.want {
			t.Errorf("Bytes(%d) is %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestCount(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{
		{0, "0 records"},
		{1, "1 record"},
		{2, "2 records"},
		{3163381, "3,163,381 records"},
	} {
		if got := Count(tc.n, "record"); got != tc.want {
			t.Errorf("Count(%d) is %q, want %q", tc.n, got, tc.want)
		}
	}
}

// Zero over zero is the case the negative return exists for, and a report that
// reads it as zero percent says the corpus holds none of something when what it
// holds is nothing at all.
func TestShare(t *testing.T) {
	for _, tc := range []struct {
		part, whole int
		want        float64
	}{
		{1, 4, 0.25},
		{0, 4, 0},
		{4, 4, 1},
		{0, 0, -1},
		{1, 0, -1},
	} {
		if got := Share(tc.part, tc.whole); got != tc.want {
			t.Errorf("Share(%d, %d) is %v, want %v", tc.part, tc.whole, got, tc.want)
		}
	}
	if Percent(Share(0, 0)) != "n/a" {
		t.Error("nothing over nothing printed as a percentage")
	}
}

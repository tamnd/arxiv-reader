package corpus

import "testing"

func TestLicenceFromURL(t *testing.T) {
	cases := []struct {
		in   string
		want Licence
	}{
		// The six, as arXiv's own OAI-PMH records actually write them. These
		// strings were copied out of a live ListRecords response rather than
		// out of the documentation.
		{"http://arxiv.org/licenses/nonexclusive-distrib/1.0/", LicenceArXiv},
		{"http://creativecommons.org/licenses/by/4.0/", LicenceCCBY},
		{"http://creativecommons.org/licenses/by-sa/4.0/", LicenceCCBYSA},
		{"http://creativecommons.org/licenses/by-nc-sa/4.0/", LicenceCCBYNCSA},
		{"http://creativecommons.org/licenses/by-nc-nd/4.0/", LicenceCCBYNCND},
		{"http://creativecommons.org/publicdomain/zero/1.0/", LicenceCC0},

		// https, and the trailing slash, and the case, all mean nothing.
		{"https://creativecommons.org/licenses/by/4.0/", LicenceCCBY},
		{"https://creativecommons.org/licenses/by/4.0", LicenceCCBY},
		{"HTTPS://CreativeCommons.org/licenses/BY/4.0/", LicenceCCBY},
		{"  http://creativecommons.org/licenses/by/4.0/  ", LicenceCCBY},

		// The older point versions arXiv served, which mean the same thing for
		// anything this corpus does.
		{"http://creativecommons.org/licenses/by/3.0/", LicenceCCBY},
		{"http://creativecommons.org/licenses/by-nc-sa/3.0/", LicenceCCBYNCSA},
		{"http://creativecommons.org/licenses/publicdomain/", LicenceCC0},

		// Papers from before arXiv offered a choice. The same permissions as
		// the default licence, so the same value.
		{"http://arxiv.org/licenses/assumed-1991-2003/", LicenceArXiv},
	}
	for _, c := range cases {
		got, err := LicenceFromURL(c.in)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %s, want %s", c.in, got, c.want)
		}
	}
}

// arXiv only started offering a choice in 2004, so a record with no licence
// element is the ordinary case for the older half of the corpus. Reading that
// silence as permission is the one mistake in this project that a later commit
// cannot undo, so it resolves to the strictest of the six and not to unknown.
func TestNoLicenceIsTheDefaultLicence(t *testing.T) {
	for _, in := range []string{"", "   "} {
		got, err := LicenceFromURL(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != LicenceArXiv {
			t.Errorf("%q gave %s, want %s", in, got, LicenceArXiv)
		}
		if AccessFor(got).MayPublishText() {
			t.Errorf("%q resolved to something that permits publishing the text", in)
		}
	}
}

// A licence nobody has seen before must be an error and not a guess. The
// alternative is a URL that is one character off the CC BY one quietly
// publishing a paper that may not be published.
func TestAnUnknownURLIsAnError(t *testing.T) {
	for _, in := range []string{
		"https://opensource.org/licenses/MIT",
		"http://creativecommons.org/licenses/by-nd/4.0/",
		"nonsense",
	} {
		got, err := LicenceFromURL(in)
		if err == nil {
			t.Errorf("%q was read as %s", in, got)
		}
		if got != LicenceUnknown {
			t.Errorf("%q failed to %s, and a failure should be the strictest value", in, got)
		}
	}
}

// Every licence's own URL has to round trip, or the census and the front matter
// will disagree about the same paper.
func TestURLRoundTrips(t *testing.T) {
	for _, l := range Licences {
		if l == LicenceUnknown {
			continue
		}
		got, err := LicenceFromURL(l.URL())
		if err != nil {
			t.Errorf("%s: %v", l, err)
			continue
		}
		if got != l {
			t.Errorf("%s round tripped to %s", l, got)
		}
	}
}

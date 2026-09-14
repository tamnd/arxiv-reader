package corpus

import "testing"

func TestLicenceFromLabel(t *testing.T) {
	cases := map[string]Licence{
		"CC BY 4.0":                   LicenceCCBY,
		"License: CC BY 4.0":          LicenceCCBY,
		"licence: cc by 4.0":          LicenceCCBY,
		"  CC   BY   4.0  ":           LicenceCCBY,
		"CC BY 4.0.":                  LicenceCCBY,
		"CC BY-SA 4.0":                LicenceCCBYSA,
		"CC BY-NC-SA 4.0":             LicenceCCBYNCSA,
		"CC BY-NC-ND 4.0":             LicenceCCBYNCND,
		"CC Zero 1.0":                 LicenceCC0,
		"CC0 1.0":                     LicenceCC0,
		"public domain":               LicenceCC0,
		"arXiv.org perpetual license": LicenceArXiv,
		"arXiv non-exclusive license": LicenceArXiv,
		"nonexclusive-distrib/1.0":    LicenceArXiv,
		// arXiv has printed CC BY at 3.0 and at 4.0 and both mean republish
		// with attribution, so the version is not part of the decision.
		"CC BY 3.0": LicenceCCBY,
	}
	for label, want := range cases {
		got, ok := LicenceFromLabel(label)
		if !ok {
			t.Errorf("%q was not recognised", label)
			continue
		}
		if got != want {
			t.Errorf("%q is %s, want %s", label, got, want)
		}
	}
}

// The share-alike and non-commercial labels all start with "cc by", so the
// order the prefixes are tried in is the whole correctness of this function.
// Reading CC BY-NC-ND as CC BY would publish a translation of a paper whose
// author forbade derivative works.
func TestTheMostRestrictiveLabelWins(t *testing.T) {
	for label, want := range map[string]Access{
		"CC BY 4.0":       AccessOpen,
		"CC BY-SA 4.0":    AccessShareAlike,
		"CC BY-NC-SA 4.0": AccessShareAlike,
		"CC BY-NC-ND 4.0": AccessVerbatim,
	} {
		l, ok := LicenceFromLabel(label)
		if !ok {
			t.Fatalf("%q was not recognised", label)
		}
		if got := AccessFor(l); got != want {
			t.Errorf("%q gives %s access, want %s", label, got, want)
		}
	}
}

// A label nobody has seen before is arXiv changing its wording. It is worth a
// note and it is not worth refusing a paper over, so the caller decides, and
// unknown is reported as unknown rather than as the default licence.
func TestAnUnrecognisedLabelSaysSo(t *testing.T) {
	for _, label := range []string{"", "   ", "License:", "Open Access", "MIT", "CC"} {
		l, ok := LicenceFromLabel(label)
		if ok {
			t.Errorf("%q was read as %s", label, l)
		}
		if l != LicenceUnknown {
			t.Errorf("%q came back as %s, want unknown", label, l)
		}
	}
}

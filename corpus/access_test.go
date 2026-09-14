package corpus

import "testing"

func TestAccessFor(t *testing.T) {
	cases := []struct {
		licence   Licence
		access    Access
		text      bool
		translate bool
	}{
		{LicenceCC0, AccessOpen, true, true},
		{LicenceCCBY, AccessOpen, true, true},
		{LicenceCCBYSA, AccessShareAlike, true, true},
		{LicenceCCBYNCSA, AccessShareAlike, true, true},
		{LicenceCCBYNCND, AccessVerbatim, true, false},
		{LicenceArXiv, AccessRecord, false, false},
		{LicenceUnknown, AccessRecord, false, false},
	}
	for _, c := range cases {
		got := AccessFor(c.licence)
		if got != c.access {
			t.Errorf("AccessFor(%s) = %s, want %s", c.licence, got, c.access)
		}
		if got.MayPublishText() != c.text {
			t.Errorf("%s MayPublishText = %v, want %v", c.licence, got.MayPublishText(), c.text)
		}
		if got.MayTranslate() != c.translate {
			t.Errorf("%s MayTranslate = %v, want %v", c.licence, got.MayTranslate(), c.translate)
		}
	}
}

// The no-derivatives licence is the one the gate exists for. It permits the
// English text and forbids every translation, and it is the case somebody will
// eventually get wrong by grouping it with the other Creative Commons
// licences, so it gets a test of its own.
func TestNoDerivativesForbidsTranslation(t *testing.T) {
	a := AccessFor(LicenceCCBYNCND)
	if !a.MayPublishText() {
		t.Fatal("cc-by-nc-nd permits republishing the text")
	}
	if a.MayTranslate() {
		t.Fatal("cc-by-nc-nd forbids derivative works, so it forbids translation")
	}
}

// An unresolved licence has to be as strict as the default licence. A paper
// the corpus has not looked at yet is a paper the corpus may not publish.
func TestUnknownIsAsStrictAsTheDefault(t *testing.T) {
	if AccessFor(LicenceUnknown) != AccessFor(LicenceArXiv) {
		t.Fatal("unknown must be treated exactly as the arXiv default licence is")
	}
}

func TestParseLicence(t *testing.T) {
	for _, l := range Licences {
		got, err := ParseLicence(string(l))
		if err != nil {
			t.Fatalf("ParseLicence(%q): %v", l, err)
		}
		if got != l {
			t.Errorf("ParseLicence(%q) = %q", l, got)
		}
	}
	if got, err := ParseLicence(""); err != nil || got != LicenceUnknown {
		t.Errorf(`ParseLicence("") = %q, %v, want unknown and no error`, got, err)
	}
	if _, err := ParseLicence("cc-by-nd"); err == nil {
		t.Error("ParseLicence accepted a licence arXiv does not offer")
	}
}

func TestPublishedLicence(t *testing.T) {
	cases := []struct {
		source Licence
		want   Licence
	}{
		{LicenceCC0, LicenceCCBY},
		{LicenceCCBY, LicenceCCBY},
		{LicenceCCBYSA, LicenceCCBYSA},
		{LicenceCCBYNCSA, LicenceCCBYNCSA},
		{LicenceCCBYNCND, LicenceCCBYNCND},
	}
	for _, c := range cases {
		got, err := PublishedLicence(c.source)
		if err != nil {
			t.Fatalf("PublishedLicence(%s): %v", c.source, err)
		}
		if got != c.want {
			t.Errorf("PublishedLicence(%s) = %s, want %s", c.source, got, c.want)
		}
	}
	for _, source := range []Licence{LicenceArXiv, LicenceUnknown} {
		if _, err := PublishedLicence(source); err == nil {
			t.Errorf("PublishedLicence(%s) returned a licence for a paper that may not be published", source)
		}
	}
}

func TestLicenceURL(t *testing.T) {
	for _, l := range Licences {
		url := l.URL()
		if l == LicenceUnknown {
			if url != "" {
				t.Errorf("unknown has a licence URL: %q", url)
			}
			continue
		}
		if url == "" {
			t.Errorf("%s has no licence URL", l)
		}
	}
}

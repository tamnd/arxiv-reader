package corpus

import (
	"fmt"
	"strings"
)

// LicenceFromURL reads the licence out of the URL arXiv publishes for it.
//
// Every surface arXiv offers states the licence as a URL rather than as a name,
// so this is the function that stands between a harvest and the licence gate.
//
// An empty URL is the default licence and not an error. arXiv only started
// offering a choice in 2004 and the records from before that carry no licence
// element at all, which means the submitter agreed to arXiv's own terms. That
// is the strictest of the six, and reading silence as permission is the one
// mistake in this whole project that cannot be undone by a later commit.
//
// The version in the URL is ignored on purpose. arXiv has served CC BY at 3.0
// and at 4.0 over the years and both mean the same thing for what this corpus
// does with a paper, which is republish it with attribution. Recording the
// point version would split every count in the census in two for no decision it
// would change.
func LicenceFromURL(raw string) (Licence, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return LicenceArXiv, nil
	}
	// Compared without the scheme, because arXiv's own records say http and its
	// documentation says https, and the two are the same licence.
	s = strings.ToLower(s)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "www.")
	s = strings.Trim(s, "/")

	switch {
	case strings.HasPrefix(s, "creativecommons.org/publicdomain/zero/"),
		// The pre-2009 CC public domain declaration, which a handful of the
		// older records still carry.
		strings.HasPrefix(s, "creativecommons.org/licenses/publicdomain"):
		return LicenceCC0, nil
	case strings.HasPrefix(s, "creativecommons.org/licenses/by-nc-nd/"):
		return LicenceCCBYNCND, nil
	case strings.HasPrefix(s, "creativecommons.org/licenses/by-nc-sa/"):
		return LicenceCCBYNCSA, nil
	case strings.HasPrefix(s, "creativecommons.org/licenses/by-sa/"):
		return LicenceCCBYSA, nil
	case strings.HasPrefix(s, "creativecommons.org/licenses/by/"):
		return LicenceCCBY, nil
	case strings.HasPrefix(s, "arxiv.org/licenses/nonexclusive-distrib/"):
		return LicenceArXiv, nil
	case strings.HasPrefix(s, "arxiv.org/licenses/assumed-1991-2003"):
		// Papers from before arXiv offered a choice. arXiv assumes the
		// submitter granted it the right to distribute and nothing more, which
		// is the same set of permissions as the default licence, so this maps
		// onto it rather than becoming a seventh value. The access outcome is
		// identical: a record and a link, and no text.
		return LicenceArXiv, nil
	}
	return LicenceUnknown, fmt.Errorf("corpus: %q is not a licence arXiv publishes", raw)
}

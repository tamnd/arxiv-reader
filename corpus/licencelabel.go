package corpus

import "strings"

// LicenceFromLabel reads the licence out of the name arXiv prints for it.
//
// Every machine readable surface states the licence as a URL and
// LicenceFromURL handles those. The HTML rendering is the one place arXiv
// states it as a name, in the info box at the top of the page, and the link
// beside that name points at arXiv's help page rather than at the deed. So a
// rendering can only be crosschecked against the licence the corpus resolved by
// reading the printed name, which is what this does.
//
// The second return is false for a label this does not recognise, and that is
// not the same as the default licence. A label nobody has seen before is
// arXiv changing its wording, which is worth a note and is not worth refusing a
// paper over, so the caller decides. Reading an unknown label as permission
// would be the one mistake in this project that cannot be undone by a later
// commit, and reading it as a refusal would stop the pipeline the day arXiv
// added a full stop.
func LicenceFromLabel(raw string) (Licence, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.TrimPrefix(s, "license:")
	s = strings.TrimPrefix(s, "licence:")
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimSuffix(s, ".")
	switch {
	case s == "":
		return LicenceUnknown, false
	// The version number is dropped for the same reason LicenceFromURL drops
	// it: arXiv has printed CC BY at 3.0 and at 4.0 and both mean republish
	// with attribution.
	case strings.HasPrefix(s, "cc by-nc-nd"):
		return LicenceCCBYNCND, true
	case strings.HasPrefix(s, "cc by-nc-sa"):
		return LicenceCCBYNCSA, true
	case strings.HasPrefix(s, "cc by-sa"):
		return LicenceCCBYSA, true
	case strings.HasPrefix(s, "cc by"):
		return LicenceCCBY, true
	case strings.HasPrefix(s, "cc zero"), strings.HasPrefix(s, "cc0"), s == "public domain":
		return LicenceCC0, true
	case strings.Contains(s, "arxiv.org perpetual"), strings.Contains(s, "nonexclusive-distrib"),
		strings.Contains(s, "non-exclusive license"):
		return LicenceArXiv, true
	}
	return LicenceUnknown, false
}

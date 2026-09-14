// Package corpus holds the things every other package in the toolchain has to
// agree about: what an arXiv licence permits, how an access class follows from
// it, and where a file for a given paper lives on disk.
//
// Nothing here talks to the network or to a model. The decisions that say what
// may be published should be checkable offline, by reading a table, without a
// subscription and without a key.
package corpus

import "fmt"

// Licence is one of the six licences arXiv lets a submitter choose, plus the
// value the corpus uses when it has not established which one applies.
//
// The strings are arXiv's own, as they come back from the OAI-PMH arXiv
// metadata format, so a value here can be compared against a harvested record
// without a translation step in between.
type Licence string

const (
	// LicenceCC0 is the CC0 1.0 public domain dedication.
	LicenceCC0 Licence = "cc0"
	// LicenceCCBY is CC BY 4.0.
	LicenceCCBY Licence = "cc-by"
	// LicenceCCBYSA is CC BY-SA 4.0.
	LicenceCCBYSA Licence = "cc-by-sa"
	// LicenceCCBYNCSA is CC BY-NC-SA 4.0.
	LicenceCCBYNCSA Licence = "cc-by-nc-sa"
	// LicenceCCBYNCND is CC BY-NC-ND 4.0, which forbids derivative works and
	// therefore forbids translation.
	LicenceCCBYNCND Licence = "cc-by-nc-nd"
	// LicenceArXiv is arXiv's own nonexclusive distribution licence, which is
	// the default and grants arXiv the right to distribute and nobody else the
	// right to republish.
	LicenceArXiv Licence = "arxiv-1.0"
	// LicenceUnknown is the value for a paper whose licence has not been
	// resolved yet. It is treated exactly as strictly as the default licence.
	LicenceUnknown Licence = "unknown"
)

// Licences lists every value in the order the licence table in the spec uses,
// which is most permissive first.
var Licences = []Licence{
	LicenceCC0,
	LicenceCCBY,
	LicenceCCBYSA,
	LicenceCCBYNCSA,
	LicenceCCBYNCND,
	LicenceArXiv,
	LicenceUnknown,
}

// URL is the canonical address of the licence deed, or the empty string for
// the unknown case.
func (l Licence) URL() string {
	switch l {
	case LicenceCC0:
		return "https://creativecommons.org/publicdomain/zero/1.0/"
	case LicenceCCBY:
		return "https://creativecommons.org/licenses/by/4.0/"
	case LicenceCCBYSA:
		return "https://creativecommons.org/licenses/by-sa/4.0/"
	case LicenceCCBYNCSA:
		return "https://creativecommons.org/licenses/by-nc-sa/4.0/"
	case LicenceCCBYNCND:
		return "https://creativecommons.org/licenses/by-nc-nd/4.0/"
	case LicenceArXiv:
		return "https://arxiv.org/licenses/nonexclusive-distrib/1.0/"
	default:
		return ""
	}
}

// SPDX is the identifier a licence field in front matter carries.
//
// A different string from the Licence value on purpose. The Licence value is
// arXiv's own and it says which of six choices the submitter made. This is what
// our file is under, it is written where a tool looking for a licence expects
// to find one, and SPDX is what those tools read. The empty string is not a
// licence: it is the answer for a paper none of whose content may be published,
// and a file carrying it should not exist.
func (l Licence) SPDX() string {
	switch l {
	case LicenceCC0:
		return "CC0-1.0"
	case LicenceCCBY:
		return "CC-BY-4.0"
	case LicenceCCBYSA:
		return "CC-BY-SA-4.0"
	case LicenceCCBYNCSA:
		return "CC-BY-NC-SA-4.0"
	case LicenceCCBYNCND:
		return "CC-BY-NC-ND-4.0"
	default:
		return ""
	}
}

// ParseLicence reads a licence from a harvested record or from front matter.
//
// An empty string is unknown rather than an error, because a record that names
// no licence is the ordinary case for a paper submitted before arXiv offered a
// choice, and the corpus handles that by publishing nothing rather than by
// failing to load.
func ParseLicence(s string) (Licence, error) {
	if s == "" {
		return LicenceUnknown, nil
	}
	for _, l := range Licences {
		if Licence(s) == l {
			return l, nil
		}
	}
	return LicenceUnknown, fmt.Errorf("corpus: %q is not an arXiv licence", s)
}

// Access is what the corpus may do with a paper, derived from the licence of
// the version it published.
type Access string

const (
	// AccessOpen is cc0 and cc-by. Everything is permitted, including
	// translation.
	AccessOpen Access = "open"
	// AccessShareAlike is cc-by-sa and cc-by-nc-sa. Everything is permitted and
	// the translation inherits the same licence.
	AccessShareAlike Access = "share-alike"
	// AccessVerbatim is cc-by-nc-nd. The English text and the figures may be
	// republished as they stand and no translation may ever be made, because a
	// translation is a derivative work.
	AccessVerbatim Access = "verbatim"
	// AccessRecord is arxiv-1.0 and unknown. Metadata, structure, tags and
	// graph edges only, with a link out to arXiv for the text.
	AccessRecord Access = "record"
)

// AccessFor returns the access class a licence produces.
//
// This is the gate the whole project sits behind, so it is one function, it has
// no configuration, and every caller goes through it rather than writing the
// comparison out again.
func AccessFor(l Licence) Access {
	switch l {
	case LicenceCC0, LicenceCCBY:
		return AccessOpen
	case LicenceCCBYSA, LicenceCCBYNCSA:
		return AccessShareAlike
	case LicenceCCBYNCND:
		return AccessVerbatim
	default:
		return AccessRecord
	}
}

// MayPublishText reports whether the full English text and the figures of a
// paper may be committed to the corpus.
func (a Access) MayPublishText() bool {
	return a == AccessOpen || a == AccessShareAlike || a == AccessVerbatim
}

// MayTranslate reports whether a translation may be made.
//
// A translation is a derivative work, so this is false for the no-derivatives
// licence and false for everything the corpus only holds a record of.
func (a Access) MayTranslate() bool {
	return a == AccessOpen || a == AccessShareAlike
}

// PublishedLicence is the licence our own file carries, given the licence of
// the article it was made from.
//
// Share-alike propagates, so a translation of a CC BY-SA paper is CC BY-SA and
// not CC BY. Everything the corpus writes for itself, which is the structure,
// the tags and the reports, is CC BY regardless.
func PublishedLicence(source Licence) (Licence, error) {
	switch source {
	case LicenceCC0, LicenceCCBY:
		return LicenceCCBY, nil
	case LicenceCCBYSA:
		return LicenceCCBYSA, nil
	case LicenceCCBYNCSA:
		return LicenceCCBYNCSA, nil
	case LicenceCCBYNCND:
		return LicenceCCBYNCND, nil
	default:
		return LicenceUnknown, fmt.Errorf("corpus: nothing may be published for a %s paper", source)
	}
}

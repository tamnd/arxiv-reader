package metadata

// Fill folds an incoming record onto a stored one.
//
// The rule is that an empty field in the incoming record does not clear a field
// that is set in the stored one.
//
// That is not a general belief about data, it is a fact about arXiv's surfaces.
// No surface says everything. The OAI-PMH arXivRaw format has the version
// history, the sizes and the licence, and gives the author list as one string
// that nothing can reliably split. The arXiv format has the authors split into
// surnames and forenames, and no version history at all: its created date on
// math/9602216 is 2024-03-03, twenty eight years after the paper was submitted,
// because it means the date of the metadata and not the date of the paper. The
// Kaggle snapshot has structured authors and versions and one licence for the
// whole paper rather than one per version.
//
// So every harvest writes a partial record, and a merge that let the last
// surface to run win would mean the others may as well not have run at all.
//
// The cost of the rule is that a field arXiv genuinely removes never clears.
// That is what Replace is for, and a caller that knows the incoming record is
// the whole truth should use it.
func Fill(stored, incoming Record) Record {
	out := incoming
	out.ID = pick(incoming.ID, stored.ID)
	out.Title = pick(incoming.Title, stored.Title)
	out.Abstract = pick(incoming.Abstract, stored.Abstract)
	out.DOI = pick(incoming.DOI, stored.DOI)
	out.JournalRef = pick(incoming.JournalRef, stored.JournalRef)
	out.ReportNo = pick(incoming.ReportNo, stored.ReportNo)
	out.Comments = pick(incoming.Comments, stored.Comments)
	out.MSCClass = pick(incoming.MSCClass, stored.MSCClass)
	out.ACMClass = pick(incoming.ACMClass, stored.ACMClass)
	out.Source = Source(pick(string(incoming.Source), string(stored.Source)))
	out.Harvested = pick(incoming.Harvested, stored.Harvested)

	if len(out.Authors) == 0 {
		out.Authors = stored.Authors
	}
	if len(out.Categories) == 0 {
		out.Categories = stored.Categories
	}
	out.Versions = fillVersions(stored.Versions, incoming.Versions)
	return out
}

// Replace takes the incoming record whole, except that it still will not
// unresolve a licence.
//
// The licence exception is not negotiable in either mode. The Kaggle snapshot
// carries no per version licence at all, so a re-bootstrap after the census
// would otherwise wipe out the one field the entire content plane depends on,
// and it would do it silently across three million records.
func Replace(stored, incoming Record) Record {
	out := incoming
	out.Versions = keepLicences(stored.Versions, incoming.Versions)
	return out
}

// fillVersions keeps every version either side knows about.
//
// A surface that knows about fewer versions than the stored record does is a
// surface that cannot see them, not a paper that lost them. arXiv never
// withdraws a version: a withdrawal is itself a new version whose text says so.
func fillVersions(stored, incoming []Version) []Version {
	if len(incoming) == 0 {
		return stored
	}
	byNumber := make(map[int]Version, len(stored)+len(incoming))
	order := make([]int, 0, len(stored)+len(incoming))
	add := func(v Version) {
		if _, seen := byNumber[v.Version]; !seen {
			order = append(order, v.Version)
		}
		byNumber[v.Version] = v
	}
	for _, v := range stored {
		add(v)
	}
	for _, v := range incoming {
		was, seen := byNumber[v.Version]
		if seen {
			if v.Created.IsZero() {
				v.Created = was.Created
			}
			if v.Licence == "" {
				v.Licence, v.LicenceFrom = was.Licence, was.LicenceFrom
			}
		}
		add(v)
	}
	out := make([]Version, 0, len(order))
	for _, n := range order {
		out = append(out, byNumber[n])
	}
	sortVersions(out)
	return out
}

// keepLicences carries a resolved licence forward onto a version that has none.
func keepLicences(stored, incoming []Version) []Version {
	if len(stored) == 0 {
		return incoming
	}
	prior := make(map[int]Version, len(stored))
	for _, v := range stored {
		prior[v.Version] = v
	}
	out := append([]Version(nil), incoming...)
	for i, v := range out {
		if v.Licence != "" {
			continue
		}
		if was, ok := prior[v.Version]; ok && was.Licence != "" {
			out[i].Licence, out[i].LicenceFrom = was.Licence, was.LicenceFrom
		}
	}
	return out
}

func pick(incoming, stored string) string {
	if incoming != "" {
		return incoming
	}
	return stored
}

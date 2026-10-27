package analysis

import (
	"fmt"
	"strconv"

	"github.com/tamnd/arxiv-reader/objects"
	"github.com/tamnd/arxiv-reader/tags"
)

// TagSeed is what the tag space of the analysis plane of a paper is derived
// from. It is not the paper id, so the analysis plane does not hand out the
// tags of the content plane in the same order.
func TagSeed(paper string) string { return paper + "#analysis" }

// Tagged is the result of AssignTags.
type Tagged struct {
	Register tags.Register
	Run      *tags.Run
	// Kept, Added and Buried count the tags that stayed, the new tags and
	// the tags that got a tombstone.
	Kept, Added, Buried int
}

// AssignTags gives a tag to each object of one version of a paper (AX3.9),
// and sets the field tag of the records. prev is the records that the
// register was last assigned against, which are the records of the version of
// the register as the objects file held them before this run. The four passes
// of the tag matcher carry the register from prev to the new records first,
// so a tag stays on its object when the numbers move, and an object that is
// gone gets a tombstone. taken is the tags of the content plane of the paper,
// which a new tag must not take.
func AssignTags(paper string, version int, recs []objects.Record, prev []objects.Record, old tags.Register, taken []tags.Tag) (Tagged, error) {
	cur := tagObjects(recs)
	if len(cur) == 0 {
		return Tagged{Register: old}, nil
	}
	reg := old
	var out Tagged
	if len(old.Entries) > 0 {
		if old.Version > version {
			return out, fmt.Errorf("the tag register of %s is at v%d, and v%d is older, so its objects get no tags", paper, old.Version, version)
		}
		m := old.Move(tags.Match(tagObjects(prev), cur), version)
		if len(m.Stray) > 0 {
			return out, fmt.Errorf("the tag register of %s names %s, which the objects of v%d do not have", paper, m.Stray[0], old.Version)
		}
		reg = m.Register
		out.Buried = len(m.Buried)
	}
	plan, err := tags.AssignWith(TagSeed(paper), cur, reg, taken)
	if err != nil {
		return out, err
	}
	if err := plan.Err(); err != nil {
		return out, err
	}
	plan.Register.Version = version
	for i := range recs {
		if t, ok := plan.Assigned[recs[i].Local]; ok && recs[i].Path == "source" {
			recs[i].Tag = string(t)
		}
	}
	out.Register, out.Run = plan.Register, plan.Run
	out.Kept, out.Added = plan.Kept, len(plan.Added)
	return out, nil
}

// tagObjects is the objects of the source path in the form of the tag matcher.
// The text is the content hash of AX3.8, which each record has, also a record
// of a version of class record.
func tagObjects(recs []objects.Record) []tags.Object {
	var out []tags.Object
	for _, r := range recs {
		if r.Path != "source" {
			continue
		}
		out = append(out, tags.Object{File: r.File, Local: r.Local, Class: r.Kind, Label: r.Label, Text: r.Hash})
	}
	return out
}

// VersionRecords is the records of one version.
func VersionRecords(recs []objects.Record, version int) []objects.Record {
	v := strconv.Itoa(version)
	var out []objects.Record
	for _, r := range recs {
		if r.Version == v {
			out = append(out, r)
		}
	}
	return out
}

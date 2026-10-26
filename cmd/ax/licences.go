package main

import (
	"errors"
	"io/fs"

	"github.com/tamnd/arxiv-cli/pkg/axid"
	"github.com/tamnd/arxiv-reader/corpus"
	"github.com/tamnd/arxiv-reader/metadata"
)

// licences reads the licence of a version from the metadata plane, one month
// at a time, and keeps each month that it read.
type licences struct {
	plane  metadata.Plane
	months map[string]map[string]metadata.Record
}

// text tells if a version may hold text of the paper (AX5.4). known is false
// when the plane has no licence for the version.
func (l *licences) text(paper string, version int) (text, known bool) {
	lic, ok := l.licence(paper, version)
	if !ok || lic == "" || lic == corpus.LicenceUnknown {
		return false, false
	}
	return corpus.AccessFor(lic).MayPublishText(), true
}

func (l *licences) licence(paper string, version int) (corpus.Licence, bool) {
	id, err := axid.Parse(paper)
	if err != nil {
		return "", false
	}
	shard := corpus.Shard(id)
	if l.months == nil {
		l.months = map[string]map[string]metadata.Record{}
	}
	month, ok := l.months[shard]
	if !ok {
		month = map[string]metadata.Record{}
		err := l.plane.Scan(shard, func(r metadata.Record) error {
			month[r.ID] = r
			return nil
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", false
		}
		l.months[shard] = month
	}
	r, ok := month[id.Canonical]
	if !ok {
		return "", false
	}
	v, ok := r.VersionAt(version)
	if !ok {
		return "", false
	}
	return v.Licence, true
}

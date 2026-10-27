package analysis

import "time"

// Summary is reports/census.json, the counts of reports/census.md that other
// programs read. m2 corpus status -layer research reads it for the first two
// numbers of AX1.5: the papers with no state and the census gaps.
type Summary struct {
	Generated time.Time `json:"generated"`
	// Since is the first month that the report reads, as YYYY-MM, or empty.
	Since string `json:"since,omitempty"`
	// ScopeCounted is false when the report did not read the metadata plane,
	// and then Scope, Extracted, Stopped, NoState, Closed and ClosedOutside
	// are zero.
	ScopeCounted bool `json:"scope_counted"`
	// Scope is the open and share-alike papers in scope. Extracted and Stopped
	// are the ones whose latest version has a census record or a stop record,
	// and NoState is the rest.
	Scope     int `json:"scope"`
	Extracted int `json:"extracted"`
	Stopped   int `json:"stopped"`
	NoState   int `json:"no_state"`
	// Closed is the papers in scope that are not open or share-alike, and
	// ClosedOutside the ones with a structure from TheoremGraph (way R1).
	Closed        int `json:"closed"`
	ClosedOutside int `json:"closed_outside"`
	// Versions is the versions with a census record. Gaps is their gaps and
	// phantoms, and WithGap the versions with at least one.
	Versions int `json:"versions"`
	Gaps     int `json:"gaps"`
	WithGap  int `json:"with_gap"`
	Waivers  int `json:"waivers"`
	// Strength is the versions of each strength of AX4.6.
	Strength map[string]int `json:"strength"`
	// StopReasons is the stop records of each reason of AX1.6.
	StopReasons map[string]int `json:"stop_reasons"`
	// Areas is the same counts for each MSC code at two digits. Its scope
	// counts are zero when ScopeCounted is false.
	Areas map[string]AreaSummary `json:"areas,omitempty"`
}

// AreaSummary is the counts of one MSC code at two digits.
type AreaSummary struct {
	Scope     int `json:"scope"`
	Extracted int `json:"extracted"`
	Stopped   int `json:"stopped"`
	NoState   int `json:"no_state"`
	Versions  int `json:"versions"`
	Gaps      int `json:"gaps"`
}

// Summary returns the counts of the report.
func (rp *Report) Summary(scopeCounted bool) Summary {
	s := Summary{
		Generated:    rp.Generated.UTC(),
		Since:        rp.Since,
		ScopeCounted: scopeCounted,
		Versions:     rp.All.Papers,
		Gaps:         rp.All.Gaps,
		WithGap:      rp.All.WithGap,
		Waivers:      rp.All.Waivers,
		Strength:     map[string]int{},
		StopReasons:  map[string]int{},
	}
	for k, v := range rp.All.Strength {
		s.Strength[k] = v
	}
	for k, v := range rp.Stopped {
		s.StopReasons[k] = v
	}
	area := func(code string) AreaSummary {
		if s.Areas == nil {
			s.Areas = map[string]AreaSummary{}
		}
		return s.Areas[code]
	}
	for code, r := range rp.codes {
		a := area(code)
		a.Versions, a.Gaps = r.Papers, r.Gaps
		s.Areas[code] = a
	}
	if scopeCounted {
		for code, x := range rp.Areas {
			a := area(code)
			a.Scope, a.Extracted, a.Stopped = x.Scope, x.Extracted, x.Halted
			a.NoState = x.Scope - x.Extracted - x.Halted
			s.Areas[code] = a
		}
		s.Scope, s.Extracted, s.Stopped = rp.Scope, rp.Extracted, rp.Halted
		s.NoState = rp.Scope - rp.Extracted - rp.Halted
		s.Closed, s.ClosedOutside = rp.Closed, rp.ClosedOutside
	}
	return s
}

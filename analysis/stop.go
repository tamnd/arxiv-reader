package analysis

import (
	"encoding/json"
	"fmt"
	"os"
)

// The reasons of the state stopped (AX1.6). The list is closed.
const (
	StopNoSource  = "no-source"
	StopWithdrawn = "withdrawn"
	StopNotMath   = "not-math"
	StopLicence   = "licence"
	StopFailed    = "failed"
)

// StopReasons is the closed list of the reasons, in the order of the report.
var StopReasons = []string{StopNoSource, StopWithdrawn, StopNotMath, StopLicence, StopFailed}

// Stopped is the state of a version that cannot go further.
const Stopped = "stopped"

// Stop is the record of a version that cannot go further, in
// analysis/stopped/<YYMM>/<id>v<n>.json. A version with a census record is
// extracted, and a version with a stop record is stopped.
type Stop struct {
	Paper   string `json:"paper"`
	Version int    `json:"version"`
	State   string `json:"state"`
	Reason  string `json:"reason"`
	Detail  string `json:"detail,omitempty"`
	By      string `json:"by"`
	On      string `json:"on"`
}

// KnownReason tells if a reason is in the closed list.
func KnownReason(reason string) bool {
	for _, r := range StopReasons {
		if r == reason {
			return true
		}
	}
	return false
}

// ReadStop reads a stop record.
func ReadStop(file string) (Stop, error) {
	var s Stop
	b, err := os.ReadFile(file)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("%s: %w", file, err)
	}
	return s, nil
}

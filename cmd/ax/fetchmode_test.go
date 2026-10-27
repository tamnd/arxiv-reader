package main

import (
	"errors"
	"testing"
)

func TestFetchModeAnalysisIsRefused(t *testing.T) {
	for _, args := range [][]string{
		{"--mode", "analysis", "2401.00001v1"},
		{"source", "-mode", "analysis", "2401.00001v1"},
		{"source", "--mode=analysis", "2401.00001v1"},
		{"render", "-mode=files", "2401.00001v1"},
	} {
		if err := runFetch(args); !errors.Is(err, errAnalysisMode) {
			t.Errorf("ax fetch %v gave %v, and it must be refused until AQ01 is settled", args, err)
		}
	}
}

package main

import (
	"slices"
	"strings"
	"testing"
)

func TestFetchMode(t *testing.T) {
	for _, c := range []struct {
		args []string
		mode string
		rest []string
	}{
		{[]string{"--mode", "analysis", "2401.00001v1"}, "analysis", []string{"2401.00001v1"}},
		{[]string{"source", "-mode", "analysis", "2401.00001v1"}, "analysis", []string{"source", "2401.00001v1"}},
		{[]string{"source", "--mode=analysis", "2401.00001v1"}, "analysis", []string{"source", "2401.00001v1"}},
	} {
		mode, rest, ok := fetchMode(c.args)
		if !ok || mode != c.mode || !slices.Equal(rest, c.rest) {
			t.Errorf("fetchMode(%v) = %q, %v, %v", c.args, mode, rest, ok)
		}
	}
	if _, _, ok := fetchMode([]string{"source", "2401.00001v1"}); ok {
		t.Error("an ax fetch with no --mode has a mode")
	}
	if err := runFetch([]string{"render", "-mode=files", "2401.00001v1"}); err == nil || !strings.Contains(err.Error(), "only the mode analysis") {
		t.Errorf("ax fetch -mode=files gave %v", err)
	}
}

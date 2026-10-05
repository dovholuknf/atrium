//go:build darwin

package roomstats

import "testing"

func TestParseTopIdle(t *testing.T) {
	v, ok := parseTopIdle("CPU usage: 41.29% user, 55.36% sys, 3.34% idle ")
	if !ok || v != 3.34 {
		t.Errorf("got %v %v", v, ok)
	}
	if _, ok := parseTopIdle("Processes: 400 total"); ok {
		t.Error("parsed a line with no cpu")
	}
}

package inputlag

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	cases := map[string]time.Duration{
		"":     0,
		"0":    0,
		"off":  0,
		"1":    defaultThreshold,
		"on":   defaultThreshold,
		" ON ": defaultThreshold,
		"5":    5 * time.Millisecond,
		"250":  250 * time.Millisecond,
		"-3":   defaultThreshold,
		"lots": defaultThreshold,
	}
	for in, want := range cases {
		if got := parse(in); got != want {
			t.Errorf("parse(%q) = %v, want %v", in, got, want)
		}
	}
}

// keep puts the switch back as the test found it.
func keep(t *testing.T) {
	wasT, wasP := threshold.Load(), pinned
	t.Cleanup(func() {
		threshold.Store(wasT)
		pinned = wasP
	})
}

func TestOverIsFalseWhenOff(t *testing.T) {
	keep(t)
	threshold.Store(0)
	if On() || Over(time.Hour) {
		t.Fatal("off must never report a hop as slow")
	}
	threshold.Store(int64(10 * time.Millisecond))
	if !Over(10*time.Millisecond) || Over(9*time.Millisecond) {
		t.Fatal("the threshold is inclusive and nothing under it logs")
	}
}

// The gear switches it both ways, at the default threshold.
func TestSetLiveSwitchesItWhenNotPinned(t *testing.T) {
	keep(t)
	pinned = false
	threshold.Store(0)
	if !SetLive(true) || !On() || !Over(defaultThreshold) {
		t.Fatal("switching it on from a setting did not take")
	}
	if !SetLive(false) || On() {
		t.Fatal("switching it off from a setting did not take")
	}
}

// The variable is an override. A process started with a 5ms threshold keeps it
// whatever the checkbox says.
func TestSetLiveLeavesAPinnedThresholdAlone(t *testing.T) {
	keep(t)
	pinned = true
	threshold.Store(int64(5 * time.Millisecond))
	if SetLive(false) {
		t.Fatal("the setting reported it took over a pinned threshold")
	}
	if !Over(5 * time.Millisecond) {
		t.Fatal("the setting moved a threshold the variable pinned")
	}
}

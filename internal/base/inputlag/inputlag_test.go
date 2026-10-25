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

func TestOverIsFalseWhenOff(t *testing.T) {
	sw := newSwitch("")
	if sw.On() || sw.Over(time.Hour) {
		t.Fatal("off must never report a hop as slow")
	}
	sw.threshold.Store(int64(10 * time.Millisecond))
	if !sw.Over(10*time.Millisecond) || sw.Over(9*time.Millisecond) {
		t.Fatal("the threshold is inclusive and nothing under it logs")
	}
}

// The gear switches it both ways, at the default threshold.
func TestSetLiveSwitchesItWhenNotPinned(t *testing.T) {
	sw := newSwitch("")
	if !sw.SetLive(true) || !sw.On() || !sw.Over(defaultThreshold) {
		t.Fatal("switching it on from a setting did not take")
	}
	if !sw.SetLive(false) || sw.On() {
		t.Fatal("switching it off from a setting did not take")
	}
}

// The variable is an override. A process started with a 5ms threshold keeps it
// whatever the checkbox says.
func TestSetLiveLeavesAPinnedThresholdAlone(t *testing.T) {
	sw := newSwitch("5")
	if sw.SetLive(false) {
		t.Fatal("the setting reported it took over a pinned threshold")
	}
	if !sw.Over(5 * time.Millisecond) {
		t.Fatal("the setting moved a threshold the variable pinned")
	}
}

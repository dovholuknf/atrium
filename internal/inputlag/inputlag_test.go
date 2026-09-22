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
	was := threshold
	defer func() { threshold = was }()
	threshold = 0
	if On() || Over(time.Hour) {
		t.Fatal("off must never report a hop as slow")
	}
	threshold = 10 * time.Millisecond
	if !Over(10*time.Millisecond) || Over(9*time.Millisecond) {
		t.Fatal("the threshold is inclusive and nothing under it logs")
	}
}

package cli

import (
	"strings"
	"testing"
)

func TestRoomNameOfKeepsWhatAHubNameTakesAndTurnsAwayTheRest(t *testing.T) {
	for host, want := range map[string]string{
		"m1mini":                "m1mini",
		"Mac-17.local":          "Mac-17.local",
		"Clint's Mac":           "Clint-s-Mac",
		"":                      "room",
		"...":                   "room",
		strings.Repeat("a", 80): strings.Repeat("a", 64),
	} {
		if got := roomNameOf(host); got != want {
			t.Errorf("roomNameOf(%q) = %q, want %q", host, got, want)
		}
	}
}

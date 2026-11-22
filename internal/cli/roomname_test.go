package cli

import (
	"strings"
	"testing"
)

func TestRoomNameOfKeepsWhatAHubNameTakesAndTurnsAwayTheRest(t *testing.T) {
	for host, want := range map[string]string{
		"room-a":                "room-a",
		"Mac-17.local":          "Mac-17.local",
		"Operator's Mac":        "Operator-s-Mac",
		"":                      "room",
		"...":                   "room",
		strings.Repeat("a", 80): strings.Repeat("a", 64),
	} {
		if got := roomNameOf(host); got != want {
			t.Errorf("roomNameOf(%q) = %q, want %q", host, got, want)
		}
	}
}

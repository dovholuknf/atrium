package api

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/inputlag"
)

// The gear's checkbox switches this room's logging at once, keeps it for the
// next start, and reads back what is in force.
func TestTheInputLagSettingSwitchesTheRoomLive(t *testing.T) {
	if !inputlag.SetLive(false) {
		t.Skip(inputlag.Env + " is set, so the setting cannot switch the logging")
	}
	t.Cleanup(func() { inputlag.SetLive(false) })
	srv, st, _ := fileServer(t)

	if rec := settingsPost(t, srv, `{"input_lag_log":true}`); rec.Code != 200 {
		t.Fatalf("the switch answered %d: %s", rec.Code, rec.Body.String())
	}
	if !inputlag.On() {
		t.Fatal("the room's logging did not come on")
	}
	if got := settingsGet(t, srv)["input_lag_log"]; got != true {
		t.Fatalf("settings read back input_lag_log=%v", got)
	}

	// A restart applies what was stored.
	inputlag.SetLive(false)
	ApplyInputLag(st)
	if !inputlag.On() {
		t.Fatal("a room coming up did not apply the stored switch")
	}

	if rec := settingsPost(t, srv, `{"input_lag_log":false}`); rec.Code != 200 {
		t.Fatalf("switching it off answered %d", rec.Code)
	}
	if inputlag.On() {
		t.Fatal("the room's logging did not go off")
	}
}

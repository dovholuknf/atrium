//go:build integration

package api

import "testing"

// The room cog's checkbox for the unexpected-exit notice: on by default, and
// off when switched off.
func TestTheUnexpectedExitSettingIsOnUntilSwitchedOff(t *testing.T) {
	srv, st, _ := fileServer(t)

	if got := settingsGet(t, srv)["unexpected_exit_wake"]; got != true {
		t.Fatalf("a new room reads unexpected_exit_wake=%v", got)
	}
	if rec := settingsPost(t, srv, `{"unexpected_exit_wake":false}`); rec.Code != 200 {
		t.Fatalf("switching it off answered %d: %s", rec.Code, rec.Body.String())
	}
	if st.UnexpectedExitOn() {
		t.Fatal("the room still has the notice on")
	}
	if got := settingsGet(t, srv)["unexpected_exit_wake"]; got != false {
		t.Fatalf("settings read back unexpected_exit_wake=%v", got)
	}
	if rec := settingsPost(t, srv, `{"unexpected_exit_wake":true}`); rec.Code != 200 || !st.UnexpectedExitOn() {
		t.Fatalf("switching it back on answered %d", rec.Code)
	}
}

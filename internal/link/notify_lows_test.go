package link

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The cr48 lows in notify (f-025).

// A NUL in a card's name used to fail the run, because exec refuses an
// environment value holding one, and three of those switched notify off. The
// environment now drops it, and stdin carries the name whole.
func TestANULInACardNameStillNotifies(t *testing.T) {
	sink, out := recordingCommand(t)
	res := sink.Send(context.Background(), Notice{Name: "bad\x00name", Reason: "input", Card: "c", Room: "r"})
	if !res.OK {
		t.Fatalf("a name with a NUL failed the send: %+v", res)
	}
	raw, _ := os.ReadFile(out)
	var rec struct{ Name, Stdin string }
	if err := json.Unmarshal(bytes.TrimSpace(raw), &rec); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	if rec.Name != "badname" {
		t.Errorf("the environment carried %q, want the name without its NUL", rec.Name)
	}
	var line map[string]string
	_ = json.Unmarshal([]byte(rec.Stdin), &line)
	if line["name"] != "bad\x00name" {
		t.Errorf("stdin carried %q, want the name whole", line["name"])
	}
}

// A command that has gone from the PATH does not stop notify being turned off.
func TestAStaleCommandDoesNotBlockTurningNotifyOff(t *testing.T) {
	n, _, _ := armed(t)
	if err := n.Configure(false, []string{"definitely-not-a-program-zzz"}); err != nil {
		t.Fatalf("turning notify off with a stale command was refused: %v", err)
	}
	if n.Status().Enabled {
		t.Error("notify is still on")
	}
	if err := n.Configure(true, []string{"definitely-not-a-program-zzz"}); err == nil {
		t.Error("turning it back on with a command that is not there was allowed")
	}
}

// The origin:agent skip matches the way the control tools match a tag.
func TestTheAgentOriginSkipIgnoresCaseAndSpace(t *testing.T) {
	payload := `{"status":"needs-permission","waiting_since":"T1","tags":[" Origin:Agent "]}`
	if got, ok := NotifyIdentity("a", json.RawMessage(payload)); ok {
		t.Errorf("an agent-started card with its tag spelled differently notified: %+v", got)
	}
}

// TURNING NOTIFY BACK ON STARTS EVERY ROOM UNSEEDED. Nothing is recorded while
// it is off, so a room still marked seeded from last time would announce
// everything that piled up since on its next change.
func TestReenablingNotifyForgetsWhichRoomsWereSeeded(t *testing.T) {
	n, st, rs := armed(t)
	// A room seeded while notify was on, then an empty cache for it, so seed()
	// has nothing to mark it with on the way back on.
	n.markSeeded("sparta")
	st.mu.Lock()
	st.cache["sparta"] = nil
	st.mu.Unlock()
	if err := n.Configure(false, []string{os.Args[0]}); err != nil {
		t.Fatal(err)
	}
	if err := n.Configure(true, []string{os.Args[0]}); err != nil {
		t.Fatal(err)
	}
	if n.isSeeded("sparta") {
		t.Fatal("a room seeded before notify was switched off still reads as seeded")
	}
	// Its first announcement after that is recorded silently.
	n.Announced("sparta", []CardState{{ID: "c1", Status: "needs-input",
		Payload: json.RawMessage(`{"status":"needs-input","waiting_since":"T9"}`)}})
	if got := rs.count(); got != 0 {
		t.Errorf("the first announcement after re-enabling notified %d times, want none", got)
	}
	if !strings.Contains(st.settings[settingNotifySeeded+"sparta"], "T") {
		t.Errorf("the room was not seeded by its first announcement: %q", st.settings[settingNotifySeeded+"sparta"])
	}
}

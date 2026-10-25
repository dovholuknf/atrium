//go:build integration

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/link"
)

func runRoomHub(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := roomHubCmd()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(args)
	err := c.Execute()
	return out.String(), err
}

func TestRoomHubChangesTheAddressAndKeepsTheOldFile(t *testing.T) {
	dir := t.TempDir()
	old := `{"hub": "192.168.1.68:7779", "room": "sgg"}`
	if err := os.WriteFile(filepath.Join(dir, "room.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runRoomHub(t, "--dir", dir)
	if err != nil || strings.TrimSpace(out) != "192.168.1.68:7779" {
		t.Fatalf("with no argument it printed %q, %v", out, err)
	}

	out, err = runRoomHub(t, "--dir", dir, "http://192.168.102.41:7779/")
	if err != nil {
		t.Fatalf("changing the address: %v\n%s", err, out)
	}
	if !strings.Contains(out, "restart") {
		t.Errorf("it did not say to restart the room:\n%s", out)
	}
	now, _ := os.ReadFile(filepath.Join(dir, "room.json"))
	if !strings.Contains(string(now), `"192.168.102.41:7779"`) || !strings.Contains(string(now), `"sgg"`) {
		t.Errorf("room.json is now %s, want the new hub and the same room", now)
	}
	baks, _ := filepath.Glob(filepath.Join(dir, "room.json.bak-*"))
	if len(baks) != 1 {
		t.Fatalf("%d backups, want 1", len(baks))
	}
	if got, _ := os.ReadFile(baks[0]); string(got) != old {
		t.Errorf("the backup is %q, want the file as it was", got)
	}
}

func TestRoomHubRefusesWhatIsNotAnAddressOrNotADirectRoom(t *testing.T) {
	dir := t.TempDir()
	if _, err := runRoomHub(t, "--dir", dir, "10.0.0.1:7779"); err == nil {
		t.Error("a room that never joined was given an address")
	}
	path := filepath.Join(dir, "room.json")
	for _, c := range []struct{ body, arg string }{
		{`{"hub": "a:1", "room": "r"}`, "no-port"},
		{`{"transport": "ziti", "hub": "", "room": "r"}`, "10.0.0.1:7779"},
	} {
		if err := os.WriteFile(path, []byte(c.body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := runRoomHub(t, "--dir", dir, c.arg); err == nil {
			t.Errorf("%s with %s was accepted", c.body, c.arg)
		}
		if got, _ := os.ReadFile(path); string(got) != c.body {
			t.Errorf("a refused change still wrote room.json: %s", got)
		}
	}
}

// A running room that reaches no rung takes the address `atrium room hub` wrote at its next attempt, with no restart.
func TestARoomThatCannotReachItsHubTakesTheNewAddressWithoutARestart(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "room.json"), []byte(`{"hub": "192.168.1.68:7779", "room": "sgg"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	keys := link.Keys{Dir: dir}
	saved, _ := keys.Joined()
	l, _, err := roomLadder(keys, saved, link.RoomTransports{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runRoomHub(t, "--dir", dir, "192.168.102.41:7779"); err != nil {
		t.Fatal(err)
	}
	reloadRungs(keys, l)
	if got := ladderSays(l.Rungs()); !strings.Contains(got, "192.168.102.41:7779") || strings.Contains(got, "192.168.1.68") {
		t.Fatalf("the ladder is %s, want the new address and not the old", got)
	}
}

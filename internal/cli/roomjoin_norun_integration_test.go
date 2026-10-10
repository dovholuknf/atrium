//go:build integration

package cli

import (
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/link"
)

// `room join --no-run` saves the join and returns, which is what lets a
// provisioner join over ssh and hand the running to a service. Without the flag
// the command runs the room and never returns, so a hang here is the failure.
func TestRoomJoinNoRunSavesAndReturns(t *testing.T) {
	dir := t.TempDir()
	tok, err := link.MintOverlayToken("ziti", "vm1", "atrium-hub", "")
	if err != nil {
		t.Fatal(err)
	}
	c := joinCmd()
	c.SetArgs([]string{tok, "--dir", dir, "--identity", "vm1.json", "--no-run"})

	done := make(chan error, 1)
	go func() { done <- c.Execute() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("join --no-run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("join --no-run did not return, so it went on to run the room")
	}

	saved, err := link.Keys{Dir: dir}.Joined()
	if err != nil {
		t.Fatalf("nothing saved: %v", err)
	}
	if saved.Room != "vm1" || saved.Transport != "ziti" {
		t.Fatalf("saved %+v, want room vm1 over ziti", saved)
	}
}

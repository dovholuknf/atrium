//go:build integration

package daemon

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/store"
)

// A launch into a directory a live card holds is refused naming that card, and `skip` still hands the card back.
func TestLaunchRefusedInADirectoryACardHolds(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	dir := t.TempDir()
	held, _, err := d.st.Register(store.Observed{WireName: "holder", Worktree: filepath.ToSlash(dir), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	h := slowHarness(t, d)
	_, err = d.Launch(shared.LaunchRequest{Harness: h, Cwd: filepath.ToSlash(dir)})
	if err == nil || !strings.Contains(err.Error(), held.ID) {
		t.Fatalf("err = %v, want a refusal naming %s", err, held.ID)
	}
	got, err := d.Launch(shared.LaunchRequest{Harness: h, Cwd: filepath.ToSlash(dir), IfRunning: "skip"})
	if err != nil || got.ID != held.ID {
		t.Fatalf("skip = %v, %v, want the card already there", got, err)
	}
}

// A launch whose cwd names a share is refused before anything stats it, so it opens no SMB session.
func TestLaunchRefusesANetworkCwd(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	h := slowHarness(t, d)
	_, err := d.Launch(shared.LaunchRequest{Harness: h, Cwd: "//evil/share"})
	if err == nil || !strings.Contains(err.Error(), "network path") {
		t.Fatalf("err = %v, want a network path refusal", err)
	}
}

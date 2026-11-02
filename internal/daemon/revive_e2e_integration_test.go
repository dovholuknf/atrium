//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/ptyhost"
	"github.com/dovholuknf/atrium/internal/store"
)

// hostShellCard launches a card on a real shell that lands in the pty host.
func hostShellCard(t *testing.T, d *Daemon) *store.Task {
	t.Helper()
	cmd, args := "sh", []string{"-c", "read x"}
	if runtime.GOOS == "windows" {
		cmd, args = "cmd.exe", []string{"/d", "/k"}
	}
	home := t.TempDir()
	if _, err := d.st.SaveHarness(store.Harness{ID: "shelltest", Label: "shell test", Enabled: true, Cmd: cmd,
		Args: args, LaunchMode: store.LaunchPTY, Env: map[string]string{"HOME": home, "USERPROFILE": home,
			"ATRIUM_HUB_URL": "http://127.0.0.1:1", "ATRIUM_BOARD_URL": "http://127.0.0.1:1"}}); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "atrium-revive-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	task, err := d.Launch(shared.LaunchRequest{Harness: "shelltest", Cwd: dir})
	if err != nil {
		t.Skipf("could not spawn a test runner on this machine: %v", err)
	}
	if r := d.sup.get(task.ID); r == nil || hostOf(r) == nil {
		t.Fatalf("the card's runner is not in the host: %+v", r)
	}
	return task
}

// b-cards-auto-revive part 5: the room and the pty host both go down with no wind-down, as a reboot or an OOM takes
// them, and both start again. Every card that was live comes back on its own, and a card closed on purpose does not.
func TestEveryLiveCardComesBackAfterTheRoomAndTheHostAreKilled(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	_ = t.TempDir()
	dir, err := os.MkdirTemp("", "ht")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	h, err := ptyhost.Listen(dir, ptyhost.Options{})
	if err != nil {
		t.Fatal(err)
	}
	d := daemonAt(t, dir)
	d.ph.opts = ptyhost.StartOptions{Exe: filepath.Join(dir, "no-such-atrium.exe")}

	a, b, closed := hostShellCard(t, d), hostShellCard(t, d), hostShellCard(t, d)
	if err := d.StopRunner(closed.ID); err != nil {
		t.Fatal(err)
	}
	hostWait(t, 10*time.Second, "the closed card's runner to stop", func() bool { return d.sup.get(closed.ID) == nil })

	// The room goes first and winds nothing down, then the host takes every runner with it.
	_ = d.Close()
	h.Close()

	h2, err := ptyhost.Listen(dir, ptyhost.Options{})
	if err != nil {
		t.Fatal(err)
	}
	d2 := daemonAt(t, dir)
	t.Cleanup(h2.Close)
	d2.ph.opts = ptyhost.StartOptions{Exe: filepath.Join(dir, "no-such-atrium.exe")}
	d2.waitForHost()
	d2.reattachRuns()
	d2.startFixtures()
	d2.reopenSaved()

	for _, task := range []*store.Task{a, b} {
		if r := d2.sup.get(task.ID); r == nil || hostOf(r) == nil {
			t.Fatalf("live card %s did not come back in the host: %+v", task.ID, r)
		}
	}
	if d2.sup.get(closed.ID) != nil {
		t.Fatal("the card closed on purpose came back")
	}
	if got := len(listOf(t, d2)); got != 2 {
		t.Fatalf("the new host holds %d runs, want the 2 live cards", got)
	}
}

//go:build integration

package daemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A card the pty host still holds has no runner until reattachRuns adopts it, so a reaper tick before that files it dead
// and the peers list drops a card with a live process.
func TestTheReaperWaitsForTheReattachBeforeFilingAnyCardDead(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d, err := testNew(Options{
		AgentAddr: freePort(t), HumanAddr: freePort(t), LongPoll: time.Second,
		DBPath:       filepath.ToSlash(filepath.Join(t.TempDir(), "atrium.db")),
		LocationFile: filepath.Join(t.TempDir(), "daemon.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	closeAtCleanup(t, d)
	task := quietTask(t, d, "readopted", 2147483000)
	if err := d.st.SetStatus(task.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}

	d.reattached = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.reap(ctx, 10*time.Millisecond)

	time.Sleep(200 * time.Millisecond) // wait: a quiet window, a tick that acted would have filed it by now
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusRunning {
		t.Fatalf("the reaper moved a card to %s before the reattach finished", got.Status)
	}

	close(d.reattached)
	waitFor(t, func() bool {
		g, err := d.st.Get(task.ID)
		return err == nil && g.Status == store.StatusDead
	})
}

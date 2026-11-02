//go:build integration

package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/ptyhost"
	"github.com/dovholuknf/atrium/internal/store"
)

func TestRestartPtyHostReopensOnAHostItReplaced(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	dir, h := hostFixture(t)
	d := daemonAt(t, dir)
	task := shellCard(t, d)
	if err := d.onSession(SessionEvent{Agent: task.WireName, Event: "start", TaskID: task.ID, Runner: "shelltest"}); err != nil {
		t.Fatal(err)
	}
	oldRun := d.sup.get(task.ID).runID
	d.hk.endHost = func(int, string) error {
		h.Close()
		nh, err := ptyhost.Listen(dir, ptyhost.Options{})
		if err != nil {
			return err
		}
		t.Cleanup(nh.Close)
		return nil
	}
	rep, err := d.RestartPtyHost(context.Background(), time.Second, "test", true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Ended != 1 || rep.Wanted != 1 || rep.Reopened != 1 {
		t.Fatalf("report %+v, want one ended, one wanted, one reopened", rep)
	}
	r2 := d.sup.get(task.ID)
	if r2 == nil || r2.runID == oldRun || hostOf(r2) == nil {
		t.Fatalf("the card is not running in the new host: %+v", r2)
	}
	if got, _ := d.st.Get(task.ID); got.Status == store.StatusDead {
		t.Fatalf("the card was filed dead by the restart")
	}
}

func TestRestartPtyHostIsRefusedWithTheSettingOff(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	if _, err := d.RestartPtyHost(context.Background(), time.Second, "", true); err == nil {
		t.Fatal("a room with no pty host restarted it")
	}
}

func TestRestartPtyHostRouteIsForThisMachineOnly(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	d.opts.ShutdownToken = "secret"
	rec := httptest.NewRecorder()
	d.handleRestartPtyHost(rec, httptest.NewRequest(http.MethodPost, "/v1/restart-pty-host?immediate=1", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a request with no token got %d, want 403", rec.Code)
	}
}

func TestEndHostNeverEndsItsOwnProcess(t *testing.T) {
	if err := endHost(os.Getpid(), ""); err == nil {
		t.Fatal("endHost would have killed the room")
	}
	if err := endHost(0, ""); err == nil {
		t.Fatal("endHost took pid 0")
	}
}

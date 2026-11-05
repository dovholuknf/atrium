package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon/cycle"
	"github.com/dovholuknf/atrium/internal/ptyhost"
)

// RESTARTING THE PTY HOST WHILE THE ROOM STAYS UP. A new claude binary is only picked up by a runner that is started
// afresh, and the pty host keeps runners across a room restart on purpose. This ends the host's runners the way
// `atrium stop --runners` does, replaces the host, and reopens the cards the way a room start does, so the ssh route
// and the hub link that ride this process are never cut. Every step is the room restart's own: the wrap-up and its
// wakes, `stopSupervised`, `startFixtures` and `reopenSaved`.

// hostGone is how long endHost waits for the old host to stop answering.
const hostGone = 10 * time.Second

// PtyHostReport is what RestartPtyHost did.
type PtyHostReport struct {
	Wrap     WrapReport `json:"wrap"`
	Ended    int        `json:"ended"`
	Wanted   int        `json:"wanted"`
	Reopened int        `json:"reopened"`
	OldPid   int        `json:"old_pid"`
	NewPid   int        `json:"new_pid"`
}

// endHost ends the host process at pid and returns once its address no longer answers. The host holds nothing by now.
func endHost(pid int, addr string) error {
	if pid <= 0 || pid == os.Getpid() {
		return fmt.Errorf("the pty host's pid is %d, which is not a process to end", pid)
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := p.Kill(); err != nil {
		return err
	}
	deadline := time.Now().Add(hostGone)
	for time.Now().Before(deadline) {
		cl, err := ptyhost.DialTimeout(addr, hostDial)
		if err != nil {
			return nil
		}
		_ = cl.Close()
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("the pty host (pid %d) still answers after %s", pid, hostGone)
}

// RestartPtyHost restarts this room's pty host and reopens the cards that had a runner in it.
func (d *Daemon) RestartPtyHost(ctx context.Context, wait time.Duration, why string, immediate bool) (PtyHostReport, error) {
	var rep PtyHostReport
	if !d.st.PtyHostOn() {
		return rep, errors.New("pty_host is off, so this room has no pty host to restart")
	}
	if d.stop.stopping() {
		return rep, errors.New("this room is stopping")
	}
	if !immediate {
		rep.Wrap = d.WrapUpForRestart(ctx, wait, why)
	}
	// One at a time, and a stop that arrives meanwhile is refused as one with the other kind of ask.
	if !d.stopAsked.CompareAndSwap(stopAskedNone, stopAskedRunners) {
		return rep, errors.New("a stop or a pty host restart is already in flight")
	}
	defer d.stopAsked.Store(stopAskedNone)
	d.stopRunners.Store(true)
	defer d.stopRunners.Store(false)

	cl, addr, err := d.hostClient(false, false)
	if err != nil {
		return rep, fmt.Errorf("no pty host answered: %w", err)
	}
	pr, err := cl.Probe()
	if err != nil {
		return rep, fmt.Errorf("probe the pty host: %w", err)
	}
	rep.OldPid = pr.Pid
	log.Printf("[atrium] restarting the pty host (pid %d), room staying up. %s", pr.Pid, logLine(orWord(why, "no reason given")))

	rep.Ended = len(d.sup.all())
	d.shortGate()
	d.stopSupervised(10 * time.Second)
	d.stopShells()

	d.ph.close()
	endErr := d.hk.endHost(pr.Pid, addr)
	if endErr != nil {
		log.Printf("[atrium] could not end the pty host: %v. reopening on whatever answers", endErr)
	}
	d.waitForHost()
	cl, _, err = d.hostClient(false, false)
	if err != nil {
		return rep, fmt.Errorf("no pty host came back, so nothing was reopened (a room restart reopens them): %w", err)
	}
	if np, err := cl.Probe(); err == nil {
		rep.NewPid = np.Pid
	}

	rep.Wanted = len(d.reopenWanted())
	d.startFixtures()
	d.reopenSaved()
	// reopenWanted skips a card that has a runner, so what it still lists did not come back.
	rep.Reopened = rep.Wanted - len(d.reopenWanted())
	log.Printf("[atrium] pty host restarted (pid %d, now %d): %d runner(s) ended, %d of %d card(s) reopened",
		rep.OldPid, rep.NewPid, rep.Ended, rep.Reopened, rep.Wanted)
	return rep, endErr
}

// handleRestartPtyHost is POST /v1/restart-pty-host: answered when the cards are back. `?wait=` is seconds for the
// wrap-up and `?immediate=1` skips it.
func (d *Daemon) handleRestartPtyHost(w http.ResponseWriter, r *http.Request) {
	if !d.shutdownAllowed(w, r) {
		return
	}
	wait := d.st.RestartWrapWait()
	if n := cycle.ParseSeconds(r.URL.Query().Get("wait")); n > 0 {
		wait = cycle.CapWrapWait(n)
	}
	rep, err := d.RestartPtyHost(r.Context(), wait, r.URL.Query().Get("why"), truthy(r.URL.Query().Get("immediate")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rep)
}

package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

// THE STOP THAT LEAVES RUNNERS UP. See docs/rnd/rolling-restart-design.md sections 3.4 and 5.
//
// With `pty_host` on, a runner lives in the host and not in this process, so `atrium stop` stops the daemon and
// leaves every host runner running for the next daemon to reattach. `atrium stop --runners` is today's stop: every
// runner is wound down with its exit keys first. What a stop that leaves runners up must still wait for is the
// work that lives only in this process and lasts seconds, which is the short gate below.

const (
	// reattachLineKey is what a reattached runner's typed line is marked unsure with. The line was in memory, the
	// operator may have half a command on it, and only Enter or control-c proves it empty.
	reattachLineKey = "a daemon restart, which loses what was typed"
)

// gatePoll is how often the short gate looks again, and gateGiveUp how long it waits before it says who holds it
// and stops anyway. The Daemon's own fields of the same names shorten them for a test.
const (
	defaultGatePoll   = 250 * time.Millisecond
	defaultGateGiveUp = 2 * time.Minute
)

// leavesRunnersUp reports whether this stop ends fewer runners than the room holds, which is when the gate and the
// in-job refusal apply.
func (d *Daemon) leavesRunnersUp(runners bool) bool {
	if runners {
		return false
	}
	for _, r := range d.sup.all() {
		if hostOf(r) != nil {
			return true
		}
	}
	for _, r := range d.sup.allShells() {
		if hostOf(r) != nil {
			return true
		}
	}
	return false
}

// shortGate waits while work that exists only in this process is under way, so a stop that leaves runners up does
// not cut it. Bounded: after gateGiveUp it logs which card holds what and stops anyway, which costs what that work
// costs when a restart cuts it, and never a runner.
func (d *Daemon) shortGate() {
	if d.stopRunners.Load() || !d.leavesRunnersUp(false) {
		return
	}
	poll, giveUp := d.gatePoll, d.gateGiveUp
	if poll <= 0 {
		poll = defaultGatePoll
	}
	if giveUp <= 0 {
		giveUp = defaultGateGiveUp
	}
	deadline := time.Now().Add(giveUp)
	var last string
	for {
		why := d.gateBusy()
		if why == "" {
			return
		}
		if why != last {
			log.Printf("[atrium] the stop is waiting for %s", why)
			last = why
		}
		if time.Now().After(deadline) {
			log.Printf("[atrium] gave up waiting after %s for %s, stopping anyway. no runner is ended by this",
				giveUp, why)
			return
		}
		time.Sleep(poll)
	}
}

// gateBusy names what holds the gate, or is empty when nothing does.
func (d *Daemon) gateBusy() string {
	if id := d.nctx.busyCard(); id != "" {
		return "a new-context cycle on " + id
	}
	if key := d.launching.heldKey(); key != "" {
		return "a launch in progress (" + key + ")"
	}
	if d.wake != nil && !d.wake.deliver.TryLock() {
		return "a restart wake being delivered"
	} else if d.wake != nil {
		d.wake.deliver.Unlock()
	}
	for _, rs := range [][]*runner{d.sup.all(), d.sup.allShells()} {
		for _, r := range rs {
			if !r.injectMu.TryLock() {
				return "an injection into " + r.taskID
			}
			r.injectMu.Unlock()
			if !r.pasteMu.TryLock() {
				return "a paste into " + r.taskID
			}
			r.pasteMu.Unlock()
		}
	}
	return ""
}

// busyCard is a card inside a new-context cycle that has not ended, or "".
func (n *newContexts) busyCard() string {
	if n == nil {
		return ""
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for id, cur := range n.by {
		if cur != nil && cur.step != NewContextFailed {
			return id
		}
	}
	return ""
}

// heldKey is a key whose mutex is held now, or "".
func (k *keyedMutex) heldKey() string {
	if k == nil {
		return ""
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	for key, m := range k.locks {
		if !m.TryLock() {
			return key
		}
		m.Unlock()
	}
	return ""
}

// shutdownBody is what `atrium stop` may send: whether to end the runners the pty host holds too.
type shutdownBody struct {
	Runners bool `json:"runners"`
}

// hostInJobCause says why a pty host in a job would die with it. The job is inherited down the chain of whatever
// started this room, so the parent is the nearest name there is for it.
func hostInJobCause(ppid int) string {
	return fmt.Sprintf("the pty host is inside a job object that refused breakaway, inherited from the process that "+
		"started this room (its parent was pid %d, usually a terminal tab or a scheduled task)", ppid)
}

// hostInJobRefusal is what a stop answers when the host would die with the job it sits in.
func hostInJobRefusal(ppid int) string {
	return hostInJobCause(ppid) + ", so this restart would end every runner. restart this room once from a jobless " +
		"start (scripts/live start-atrium.ps1, or `atrium run` started by a service), which ends runners once and " +
		"keeps the next restart from ending them. or stop with --runners to end them now"
}

// wantsRunners reads the request's ask to end runners, from the body or `?runners=1`.
func wantsRunners(r *http.Request) bool {
	if truthy(r.URL.Query().Get("runners")) {
		return true
	}
	if r.Body == nil {
		return false
	}
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
	var b shutdownBody
	_ = json.Unmarshal(raw, &b)
	return b.Runners
}

// handleHostState is `GET /v1/preflight`: what a deploy needs to know before it decides to hold the agents.
func (d *Daemon) handleHostState(w http.ResponseWriter, _ *http.Request) {
	build, _ := d.hostBuild.Load().(string)
	// A runner started before the setting went on lives in this process and ends with it, so a deploy that skips the
	// hold needs to know there are none.
	var held, local int
	for _, r := range append(d.sup.all(), d.sup.allShells()...) {
		if hostOf(r) != nil {
			held++
		} else {
			local++
		}
	}
	host := map[string]any{"on": d.st.PtyHostOn(), "in_job": d.hostInJob.Load(), "build": build}
	if d.hostInJob.Load() {
		host["in_job_cause"] = hostInJobCause(os.Getppid())
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"pty_host": host,
		"runners":  map[string]any{"host": held, "in_process": local},
	})
}

// allRunnersSurvive is true when there are runners and every runner and shell is held by the pty host, so a stop that was not asked to
// end runners ends none of them.
func (d *Daemon) allRunnersSurvive() bool {
	if d.stopRunners.Load() || !d.leavesRunnersUp(false) {
		return false
	}
	for _, r := range append(d.sup.all(), d.sup.allShells()...) {
		if hostOf(r) == nil {
			return false
		}
	}
	return true
}

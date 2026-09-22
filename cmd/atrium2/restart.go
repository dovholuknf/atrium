package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/link"
)

// Restarting a room because its hub asked it to.
//
// The hub spawns NOTHING on this machine. It sends the instruction over the link
// and this is the whole of the room's part: park the other agents so nothing is
// interrupted mid-tool, spawn a detached restarter that outlives this process,
// and wind down the way ctrl-c does. The restarter waits for this room to
// release its ports, then starts `atrium2 room` again on the same database.
//
// A room started by hand and asked to restart therefore comes back, which is the
// difference from an upgrade: an upgrade relies on a supervisor to start the new
// binary, and a restart brings itself back because a hub asked and there may be
// no supervisor.

const (
	// roomRestartDelay is how long the detached restarter waits before touching
	// anything, long enough for this process to begin winding down.
	roomRestartDelay = 4 * time.Second
	// roomStopGrace bounds how long the restarter waits for this room's ports to
	// come free before it starts the new one. A wind-down gives every runner ten
	// seconds, a stuck one two more to die, and the listeners five, so a busy
	// room takes most of twenty seconds on its own. A minute leaves room for that.
	roomStopGrace = 60 * time.Second
	// restartLogName is the restarter's own record, kept beside the room's keys.
	// See restartLog.
	restartLogName = "restart.log"
	// roomParkWait is how long a busy agent gets to reach a stopping point.
	roomParkWait = 90 * time.Second
	// roomParkPoll is how often to ask whether they have settled.
	roomParkPoll = 2 * time.Second
	// roomParkIdleAfter is how many seconds without activity means a session is
	// not working. See the same constant on the CLI control server.
	roomParkIdleAfter = 120
)

// roomLaunch is everything that decides WHICH room `atrium2 room` becomes. The
// restarter has to pass all of it on. Leaving one out starts a different room:
// without --dir it reads the default key directory, which on a machine joined
// more than once holds some other room's name and hub.
type roomLaunch struct {
	dir, db, human, agent string
	isolated, upgrades    bool
}

// restartArgs is the command line the detached restarter runs.
func (l roomLaunch) restartArgs() []string {
	args := []string{"room",
		"--restart-after", roomRestartDelay.String(),
		"--dir", l.dir,
		"--http", l.human, "--agent", l.agent}
	if strings.TrimSpace(l.db) != "" {
		args = append(args, "--db", l.db)
	}
	if l.isolated {
		args = append(args, "--isolated")
	}
	if l.upgrades {
		args = append(args, "--accept-upgrades")
	}
	return args
}

// restartLog appends one line to restart.log in the room's key directory, and
// says it on the log as well.
//
// THE RESTARTER IS OTHERWISE SILENT. It is detached, so nobody is reading its
// output, and the process that spawned it is gone by the time it matters. A
// restart that dies on the way back leaves nothing behind unless it writes it
// somewhere itself, and this file is that somewhere. Best effort: failing to
// write it never stops a restart.
func restartLog(dir, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[atrium] %s", msg)
	f, err := os.OpenFile(filepath.Join(dir, restartLogName), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s  pid %d  %s\n", time.Now().Format(time.RFC3339), os.Getpid(), msg)
}

// onHubRestart is the room's OnRestart handler: park, schedule, wind down.
func onHubRestart(boardURL string, l roomLaunch, stop func()) func(link.RestartAsk) {
	return func(ask link.RestartAsk) {
		why := strings.TrimSpace(ask.Why)
		if why != "" {
			log.Printf("[atrium] the hub asked this room to restart, for: %s", why)
		} else {
			log.Printf("[atrium] the hub asked this room to restart")
		}

		wait := roomParkWait
		if ask.WaitSeconds > 0 {
			wait = time.Duration(ask.WaitSeconds) * time.Second
		}
		busy := parkRoomAgents(boardURL, why, wait)
		if len(busy) > 0 && !ask.Force {
			restartLog(l.dir, "NOT restarting: still working: %s. pass force to interrupt them",
				strings.Join(busy, ", "))
			return
		}

		if err := spawnRoomRestart(l); err != nil {
			restartLog(l.dir, "could not schedule the restart: %v", err)
			return
		}
		log.Printf("[atrium] restart scheduled in %s, winding this room down", roomRestartDelay)
		stop()
	}
}

// spawnRoomRestart starts a detached copy of this binary that waits for the
// ports to free and then runs `atrium2 room` again.
//
// Detached and released, so it survives this process winding down. The same
// launch in full, so the room that comes back is the one that went away rather
// than a default it happened to pick.
//
// ITS OUTPUT GOES SOMEWHERE. A room whose stderr is a file, which is how a
// script starts one, hands that file on and the restarted room keeps writing
// the same log. Anything else, a console or a pipe somebody is reading, gets
// restart.log instead: a console is gone with this process, and a pipe held open
// by the child keeps its reader waiting for an end that never comes.
func spawnRoomRestart(l roomLaunch) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	args := l.restartArgs()
	out := os.Stderr
	if fi, err := out.Stat(); err != nil || !fi.Mode().IsRegular() {
		f, err := os.OpenFile(filepath.Join(l.dir, restartLogName), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			f = nil
		}
		out = f
	}
	p, err := startDetached(self, args, out)
	if out != nil && out != os.Stderr {
		_ = out.Close()
	}
	if err != nil {
		return err
	}
	restartLog(l.dir, "spawned restarter pid %d: %s %s", p.Pid, self, strings.Join(args, " "))
	// Released rather than waited on: tying its lifetime to this process is the
	// opposite of what detached means.
	return p.Release()
}

// waitForRoomRestart is what `--restart-after` does: give the old room time to
// let go of its address, then return so startup can bind it. It reports whether
// the address came free inside the grace.
//
// A LISTEN, NOT A CONNECT. The question is whether this process can take the
// port, and the only honest test of that is trying to. Starting while the old
// one still holds it produces a second room that cannot bind and exits, which
// looks exactly like the restart having done nothing.
//
// BINDING THE PORT IS TAKEN AS PROOF THE OLD ROOM'S DATABASE IS GONE TOO. That
// coupling is real because the old room closes its store on the way down, in the
// daemon's shutdown before Run returns, and only then does the process exit and
// free this port. See internal/daemon/daemon.go shutdown, which closes the store
// explicitly so this is a guarantee rather than a side effect of process exit.
func waitForRoomRestart(after time.Duration, human string) bool {
	time.Sleep(after)
	deadline := time.Now().Add(roomStopGrace)
	for time.Now().Before(deadline) {
		ln, err := net.Listen("tcp", human)
		if err == nil {
			_ = ln.Close()
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

// ── parking the other agents ──────────────────────────────

// parkCard is one supervised session as the room's board reports it.
type parkCard struct {
	ID           string `json:"id"`
	DisplayTitle string `json:"display_title"`
	Status       string `json:"status"`
	Supervised   bool   `json:"supervised"`
	Idle         int    `json:"idle_seconds"`
	Activity     *struct {
		What string `json:"what"`
	} `json:"activity"`
}

// parkRoomAgents tells everything working on this room to stop, then waits for
// it, and returns whoever was still busy when the wait ran out.
//
// A compact port of the CLI control server's parkAgents, against this room's own
// loopback board. The rules are the same and the header there carries the
// reasoning: a session waiting on a human is safe, a session that said it was
// over is not working, and stale activity is not activity.
func parkRoomAgents(boardURL, why string, wait time.Duration) []string {
	busy := busyRoomAgents(boardURL)
	if len(busy) == 0 {
		return nil
	}
	for id := range busy {
		_ = tellRoomToPark(boardURL, id, why)
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		time.Sleep(roomParkPoll)
		busy = busyRoomAgents(boardURL)
		if len(busy) == 0 {
			return nil
		}
	}
	titles := make([]string, 0, len(busy))
	for _, title := range busy {
		titles = append(titles, title)
	}
	sort.Strings(titles)
	return titles
}

// busyRoomAgents maps card id to title for every supervised session that looks
// like it is working right now.
func busyRoomAgents(boardURL string) map[string]string {
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Get(boardURL + "/v1/tasks")
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	var body struct {
		Tasks []parkCard `json:"tasks"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil
	}
	busy := map[string]string{}
	for _, t := range body.Tasks {
		if !t.Supervised {
			continue
		}
		switch t.Status {
		case "needs-input", "needs-permission", "done", "dead", "shelved":
			continue
		}
		// Stale activity is not activity: it is written when a tool starts and
		// nothing writes when a turn ends, so a session that stopped an hour ago
		// still reads as busy. A session thinking has nothing half written.
		if t.Idle > roomParkIdleAfter {
			continue
		}
		if t.Activity != nil && t.Activity.What == "thinking" {
			continue
		}
		busy[t.ID] = t.DisplayTitle
	}
	return busy
}

// tellRoomToPark queues the stop-now message on a session, delivered as a block
// on its next tool call. Best effort: a session that cannot be told is one that
// gets interrupted, which is the situation without any of this.
func tellRoomToPark(boardURL, id, why string) error {
	text := "atrium is about to restart, so this terminal is going to close. " +
		"Stop at a safe point NOW: finish or abandon the edit you are in the middle of, " +
		"do not start anything new, and do not run another tool. " +
		"Your conversation is kept and you will be resumed."
	if why != "" {
		text += " The restart is for: " + why + "."
	}
	payload, _ := json.Marshal(map[string]string{"text": text})
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Post(boardURL+"/v1/tasks/"+id+"/message",
		"application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

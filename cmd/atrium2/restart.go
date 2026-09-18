package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
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
	// come free before it starts the new one.
	roomStopGrace = 20 * time.Second
	// roomParkWait is how long a busy agent gets to reach a stopping point.
	roomParkWait = 90 * time.Second
	// roomParkPoll is how often to ask whether they have settled.
	roomParkPoll = 2 * time.Second
	// roomParkIdleAfter is how many seconds without activity means a session is
	// not working. See the same constant on the CLI control server.
	roomParkIdleAfter = 120
)

// onHubRestart is the room's OnRestart handler: park, schedule, wind down.
func onHubRestart(boardURL, human, agent, db string, stop func()) func(link.RestartAsk) {
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
			log.Printf("[atrium] NOT restarting: still working: %s. pass force to interrupt them",
				strings.Join(busy, ", "))
			return
		}

		if err := spawnRoomRestart(human, agent, db); err != nil {
			log.Printf("[atrium] could not schedule the restart: %v", err)
			return
		}
		log.Printf("[atrium] restart scheduled in %s, winding this room down", roomRestartDelay)
		stop()
	}
}

// spawnRoomRestart starts a detached copy of this binary that waits for the
// ports to free and then runs `atrium2 room` again.
//
// Detached and released, so it survives this process winding down. Same
// database, address and agent listener, so the room that comes back is the one
// that went away rather than a default it happened to pick.
func spawnRoomRestart(human, agent, db string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"room",
		"--restart-after", roomRestartDelay.String(),
		"--http", human, "--agent", agent}
	if strings.TrimSpace(db) != "" {
		args = append(args, "--db", db)
	}
	cmd := exec.Command(self, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	// Released rather than waited on: tying its lifetime to this process is the
	// opposite of what detached means.
	return cmd.Process.Release()
}

// waitForRoomRestart is what `--restart-after` does: give the old room time to
// let go of its address, then return so startup can bind it.
//
// A LISTEN, NOT A CONNECT. The question is whether this process can take the
// port, and the only honest test of that is trying to. Starting while the old
// one still holds it produces a second room that cannot bind and exits, which
// looks exactly like the restart having done nothing.
func waitForRoomRestart(after time.Duration, human string) {
	time.Sleep(after)
	deadline := time.Now().Add(roomStopGrace)
	for time.Now().Before(deadline) {
		ln, err := net.Listen("tcp", human)
		if err == nil {
			_ = ln.Close()
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
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

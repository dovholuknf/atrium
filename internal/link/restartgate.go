package link

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// The hub restart gate. See docs/hub-restart-gate.md.
//
// A HUB RESTART DROPS EVERY BOARD PANE, and the deploy that does it runs from a
// script with no idea whether somebody is typing into one. So the script asks
// first, and the hub answers once nobody is:
//
//	script  POST /_hub/restart          held open until the answer
//	board   POST /_hub/restart/input    a keystroke or a click, throttled
//	board   POST /_hub/restart/pause    the countdown toast was clicked
//	board   POST /_hub/restart/resume   the paused toast's resume button
//	board   GET  /_hub/restart          where things stand, for a window that
//	                                    opened after the last event
//
// THE HUB HOLDS THE COUNTDOWN, not the script and not a board. Every board
// window hears it on the same stream, so a click in any one of them pauses all
// of them, and the script only learns the answer. Nothing restarts without
// `go`: the gate never stops the hub itself.
//
// IN MEMORY ON PURPOSE. A pause outliving the process it paused would be a
// pause on a hub that is already gone, and a restart for any other reason is
// the end of what it was guarding.

// restartEvent is the stream event every board hears.
const restartEvent = "hub-restart"

// The script's knobs, in seconds, and how far each may be turned.
const (
	defaultCountdown = 5
	defaultIdle      = 10
	defaultWait      = 300
	maxCountdown     = 60
	maxIdle          = 600
	maxWait          = 3600
)

// restartGate is one hub's gate.
type restartGate struct {
	mu        sync.Mutex
	lastInput time.Time
	paused    bool
	// asking is true while a script is holding a request open. One at a time:
	// two deploys racing is a mistake worth refusing out loud.
	asking bool
	// until is when the countdown on screen ends, zero when none is showing.
	until time.Time

	// wake is poked by every input, pause and resume, so a waiting request
	// re-reads the state rather than sleeping through it.
	wake chan struct{}

	// emit says a state to every board. boards is how many are listening.
	emit   func(state map[string]any)
	boards func() int
	audit  func(kind, detail string)
	now    func() time.Time
}

func newRestartGate(emit func(map[string]any), boards func() int, audit func(string, string)) *restartGate {
	return &restartGate{
		wake: make(chan struct{}, 1), emit: emit, boards: boards, audit: audit, now: time.Now,
	}
}

func (g *restartGate) poke() {
	select {
	case g.wake <- struct{}{}:
	default:
	}
}

// input records that somebody did something on a board.
func (g *restartGate) input() {
	g.mu.Lock()
	g.lastInput = g.now()
	counting := !g.until.IsZero()
	g.mu.Unlock()
	if counting {
		g.poke()
	}
}

// pause stops a countdown and holds every later one until resume.
func (g *restartGate) pause() map[string]any {
	g.mu.Lock()
	was := g.paused
	g.paused = true
	g.until = time.Time{}
	g.mu.Unlock()
	if !was {
		g.emit(map[string]any{"state": "paused"})
		g.audit(restartEvent, "paused from the board")
	}
	g.poke()
	return g.state()
}

// resume lifts a pause. The click that resumed counts as input, so a waiting
// restart still waits out the idle window before its countdown.
func (g *restartGate) resume() map[string]any {
	g.mu.Lock()
	was := g.paused
	g.paused = false
	g.lastInput = g.now()
	g.mu.Unlock()
	if was {
		g.emit(map[string]any{"state": "resumed"})
		g.audit(restartEvent, "resumed from the board")
	}
	g.poke()
	return g.state()
}

// state is where things stand, for a board that opened after the last event.
func (g *restartGate) state() map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	left := 0.0
	if !g.until.IsZero() {
		if d := g.until.Sub(g.now()); d > 0 {
			left = d.Seconds()
		}
	}
	return map[string]any{
		"paused": g.paused, "waiting": g.asking, "countdown_left": left, "boards": g.boards(),
	}
}

// claim takes the one slot a script may hold. False means another script
// already holds it.
func (g *restartGate) claim() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.asking {
		return false
	}
	g.asking = true
	return true
}

// ask is one script's request, answered `go`, `paused` or `busy`.
//
// `go` means restart now. `paused` and `busy` both mean do not: the first
// because somebody clicked, the second because the boards never went quiet
// before the script's wait ran out.
//
// The caller holds the claim, and this releases it.
func (g *restartGate) ask(ctx context.Context, countdown, idle, wait time.Duration) (string, error) {
	shown := false
	defer func() {
		g.mu.Lock()
		g.asking = false
		g.until = time.Time{}
		g.mu.Unlock()
	}()
	// A countdown on screen that is not going to finish is taken down, or the
	// boards would count to zero and wait for a restart nobody is doing.
	takeDown := func() {
		if shown {
			g.emit(map[string]any{"state": "cancelled"})
			shown = false
		}
	}

	deadline := g.now().Add(wait)
	sleep := func(d time.Duration) bool {
		if d <= 0 {
			return ctx.Err() == nil
		}
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return false
		case <-g.wake:
		case <-t.C:
		}
		return true
	}

	for {
		now := g.now()
		g.mu.Lock()
		paused, last := g.paused, g.lastInput
		g.mu.Unlock()

		if !now.Before(deadline) {
			takeDown()
			if paused {
				g.audit(restartEvent, "the deploy gave up: paused from the board")
				return "paused", nil
			}
			g.audit(restartEvent, "the deploy gave up: the board never went idle")
			return "busy", nil
		}
		if paused {
			// The pause already took the countdown down on every board.
			shown = false
			if !sleep(deadline.Sub(now)) {
				return "", ctx.Err()
			}
			continue
		}
		// NOBODY WATCHING, NOBODY TO WARN. Asked every pass, so the last
		// window closing during a wait lets the restart through.
		if g.boards() == 0 {
			takeDown()
			g.audit(restartEvent, "restarting: no board is open")
			return "go", nil
		}
		if quiet := now.Sub(last); quiet < idle {
			takeDown()
			if !sleep(minDur(idle-quiet, deadline.Sub(now))) {
				return "", ctx.Err()
			}
			continue
		}

		// Idle long enough. Count down where everybody can see it.
		start := now
		g.mu.Lock()
		g.until = start.Add(countdown)
		g.mu.Unlock()
		g.emit(map[string]any{"state": "countdown", "seconds": countdown.Seconds()})
		shown = true
		for {
			g.mu.Lock()
			left := g.until.Sub(g.now())
			stop := g.paused || g.until.IsZero() || g.lastInput.After(start)
			g.mu.Unlock()
			if stop || left <= 0 {
				break
			}
			if !sleep(left) {
				takeDown()
				return "", ctx.Err()
			}
		}
		g.mu.Lock()
		paused, last = g.paused, g.lastInput
		g.until = time.Time{}
		g.mu.Unlock()
		if paused {
			shown = false
			continue
		}
		// SOMEBODY STARTED TYPING DURING THE COUNTDOWN. Back to waiting for
		// quiet, without a pause: nobody asked for one.
		if last.After(start) {
			takeDown()
			continue
		}
		if ctx.Err() != nil {
			takeDown()
			return "", ctx.Err()
		}
		g.emit(map[string]any{"state": "restarting"})
		g.audit(restartEvent, "restarting after a countdown nobody paused")
		return "go", nil
	}
}

func minDur(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// seconds reads one knob off the request, defaulted and clamped.
func seconds(v float64, def, max int) time.Duration {
	if v <= 0 {
		v = float64(def)
	}
	if v > float64(max) {
		v = float64(max)
	}
	return time.Duration(v * float64(time.Second))
}

// serveRestart answers every `/_hub/restart` path. `sub` is what follows it.
func (p *Proxy) serveRestart(w http.ResponseWriter, r *http.Request, sub string) {
	g := p.restart
	switch sub {
	case "":
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(g.state())
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		// THE ASK IS LOOPBACK ONLY. It restarts nothing itself, but it is the
		// deploy script's door and nothing reached over an overlay is a deploy
		// script. Pausing is not guarded the same way: a pause only ever holds a
		// restart back, and the phone board is exactly where one gets clicked.
		if !loopbackRemote(r.RemoteAddr) {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"error":"a hub restart is asked for from the machine the hub runs on"}`)
			return
		}
		var body struct {
			Countdown float64 `json:"countdown"`
			Idle      float64 `json:"idle"`
			Wait      float64 `json:"wait"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body)
		if !g.claim() {
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"error":"a restart is already waiting for an answer"}`)
			return
		}
		// The headers go now, so a script waiting minutes for its answer is
		// holding a request that has been accepted rather than one that looks
		// hung.
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		answer, err := g.ask(r.Context(),
			seconds(body.Countdown, defaultCountdown, maxCountdown),
			seconds(body.Idle, defaultIdle, maxIdle),
			seconds(body.Wait, defaultWait, maxWait))
		if err != nil {
			// The script went away, so nobody is reading an answer.
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answer": answer})
	case "input", "pause", "resume":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch sub {
		case "input":
			g.input()
			fmt.Fprint(w, `{"ok":true}`)
		case "pause":
			_ = json.NewEncoder(w).Encode(g.pause())
		case "resume":
			_ = json.NewEncoder(w).Encode(g.resume())
		}
	default:
		http.NotFound(w, r)
	}
}

package link

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/edge"
)

// The hub restart gate. See docs/fabric/hub-restart-gate.md.
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
//
// HELD MEANS HELD UNTIL SOMEBODY SAYS GO. A script that says it can re-poll
// (`hold` in its ask) gets a bounded long-poll per request and an ask that
// outlives any one of them. A pause holds that ask with no timeout, however
// long it takes, and a resume starts the idle wait over. The ask lives only as
// long as the script keeps polling: one that stops for `lease` is taken as
// gone, and its countdown comes down.

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
	// How long one request of a re-polling script is held before it is
	// answered `waiting` and asked again.
	maxHold = 60
	// How long an ask survives with no script polling it.
	defaultLease = 30 * time.Second
)

// heldAsk is one re-polling script's ask. It outlives each request, and is
// found again by id.
type heldAsk struct {
	id     string
	done   chan struct{}
	answer string
	why    string
	cancel context.CancelFunc
	// Guarded by the gate's mu.
	attached  int
	seen      time.Time
	delivered bool
}

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
	// why is the reason for the last go. See saidGo.
	why string
	// held is a re-polling script's ask, kept until its answer is collected
	// or nobody polls it for `lease`. It holds the slot as `asking` does.
	held  *heldAsk
	lease time.Duration
	asks  int

	// wake is poked by every input, pause and resume, so a waiting request
	// re-reads the state rather than sleeping through it.
	wake chan struct{}

	// boot names this hub process. A board that saw `restarting` holds its
	// cover until `GET /_hub/restart` answers with a different one, which only
	// the new hub can. A stream reopening cannot say that: it may be the old
	// hub's stream coming back from a blip before the old hub goes.
	boot string

	// emit says a state to every board. boards is how many are listening.
	emit   func(state map[string]any)
	boards func() int
	audit  func(kind, detail string)
	now    func() time.Time
}

func newRestartGate(emit func(map[string]any), boards func() int, audit func(string, string)) *restartGate {
	return &restartGate{
		wake: make(chan struct{}, 1), emit: emit, boards: boards, audit: audit, now: time.Now,
		lease: defaultLease, boot: bootID(),
	}
}

func bootID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprint(time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
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
		"boot": g.boot,
	}
}

// claim takes the one slot a script may hold. False means another script
// already holds it.
func (g *restartGate) claim() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.asking || g.held != nil {
		return false
	}
	g.asking = true
	return true
}

// start runs a claimed ask for a re-polling script, detached from any one
// request. The ask ends with its answer, or is cancelled once nobody has polled
// it for `lease`: the script went away and nobody would restart on a `go`.
func (g *restartGate) start(countdown, idle, wait time.Duration) *heldAsk {
	ctx, cancel := context.WithCancel(context.Background())
	g.mu.Lock()
	g.asks++
	a := &heldAsk{
		id: fmt.Sprintf("%d-%d", g.now().UnixNano(), g.asks), done: make(chan struct{}),
		cancel: cancel, seen: g.now(),
	}
	g.held = a
	lease := g.lease
	g.mu.Unlock()
	go func() {
		answer, err := g.ask(ctx, countdown, idle, wait, true)
		if err == nil {
			a.answer, a.why = answer, g.lastWhy()
		}
		close(a.done)
	}()
	go func() {
		defer cancel()
		tick := time.NewTicker(maxDur(lease/4, 10*time.Millisecond))
		defer tick.Stop()
		for range tick.C {
			g.mu.Lock()
			gone := a.delivered || (a.attached == 0 && g.now().Sub(a.seen) > lease)
			if gone && g.held == a {
				g.held = nil
			}
			g.mu.Unlock()
			if gone {
				return
			}
		}
	}()
	return a
}

// find is the held ask with this id, nil when there is none: it was answered
// and collected, its script stopped polling, or this is a hub that restarted
// since the ask was made.
func (g *restartGate) find(id string) *heldAsk {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.held == nil || g.held.id != id {
		return nil
	}
	return g.held
}

// collect waits up to `hold` for a held ask's answer. Empty means none yet, and
// the script asks again.
func (g *restartGate) collect(ctx context.Context, a *heldAsk, hold time.Duration) string {
	g.mu.Lock()
	a.attached++
	g.mu.Unlock()
	t := time.NewTimer(hold)
	defer t.Stop()
	answer := ""
	select {
	case <-a.done:
		answer = a.answer
	case <-t.C:
	case <-ctx.Done():
	}
	g.mu.Lock()
	a.attached--
	a.seen = g.now()
	// An empty answer after done is an ask that was cancelled, which is not
	// something to hand a script.
	if answer != "" {
		a.delivered = true
		if g.held == a {
			g.held = nil
		}
	}
	g.mu.Unlock()
	return answer
}

// ask is one script's request, answered `go`, `paused` or `busy`.
//
// `go` means restart now. `paused` and `busy` both mean do not: the first
// because somebody clicked, the second because the boards never went quiet
// before the script's wait ran out.
//
// A PATIENT ask never answers `paused`. It waits out a pause for as long as it
// lasts, and a resume starts its wait over, so the boards get the whole idle
// window again rather than whatever was left of it before the click. Only a
// script that re-polls asks patiently: one waiting on a single request would
// time out and read the silence as no hub at all, which is a `go`.
//
// The caller holds the claim, and this releases it.
func (g *restartGate) ask(ctx context.Context, countdown, idle, wait time.Duration, patient bool) (string, error) {
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
	// Until an input, a pause or a resume pokes it, with no timer at all.
	sleepHeld := func() bool {
		select {
		case <-ctx.Done():
			return false
		case <-g.wake:
			return true
		}
	}
	held := false

	for {
		now := g.now()
		g.mu.Lock()
		paused, last := g.paused, g.lastInput
		g.mu.Unlock()

		if paused && patient {
			// The pause already took the countdown down on every board.
			shown = false
			if !held {
				held = true
				g.audit(restartEvent, "the deploy is held until the board resumes it")
			}
			if !sleepHeld() {
				return "", ctx.Err()
			}
			continue
		}
		if held {
			held = false
			deadline = now.Add(wait)
		}

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
			g.saidGo("no board is open")
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
		watching := g.boards()
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
		g.saidGo(fmt.Sprintf("counted down on %d board stream(s) and nobody paused", watching))
		return "go", nil
	}
}

// saidGo records why the gate said go, for the audit line and the script.
//
// THE TWO GOES ARE TOLD APART OUT LOUD. With one message for both, a deploy
// that counted down on an open board read exactly like one that found no board,
// and the gate was blamed for not counting a board it had counted.
func (g *restartGate) saidGo(why string) {
	g.mu.Lock()
	g.why = why
	g.mu.Unlock()
	g.audit(restartEvent, "restarting: "+why)
}

// lastWhy is the reason for the last go. One ask runs at a time, so it is that
// ask's.
func (g *restartGate) lastWhy() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.why
}

// goAnswer is the body a script reads. `why` rides on a go only.
func goAnswer(answer, why string) map[string]any {
	out := map[string]any{"answer": answer}
	if answer == "go" && why != "" {
		out["why"] = why
	}
	return out
}

func minDur(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func maxDur(a, b time.Duration) time.Duration {
	if a > b {
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
		// THE ASK IS THE MACHINE'S OWN USER'S ONLY. It restarts nothing itself, but it is the
		// deploy script's door and nothing reached over an overlay is a deploy
		// script. Pausing is not guarded the same way: a pause only ever holds a
		// restart back, and the phone board is exactly where one gets clicked.
		if !edge.LocalOperator(r) {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprintf(w, `{"error":%q}`, "a hub restart is asked for from the machine the hub runs on"+edge.ProxyNote(r))
			return
		}
		var body struct {
			Countdown float64 `json:"countdown"`
			Idle      float64 `json:"idle"`
			Wait      float64 `json:"wait"`
			// Hold is how long this request may be held, from a script that
			// re-polls. Ask is the id of the ask it is polling.
			Hold float64 `json:"hold"`
			Ask  string  `json:"ask"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body)
		if body.Hold > 0 || body.Ask != "" {
			p.serveHeldAsk(w, r, body.Ask, seconds(body.Hold, maxHold, maxHold),
				seconds(body.Countdown, defaultCountdown, maxCountdown),
				seconds(body.Idle, defaultIdle, maxIdle),
				seconds(body.Wait, defaultWait, maxWait))
			return
		}
		// A SCRIPT THAT DOES NOT RE-POLL waits on this one request, so its ask
		// is not patient: a pause still ends in `paused` when its wait runs out.
		// Holding it forever would end in its own timeout, read as no hub.
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
			seconds(body.Wait, defaultWait, maxWait), false)
		if err != nil {
			// The script went away, so nobody is reading an answer.
			return
		}
		_ = json.NewEncoder(w).Encode(goAnswer(answer, g.lastWhy()))
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

// serveHeldAsk answers one request of a re-polling script. The first request
// starts the ask and later ones name it. Each is held for at most `hold`, then
// answered `waiting` with the ask's id and whether it is paused, and the
// script asks again.
func (p *Proxy) serveHeldAsk(w http.ResponseWriter, r *http.Request, id string,
	hold, countdown, idle, wait time.Duration) {
	g := p.restart
	var a *heldAsk
	if id == "" {
		if !g.claim() {
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"error":"a restart is already waiting for an answer"}`)
			return
		}
		a = g.start(countdown, idle, wait)
	} else if a = g.find(id); a == nil {
		// A HUB THAT RESTARTED has no memory of the ask, and neither does one
		// whose ask ended. Either way the script is told so rather than left
		// polling for an answer that is never coming.
		w.WriteHeader(http.StatusGone)
		fmt.Fprint(w, `{"error":"this hub has no such restart ask. it restarted, or the ask ended"}`)
		return
	}
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	answer := g.collect(r.Context(), a, hold)
	if r.Context().Err() != nil {
		return
	}
	if answer != "" {
		_ = json.NewEncoder(w).Encode(goAnswer(answer, a.why))
		return
	}
	st := g.state()
	_ = json.NewEncoder(w).Encode(map[string]any{"answer": "waiting", "ask": a.id, "paused": st["paused"]})
}

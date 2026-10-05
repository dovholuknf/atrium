package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// gateUnderTest is a gate whose boards and events the test controls.
type gateUnderTest struct {
	g      *restartGate
	mu     sync.Mutex
	states []string
	boards int
}

func newGateUnderTest(boards int) *gateUnderTest {
	t := &gateUnderTest{boards: boards}
	t.g = newRestartGate(func(s map[string]any) {
		t.mu.Lock()
		t.states = append(t.states, s["state"].(string))
		t.mu.Unlock()
	}, func() int {
		t.mu.Lock()
		defer t.mu.Unlock()
		return t.boards
	}, func(string, string) {})
	return t
}

// gateClock is the time a gate reads, which a test can stop.
//
// A STOPPED CLOCK IS WHAT MAKES THESE TESTS SAFE ON A LOADED MACHINE. With a
// running one, a test that sleeps between keystrokes or reacts to a countdown
// is racing the gate: a goroutine held back past the idle window or the
// countdown lets the gate see quiet, or finish, before the test acts. While the
// clock is stopped the gate still wakes on its real timers but sees no time
// pass, so nothing counts down until the test says so.
type gateClock struct {
	mu      sync.Mutex
	stopped bool
	at      time.Time
	offset  time.Duration
	reads   int
}

// stopClock gives a gate a clock stopped at the real time now.
func stopClock(g *restartGate) *gateClock {
	c := &gateClock{stopped: true, at: time.Now()}
	g.now = c.now
	return c
}

func (c *gateClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	if c.stopped {
		return c.at
	}
	return time.Now().Add(c.offset)
}

// advance moves a stopped clock on by d.
func (c *gateClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

// start lets the clock run from where it stopped.
func (c *gateClock) start() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offset = c.at.Sub(time.Now())
	c.stopped = false
}

// waitRead waits for the clock to be read more than n times, which is the gate
// taking another look.
func (c *gateClock) waitRead(tb testing.TB, n int) {
	tb.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		reads := c.reads
		c.mu.Unlock()
		if reads > n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	tb.Fatalf("the gate never looked at the clock again after %d reads", n)
}

func (c *gateClock) readCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads
}

func (t *gateUnderTest) said() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.states...)
}

// waitSaid waits for a state to have been said.
func (t *gateUnderTest) waitSaid(tb testing.TB, state string) {
	tb.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, s := range t.said() {
			if s == state {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	tb.Fatalf("the gate never said %q, it said %v", state, t.said())
}

type answer struct {
	said string
	err  error
}

func (t *gateUnderTest) ask(ctx context.Context, countdown, idle, wait time.Duration) <-chan answer {
	return t.askPatient(ctx, countdown, idle, wait, false)
}

func (t *gateUnderTest) askPatient(ctx context.Context, countdown, idle, wait time.Duration,
	patient bool) <-chan answer {
	out := make(chan answer, 1)
	if !t.g.claim() {
		out <- answer{err: context.Canceled}
		return out
	}
	go func() {
		s, err := t.g.ask(ctx, countdown, idle, wait, patient)
		out <- answer{s, err}
	}()
	return out
}

func got(tb testing.TB, ch <-chan answer) answer {
	tb.Helper()
	select {
	case a := <-ch:
		return a
	case <-time.After(10 * time.Second):
		tb.Fatal("the gate never answered")
		return answer{}
	}
}

// NO BOARD OPEN IS NOBODY TO WARN, so the answer is immediate and nothing is
// counted down.
func TestNoBoardOpenGoesStraightAway(t *testing.T) {
	gt := newGateUnderTest(0)
	a := got(t, gt.ask(context.Background(), time.Second, time.Hour, time.Minute))
	if a.said != "go" {
		t.Fatalf("with no board open the gate said %q", a.said)
	}
	if len(gt.said()) != 0 {
		t.Errorf("with no board open the gate still said %v", gt.said())
	}
}

// A quiet board gets the countdown, then the restart.
func TestAQuietBoardIsCountedDownThenRestarted(t *testing.T) {
	gt := newGateUnderTest(1)
	a := got(t, gt.ask(context.Background(), 150*time.Millisecond, 50*time.Millisecond, time.Minute))
	if a.said != "go" {
		t.Fatalf("a quiet board ended in %q", a.said)
	}
	if s := strings.Join(gt.said(), ","); s != "countdown,restarting" {
		t.Errorf("the boards were told %s, not countdown then restarting", s)
	}
}

// TYPING HOLDS THE COUNTDOWN BACK. Input inside the idle window means no
// countdown yet, however long the typing goes on.
func TestTypingHoldsTheCountdownBack(t *testing.T) {
	gt := newGateUnderTest(1)
	clock := stopClock(gt.g)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Typing before the ask, so the gate's first look already sees it.
	gt.g.input()
	ch := gt.ask(ctx, 100*time.Millisecond, 300*time.Millisecond, time.Minute)
	// A keystroke every 100ms for 800ms of the gate's time, well past the 300ms
	// idle window, and the gate takes a look between each.
	for i := 0; i < 8; i++ {
		clock.advance(100 * time.Millisecond)
		gt.g.input()
		clock.waitRead(t, clock.readCount())
	}
	if len(gt.said()) != 0 {
		t.Fatalf("a board being typed into was told %v", gt.said())
	}
	clock.start()
	if a := got(t, ch); a.said != "go" {
		t.Fatalf("once the typing stopped the gate said %q", a.said)
	}
}

// Typing during the countdown takes it down and waits for quiet again, without
// pausing anything.
func TestTypingDuringTheCountdownStartsTheWaitAgain(t *testing.T) {
	gt := newGateUnderTest(1)
	// Stopped, so the countdown cannot finish before the keystroke lands.
	clock := stopClock(gt.g)
	ch := gt.ask(context.Background(), 400*time.Millisecond, 50*time.Millisecond, time.Minute)
	gt.waitSaid(t, "countdown")
	// A keystroke after the countdown started, not at the same instant.
	clock.advance(time.Millisecond)
	gt.g.input()
	gt.waitSaid(t, "cancelled")
	clock.start()
	if a := got(t, ch); a.said != "go" {
		t.Fatalf("after the typing stopped the gate said %q", a.said)
	}
	if s := strings.Join(gt.said(), ","); s != "countdown,cancelled,countdown,restarting" {
		t.Errorf("the boards were told %s", s)
	}
}

// A CLICK PAUSES, AND THE PAUSE HOLDS until the script's wait runs out.
func TestAPauseHoldsUntilTheWaitRunsOut(t *testing.T) {
	gt := newGateUnderTest(1)
	ch := gt.ask(context.Background(), 5*time.Second, 10*time.Millisecond, 600*time.Millisecond)
	gt.waitSaid(t, "countdown")
	gt.g.pause()
	if a := got(t, ch); a.said != "paused" {
		t.Fatalf("a paused restart ended in %q", a.said)
	}
	if st := gt.g.state(); st["paused"] != true {
		t.Errorf("the pause did not outlive the script that gave up: %v", st)
	}
	// A new ask while paused is held too, and says paused rather than go.
	if a := got(t, gt.ask(context.Background(), 10*time.Millisecond, 10*time.Millisecond,
		200*time.Millisecond)); a.said != "paused" {
		t.Fatalf("an ask made while paused said %q", a.said)
	}
}

// Resume lets a held restart through, after the idle window and a fresh
// countdown.
func TestResumeLetsAHeldRestartThrough(t *testing.T) {
	gt := newGateUnderTest(1)
	ch := gt.ask(context.Background(), 150*time.Millisecond, 50*time.Millisecond, time.Minute)
	gt.waitSaid(t, "countdown")
	gt.g.pause()
	gt.waitSaid(t, "paused")
	time.Sleep(200 * time.Millisecond)
	select {
	case a := <-ch:
		t.Fatalf("a paused restart answered %q before it was resumed", a.said)
	default:
	}
	gt.g.resume()
	if a := got(t, ch); a.said != "go" {
		t.Fatalf("a resumed restart said %q", a.said)
	}
	if s := strings.Join(gt.said(), ","); s != "countdown,paused,resumed,countdown,restarting" {
		t.Errorf("the boards were told %s", s)
	}
}

// HELD MEANS HELD. A patient ask outlasts its wait while paused, and after the
// resume it gets the idle window and a fresh countdown, then `go`.
func TestAPatientPauseOutlastsTheWaitThenGoesOnResume(t *testing.T) {
	gt := newGateUnderTest(1)
	const wait = 300 * time.Millisecond
	ch := gt.askPatient(context.Background(), 150*time.Millisecond, 50*time.Millisecond, wait, true)
	gt.waitSaid(t, "countdown")
	gt.g.pause()
	gt.waitSaid(t, "paused")
	// Three waits' worth, where an impatient ask would have said `paused`.
	time.Sleep(3 * wait)
	select {
	case a := <-ch:
		t.Fatalf("a held restart answered %q %v before it was resumed", a.said, a.err)
	default:
	}
	gt.g.resume()
	if a := got(t, ch); a.said != "go" {
		t.Fatalf("a resumed restart said %q", a.said)
	}
	if s := strings.Join(gt.said(), ","); s != "countdown,paused,resumed,countdown,restarting" {
		t.Errorf("the boards were told %s", s)
	}
}

// An ask made while already paused is held too, with no timeout, and a resume
// lets it through.
func TestAPatientAskMadeWhilePausedWaitsForResume(t *testing.T) {
	gt := newGateUnderTest(1)
	// A STOPPED CLOCK, because after the resume the ask needs the idle window and
	// the countdown, 70ms, inside a 100ms wait. On the real clock a loaded
	// machine eats the 30ms spare and the ask gives up `busy`, which is the wait
	// doing its job and not a gate fault. Here the test says how much time passes.
	clock := stopClock(gt.g)
	gt.g.pause()
	ch := gt.askPatient(context.Background(), 50*time.Millisecond, 20*time.Millisecond,
		100*time.Millisecond, true)
	time.Sleep(400 * time.Millisecond)
	select {
	case a := <-ch:
		t.Fatalf("an ask made during a pause answered %q before the resume", a.said)
	default:
	}
	gt.g.resume()
	// 10ms at a time, so the wait's 100ms is never overstepped by one jump.
	for i := 0; i < 9; i++ {
		select {
		case a := <-ch:
			if a.said != "go" {
				t.Fatalf("after the resume the ask said %q", a.said)
			}
			return
		case <-time.After(20 * time.Millisecond):
			clock.advance(10 * time.Millisecond)
		}
	}
	if a := got(t, ch); a.said != "go" {
		t.Fatalf("after the resume the ask said %q", a.said)
	}
}

// A resume starts the idle wait over: a board still busy after the resume gets
// a whole wait before `busy`, not whatever was left before the pause.
func TestAResumeStartsTheWaitOver(t *testing.T) {
	gt := newGateUnderTest(1)
	const wait = 300 * time.Millisecond
	ch := gt.askPatient(context.Background(), 5*time.Second, time.Hour, wait, true)
	gt.g.pause()
	time.Sleep(2 * wait)
	gt.g.resume()
	resumed := time.Now()
	a := got(t, ch)
	if a.said != "busy" {
		t.Fatalf("a board busy after the resume ended in %q", a.said)
	}
	if d := time.Since(resumed); d < wait-20*time.Millisecond {
		t.Errorf("after the resume the ask gave up in %v, not a fresh %v", d, wait)
	}
}

// Boards that never go quiet are a `busy`, never a restart.
func TestABoardThatNeverGoesQuietIsBusy(t *testing.T) {
	gt := newGateUnderTest(1)
	gt.g.input()
	a := got(t, gt.ask(context.Background(), 10*time.Millisecond, time.Hour, 200*time.Millisecond))
	if a.said != "busy" {
		t.Fatalf("a board that never went quiet ended in %q", a.said)
	}
}

// A script that goes away takes its countdown off every board.
func TestAScriptGoingAwayTakesTheCountdownDown(t *testing.T) {
	gt := newGateUnderTest(1)
	ctx, cancel := context.WithCancel(context.Background())
	ch := gt.ask(ctx, 5*time.Second, 10*time.Millisecond, time.Minute)
	gt.waitSaid(t, "countdown")
	cancel()
	if a := got(t, ch); a.err == nil {
		t.Fatalf("a cancelled ask answered %q", a.said)
	}
	gt.waitSaid(t, "cancelled")
	if st := gt.g.state(); st["waiting"] != false {
		t.Errorf("a cancelled ask still holds the slot: %v", st)
	}
}

// Through the proxy: the ask is loopback only, one at a time, and every board
// stream hears the countdown.
func TestTheRestartEndpointThroughTheProxy(t *testing.T) {
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	front := httptest.NewServer(p)
	defer front.Close()

	// Not from this machine: refused before anything is held.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/_hub/restart", strings.NewReader(`{}`))
	r.RemoteAddr = "192.0.2.7:5555"
	p.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("an ask from elsewhere answered %d", w.Code)
	}

	events, shut := listen(t, front.URL+"/v1/events/hub")
	defer shut()
	// The stream is registered once its headers are back, which `listen`
	// waited for, so the gate counts one board.
	type result struct {
		code int
		body map[string]any
	}
	first := make(chan result, 1)
	go func() {
		res, err := http.Post(front.URL+"/_hub/restart", "application/json",
			strings.NewReader(`{"countdown":0.2,"idle":0.05,"wait":30}`))
		if err != nil {
			first <- result{}
			return
		}
		defer res.Body.Close()
		var body map[string]any
		_ = json.NewDecoder(res.Body).Decode(&body)
		first <- result{res.StatusCode, body}
	}()

	e := waitEvent(t, events, restartEvent)
	if obj := fields(t, e.Data); obj["state"] != "countdown" {
		t.Fatalf("the first restart event said %v", obj)
	}
	// A second ask while the first is held is refused out loud.
	res, err := http.Post(front.URL+"/_hub/restart", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Errorf("a second ask answered %d, not 409", res.StatusCode)
	}

	e = waitEvent(t, events, restartEvent)
	if obj := fields(t, e.Data); obj["state"] != "restarting" {
		t.Fatalf("the second restart event said %v", obj)
	}
	out := <-first
	if out.code != http.StatusOK || out.body["answer"] != "go" {
		t.Fatalf("the ask ended %d %v", out.code, out.body)
	}

	// The board's own calls answer as the gate's state.
	res, err = http.Post(front.URL+"/_hub/restart/pause", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var st map[string]any
	_ = json.NewDecoder(res.Body).Decode(&st)
	res.Body.Close()
	if st["paused"] != true {
		t.Errorf("pause answered %v", st)
	}
	if e := waitEvent(t, events, restartEvent); fields(t, e.Data)["state"] != "paused" {
		t.Errorf("a pause said %s", e.Data)
	}
}

// A hub event reaches a board scoped to one room, which `emit` would skip.
func TestABroadcastReachesARoomScopedBoard(t *testing.T) {
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	s := p.feeds.add("beta")
	defer p.feeds.drop(s)
	p.feeds.broadcast(Event{Kind: restartEvent, Data: []byte(`{"state":"countdown"}`)})
	select {
	case e := <-s.ch:
		if e.Kind != restartEvent {
			t.Fatalf("a scoped board heard %q", e.Kind)
		}
	case <-time.After(time.Second):
		t.Fatal("a board scoped to a room did not hear the hub restart")
	}
	if n := p.feeds.watchers(); n != 1 {
		t.Errorf("one board open counted as %d", n)
	}
}

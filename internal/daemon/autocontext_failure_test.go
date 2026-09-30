package daemon

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// What an automatic run does when a step fails. See design section 5.

const capturePromptText = "Your context is about to be cleared"

func (r *autoRig) count(s string) int { return strings.Count(r.f.written(), s) }

// failed waits for the card's chip to fail and returns its reason.
func (r *autoRig) failed() string {
	r.t.Helper()
	until(r.t, "the chip to fail", func() bool { return failedWith(r.d, r.id()) != "" })
	return failedWith(r.d, r.id())
}

// retryDue lets autoTiming.retryAfter pass.
func retryDue() { time.Sleep(autoTiming.retryAfter + 5*time.Millisecond) }

// agentRig is an agent card past the line, with the capture prompt starting no turn within
// 80 ms so a failure takes no time.
func agentRig(t *testing.T) *autoRig {
	t.Helper()
	r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag, OriginAgentTag)
	ncTiming.captureBegin = 80 * time.Millisecond
	r.size(bigK)
	autoQuiet()
	return r
}

// gaveUp takes an agent card through two failed attempts, and returns it holding GAVE_UP.
func gaveUp(t *testing.T) *autoRig {
	t.Helper()
	r := agentRig(t)
	r.wantStart("first")
	if reason := r.failed(); !strings.Contains(reason, "It will try once more") {
		t.Fatalf("the first failure does not say it will retry: %q", reason)
	}
	retryDue()
	r.tick()
	until(t, "the second failure", func() bool {
		return strings.Contains(failedWith(r.d, r.id()), "Not retrying")
	})
	if s := r.d.auto.get(r.id()); s == nil || s.state != autoGaveUp {
		t.Fatalf("after two failures the card is %+v, want gave-up", s)
	}
	return r
}

// The first failure leaves the chip and one retry after retryAfter, the second gives up,
// GAVE_UP holds across ticks, and a press, a dismissal or a new conversation clears it.
func TestAutoContextCaptureTimeoutRetriesOnceThenGivesUp(t *testing.T) {
	r := agentRig(t)
	autoTiming.retryAfter = 300 * time.Millisecond
	r.wantStart("first")
	reason := r.failed()
	if !strings.Contains(reason, "capture prompt") || !strings.HasSuffix(reason, "It will try once more") {
		t.Fatalf("the first failure reads %q", reason)
	}
	if v, _ := r.d.newContextFor(r.id()).(map[string]any); v == nil || v["auto"] != true {
		t.Fatalf("the failed chip is not marked auto: %v", v)
	}
	if s := r.d.auto.get(r.id()); s == nil || s.state != autoFired || s.retryAt.IsZero() || s.attempts != 1 {
		t.Fatalf("after one failure the card is %+v, want fired with a retry due", s)
	}
	if n := len(r.autoFailureNotices()); n != 0 {
		t.Fatalf("the launcher heard of a failure that will be retried")
	}

	// Not before retryAfter.
	autoTiming.retryAfter = time.Hour
	r.wantNone("before the retry is due")
	autoTiming.retryAfter = 30 * time.Millisecond

	// The retry is the same crossing, so the minimum gap does not hold it.
	retryDue()
	r.tick()
	if s := r.d.auto.get(r.id()); s == nil || s.attempts != 2 || !r.begun() {
		t.Fatalf("the retry did not start: %+v", s)
	}
	until(t, "the second failure", func() bool { return strings.Contains(failedWith(r.d, r.id()), "Not retrying") })
	if got := r.count(capturePromptText); got != 2 {
		t.Fatalf("%d capture prompts, want 2", got)
	}
	if !strings.Contains(failedWith(r.d, r.id()), "press New context to try again") {
		t.Fatalf("the last failure does not tell the person what to do: %q", failedWith(r.d, r.id()))
	}
	if s := r.d.auto.get(r.id()); s == nil || s.state != autoGaveUp {
		t.Fatalf("the card is %+v, want gave-up", s)
	}

	// Held across ticks, however long.
	autoTiming.minGap = 0
	for i := 0; i < 3; i++ {
		retryDue()
		r.tick()
		if got := r.count(capturePromptText); got != 2 {
			t.Fatalf("tick %d typed a third capture prompt", i)
		}
	}

	// Two attempts are recorded, each with its reason and its attempt.
	var failures []map[string]any
	for _, e := range r.notified(autoContextBy) {
		if _, ok := e["failed"]; ok {
			failures = append(failures, e)
		}
	}
	if len(failures) != 2 || failures[0]["attempt"] != float64(1) || failures[1]["attempt"] != float64(2) {
		t.Fatalf("failure events %v", failures)
	}
}

func TestAutoContextGaveUpClearsOnAPressDismissalOrNewConversation(t *testing.T) {
	t.Run("a press", func(t *testing.T) {
		r := gaveUp(t)
		if err := r.d.StartNewContext(r.id()); err != nil {
			t.Fatal(err)
		}
		r.tick()
		if s := r.d.auto.get(r.id()); s == nil || s.state != autoArmed || s.attempts != 0 {
			t.Fatalf("after a press the card is %+v, want armed", s)
		}
	})
	t.Run("a dismissal", func(t *testing.T) {
		r := gaveUp(t)
		req := httptest.NewRequest(http.MethodDelete, "/v1/tasks/"+r.id()+"/new-context", nil)
		req.SetPathValue("id", r.id())
		rec := httptest.NewRecorder()
		r.d.handleNewContext(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("dismiss answered %d", rec.Code)
		}
		r.tick()
		if s := r.d.auto.get(r.id()); s == nil || s.state != autoArmed {
			t.Fatalf("after a dismissal the card is %+v, want armed", s)
		}
	})
	t.Run("a new conversation", func(t *testing.T) {
		r := gaveUp(t)
		r.d.wake.sawConversation(r.id(), "conv-2")
		r.tick()
		if s := r.d.auto.get(r.id()); s == nil || s.state != autoArmed {
			t.Fatalf("in a new conversation the card is %+v, want armed", s)
		}
	})
}

// With the guard failing, no /clear is ever typed: not on the first attempt and not on the
// retry.
func TestAutoContextNeverClearsWithoutHandoff(t *testing.T) {
	for name, prepare := range map[string]func(r *autoRig){
		"no file": func(r *autoRig) {},
		"an old file with no token": func(r *autoRig) {
			writeAged(t, filepath.Join(r.dir, HandoffName(r.task)), handoffBody, time.Hour)
		},
		"a file under the floor": func(r *autoRig) {
			_ = os.WriteFile(filepath.Join(r.dir, HandoffName(r.task)), []byte("short"), 0o644)
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := agentRig(t)
			prepare(r)
			r.wantStart("first")
			for attempt := 1; attempt <= 2; attempt++ {
				r.capturePromptedTimes(attempt)
				r.d.act.set(r.id(), ActivityThinking, "")
				time.Sleep(5 * time.Millisecond)
				ncTurnEnds(r.d, r.id())
				until(t, "the failure", func() bool { return failedWith(r.d, r.id()) != "" })
				if attempt == 1 {
					retryDue()
					r.tick()
				}
			}
			until(t, "the last failure", func() bool { return strings.Contains(failedWith(r.d, r.id()), "Not retrying") })
			time.Sleep(30 * time.Millisecond)
			if strings.Contains(r.f.written(), "/clear") {
				t.Fatalf("typed /clear over a handoff that failed the guard: %q", r.f.written())
			}
		})
	}
}

// capturePromptedTimes waits for the nth capture prompt.
func (r *autoRig) capturePromptedTimes(n int) {
	r.t.Helper()
	until(r.t, "a capture prompt", func() bool { return r.count(capturePromptText) >= n })
}

// autoFailureNotices is the launcher's notices that atrium could not cycle the card.
func (r *autoRig) autoFailureNotices() []string {
	var out []string
	for _, m := range r.autoNotices() {
		if strings.Contains(m, "could not cycle") {
			out = append(out, m)
		}
	}
	return out
}

// /clear typed and no SessionStart: the state is unknown and only a SessionStart proves it,
// so nothing is retried and a person decides.
func TestAutoContextClearUnprovenNotRetried(t *testing.T) {
	r := agentRig(t)
	r.wantStart("first")
	r.captureTurn()
	until(t, "/clear", func() bool { return strings.Contains(r.f.written(), "/clear") })
	reason := r.failed()
	if !strings.Contains(reason, "did not clear") || !strings.Contains(reason, "Not retrying") {
		t.Fatalf("the reason is %q", reason)
	}
	if s := r.d.auto.get(r.id()); s == nil || s.state != autoGaveUp || !s.retryAt.IsZero() {
		t.Fatalf("after an unproven clear the card is %+v, want gave-up with no retry", s)
	}
	autoTiming.minGap = 0
	for i := 0; i < 3; i++ {
		retryDue()
		r.tick()
	}
	if n := r.count(capturePromptText); n != 1 {
		t.Fatalf("%d capture prompts, an unproven clear must never be retried", n)
	}
	if n := r.count("/clear"); n != 1 {
		t.Fatalf("/clear typed %d times", n)
	}
	if n := len(r.autoFailureNotices()); n != 1 {
		t.Fatalf("the launcher heard %d times", n)
	}
}

// Cleared and the wake fails: the context is gone, so the wake alone is tried again, once.
func TestAutoContextWakeFailureRetriesWakeOnly(t *testing.T) {
	r := agentRig(t)
	wake := newContextWake(HandoffName(r.task))
	r.wantStart("first")
	r.captureTurn()
	file := filepath.Join(r.dir, HandoffName(r.task))
	until(t, "/clear", func() bool { return strings.Contains(r.f.written(), "/clear") })
	// The new conversation has no reply yet, so the card reads as nothing.
	r.d.ctx.started(r.id(), "sess-new")
	// The handoff goes missing, so the wake cannot be typed.
	if err := os.Rename(file, file+".away"); err != nil {
		t.Fatal(err)
	}
	r.newSession("conv-2")
	reason := r.failed()
	if !strings.Contains(reason, "wake") || !strings.Contains(reason, "It will try once more") {
		t.Fatalf("the reason is %q", reason)
	}
	if s := r.d.auto.get(r.id()); s == nil || !s.wakeOnly || s.retryAt.IsZero() {
		t.Fatalf("the card is %+v, want a wake only retry due", s)
	}

	// The file is back. The retry is due on a tick, on a card that reads as nothing.
	if err := os.Rename(file+".away", file); err != nil {
		t.Fatal(err)
	}
	autoTiming.minGap = 0
	retryDue()
	r.tick()
	until(t, "the wake", func() bool { return r.count(wake) == 1 })
	until(t, "the chip to go", func() bool { return r.d.newContextFor(r.id()) == nil })
	if n := r.count(capturePromptText); n != 1 {
		t.Fatalf("the retry typed the capture prompt again: %d", n)
	}
	if n := r.count("/clear"); n != 1 {
		t.Fatalf("the retry typed /clear again: %d", n)
	}
	// And only once: the run is over, so nothing more is typed however many ticks.
	for i := 0; i < 3; i++ {
		retryDue()
		r.tick()
	}
	if n := r.count(wake); n != 1 {
		t.Fatalf("the wake was typed %d times", n)
	}
}

// The wake retry is tried once. If that fails too the card is given up on.
func TestAutoContextWakeRetryGivesUpAfterOne(t *testing.T) {
	r := agentRig(t)
	r.wantStart("first")
	r.captureTurn()
	file := filepath.Join(r.dir, HandoffName(r.task))
	until(t, "/clear", func() bool { return strings.Contains(r.f.written(), "/clear") })
	r.d.ctx.started(r.id(), "sess-new")
	if err := os.Rename(file, file+".away"); err != nil {
		t.Fatal(err)
	}
	r.newSession("conv-2")
	r.failed()
	retryDue()
	r.tick()
	until(t, "the second failure", func() bool { return strings.Contains(failedWith(r.d, r.id()), "Not retrying") })
	if s := r.d.auto.get(r.id()); s == nil || s.state != autoGaveUp {
		t.Fatalf("the card is %+v, want gave-up", s)
	}
	for i := 0; i < 3; i++ {
		retryDue()
		r.tick()
	}
	if n := r.count(newContextWake(HandoffName(r.task))); n != 0 {
		t.Fatalf("the wake was typed %d times without its file", n)
	}
	if n := len(r.autoFailureNotices()); n != 1 {
		t.Fatalf("the launcher heard %d times", n)
	}
}

// One notice for an agent card, none for a human card, none again for the same session.
func TestAutoContextFailureNotifiesLauncherOnce(t *testing.T) {
	r := gaveUp(t)
	if n := len(r.autoFailureNotices()); n != 1 {
		t.Fatalf("the launcher heard %d times, want once", n)
	}
	// A second give up in the same session, as after a restart met the card again: the
	// claim is in the store, so it says nothing.
	r.d.autoGaveUpNotice(r.id(), "again")
	if n := len(r.autoFailureNotices()); n != 1 {
		t.Fatalf("the launcher heard %d times after a repeat", n)
	}

	t.Run("a human card has the chip and no notice", func(t *testing.T) {
		h := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag)
		ncTiming.captureBegin = 80 * time.Millisecond
		h.size(bigK)
		autoQuiet()
		h.wantStart("first")
		h.failed()
		retryDue()
		h.tick()
		until(t, "the second failure", func() bool { return strings.Contains(failedWith(h.d, h.id()), "Not retrying") })
		fresh, err := h.d.st.RecordNotice(h.id(), NoticeAutoContext, "failed:"+h.d.ctx.sessionOf(h.task))
		if err != nil || !fresh {
			t.Fatalf("a human card's failure was claimed as a notice (fresh=%v, err=%v)", fresh, err)
		}
	})
}

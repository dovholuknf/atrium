//go:build integration

package daemon

import (
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/store"
)

func (f *kaFix) coldCounter() *atomic.Int32 {
	var n atomic.Int32
	f.k.cold = func(*store.Task) error { n.Add(1); return nil }
	return &n
}

func (f *kaFix) coldEvents() []map[string]any {
	evs, err := f.st.Events(f.task.ID, 50)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []map[string]any
	for _, e := range evs {
		var p map[string]any
		if json.Unmarshal(e.Payload, &p) == nil && p["by"] == coldHandoffBy {
			out = append(out, p)
		}
	}
	return out
}

func TestColdHandoffFiresOnTheBreakEvenStop(t *testing.T) {
	t.Parallel()
	f := newKAFix(t)
	n := f.coldCounter()
	f.reply(f.now.Add(-6*time.Hour), replyOpt{})
	f.seed(5, 0.06, 56*time.Minute)
	f.tick()
	if f.state() != store.KeepaliveBreakEven {
		t.Fatalf("state = %s, want break-even", f.state())
	}
	if n.Load() != 1 {
		t.Fatalf("cycles = %d, want 1", n.Load())
	}
	evs := f.coldEvents()
	if len(evs) != 1 || evs[0]["reason"] != coldHandoffReason || evs[0]["started"] != true {
		t.Fatalf("events = %v", evs)
	}
	f.tick()
	if n.Load() != 1 {
		t.Fatalf("cycles after a second tick = %d, want 1", n.Load())
	}
}

func TestColdHandoffLeavesAPingedCardAlone(t *testing.T) {
	t.Parallel()
	f := newKAFix(t)
	n := f.coldCounter()
	f.reply(f.now.Add(-56*time.Minute), replyOpt{})
	f.tick()
	if f.forks() != 1 || n.Load() != 0 {
		t.Fatalf("forks = %d cycles = %d, want 1 and 0", f.forks(), n.Load())
	}
	f.reply(f.now.Add(-30*time.Minute), replyOpt{})
	f.tick()
	if n.Load() != 0 {
		t.Fatalf("a card not yet due cycled")
	}
}

func TestColdHandoffFiresOnACardKeepaliveDoesNotCover(t *testing.T) {
	t.Parallel()
	for name, setup := range map[string]func(f *kaFix){
		"keep-alive off": func(f *kaFix) {
			f.st.SetKeepaliveState(f.task.ID, store.KeepaliveOff)
			f.reply(f.now.Add(-56*time.Minute), replyOpt{})
		},
		"model not covered": func(f *kaFix) { f.reply(f.now.Add(-56*time.Minute), replyOpt{model: "claude-opus-5"}) },
		"fast mode":         func(f *kaFix) { f.reply(f.now.Add(-56*time.Minute), replyOpt{speed: "fast"}) },
		"room suspended": func(f *kaFix) {
			f.st.SetKeepaliveSuspended("test")
			f.reply(f.now.Add(-56*time.Minute), replyOpt{})
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newKAFix(t)
			n := f.coldCounter()
			setup(f)
			f.tick()
			if n.Load() != 1 || f.forks() != 0 {
				t.Fatalf("cycles = %d forks = %d, want 1 and 0", n.Load(), f.forks())
			}
		})
	}
}

func TestColdHandoffWaitsForTheMargin(t *testing.T) {
	t.Parallel()
	f := newKAFix(t)
	n := f.coldCounter()
	f.st.SetKeepaliveState(f.task.ID, store.KeepaliveOff)
	f.reply(f.now.Add(-30*time.Minute), replyOpt{})
	f.tick()
	if n.Load() != 0 {
		t.Fatal("cycled with half an hour of cache left")
	}
	f.now = f.now.Add(26 * time.Minute)
	f.tick()
	if n.Load() != 1 {
		t.Fatalf("cycles = %d, want 1 inside the margin", n.Load())
	}
}

func TestColdHandoffGuards(t *testing.T) {
	t.Parallel()
	cases := map[string]func(f *kaFix){
		"mid-turn": func(f *kaFix) { f.st.SetStatus(f.task.ID, store.StatusRunning) },
		"dialog open": func(f *kaFix) {
			if _, _, err := f.st.RecordPermission(f.task.ID, "Bash", "ls", "k1", ""); err != nil {
				f.t.Fatal(err)
			}
			f.st.SetStatus(f.task.ID, store.StatusNeedsInput)
		},
		"room switch off": func(f *kaFix) { f.st.SetColdHandoff(false) },
		"card switch off": func(f *kaFix) {
			f.st.SetOverrides(f.task.ID, map[string]string{api.OverrideColdHandoff: "off"})
		},
		"cycle under way": func(f *kaFix) { f.k.holding = func(string) bool { return true } },
		"5m cache": func(f *kaFix) {
			f.reply(f.now.Add(-4*time.Minute), replyOpt{ttl5m: true})
		},
		"small context": func(f *kaFix) { f.reply(f.now.Add(-56*time.Minute), replyOpt{ctx: 40_000}) },
		"already cold":  func(f *kaFix) { f.reply(f.now.Add(-61*time.Minute), replyOpt{}) },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newKAFix(t)
			n := f.coldCounter()
			f.st.SetKeepaliveState(f.task.ID, store.KeepaliveOff)
			if name != "5m cache" && name != "small context" && name != "already cold" {
				f.reply(f.now.Add(-56*time.Minute), replyOpt{})
			}
			setup(f)
			f.tick()
			if n.Load() != 0 {
				t.Fatalf("%s: a cycle started", name)
			}
		})
	}
}

func TestColdHandoffOncePerColdEvent(t *testing.T) {
	t.Parallel()
	f := newKAFix(t)
	n := f.coldCounter()
	f.st.SetKeepaliveState(f.task.ID, store.KeepaliveOff)
	f.reply(f.now.Add(-56*time.Minute), replyOpt{})
	f.tick()
	f.tick()
	if n.Load() != 1 {
		t.Fatalf("cycles = %d, want 1 with no new turn", n.Load())
	}
	f.k.cooled = map[string]time.Time{}
	f.tick()
	if n.Load() != 1 {
		t.Fatalf("cycles = %d after the memory was lost, want 1: the card's events should say", n.Load())
	}
	f.now = f.now.Add(2 * time.Hour)
	f.reply(f.now.Add(-56*time.Minute), replyOpt{})
	f.tick()
	if n.Load() != 1 {
		t.Fatalf("cycles = %d, want 1: a turn atrium started does not re-arm", n.Load())
	}
	f.k.coldRearm(f.task.ID)
	f.tick()
	if n.Load() != 2 {
		t.Fatalf("cycles = %d, want 2 after a typed turn went cold", n.Load())
	}
}

func TestColdHandoffBusyCycleIsRetried(t *testing.T) {
	t.Parallel()
	f := newKAFix(t)
	var calls atomic.Int32
	f.k.cold = func(*store.Task) error {
		if calls.Add(1) == 1 {
			return errNewContextBusy
		}
		return nil
	}
	f.st.SetKeepaliveState(f.task.ID, store.KeepaliveOff)
	f.reply(f.now.Add(-56*time.Minute), replyOpt{})
	f.tick()
	f.tick()
	f.tick()
	if calls.Load() != 2 || len(f.coldEvents()) != 1 {
		t.Fatalf("calls = %d events = %d, want 2 and 1", calls.Load(), len(f.coldEvents()))
	}
}

func TestColdHandoffSurvivesARoomRestartAndWaitsForAPerson(t *testing.T) {
	t.Parallel()
	f := newKAFix(t)
	n := f.coldCounter()
	f.st.SetKeepaliveState(f.task.ID, store.KeepaliveOff)
	f.reply(f.now.Add(-56*time.Minute), replyOpt{})
	f.tick()
	if n.Load() != 1 {
		t.Fatalf("cycles = %d, want 1", n.Load())
	}
	f.k.cooled = map[string]time.Time{}
	for i := 0; i < 3; i++ {
		f.now = f.now.Add(3 * time.Hour)
		f.reply(f.now.Add(-56*time.Minute), replyOpt{ctx: 90_000})
		f.tick()
	}
	if n.Load() != 1 {
		t.Fatalf("cycles = %d, want 1: the continuation grew context and idled out, which is not a person", n.Load())
	}
	f.k.coldRearm(f.task.ID)
	f.now = f.now.Add(3 * time.Hour)
	f.reply(f.now.Add(-56*time.Minute), replyOpt{ctx: 90_000})
	f.tick()
	if n.Load() != 2 {
		t.Fatalf("cycles = %d, want 2 after a person's turn", n.Load())
	}
	f.now = f.now.Add(3 * time.Hour)
	f.reply(f.now.Add(-56*time.Minute), replyOpt{ctx: 90_000})
	f.tick()
	if n.Load() != 2 {
		t.Fatalf("cycles = %d, want 2: the second handoff spent it again", n.Load())
	}
}

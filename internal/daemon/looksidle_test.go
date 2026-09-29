package daemon

import (
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Frames as Claude Code draws them, cut down to what the classifier reads. One
// working, one idle, so a Claude Code change to either string fails here.
const (
	workingFrame = "\x1b[2K✻ Thinking… (12s · ↓ 1.2k tokens · esc to interrupt)\r\n\r\n" +
		"╭──────────────╮\r\n│ >            │\r\n╰──────────────╯\r\n  ? for shortcuts\r\n"
	idleFrame = "\x1b[2K\r\n" +
		"╭──────────────╮\r\n│ >            │\r\n╰──────────────╯\r\n  ? for shortcuts\r\n"
)

func TestClassifyFrame(t *testing.T) {
	cases := []struct {
		name  string
		frame string
		idle  bool
		why   string
	}{
		{"idle prompt", idleFrame, true, frameIdle},
		{"idle after an earlier working frame", workingFrame + idleFrame, true, frameIdle},
		{"spinner above the box", workingFrame, false, frameHint},
		{"working after an idle one", idleFrame + workingFrame, false, frameHint},
		{"no box", "building...\r\nstill going\r\n", false, frameNoBox},
		{"box half drawn", "╭──────────────╮\r\n│ >", false, frameOpenBox},
	}
	for _, c := range cases {
		idle, why := classifyFrame([]byte(c.frame))
		if idle != c.idle || why != c.why {
			t.Errorf("%s: got %v/%s, wanted %v/%s", c.name, idle, why, c.idle, c.why)
		}
	}
}

// runningCard is a claude card with a runner whose pty last spoke `quiet` ago and
// whose last frame is `frame`, mid-turn and thinking.
func runningCard(t *testing.T, d *Daemon, frame string, quiet time.Duration) (*store.Task, *runner) {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{WireName: "idle-test", Worktree: "/tmp/atrium-test", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	r := &runner{
		taskID:   task.ID,
		pty:      newFakePTY(),
		started:  time.Now().Add(-time.Hour),
		buf:      newRing(1<<16, 80),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
	_, _ = r.buf.Write([]byte(frame))
	r.lastOut.Store(time.Now().Add(-quiet).UnixNano())
	d.sup.add(r)
	d.act.set(task.ID, ActivityThinking, "")
	return task, r
}

func TestARunningCardWithASilentIdleScreenLooksIdle(t *testing.T) {
	d := testDaemon(t)
	task, _ := runningCard(t, d, workingFrame+idleFrame, time.Minute)

	if err := d.watchLooksIdle(time.Now()); err != nil {
		t.Fatal(err)
	}
	a := d.act.get(task.ID)
	if a == nil || !a.LooksIdle || a.IdleSeconds < 59 {
		t.Fatalf("activity %+v, wanted looks idle after a minute of silence", a)
	}
	// A guess is never a status.
	if got, _ := d.st.Get(task.ID); got.Status != store.StatusRunning {
		t.Fatalf("status moved to %s", got.Status)
	}
	if d.looksIdleFired.Load() != 1 {
		t.Fatalf("fired %d times, wanted 1", d.looksIdleFired.Load())
	}
}

func TestLooksIdleNeedsSilenceAndAnIdleFrame(t *testing.T) {
	d := testDaemon(t)
	task, r := runningCard(t, d, idleFrame, time.Second)
	_ = d.watchLooksIdle(time.Now())
	if a := d.act.get(task.ID); a.LooksIdle {
		t.Fatal("flagged after one second of silence")
	}

	// Silent but under a live spinner: a long Bash, not an idle prompt.
	r.lastOut.Store(time.Now().Add(-time.Minute).UnixNano())
	_, _ = r.buf.Write([]byte(workingFrame))
	_ = d.watchLooksIdle(time.Now())
	if a := d.act.get(task.ID); a.LooksIdle {
		t.Fatal("flagged a silent turn whose spinner is still up")
	}
}

func TestLooksIdleIsClaudeOnly(t *testing.T) {
	d := testDaemon(t)
	other, _, err := d.st.Register(store.Observed{WireName: "codex-test", Worktree: "/tmp/atrium-test2", Runner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	r := &runner{taskID: other.ID, pty: newFakePTY(), started: time.Now().Add(-time.Hour),
		buf: newRing(1<<16, 80), watchers: map[chan []byte]struct{}{}, done: make(chan struct{})}
	_, _ = r.buf.Write([]byte(idleFrame))
	d.sup.add(r)
	d.act.set(other.ID, ActivityThinking, "")
	_ = d.watchLooksIdle(time.Now())
	if a := d.act.get(other.ID); a.LooksIdle {
		t.Fatal("flagged a codex card")
	}
}

func TestLooksIdleClearsOnOutputKeystrokeAndHook(t *testing.T) {
	for _, cause := range []string{"output", "keystroke", "hook"} {
		d := testDaemon(t)
		task, r := runningCard(t, d, idleFrame, time.Minute)
		_ = d.watchLooksIdle(time.Now())
		if a := d.act.get(task.ID); !a.LooksIdle {
			t.Fatalf("%s: not flagged", cause)
		}
		switch cause {
		case "output":
			r.deliverOutput([]byte("x"))
		case "keystroke":
			r.noteOperatorTyped([]byte("a"))
		case "hook":
			d.onActivity(ActivityEvent{TaskID: task.ID, Event: "idle"})
		}
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if a := d.act.get(task.ID); a == nil || !a.LooksIdle {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if a := d.act.get(task.ID); a != nil && a.LooksIdle {
			t.Fatalf("%s left the badge up", cause)
		}
	}
}

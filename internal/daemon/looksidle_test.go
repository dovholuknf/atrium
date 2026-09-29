package daemon

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Frames laid out as Claude Code draws them (rules, prompt, footer), cut down to
// what the classifier reads. The idle one is written by hand from the real
// strings, since no captured tail was taken at a settled prompt. The two real
// captures are in testdata and are tested below.
const (
	frameRule    = "────────────────────────────────────────────────────────────────"
	workingFrame = "\x1b[H\x1b[2J· Ionizing… (12s · ↓ 1.2k tokens)\r\n\r\n" + frameRule + "\r\n❯ \r\n" + frameRule +
		"\r\n  ⏵⏵ auto mode on (shift+tab to cycle) · esc to interrupt · ← for agents\r\n"
	idleFrame = "\x1b[H\x1b[2J✻ Crunched for 1m 0s\r\n\r\n" + frameRule + "\r\n❯ \r\n" + frameRule +
		"\r\n  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents\r\n"
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
		{"working footer", workingFrame, false, frameHint},
		{"working after an idle one", idleFrame + workingFrame, false, frameHint},
		{"status line rows under the box", idleFrame + "  ~/repo (main) [22:54] | 5h 1% | wk 26% | ctx 64k\r\n  Checking for updates\r\n", true, frameIdle},
		{"five rows under the box", idleFrame + "a\r\nb\r\nc\r\nd\r\n", false, frameNoBox},
		{"hint in a status row", workingFrame + "  status\r\n", false, frameHint},
		{"no box", "building...\r\nstill going\r\nmore\r\nlines\r\n", false, frameNoBox},
		{"box half drawn", "x\r\n" + frameRule + "\r\n❯", false, frameNoBox},
		{"spinner with no footer hint", "· Ionizing… (5s)\r\n\r\n" + frameRule + "\r\n❯ \r\n" + frameRule + "\r\n  footer\r\n", false, frameSpinner},
	}
	for _, c := range cases {
		idle, why := classifyFrame([]byte(c.frame), 80, 40)
		if idle != c.idle || why != c.why {
			t.Errorf("%s: got %v/%s, wanted %v/%s", c.name, idle, why, c.idle, c.why)
		}
	}
}

// Real captures from Claude Code, 64KB raw tails. Both are mid-turn: one is the
// turn ending under its Stop hooks, one is inside a tool. Neither may read idle.
func TestClassifyFrameOnRealCaptures(t *testing.T) {
	for _, f := range []string{"frame-turn-ending.bin", "frame-mid-tool.bin"} {
		b, err := os.ReadFile("testdata/" + f)
		if err != nil {
			t.Fatal(err)
		}
		if idle, why := classifyFrame(b, 120, 40); idle || why != frameHint {
			t.Errorf("%s: got %v/%s, wanted working via %s", f, idle, why, frameHint)
		}
	}
}

// A settled prompt, taken from a session idle for over an hour. Its custom status
// line puts several rows under the bottom rule, and it must still read idle.
func TestClassifyFrameOnARealSettledPromptWithAStatusLine(t *testing.T) {
	b, err := os.ReadFile("testdata/frame-settled-statusline.bin")
	if err != nil {
		t.Fatal(err)
	}
	// The session was 206 columns wide, unlike the other captures. Read at the
	// wrong width its rules wrap and it reads as no box, which fails safe.
	if idle, why := classifyFrame(b, 206, 60); !idle {
		t.Errorf("wanted idle, got %s", why)
	}
}

// The same real capture with its working markers scrubbed is what an idle screen
// looks like, and must read idle. Guards the layout half of the signature against
// the real bytes, where the hand-written frame guards it only against itself.
func TestClassifyFrameOnARealCaptureMadeIdle(t *testing.T) {
	b, err := os.ReadFile("testdata/frame-mid-tool.bin")
	if err != nil {
		t.Fatal(err)
	}
	// What a settled turn would draw over it, cursor addressed like the real thing:
	// the tool line and spinner become a summary, and the footer loses its hint.
	b = append(bytes.Clone(b), "\x1b[16;1H\x1b[2K\x1b[19;1H\x1b[2K✻ Crunched for 10m 1s"+
		"\x1b[24;1H\x1b[2K  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents"...)
	if idle, why := classifyFrame(b, 120, 40); !idle {
		t.Errorf("wanted idle, got %s", why)
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

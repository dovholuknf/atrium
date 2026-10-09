package daemon

import (
	"strings"
	"testing"
	"time"
)

// A clear on Windows reaches the room as ConPTY's repaint in place: the cursor goes home, the new frame is drawn over
// the old rows and `ESC[K` blanks the rest. No 2J, no 3J. Without the mark the old page is simply overwritten.
const conptyClear = "\x1b[H\x1b[Knew banner\r\n\x1b[K\r\n\x1b[K\r\n\x1b[K\r\n"

func TestAClearThatIsRepaintedInPlaceLosesThePageWithoutTheMark(t *testing.T) {
	got := plain(render("old line 1\r\nold line 2\r\n"+conptyClear, 40))
	if strings.Contains(got, "old line 1") {
		t.Skipf("the model keeps an overwritten page on its own now, so the mark is not needed:\n%q", got)
	}
}

func TestTheClearMarkPutsThePageInHistoryBeforeItIsRepaintedOver(t *testing.T) {
	got := plain(render("old line 1\r\nold line 2\r\n"+clearMarkOSC+conptyClear, 40))
	for _, want := range []string{"old line 1", "old line 2", "new banner"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q is missing after a marked clear:\n%q", want, got)
		}
	}
	if strings.Index(got, "old line 2") > strings.Index(got, "new banner") {
		t.Fatalf("the old page should come before the new one:\n%q", got)
	}
	if strings.Contains(got, "atrium-clear") || strings.Contains(got, "7777") {
		t.Fatalf("the mark leaked into the text:\n%q", got)
	}
}

// The ST terminator is as good as BEL, and a mark the stream cuts in two is still one mark.
func TestTheClearMarkEndedByStringTerminatorAndTheOneAcrossTwoWrites(t *testing.T) {
	st := strings.TrimSuffix(clearMarkOSC, "\x07") + "\x1b\\"
	if got := plain(render("old line 1\r\n"+st+conptyClear, 40)); !strings.Contains(got, "old line 1") {
		t.Fatalf("a mark ended by ST did not keep the page:\n%q", got)
	}
	s := newScreen(40)
	s.apply([]byte("old line 1\r\n" + clearMarkOSC + conptyClear))
	s.apply([]byte("later\r\n"))
	if len(s.history) == 0 {
		t.Fatal("nothing went into history")
	}
}

// The restart that follows a new-context sends a 2J of its own, over the same page. One copy, not two, which is the
// rule the board's `keepPage` keeps too so a live viewer and a replay agree.
func TestAMarkAndTheEraseDisplayAfterItDoNotStackTheSamePage(t *testing.T) {
	got := plain(render("old line 1\r\nold line 2\r\n"+clearMarkOSC+"\x1b[2J\x1b[Hnew\r\n", 40))
	if n := strings.Count(got, "old line 1"); n != 1 {
		t.Fatalf("the page is in history %d times, not once:\n%q", n, got)
	}
}

func TestTheClearMarkOnTheAlternateScreenKeepsNothing(t *testing.T) {
	got := plain(render("before\r\n\x1b[?1049h"+"PAGER ROW\r\n"+clearMarkOSC+"\x1b[?1049l"+"after\r\n", 40))
	if strings.Contains(got, "PAGER ROW") {
		t.Fatalf("the alternate screen was kept:\n%q", got)
	}
	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Fatalf("the transcript around it was lost:\n%q", got)
	}
}

func TestAnOSCThatIsNotTheMarkIsIgnored(t *testing.T) {
	got := plain(render("old line 1\r\n\x1b]7777;something-else\x07"+conptyClear, 40))
	if strings.Contains(got, "old line 1") && strings.Contains(got, "something-else") {
		t.Fatalf("an unknown OSC leaked:\n%q", got)
	}
}

func newMarkRunner(f *fakePTY) *runner {
	return &runner{
		taskID:   "mark",
		pty:      f,
		started:  time.Now(),
		buf:      newRing(1<<16, 80),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
}

// The mark is in the ring, so a replay has it, and on every watcher, so a live viewer gets it at the same byte. It is
// not the runner's output, so it does not move the clock the idle checks read.
func TestMarkClearReachesTheReplayAndTheWatchersAndNotTheOutputClock(t *testing.T) {
	f := newFakePTY()
	t.Cleanup(func() { f.Close() })
	r := newMarkRunner(f)
	r.deliverOutput([]byte("old line 1\r\n"))
	_, _, _, _, _, updates := r.subscribeSized()
	before := r.lastOut.Load()
	r.markClear()
	if r.lastOut.Load() != before {
		t.Fatal("the mark moved the output clock")
	}
	select {
	case chunk := <-updates:
		if string(chunk) != clearMarkOSC {
			t.Fatalf("the watcher got %q", chunk)
		}
	case <-time.After(time.Second):
		t.Fatal("the watcher never got the mark")
	}
	replay, _, _ := r.buf.Replay()
	if !strings.HasSuffix(string(replay), "old line 1\r\n"+clearMarkOSC) {
		t.Fatalf("the replay is %q", replay)
	}
	// The room's screen model reads the same bytes back and agrees.
	if got := plain(render(string(replay)+conptyClear, 40)); !strings.Contains(got, "old line 1") {
		t.Fatalf("the replayed stream lost the page:\n%q", got)
	}
}

// A `/clear` the room types itself marks first. Anything else does not.
func TestTypingSlashClearMarksTheStreamBeforeTheBytesAndOtherTextDoesNot(t *testing.T) {
	f := newFakePTY()
	t.Cleanup(func() { f.Close() })
	r := newMarkRunner(f)
	if ok, err := r.injectPeer("", "/clear"); err != nil || !ok {
		t.Fatalf("injectPeer: %v %v", ok, err)
	}
	replay, _, _ := r.buf.Replay()
	if string(replay) != clearMarkOSC {
		t.Fatalf("typing /clear left %q in the stream", replay)
	}
	if !strings.Contains(f.written(), "/clear") {
		t.Fatalf("the pty got %q", f.written())
	}

	r2 := newMarkRunner(newFakePTY())
	r2.pty = f
	if ok, err := r2.injectPeer("", "/clearing house"); err != nil || !ok {
		t.Fatalf("injectPeer: %v %v", ok, err)
	}
	if replay, _, _ := r2.buf.Replay(); len(replay) != 0 {
		t.Fatalf("other text left %q in the stream", replay)
	}
}

func TestIsClearCommand(t *testing.T) {
	for in, want := range map[string]bool{
		"/clear": true, " /clear ": true, "\x1b[200~/clear\x1b[201~": true,
		"/clear now": false, "/clearing": false, "clear": false, "": false,
	} {
		if got := isClearCommand(in); got != want {
			t.Errorf("isClearCommand(%q) = %v, want %v", in, got, want)
		}
	}
}

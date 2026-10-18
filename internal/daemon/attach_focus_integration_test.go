//go:build integration

package daemon

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func sendFrame(t *testing.T, h *heldAttach, in attachIn) {
	t.Helper()
	frame, _ := json.Marshal(in)
	if err := h.c.Write(context.Background(), websocket.MessageText, frame); err != nil {
		t.Fatalf("could not send %q: %v", in.T, err)
	}
}

func focusSeqIs(t *testing.T, r *runner, want uint64) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		r.mu.Lock()
		got := r.activeSeq
		r.mu.Unlock()
		if got >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the daemon never took the focus frame: active seq %d, want %d", got, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TWO WINDOWS, ONE TERMINAL. The pty follows the window that last had focus or input, and goes back to the one
// that is left when the other detaches. This is the second board window that resized a terminal and never gave it back.
func TestThePTYFollowsTheWindowThatWasLastUsedAndReturnsOnDetach(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	f, r := sizedSession(t, d, "two-windows", 130, 30)

	first := holdAttach(t, d, "two-windows", 140, 32)
	ptyIs(t, f, r, viewport{140, 32})
	sendFrame(t, first, attachIn{T: "focus"})
	focusSeqIs(t, r, 1)

	// The second window is taller and wider. Using it moves the pty to it.
	second := holdAttach(t, d, "two-windows", 170, 44)
	sendFrame(t, second, attachIn{T: "focus"})
	ptyIs(t, f, r, viewport{170, 44})

	// Back to the first window, by focus. The pty follows, rows included, so no empty band is left above the first line.
	sendFrame(t, first, attachIn{T: "focus"})
	ptyIs(t, f, r, viewport{140, 32})

	// Typing in the second moves it there again, with no focus frame.
	sendFrame(t, second, attachIn{T: "in", D: "x"})
	ptyIs(t, f, r, viewport{170, 44})

	// The second window is left in place and detaches. The size comes back to the first window's.
	second.close()
	ptyIs(t, f, r, viewport{140, 32})
}

// A window that never says it is used does not take the lead from one that does, and one window is unchanged.
func TestOneWindowIsUnchangedByFocus(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	f, r := sizedSession(t, d, "one-window", 130, 30)

	only := holdAttach(t, d, "one-window", 140, 36)
	ptyIs(t, f, r, viewport{140, 36})
	before := len(f.resized())
	sendFrame(t, only, attachIn{T: "focus"})
	sendFrame(t, only, attachIn{T: "focus"})
	if after := resizesAfterAMoment(f); after != before {
		t.Fatalf("focus on the only window resized the pty: %+v", f.resized())
	}
}

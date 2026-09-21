package daemon

import (
	"testing"
	"time"
)

// SHARED MULTI-PANE INPUT at the fan-out layer, without a websocket in sight.
// The attach socket is one caller of `echoToPeers`; these tests pin the
// behaviour the attach relies on: peers get the keystrokes, the writer does
// not, and the pty is never touched.

// echoRunner is a runner with N subscribed panes and no pty, so any attempt to
// write stdin during an echo would panic and fail the test outright.
func echoRunner(t *testing.T, panes int) (*runner, []chan []byte) {
	t.Helper()
	r := &runner{
		buf:      newRing(1<<16, 80),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
	chans := make([]chan []byte, panes)
	for i := range chans {
		_, _, _, _, _, ch := r.subscribeSized()
		chans[i] = ch
	}
	return r, chans
}

// drain reads one chunk if one is waiting, else reports nothing arrived.
func drain(ch chan []byte) (string, bool) {
	select {
	case b := <-ch:
		return string(b), true
	case <-time.After(50 * time.Millisecond):
		return "", false
	}
}

// THE DEFAULT IS OFF, so nothing changes for a runner nobody opted in.
func TestEchoIsOffByDefault(t *testing.T) {
	r, chans := echoRunner(t, 3)
	writer := chans[0]

	r.echoToPeers([]byte("ls"), writer)

	for i, ch := range chans {
		if got, ok := drain(ch); ok {
			t.Fatalf("pane %d received %q with the mode off", i, got)
		}
	}
}

// ON: every OTHER pane gets the keystrokes, the writer does not, and stdin is
// written exactly once, which here means never, because the pty is nil and a
// write would panic.
func TestEchoReachesPeersButNotTheWriter(t *testing.T) {
	r, chans := echoRunner(t, 3)
	writer := chans[0]
	r.setEchoPeers(true)

	r.echoToPeers([]byte("git status"), writer)

	if got, ok := drain(writer); ok {
		t.Fatalf("the writer was echoed its own keystrokes: %q", got)
	}
	for i := 1; i < len(chans); i++ {
		got, ok := drain(chans[i])
		if !ok {
			t.Fatalf("peer %d never received the echo", i)
		}
		if got != "git status" {
			t.Fatalf("peer %d got %q, want the keystrokes verbatim", i, got)
		}
	}
}

// A nil writer channel, which is a keystroke that arrived before this attach
// subscribed, excludes nobody. Correct, because the writer is not yet a
// watcher, so there is no pane to double.
func TestEchoWithNoWriterReachesEveryPane(t *testing.T) {
	r, chans := echoRunner(t, 2)
	r.setEchoPeers(true)

	r.echoToPeers([]byte("x"), nil)

	for i, ch := range chans {
		if _, ok := drain(ch); !ok {
			t.Fatalf("pane %d missed an echo sent with no writer set", i)
		}
	}
}

// The toggle turns it back off.
func TestEchoCanBeTurnedOffAgain(t *testing.T) {
	r, chans := echoRunner(t, 2)
	r.setEchoPeers(true)
	r.setEchoPeers(false)

	r.echoToPeers([]byte("ls"), chans[0])

	if got, ok := drain(chans[1]); ok {
		t.Fatalf("a peer was echoed after the mode was turned off: %q", got)
	}
}

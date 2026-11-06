package daemon

import (
	"strings"
	"testing"
	"time"
)

func TestEraseScrollbackFromTheRunnerKeepsTheEarlierHistory(t *testing.T) {
	t.Parallel()
	r := &runner{
		taskID:   "keep",
		started:  time.Now(),
		buf:      newRing(1<<16, 40),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
	_, _, _, _, _, updates := r.subscribeSized()
	r.deliverOutput([]byte("before the clear\r\n"))
	r.deliverOutput([]byte("\x1b[3J\x1b[2J\x1b[Hafter the clear\r\n"))
	replay, _, _ := r.buf.Replay()
	got := plain(render(string(replay), 40))
	for _, want := range []string{"before the clear", "after the clear"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q is missing after ESC[3J:\n%q", want, got)
		}
	}
	for range 2 {
		if c := <-updates; strings.Contains(string(c), "\x1b[3J") {
			t.Fatalf("the board was sent the erase: %q", c)
		}
	}
}

package daemon

import (
	"io"
	"log"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/inputlag"
)

// nopPTY takes every write and keeps nothing, so a benchmark measures the
// room's own work and not a growing buffer.
type nopPTY struct{ *fakePTY }

func (nopPTY) Write(p []byte) (int, error) { return len(p), nil }

// benchKeystroke is what the room does for one keystroke and its echo, less the
// websocket reads and writes: the frame clock, the input lock and pty write,
// the pty read landing in the ring and the fan-out, and the echo clock that
// closes the gap. The same branches attach.go and deliverOutput take.
func benchKeystroke(b *testing.B, on bool) {
	if inputlag.Pinned() {
		b.Skip(inputlag.Env + " is set, so the switch cannot be flipped. Unset it and run again")
	}
	quietLog(b)
	inputlag.SetLive(on)
	b.Cleanup(func() { inputlag.SetLive(false) })
	run := &runner{
		taskID:   "bench",
		pty:      nopPTY{newFakePTY()},
		buf:      newRing(1<<16, 80),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
	key, echo := []byte("a"), []byte("a")
	var lagIn atomic.Int64
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		got := lagStart()
		if got.IsZero() {
			_ = run.writeOperatorInput(key)
		} else {
			noteLagIn(&lagIn, got)
			_ = run.writeOperatorInputTimed(key, got, "bench")
		}
		run.deliverOutput(echo)
		sent := lagStart()
		if !sent.IsZero() {
			noteLagOut(&lagIn, "bench", run.lagRead.Load(), sent, time.Now(), 0, len(echo))
		}
	}
}

// quietLog sends the log to io.Discard for one benchmark, so the switch's own
// line does not land in the middle of the results.
func quietLog(b *testing.B) {
	out := log.Writer()
	log.SetOutput(io.Discard)
	b.Cleanup(func() { log.SetOutput(out) })
}

func BenchmarkRoomKeystrokeLagOff(b *testing.B) { benchKeystroke(b, false) }
func BenchmarkRoomKeystrokeLagOn(b *testing.B)  { benchKeystroke(b, true) }

// BenchmarkLagLine is one logged line, which is all the logging costs past the
// clocks, and only for a hop over the threshold. To io.Discard, so it is the
// formatting and the log mutex and not the disk.
func BenchmarkLagLine(b *testing.B) {
	quietLog(b)
	b.ReportAllocs()
	for b.Loop() {
		inputlag.Logf("room %s echo: ws frame in -> first output out %s (%s, ws write %s, %d bytes, %d chunks queued)",
			"bench", inputlag.Ms(31*time.Millisecond), echoSplit(1, 2, 3), inputlag.Ms(time.Millisecond), 1, 0)
	}
}

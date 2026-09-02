package daemon

import (
	"bytes"
	"strings"
	"testing"
)

// The ring buffer is what an attaching browser sees first. Getting it wrong
// means landing in a terminal showing the wrong thing, which is worse than
// landing in an empty one.
func TestRingBufferKeepsTheTail(t *testing.T) {
	r := newRing(16)

	r.Write([]byte("hello"))
	if got := string(r.Snapshot()); got != "hello" {
		t.Fatalf("short write: %q", got)
	}

	// Wrapping keeps the most recent bytes, in order.
	r.Write([]byte("0123456789abcdef"))
	if got := string(r.Snapshot()); got != "0123456789abcdef" {
		t.Fatalf("after wrap: %q", got)
	}

	r2 := newRing(8)
	r2.Write([]byte("aaaa"))
	r2.Write([]byte("bbbb"))
	r2.Write([]byte("cc"))
	if got := string(r2.Snapshot()); got != "aabbbbcc" {
		t.Fatalf("wrapped tail wrong: %q", got)
	}
}

// A single write larger than the whole buffer keeps its tail, not its head.
// The end of a huge burst is the part that still matters.
func TestRingBufferHandlesOversizedWrite(t *testing.T) {
	r := newRing(8)
	r.Write(bytes.Repeat([]byte("x"), 20))
	r.Write([]byte("END"))
	got := string(r.Snapshot())
	if !strings.HasSuffix(got, "END") {
		t.Fatalf("lost the end of the stream: %q", got)
	}
	if len(got) != 8 {
		t.Fatalf("buffer grew past its size: %d bytes", len(got))
	}
}

func TestRingBufferIsEmptyBeforeAnyWrite(t *testing.T) {
	if got := newRing(32).Snapshot(); len(got) != 0 {
		t.Fatalf("fresh buffer is not empty: %q", got)
	}
}

// A slow attacher must be dropped rather than allowed to block the reader,
// because a blocked reader eventually stalls the runner itself.
func TestFanoutDoesNotBlockOnASlowWatcher(t *testing.T) {
	r := &runner{
		taskID:   "t",
		buf:      newRing(64),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
	_, ch := r.subscribe()

	// Far more than the channel buffer, with nobody reading.
	for i := 0; i < 500; i++ {
		r.fanout([]byte("chunk"))
	}
	// Reaching here at all is the assertion: a blocking fanout would deadlock
	// the test rather than fail it.
	if len(ch) == 0 {
		t.Fatal("the watcher received nothing")
	}
	r.unsubscribe(ch)
	// A closed channel still yields its buffered chunks before reporting
	// closed, so drain before asking.
	for range ch {
	}
	if _, ok := <-ch; ok {
		t.Fatal("unsubscribe left the channel open")
	}
}

// Subscribing after the runner has gone must not hang the caller waiting for
// output that will never come.
func TestSubscribeAfterExitClosesImmediately(t *testing.T) {
	r := &runner{
		taskID:   "t",
		buf:      newRing(64),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
	r.buf.Write([]byte("some earlier output"))
	close(r.done)

	backlog, ch := r.subscribe()
	if string(backlog) != "some earlier output" {
		t.Fatalf("backlog lost: %q", backlog)
	}
	if _, ok := <-ch; ok {
		t.Fatal("channel should already be closed for a runner that has exited")
	}
}

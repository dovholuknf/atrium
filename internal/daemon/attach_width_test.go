package daemon

import (
	"context"
	"sync"

	"github.com/aymanbagabas/go-pty"
)

// ATTACHING WIDE TO A SESSION THAT RAN NARROW, end to end over the socket the
// board actually uses.
//
// The unit tests either side of this one cover the ring buffer on its own.
// What they cannot cover is the ORDER, which is half the bug: a viewer's size
// arrives as a frame after the socket is up, so a daemon that writes the
// backlog first has already decided what to replay using the size the last
// viewer left behind.

// fakePTY is a terminal that records the sizes it is asked for and swallows
// everything typed at it.
//
// A real pseudo terminal would need a real process producing real output at a
// real width, which is a slow test of the operating system rather than a test
// of what to replay.
type fakePTY struct {
	mu    sync.Mutex
	sizes []viewport
	// What was typed INTO it, which the peer bus tests read back. A terminal
	// that swallows its input cannot answer whether a message was submitted,
	// and whether Enter was pressed is the whole difference between two of the
	// three states in `tellByTyping`.
	in     []byte
	closed chan struct{}
}

func newFakePTY() *fakePTY { return &fakePTY{closed: make(chan struct{})} }

// Read blocks until close, which is what a quiet terminal does. Nothing in
// these tests reads from it: output is put into the ring buffer directly, the
// way the supervisor's reader would.
func (f *fakePTY) Read(p []byte) (int, error) { <-f.closed; return 0, context.Canceled }

func (f *fakePTY) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.in = append(f.in, p...)
	return len(p), nil
}

// written is everything typed into it so far.
func (f *fakePTY) written() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return string(f.in)
}

func (f *fakePTY) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	select {
	case <-f.closed:
	default:
		close(f.closed)
	}
	return nil
}

func (f *fakePTY) Resize(cols, rows int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sizes = append(f.sizes, viewport{cols, rows})
	return nil
}

func (f *fakePTY) resized() []viewport {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]viewport(nil), f.sizes...)
}

func (f *fakePTY) Name() string                                               { return "fake-pty" }
func (f *fakePTY) Fd() uintptr                                                { return 0 }
func (f *fakePTY) Command(string, ...string) *pty.Cmd                         { return nil }
func (f *fakePTY) CommandContext(context.Context, string, ...string) *pty.Cmd { return nil }

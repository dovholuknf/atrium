package daemon

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// safeBuf collects log output written from the daemon's goroutines while the
// test reads it. A bare bytes.Buffer would be a data race.
type safeBuf struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	wake chan struct{}
}

func (b *safeBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, err := b.buf.Write(p)
	// Every write wakes whoever is waiting for a line, so waitFor reacts to
	// the log itself and never guesses how long startup takes.
	if b.wake != nil {
		close(b.wake)
		b.wake = nil
	}
	return n, err
}

func (b *safeBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitFor blocks until the log contains want. There is no wall-clock limit of
// its own: startup under load can take seconds (resolving real runners on PATH
// alone took 4s once), and a fixed guess flaked. A daemon that never gets
// there is stopped by go test's own timeout, which prints the goroutines.
func (b *safeBuf) waitFor(t *testing.T, want string) {
	t.Helper()
	// A daemon that never logs the line would otherwise hold the whole package
	// until go test's own timeout, which once took 33 minutes.
	deadline := time.After(time.Minute)
	for {
		b.mu.Lock()
		if strings.Contains(b.buf.String(), want) {
			b.mu.Unlock()
			return
		}
		if b.wake == nil {
			b.wake = make(chan struct{})
		}
		ch := b.wake
		b.mu.Unlock()
		select {
		case <-ch:
		case <-deadline:
			t.Fatalf("no %q in the log after a minute:\n%s", want, b.String())
		}
	}
}

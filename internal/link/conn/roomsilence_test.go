package conn

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

// A path that goes quiet without a reset: the far end holds the pipe open and says nothing.
func TestReadNoteGivesUpOnABlackHole(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	var n note
	start := time.Now()
	err := readNote(a, bufio.NewReader(a), 5*time.Millisecond, &n)
	if !errors.Is(err, errHubSilent) {
		t.Fatalf("err = %v, want errHubSilent", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
}

// A frame inside the window is read, and the window starts again for the next one.
func TestReadNoteTakesAFrameAndRearms(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	br := bufio.NewReader(a)
	go func() { _ = writeJSON(b, note{Beat: 7}) }()
	var n note
	if err := readNote(a, br, time.Second, &n); err != nil || n.Beat != 7 {
		t.Fatalf("n=%+v err=%v", n, err)
	}
	if err := readNote(a, br, 5*time.Millisecond, &n); !errors.Is(err, errHubSilent) {
		t.Fatalf("second read err = %v", err)
	}
}

func TestReadNoteWithNoSilenceSetsNoDeadline(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	go func() { _ = writeJSON(b, note{Beat: 1}) }()
	var n note
	if err := readNote(a, bufio.NewReader(a), 0, &n); err != nil || n.Beat != 1 {
		t.Fatalf("n=%+v err=%v", n, err)
	}
}

// A pooled connection is old when it is used, and http.Server timeouts start at accept, so any timeout drops it.
func TestTheLinkServerHasNoTimeout(t *testing.T) {
	srv := newLinkServer(http.NotFoundHandler())
	if srv.ReadHeaderTimeout != 0 || srv.ReadTimeout != 0 || srv.WriteTimeout != 0 || srv.IdleTimeout != 0 {
		t.Fatalf("the link server has a timeout: %v %v %v %v",
			srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout)
	}
}

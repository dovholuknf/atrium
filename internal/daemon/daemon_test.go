package daemon

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/testdiag"
)

// freePort asks the OS for a port nobody is using, so tests never collide with
// a real daemon.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

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

func startDaemon(t *testing.T) (*Daemon, *safeBuf, context.CancelFunc, chan error) {
	t.Helper()
	return startDaemonWith(t, nil)
}

// closeAtCleanup closes d when the test ends and waits until its database files can be deleted, so the temp dir's
// own cleanup, which runs after this one, can remove them. Every helper that makes a daemon on a temp dir uses it.
//
// Closing is not enough on Windows. A connection a goroutine was still using when the store closed is let go only
// when its query ends, and until then the file cannot be deleted.
func closeAtCleanup(t *testing.T, d *Daemon) {
	t.Helper()
	t.Cleanup(func() {
		// Before the close, while whatever the test was waiting on is still there to see.
		if t.Failed() {
			testdiag.Dump(t, "the test failed")
		}
		_ = d.Close()
		releaseDB(t, filepath.FromSlash(d.opts.DBPath))
	})
}

// releaseDB waits until the database at path and its WAL files are gone, deleting each as soon as nothing holds it.
func releaseDB(t *testing.T, path string) {
	t.Helper()
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		end := time.Now().Add(10 * time.Second)
		for {
			err := os.Remove(p)
			if err == nil || os.IsNotExist(err) {
				break
			}
			if time.Now().After(end) {
				t.Errorf("%s is still held 10s after the daemon closed: %v", p, err)
				testdiag.Dump(t, filepath.Base(p)+" is still held after the daemon closed")
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// runAtCleanup runs d until the test ends, then cancels it and waits for Run to return, so nothing Run started is
// still writing when the store closes. It is registered after closeAtCleanup and so runs before it.
func runAtCleanup(t *testing.T, d *Daemon) (context.CancelFunc, chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		errCh <- d.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Errorf("the daemon's Run had not returned 30s after it was cancelled")
			testdiag.Dump(t, "Run had not returned 30s after it was cancelled")
		}
	})
	return cancel, errCh
}

// Ctrl-C has to say what it is doing. Several seconds of silence while long
// polls drain reads as a hang, which is the whole complaint this addresses.
func TestShutdownIsNarrated(t *testing.T) {
	d, logs, cancel, errCh := startDaemon(t)

	if got := logs.String(); !strings.Contains(got, "ready. ctrl-c to stop") {
		t.Errorf("startup did not announce readiness:\n%s", got)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("clean shutdown returned an error: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("daemon did not stop")
	}

	out := logs.String()
	for _, want := range []string{
		"interrupt received, shutting down",
		"closing agent listener",
		"agent listener closed",
		"closing board listener",
		"board listener closed",
		"state is on disk at",
		"stopped in",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("shutdown log missing %q. full output:\n%s", want, out)
		}
	}
	// A clean stop is not a store failure. The address file used to be removed
	// after the store closed, and reading its setting then halted the store.
	if strings.Contains(out, "HALTED") {
		t.Errorf("a clean shutdown halted the store. full output:\n%s", out)
	}
	_ = d
}

// Shutting down must not hang waiting for a long poll to expire.
func TestShutdownIsPrompt(t *testing.T) {
	_, _, cancel, errCh := startDaemon(t)

	start := time.Now()
	cancel()
	select {
	case <-errCh:
	case <-time.After(15 * time.Second):
		t.Fatal("daemon did not stop")
	}
	if elapsed := time.Since(start); elapsed > 8*time.Second {
		t.Errorf("shutdown took %s, which is long enough to look broken", elapsed)
	}
}

// An open browser tab holds an SSE stream, and Shutdown waits for in-flight
// requests. Without releasing subscribers first, one tab makes every shutdown
// sit out the full grace period.
func TestShutdownWithAnOpenEventStream(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)

	streamed := make(chan struct{})
	go func() {
		resp, err := http.Get("http://" + d.opts.HumanAddr + "/v1/events")
		if err != nil {
			close(streamed)
			return
		}
		defer resp.Body.Close()
		close(streamed)
		// Hold the stream open exactly as a browser would.
		io.Copy(io.Discard, resp.Body)
	}()

	select {
	case <-streamed:
	case <-time.After(5 * time.Second):
		t.Fatal("event stream never opened")
	}
	// Let the subscription register before pulling the rug.
	time.Sleep(200 * time.Millisecond)

	start := time.Now()
	cancel()
	select {
	case <-errCh:
	case <-time.After(15 * time.Second):
		t.Fatal("daemon did not stop with a stream attached")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("an open event stream held shutdown for %s", elapsed)
	}
}

// A database that cannot be opened is tier one: refuse to start rather than
// run without durable state.
func TestRefusesToStartOnBadDatabase(t *testing.T) {
	dir := t.TempDir()
	// A directory where the file should be makes the open fail.
	_, err := New(Options{
		AgentAddr: freePort(t),
		HumanAddr: freePort(t),
		DBPath:    filepath.ToSlash(dir),
	})
	if err == nil {
		t.Fatal("daemon started with an unusable database")
	}
}

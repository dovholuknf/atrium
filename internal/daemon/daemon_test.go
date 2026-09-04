package daemon

import (
	"bytes"
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitFor blocks until the log contains want, or gives up.
func (b *safeBuf) waitFor(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(b.String(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("log never contained %q. got:\n%s", want, b.String())
}

func startDaemon(t *testing.T) (*Daemon, *safeBuf, context.CancelFunc, chan error) {
	t.Helper()
	logs := &safeBuf{}
	old := log.Writer()
	log.SetOutput(logs)
	t.Cleanup(func() { log.SetOutput(old) })

	dir := t.TempDir()
	d, err := New(Options{
		AgentAddr: freePort(t),
		HumanAddr: freePort(t),
		DBPath:    filepath.ToSlash(filepath.Join(dir, "atrium.db")),
		LongPoll:  2 * time.Second,
		// Never the machine's real one. Run writes this file on start and
		// deletes it on stop, so a test without this removes the address of
		// whatever daemon is actually running while the test suite runs.
		LocationFile: filepath.Join(dir, "daemon.json"),
	})
	if err != nil {
		t.Fatalf("daemon did not start: %v", err)
	}
	t.Cleanup(func() { d.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()

	// A listener accepts as soon as net.Listen returns, which is before the
	// startup lines are written. Waiting on the port would race the log.
	logs.waitFor(t, "ready. ctrl-c to stop")
	return d, logs, cancel, errCh
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

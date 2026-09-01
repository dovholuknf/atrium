package daemon

import (
	"bytes"
	"context"
	"log"
	"net"
	"path/filepath"
	"strings"
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

func startDaemon(t *testing.T) (*Daemon, *bytes.Buffer, context.CancelFunc, chan error) {
	t.Helper()
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(old) })

	d, err := New(Options{
		AgentAddr: freePort(t),
		HumanAddr: freePort(t),
		DBPath:    filepath.ToSlash(filepath.Join(t.TempDir(), "atrium.db")),
		LongPoll:  2 * time.Second,
	})
	if err != nil {
		t.Fatalf("daemon did not start: %v", err)
	}
	t.Cleanup(func() { d.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()

	// Wait for both listeners rather than sleeping blind.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", d.opts.HumanAddr, 200*time.Millisecond)
		if err == nil {
			c.Close()
			return d, &logs, cancel, errCh
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	t.Fatal("daemon never came up")
	return nil, nil, nil, nil
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

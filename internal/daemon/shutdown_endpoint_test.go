package daemon

import (
	"context"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startDaemonWith is startDaemon with control over the options, for the cases
// that turn a guard on.
func startDaemonWith(t *testing.T, mutate func(*Options)) (*Daemon, *safeBuf, context.CancelFunc, chan error) {
	t.Helper()
	logs := &safeBuf{}
	old := log.Writer()
	log.SetOutput(logs)
	t.Cleanup(func() { log.SetOutput(old) })

	dir := t.TempDir()
	opts := Options{
		AgentAddr: freePort(t),
		HumanAddr: freePort(t),
		DBPath:    filepath.ToSlash(filepath.Join(dir, "atrium.db")),
		LongPoll:  2 * time.Second,
		// Never the machine's real one. See Options.LocationFile.
		LocationFile: filepath.Join(dir, "daemon.json"),
	}
	if mutate != nil {
		mutate(&opts)
	}
	d, err := New(opts)
	if err != nil {
		t.Fatalf("daemon did not start: %v", err)
	}
	t.Cleanup(func() { d.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()
	logs.waitFor(t, "ready. ctrl-c to stop")
	return d, logs, cancel, errCh
}

func shutdownReq(t *testing.T, d *Daemon, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://"+d.opts.HumanAddr+"/v1/shutdown", nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("X-Atrium-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("shutdown request: %v", err)
	}
	return resp
}

// The point of the endpoint is that stopping is not killing. A kill closes
// every pseudo terminal at once and takes the runners with it, so the request
// has to reach the same wind-down that ctrl-c does, narration included.
func TestShutdownEndpointWindsDownProperly(t *testing.T) {
	d, logs, cancel, errCh := startDaemon(t)
	defer cancel()

	resp := shutdownReq(t, d, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("a loopback shutdown was refused with %s", resp.Status)
	}
	resp.Body.Close()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("shutdown returned an error: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the daemon never stopped")
	}

	out := logs.String()
	for _, want := range []string{
		"shutdown requested by",
		"closing agent listener",
		"board listener closed",
		"stopped in",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("shutdown log missing %q. full output:\n%s", want, out)
		}
	}
}

// The caller has to be told it worked. Shutting down first would close the
// connection the answer travels on, and the caller would see a broken pipe.
func TestShutdownEndpointAnswersBeforeStopping(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer cancel()

	start := time.Now()
	resp := shutdownReq(t, d, "")
	answered := time.Since(start)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("shutdown was refused with %s", resp.Status)
	}
	// The wind-down alone gives supervised runners ten seconds. An answer that
	// waited for it would be indistinguishable from a hang.
	if answered > 3*time.Second {
		t.Errorf("the answer took %s, so the caller waited out the wind-down", answered)
	}
	select {
	case <-errCh:
	case <-time.After(20 * time.Second):
		t.Fatal("the daemon never stopped")
	}
}

// With a token configured, loopback is no longer enough. Configuring one is how
// an operator says the board is reachable from somewhere else.
func TestShutdownEndpointRequiresItsToken(t *testing.T) {
	d, _, cancel, errCh := startDaemonWith(t, func(o *Options) {
		o.ShutdownToken = "correct-horse"
	})
	defer cancel()

	resp := shutdownReq(t, d, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a request with no token got %s, wanted 403", resp.Status)
	}

	resp = shutdownReq(t, d, "wrong")
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a request with the wrong token got %s, wanted 403", resp.Status)
	}

	// Still running, because neither refusal may have stopped anything.
	select {
	case err := <-errCh:
		t.Fatalf("a refused shutdown stopped the daemon anyway: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	resp = shutdownReq(t, d, "correct-horse")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the right token got %s", resp.Status)
	}
	select {
	case <-errCh:
	case <-time.After(20 * time.Second):
		t.Fatal("the daemon never stopped")
	}
}

// Asking twice must not panic on a closed channel or start a second wind-down.
//
// Tested against the stopper rather than the endpoint, because a second HTTP
// request races the listener closing and would fail on the connection rather
// than on the thing being tested.
func TestShutdownIsIdempotent(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer cancel()

	// Concurrently, since two browser tabs pressing the button at once is the
	// case that would close an already-closed channel.
	done := make(chan struct{})
	for i := 0; i < 5; i++ {
		go func() { d.stop.request("a test"); done <- struct{}{} }()
	}
	for i := 0; i < 5; i++ {
		<-done
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("shutdown returned an error: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the daemon never stopped")
	}
}

package daemon

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/edge"
)

// Stopping the daemon from somewhere other than its own terminal.
//
// A kill is not a stop. The daemon owns a pseudo terminal per supervised
// runner, and on Windows closing one takes the attached process with it, so
// killing the daemon ends every runner at once. The wind-down gives each one
// ten seconds; it had no remote entry point.

// stopper lets a handler reach the shutdown that Run is waiting on. Run selects
// on the channel; requesting a stop closes it once.
type stopper struct {
	once sync.Once
	ch   chan struct{}
	// reason is what asked for the stop, for the log line.
	mu     sync.Mutex
	reason string
}

func newStopper() *stopper { return &stopper{ch: make(chan struct{})} }

func (s *stopper) request(reason string) {
	s.once.Do(func() {
		s.mu.Lock()
		s.reason = reason
		s.mu.Unlock()
		close(s.ch)
	})
}

func (s *stopper) why() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reason
}

// handleShutdown asks the daemon to wind down.
//
// With no token configured, the request must come from loopback. Configuring
// one says remote access is wanted, so it replaces the loopback rule rather
// than adding to it.
//
// Answers before shutting down: shutting down first closes the connection the
// answer travels on, and the caller sees a broken pipe.
func (d *Daemon) shutdownAllowed(w http.ResponseWriter, r *http.Request) bool {
	token := d.opts.ShutdownToken
	if token == "" {
		// A share makes the loopback rule meaningless. The tunneler runs on
		// this machine and terminates the connection here, so a request that
		// arrived from another continent still presents as 127.0.0.1. The
		// rule was "only someone at this keyboard"; with a share running it
		// would silently become "anyone the overlay admits", and a kill switch
		// is the worst thing to hand out by accident.
		if d.sharing() {
			http.Error(w,
				"a share is running, so loopback no longer means this machine. "+
					"restart with --shutdown-token to allow this, or stop the share.",
				http.StatusForbidden)
			return false
		}
		// The machine's own user, not anybody a hand-started share proxies in,
		// which d.sharing() cannot know about. See edge.LocalOperator.
		if !edge.LocalOperator(r) {
			http.Error(w, "shutdown is for the machine's own user unless a token is configured"+edge.ProxyNote(r),
				http.StatusForbidden)
			return false
		}
	} else {
		got := r.Header.Get("X-Atrium-Token")
		if got == "" {
			got = r.URL.Query().Get("token")
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, "bad or missing token", http.StatusForbidden)
			return false
		}
	}
	return true
}

// handleShutdown asks the daemon to wind down. By default the working sessions are asked to wrap up first, see
// restartwrap.go, and `?now=1` skips that and also ends a wrap-up already waiting.
func (d *Daemon) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if !d.shutdownAllowed(w, r) {
		return
	}
	from := r.RemoteAddr
	now := truthy(r.URL.Query().Get("now"))
	wait := d.st.RestartWrapWait()
	w.Header().Set("Content-Type", "application/json")
	if now {
		_, _ = w.Write([]byte(`{"ok":true,"stopping":true,"graceful":false}`))
	} else {
		fmt.Fprintf(w, `{"ok":true,"stopping":true,"graceful":true,"wait_seconds":%d}`, int(wait/time.Second))
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	reason := "a shutdown request from " + from
	if now {
		log.Printf("[atrium] shutdown requested by %s, now: no wrap-up", from)
		d.wrap.cancelRunning()
		go d.stop.request(reason)
		return
	}
	log.Printf("[atrium] shutdown requested by %s", from)
	// Off the request goroutine, so the response is written and the connection
	// free before the listener starts closing. The wrap-up is bounded by the wait.
	go func() {
		d.wrapUpForRestart(context.Background(), wait, reason)
		d.stop.request(reason)
	}()
}

// truthy reads a query flag: 1, true or yes.
func truthy(v string) bool {
	switch v {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

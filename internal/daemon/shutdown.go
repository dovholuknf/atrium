package daemon

import (
	"crypto/subtle"
	"log"
	"net/http"
	"sync"

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
func (d *Daemon) handleShutdown(w http.ResponseWriter, r *http.Request) {
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
			return
		}
		// The machine's own user, not anybody a hand-started share proxies in,
		// which d.sharing() cannot know about. See edge.LocalOperator.
		if !edge.LocalOperator(r) {
			http.Error(w, "shutdown is for the machine's own user unless a token is configured"+edge.ProxyNote(r),
				http.StatusForbidden)
			return
		}
	} else {
		got := r.Header.Get("X-Atrium-Token")
		if got == "" {
			got = r.URL.Query().Get("token")
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, "bad or missing token", http.StatusForbidden)
			return
		}
	}

	from := r.RemoteAddr
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true,"stopping":true}`))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	log.Printf("[atrium] shutdown requested by %s", from)
	// Off the request goroutine, so the response is written and the connection
	// free before the listener starts closing.
	go d.stop.request("a shutdown request from " + from)
}

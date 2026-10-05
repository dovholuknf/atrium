package daemon

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Limits on guessing the password login.
//
// Two separate problems. A guess costs an attacker one request, so failures are
// counted per source and a source that keeps failing is made to wait. And every
// request carrying a Basic header runs scrypt at about 32 MiB, so the number of
// those running at once is bounded, or parallel anonymous requests are a memory
// denial of service on a public share.
const (
	// authFreeFails are the wrong guesses a source gets before it waits.
	authFreeFails = 5
	// authLockBase is the first wait, and it doubles with each failure after.
	authLockBase = 2 * time.Second
	// authLockMax caps the wait.
	authLockMax = 15 * time.Minute
	// authFailsMost bounds the table, since sources are chosen by the caller.
	authFailsMost = 10000
	// authFailsForget is how long a quiet source is remembered.
	authFailsForget = time.Hour
	// authScryptSlots is how many password checks may run at once.
	authScryptSlots = 2
)

type failRec struct {
	n     int
	until time.Time
	last  time.Time
}

// authLimiter is the state for both limits. The zero value works.
type authLimiter struct {
	mu    sync.Mutex
	fails map[string]*failRec
	sem   chan struct{}
	// now is replaceable so a test need not sleep.
	now func() time.Time
}

func (l *authLimiter) clock() time.Time {
	if l.now != nil {
		return l.now()
	}
	return time.Now()
}

// blocked returns how long this source must still wait, or zero.
func (l *authLimiter) blocked(src string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if r, ok := l.fails[src]; ok {
		if wait := r.until.Sub(l.clock()); wait > 0 {
			return wait
		}
	}
	return 0
}

// fail records a wrong guess.
func (l *authLimiter) fail(src string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock()
	if l.fails == nil {
		l.fails = map[string]*failRec{}
	}
	r, ok := l.fails[src]
	if !ok {
		if len(l.fails) >= authFailsMost {
			l.makeRoom(now)
		}
		r = &failRec{}
		l.fails[src] = r
	}
	r.n++
	r.last = now
	if r.n >= authFreeFails {
		wait := authLockBase
		for i := authFreeFails; i < r.n && wait < authLockMax; i++ {
			wait *= 2
		}
		if wait > authLockMax {
			wait = authLockMax
		}
		r.until = now.Add(wait)
	}
}

// makeRoom drops forgotten sources, and the quietest one if that is not enough.
func (l *authLimiter) makeRoom(now time.Time) {
	var oldest string
	var oldestAt time.Time
	for k, r := range l.fails {
		if now.Sub(r.last) > authFailsForget && !r.until.After(now) {
			delete(l.fails, k)
			continue
		}
		if oldest == "" || r.last.Before(oldestAt) {
			oldest, oldestAt = k, r.last
		}
	}
	if len(l.fails) >= authFailsMost && oldest != "" {
		delete(l.fails, oldest)
	}
}

// succeed forgets a source's failures.
func (l *authLimiter) succeed(src string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, src)
}

// acquire takes a password-check slot without waiting, and says whether it got
// one. Refusing beats queueing: a queue of waiting requests is the memory the
// bound exists to save.
func (l *authLimiter) acquire() bool {
	l.mu.Lock()
	if l.sem == nil {
		l.sem = make(chan struct{}, authScryptSlots)
	}
	sem := l.sem
	l.mu.Unlock()
	select {
	case sem <- struct{}{}:
		return true
	default:
		return false
	}
}

func (l *authLimiter) release() {
	l.mu.Lock()
	sem := l.sem
	l.mu.Unlock()
	<-sem
}

// authSource names who a request is from, for counting failures.
//
// A share's proxy connects from loopback for everybody, so there the address
// the proxy appended to X-Forwarded-For is the one that means something. It is
// the LAST entry, since a caller can write the earlier ones. From anywhere that
// is not loopback the header is ignored, because then nothing trustworthy added
// it.
func authSource(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if last := strings.TrimSpace(parts[len(parts)-1]); last != "" {
				return last
			}
		}
	}
	return host
}

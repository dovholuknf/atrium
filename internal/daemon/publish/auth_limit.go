package publish

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
	AuthFreeFails = 5
	// authLockBase is the first wait, and it doubles with each failure after.
	AuthLockBase = 2 * time.Second
	// authLockMax caps the wait.
	AuthLockMax = 15 * time.Minute
	// authFailsMost bounds the table, since sources are chosen by the caller.
	AuthFailsMost = 10000
	// authFailsForget is how long a quiet source is remembered.
	AuthFailsForget = time.Hour
	// authScryptSlots is how many password checks may run at once.
	AuthScryptSlots = 2
)

type FailRec struct {
	N     int
	Until time.Time
	Last  time.Time
}

// authLimiter is the state for both limits. The zero value works.
type AuthLimiter struct {
	Mu    sync.Mutex
	Fails map[string]*FailRec
	Sem   chan struct{}
	// now is replaceable so a test need not sleep.
	Now func() time.Time
}

func (l *AuthLimiter) Clock() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

// blocked returns how long this source must still wait, or zero.
func (l *AuthLimiter) Blocked(src string) time.Duration {
	l.Mu.Lock()
	defer l.Mu.Unlock()
	if r, ok := l.Fails[src]; ok {
		if wait := r.Until.Sub(l.Clock()); wait > 0 {
			return wait
		}
	}
	return 0
}

// fail records a wrong guess.
func (l *AuthLimiter) Fail(src string) {
	l.Mu.Lock()
	defer l.Mu.Unlock()
	now := l.Clock()
	if l.Fails == nil {
		l.Fails = map[string]*FailRec{}
	}
	r, ok := l.Fails[src]
	if !ok {
		if len(l.Fails) >= AuthFailsMost {
			l.MakeRoom(now)
		}
		r = &FailRec{}
		l.Fails[src] = r
	}
	r.N++
	r.Last = now
	if r.N >= AuthFreeFails {
		wait := AuthLockBase
		for i := AuthFreeFails; i < r.N && wait < AuthLockMax; i++ {
			wait *= 2
		}
		if wait > AuthLockMax {
			wait = AuthLockMax
		}
		r.Until = now.Add(wait)
	}
}

// makeRoom drops forgotten sources, and the quietest one if that is not enough.
func (l *AuthLimiter) MakeRoom(now time.Time) {
	var oldest string
	var oldestAt time.Time
	for k, r := range l.Fails {
		if now.Sub(r.Last) > AuthFailsForget && !r.Until.After(now) {
			delete(l.Fails, k)
			continue
		}
		if oldest == "" || r.Last.Before(oldestAt) {
			oldest, oldestAt = k, r.Last
		}
	}
	if len(l.Fails) >= AuthFailsMost && oldest != "" {
		delete(l.Fails, oldest)
	}
}

// succeed forgets a source's failures.
func (l *AuthLimiter) Succeed(src string) {
	l.Mu.Lock()
	defer l.Mu.Unlock()
	delete(l.Fails, src)
}

// acquire takes a password-check slot without waiting, and says whether it got
// one. Refusing beats queueing: a queue of waiting requests is the memory the
// bound exists to save.
func (l *AuthLimiter) Acquire() bool {
	l.Mu.Lock()
	if l.Sem == nil {
		l.Sem = make(chan struct{}, AuthScryptSlots)
	}
	sem := l.Sem
	l.Mu.Unlock()
	select {
	case sem <- struct{}{}:
		return true
	default:
		return false
	}
}

func (l *AuthLimiter) Release() {
	l.Mu.Lock()
	sem := l.Sem
	l.Mu.Unlock()
	<-sem
}

// authSource names who a request is from, for counting failures.
//
// A share's proxy connects from loopback for everybody, so there the address
// the proxy appended to X-Forwarded-For is the one that means something. It is
// the LAST entry, since a caller can write the earlier ones. From anywhere that
// is not loopback the header is ignored, because then nothing trustworthy added
// it.
func AuthSource(r *http.Request) string {
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

// Package seenhost remembers the scheme and host the human's browser last reached the board on, which is what every
// link handed to the human has to use.
package seenhost

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var seen atomic.Pointer[string]

// browser is a request from a web browser. Sec-Fetch-Mode is sent only to https and loopback origins, so a board on
// http://atrium.ziti carries none, and a browser's User-Agent is what is left, and only one with an engine token: PowerShell sends Mozilla/5.0 too. The CLI and agents send neither.
func browser(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Mode") != "" {
		return true
	}
	ua := r.Header.Get("User-Agent")
	return strings.HasPrefix(ua, "Mozilla/") && !strings.Contains(ua, "PowerShell") &&
		(strings.Contains(ua, "AppleWebKit/") || strings.Contains(ua, "Gecko/"))
}

// Note records the origin of a browser request: every one is the human at that host, whether it reads, types or only
// listens. An agent curling the loopback address never overwrites it.
func Note(r *http.Request) {
	if !browser(r) || r.Host == "" {
		return
	}
	host := r.Host
	if f := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0]); f != "" {
		host = f
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if f := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); f == "http" || f == "https" {
		scheme = f
	}
	base := scheme + "://" + host
	if old := seen.Load(); old != nil && loopback(base) && !loopback(*old) {
		return
	}
	if old := seen.Swap(&base); old == nil || *old != base {
		line := fmt.Sprintf("the human's host is now %s, from %s on %s %q", base, r.RemoteAddr, r.URL.Path, r.Header.Get("User-Agent"))
		log.Printf("[atrium] %s", line)
		logFile(line)
	}
}

// HeaderName carries the hub's seen host down to a room, whose own browser requests may be few.
const HeaderName = "X-Atrium-Human-Base"

// NoteBase records a base the hub already worked out.
func NoteBase(base string) {
	if strings.HasPrefix(base, "http://") || strings.HasPrefix(base, "https://") {
		if old := seen.Load(); old != nil && loopback(base) && !loopback(*old) {
			return
		}
		seen.Store(&base)
	}
}

// Handler records every request that qualifies, then serves it.
func Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Note(r)
		next.ServeHTTP(w, r)
	})
}

// Base is the last host a browser used, else the fallback.
func Base(fallback string) string {
	if p := seen.Load(); p != nil {
		return *p
	}
	return Loopback(fallback)
}

// loopback is a base on this machine, which a tab beside the board's process can reach while the human is elsewhere.
// It never displaces a remote origin, or two tabs would flip the link between two hosts.
func loopback(base string) bool {
	return Loopback(base) != base || strings.HasPrefix(base, "http://127.0.0.1") || strings.HasPrefix(base, "https://127.0.0.1") ||
		strings.HasPrefix(base, "http://[::1]") || strings.HasPrefix(base, "https://[::1]")
}

// Loopback spells a localhost base as 127.0.0.1, since a link we choose ourselves never says localhost.
func Loopback(base string) string {
	for _, s := range []string{"http://", "https://"} {
		if rest, ok := strings.CutPrefix(base, s+"localhost"); ok && (rest == "" || rest[0] == ':' || rest[0] == '/') {
			return s + "127.0.0.1" + rest
		}
	}
	return base
}

// Reset forgets the host. Tests only.
func Reset() { seen.Store(nil) }

var (
	fileMu   sync.Mutex
	filePath string
)

// LogFile makes every change of the host land in this file too, since a hub's stderr is kept nowhere.
func LogFile(path string) {
	fileMu.Lock()
	filePath = path
	fileMu.Unlock()
}

func logFile(line string) {
	fileMu.Lock()
	defer fileMu.Unlock()
	if filePath == "" {
		return
	}
	if f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), line)
		f.Close()
	}
}

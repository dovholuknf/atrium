package daemon

import (
	"net"
	"net/http"
	"net/http/pprof"
	"strings"
)

// Go's profiler, on the loopback human listener and nowhere else.
//
// IT EXISTS BECAUSE NOTHING COULD SAY WHAT HELD THE MEMORY. The live room sat
// at thirteen gigabytes of private memory and there was no way to ask it why
// short of rebuilding it with this in, which is the restart the question was
// trying to avoid. So it is always in, and a heap profile is one `go tool pprof`
// away: `go tool pprof http://127.0.0.1:<http>/debug/pprof/heap`.
//
// WHERE IT IS NOT, and each is deliberate:
//
//   - **The agent listener.** Every session can reach that one, and a profile
//     is a map of the process that answers permission prompts.
//   - **An overlay share, the hub link, a lent card.** All three serve
//     `BoardHandler` on a listener of their own, and none of them goes through
//     the human listener's server, so wrapping only that server keeps this off
//     every one of them without any of them having to know.
//   - **A human listener bound to anything but loopback.** `:7778` is every
//     interface, and the profiler is not something to publish by leaving an
//     address blank.
//
// Each request is checked again as well, because a check made once at listen
// time is a check on the address and not on who connected: the far end must
// be loopback, the Host must name loopback, which is what stops a web page
// that rebinds its own name to 127.0.0.1 from reading a profile, and nothing
// may say it forwarded the request.
//
// Importing `net/http/pprof` also registers it on `http.DefaultServeMux`.
// Nothing in atrium serves that mux, and nothing may start to.
func withProfiling(board http.Handler, ln net.Listener) http.Handler {
	if ln == nil || !isLoopbackAddr(ln.Addr()) {
		return board
	}
	prof := http.NewServeMux()
	prof.HandleFunc("/debug/pprof/", pprof.Index)
	prof.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	prof.HandleFunc("/debug/pprof/profile", pprof.Profile)
	prof.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	prof.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/debug/pprof" && !strings.HasPrefix(r.URL.Path, "/debug/pprof/") {
			board.ServeHTTP(w, r)
			return
		}
		if !profileAllowed(r) {
			http.NotFound(w, r)
			return
		}
		prof.ServeHTTP(w, r)
	})
}

// profileAllowed is the per-request half: loopback at the far end, loopback in
// the Host, and no forwarding header.
func profileAllowed(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return false
	}
	if !isLoopbackHost(r.Host) {
		return false
	}
	for _, h := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Real-Ip"} {
		if r.Header.Get(h) != "" {
			return false
		}
	}
	return true
}

func isLoopbackAddr(a net.Addr) bool {
	tcp, ok := a.(*net.TCPAddr)
	return ok && tcp.IP != nil && tcp.IP.IsLoopback()
}

// isLoopbackHost is a Host header naming this machine by a loopback name.
func isLoopbackHost(h string) bool {
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	h = strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

package daemon

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// isProfile is whether a response is the profiler answering, rather than the
// board's page or a refusal.
func isProfile(code int, body string) bool {
	return code == http.StatusOK && strings.Contains(body, "heap profile:")
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// The one place a heap profile is served: the loopback human listener. And the
// board still answers everything else on it.
func TestTheLoopbackBoardServesAHeapProfile(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	code, body := get(t, "http://"+d.opts.HumanAddr+"/debug/pprof/heap?debug=1")
	if !isProfile(code, body) {
		t.Fatalf("the loopback board did not serve a heap profile: %d %.200q", code, body)
	}
	if code, _ := get(t, "http://"+d.opts.HumanAddr+"/v1/health"); code != http.StatusOK {
		t.Fatalf("the board stopped answering /v1/health behind the profiler: %d", code)
	}

	// Never on the agent listener, which every session can reach.
	if code, body := get(t, "http://"+d.opts.AgentAddr+"/debug/pprof/heap?debug=1"); isProfile(code, body) {
		t.Fatal("the agent listener served a heap profile")
	}
}

// An overlay share, the hub link and a lent card all serve `BoardHandler` on a
// listener of their own. None of them may profile.
func TestTheBoardHandlerAloneHasNoProfiler(t *testing.T) {
	d := testDaemon(t)
	req := httptest.NewRequest("GET", "http://127.0.0.1/debug/pprof/heap?debug=1", nil)
	req.RemoteAddr = "127.0.0.1:5555"
	rec := httptest.NewRecorder()
	d.BoardHandler().ServeHTTP(rec, req)
	if isProfile(rec.Code, rec.Body.String()) {
		t.Fatal("the board handler served a heap profile, so every overlay share would too")
	}
}

type fakeLn struct {
	net.Listener
	addr net.Addr
}

func (f fakeLn) Addr() net.Addr { return f.addr }

func TestTheProfilerRefusesAnythingNotPlainlyLocal(t *testing.T) {
	board := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "the board")
	})
	loop := withProfiling(board, fakeLn{addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7781}})
	serve := func(h http.Handler, host, remote string, hdr map[string]string) (int, string) {
		req := httptest.NewRequest("GET", "http://"+host+"/debug/pprof/heap?debug=1", nil)
		req.RemoteAddr = remote
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}

	if code, body := serve(loop, "127.0.0.1:7781", "127.0.0.1:5555", nil); !isProfile(code, body) {
		t.Fatalf("a plain local request was refused: %d %.200q", code, body)
	}
	if code, body := serve(loop, "localhost:7781", "[::1]:5555", nil); !isProfile(code, body) {
		t.Fatalf("localhost over ipv6 was refused: %d %.200q", code, body)
	}
	for name, c := range map[string]struct {
		host, remote string
		hdr          map[string]string
	}{
		"a remote peer":         {"127.0.0.1:7781", "192.0.2.7:5555", nil},
		"a rebound name":        {"evil.example:7781", "127.0.0.1:5555", nil},
		"a forwarded request":   {"127.0.0.1:7781", "127.0.0.1:5555", map[string]string{"X-Forwarded-For": "192.0.2.7"}},
		"a Forwarded header":    {"127.0.0.1:7781", "127.0.0.1:5555", map[string]string{"Forwarded": "for=192.0.2.7"}},
		"a forwarded host name": {"127.0.0.1:7781", "127.0.0.1:5555", map[string]string{"X-Forwarded-Host": "x.share.zrok.io"}},
	} {
		if code, body := serve(loop, c.host, c.remote, c.hdr); isProfile(code, body) {
			t.Errorf("%s was served a heap profile", name)
		}
	}

	// A human listener on every interface gets no profiler at all: the board
	// answers the path, as it would any other.
	wide := withProfiling(board, fakeLn{addr: &net.TCPAddr{IP: net.IPv4zero, Port: 7778}})
	if code, body := serve(wide, "127.0.0.1:7778", "127.0.0.1:5555", nil); isProfile(code, body) || body != "the board" {
		t.Fatalf("a listener on every interface profiled: %d %.200q", code, body)
	}
}

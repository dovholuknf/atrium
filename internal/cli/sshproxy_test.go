package cli

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// door is a fake hub or room door: it answers 101 and echoes, or says a status with a body.
func fakeDoor(t *testing.T, status int, body string, hits *atomic.Int64, seen *atomic.Value) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			hits.Add(1)
		}
		if seen != nil {
			seen.Store(r.Method + " " + r.URL.Path + " room=" + r.Header.Get("X-Atrium-Room") + " claim=" + r.Header.Get("X-Atrium-Claim"))
		}
		if status != http.StatusSwitchingProtocols {
			w.WriteHeader(status)
			io.WriteString(w, body)
			return
		}
		c, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer c.Close()
		io.WriteString(c, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: atrium-stream\r\n\r\n")
		io.WriteString(c, "SSH-2.0-fake\r\n")
		_, _ = io.Copy(c, rw.Reader)
		if tc, ok := c.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func runProxyT(t *testing.T, o proxyOpts, in string) (code int, out, errs string) {
	t.Helper()
	var so, se bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- runProxy(strings.NewReader(in), &so, &se, o) }()
	select {
	case code = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the proxy did not finish")
	}
	return code, so.String(), se.String()
}

const deadAddr = "http://127.0.0.1:1"

// STDOUT IS STREAM BYTES AND NOTHING ELSE: the banner, then the echo of what ssh sent.
func TestTheProxyWritesOnlyStreamBytesToStdout(t *testing.T) {
	var seen atomic.Value
	hub := fakeDoor(t, http.StatusSwitchingProtocols, "", nil, &seen)
	code, out, errs := runProxyT(t, proxyOpts{room: "sg3", hub: hub.URL, room2: deadAddr, claim: "claude@sg4"}, "ssh bytes")
	if code != 0 || out != "SSH-2.0-fake\r\nssh bytes" || errs != "" {
		t.Fatalf("code %d stdout %q stderr %q", code, out, errs)
	}
	if got := seen.Load(); got != "POST /_hub/ssh/sg3 room= claim=claude@sg4" {
		t.Fatalf("the hub door was asked %v", got)
	}
}

// EVERY REFUSAL IS ONE SENTENCE ON STDERR, exit 255, and nothing on stdout, which ssh would read as protocol.
func TestARefusalIsOneSentenceOnStderrAndExit255(t *testing.T) {
	sentence := "sg3 has remote access off. turn it on at that machine: atrium room remote-access on"
	var roomHits atomic.Int64
	hub := fakeDoor(t, http.StatusForbidden, `{"error":"`+sentence+`"}`, nil, nil)
	room := fakeDoor(t, http.StatusSwitchingProtocols, "", &roomHits, nil)
	code, out, errs := runProxyT(t, proxyOpts{room: "sg3", hub: hub.URL, room2: room.URL}, "")
	if code != 255 || out != "" || errs != sentence+"\n" {
		t.Fatalf("code %d stdout %q stderr %q", code, out, errs)
	}
	// An answer from the hub is final: the room door is not tried to get a different one.
	if roomHits.Load() != 0 {
		t.Fatal("a refusal fell through to the room door")
	}
}

// A hub door that is not there, by a 404 or a page that is not a sentence, falls to the room's door.
func TestAMissingHubDoorFallsToTheRoomDoor(t *testing.T) {
	var seen atomic.Value
	room := fakeDoor(t, http.StatusSwitchingProtocols, "", nil, &seen)
	for name, hub := range map[string]string{
		"refused": deadAddr,
		"404":     fakeDoor(t, http.StatusNotFound, "404 page not found\n", nil, nil).URL,
		"a page":  fakeDoor(t, http.StatusOK, "<html>the room's own board</html>", nil, nil).URL,
	} {
		code, out, errs := runProxyT(t, proxyOpts{room: "sg3", hub: hub, room2: room.URL, claim: "u@h"}, "x")
		if code != 0 || out != "SSH-2.0-fake\r\nx" || errs != "" {
			t.Fatalf("%s: code %d stdout %q stderr %q", name, code, out, errs)
		}
		if got := seen.Load(); got != "POST /stream room=sg3 claim=u@h" {
			t.Fatalf("%s: the room door was asked %v", name, got)
		}
	}
}

func TestNeitherDoorSaysSoInWords(t *testing.T) {
	code, out, errs := runProxyT(t, proxyOpts{room: "sg3", hub: deadAddr, room2: deadAddr}, "")
	if code != 255 || out != "" || errs != "no atrium hub or room is running on this machine\n" {
		t.Fatalf("code %d stdout %q stderr %q", code, out, errs)
	}
}

func TestViaRoomNeverTriesTheHubDoor(t *testing.T) {
	var hubHits atomic.Int64
	hub := fakeDoor(t, http.StatusSwitchingProtocols, "", &hubHits, nil)
	room := fakeDoor(t, http.StatusSwitchingProtocols, "", nil, nil)
	code, _, _ := runProxyT(t, proxyOpts{room: "sg3", via: "room", hub: hub.URL, room2: room.URL}, "")
	if code != 0 || hubHits.Load() != 0 {
		t.Fatalf("code %d, the hub door was asked %d times", code, hubHits.Load())
	}
}

func TestARoomsRefusalIsSaidAsTheRoomSaidIt(t *testing.T) {
	sentence := "this room is not attached to a hub right now"
	room := fakeDoor(t, http.StatusServiceUnavailable, `{"error":"`+sentence+`"}`, nil, nil)
	code, out, errs := runProxyT(t, proxyOpts{room: "sg3", hub: deadAddr, room2: room.URL}, "")
	if code != 255 || out != "" || errs != sentence+"\n" {
		t.Fatalf("code %d stdout %q stderr %q", code, out, errs)
	}
}

// A HUB ON THE DEFAULT PORT IS FOUND even when what boardAddress finds is something else (a room's own board).
func TestTheProxyFallsBackToTheAltHubDoor(t *testing.T) {
	var seen atomic.Value
	hub := fakeDoor(t, http.StatusSwitchingProtocols, "", nil, &seen)
	other := fakeDoor(t, http.StatusNotFound, "", nil, nil)
	code, out, errs := runProxyT(t, proxyOpts{room: "sg3", hub: other.URL, hubAlt: hub.URL, room2: deadAddr}, "x")
	if code != 0 || out != "SSH-2.0-fake\r\nx" || errs != "" {
		t.Fatalf("code %d stdout %q stderr %q", code, out, errs)
	}
	if got := seen.Load(); got == nil {
		t.Fatal("the alt hub door was never asked")
	}
}

func TestHubDoorsHonourTheFlagTheEnvAndTheDefault(t *testing.T) {
	t.Setenv("ATRIUM_BOARD_URL", "")
	if first, alt := hubDoors("127.0.0.1:9999"); first != "http://127.0.0.1:9999" || alt != "" {
		t.Fatalf("flag: %q %q", first, alt)
	}
	t.Setenv("ATRIUM_BOARD_URL", "http://127.0.0.1:7781")
	if first, alt := hubDoors(""); first != "http://127.0.0.1:7781" || alt != defaultHubBoard {
		t.Fatalf("env: %q %q", first, alt)
	}
	t.Setenv("ATRIUM_BOARD_URL", "http://localhost:7778")
	if _, alt := hubDoors(""); alt != "" {
		t.Fatalf("the default board needs no second try, got %q", alt)
	}
}

func joinT(t *testing.T, stdin io.Reader, w parentWatch) (net.Conn, <-chan struct{}) {
	t.Helper()
	near, far := net.Pipe()
	t.Cleanup(func() { near.Close(); far.Close() })
	done := make(chan struct{})
	go func() {
		joinStdio(stdin, io.Discard, near, bufio.NewReader(near), w)
		close(done)
	}()
	return far, done
}

func mustReturn(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("joinStdio did not return")
	}
}

func TestJoinStdioReturnsWhenStdinHitsEOF(t *testing.T) {
	old := stdinDrain
	stdinDrain = time.Millisecond
	t.Cleanup(func() { stdinDrain = old })
	_, done := joinT(t, strings.NewReader(""), parentWatch{})
	mustReturn(t, done)
}

func TestJoinStdioReturnsWhenTheStreamCloses(t *testing.T) {
	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })
	far, done := joinT(t, pr, parentWatch{})
	far.Close()
	mustReturn(t, done)
}

func TestJoinStdioReturnsWhenTheParentIsGone(t *testing.T) {
	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })
	var calls atomic.Int64
	w := parentWatch{every: time.Millisecond, gone: func() bool { return calls.Add(1) >= 3 }}
	_, done := joinT(t, pr, w)
	mustReturn(t, done)
	if calls.Load() < 3 {
		t.Fatalf("the parent was asked %d times", calls.Load())
	}
}

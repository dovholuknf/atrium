//go:build integration

package link

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/remoteaccess"
)

// fakeSSHD writes a banner and echoes, and half closes back when the caller does.
func fakeSSHD(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, _ = io.WriteString(c, "SSH-2.0-fake\r\n")
				_, _ = io.Copy(c, c)
				if tc, ok := c.(*net.TCPConn); ok {
					_ = tc.CloseWrite()
				}
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

type streamRoom struct {
	room *Room
	dir  string
	// seen counts requests the room's own handler was asked, which a refused stream must leave at zero.
	seen *atomic.Int64
}

type streamRig struct {
	t     *testing.T
	rig   *rig
	front *httptest.Server
	sshd  int

	mu    sync.Mutex
	audit []string
	rooms map[string]*streamRoom
}

func (s *streamRig) lines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.audit...)
}

func (s *streamRig) hasLine(kind, sub string) bool {
	for _, l := range s.lines() {
		if strings.HasPrefix(l, kind+" ") && strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// newStreamRig is a real hub on loopback mutual TLS, with the proxy in front of it, a fake sshd, and a proven room for
// each name given.
func newStreamRig(t *testing.T, access time.Duration, names ...string) *streamRig {
	t.Helper()
	s := &streamRig{t: t, rig: newRig(t), sshd: fakeSSHD(t), rooms: map[string]*streamRoom{}}
	s.rig.hub.OnStream = func(room, kind, detail string) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.audit = append(s.audit, kind+" "+room+": "+detail)
	}
	px := NewProxy(s.rig.hub, nil, "", nil)
	s.front = httptest.NewServer(px)
	t.Cleanup(s.front.Close)
	for _, n := range names {
		s.addRoom(n, access)
	}
	return s
}

func (s *streamRig) addRoom(name string, access time.Duration) *streamRoom {
	s.t.Helper()
	rk, err := s.rig.enrol(name, s.rig.secretFor(name))
	if err != nil {
		s.t.Fatal(err)
	}
	seen := &atomic.Int64{}
	room := &Room{
		Name: name, Dial: Proven{Dialer: plain{addr: s.rig.addr}, Keys: rk},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen.Add(1)
			w.Header().Set("X-Caller-Seen", r.Header.Get("X-Atrium-Caller"))
			fmt.Fprint(w, "board")
		}),
		RemoteDir: s.t.TempDir(),
		T:         Timings{Beat: 200 * time.Millisecond, Warm: 1, Backoff: 50 * time.Millisecond, Access: access},
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = room.Run(ctx) }()
	// The stream handlers write to RemoteDir after their stream ends, and the TempDir is removed right after this
	// runs, so the room is stopped and every handler waited for first.
	s.t.Cleanup(func() { cancel(); room.EndStreams() })
	waitFor(s.t, 10*time.Second, func() bool { _, ok := s.rig.attachedAs(name); return ok })
	sr := &streamRoom{room: room, dir: room.RemoteDir, seen: seen}
	s.rooms[name] = sr
	return sr
}

// set writes the room's switch the way the room's CLI does.
func (s *streamRig) set(name string, on bool, from ...string) {
	s.t.Helper()
	st := remoteaccess.State{Version: remoteaccess.Version, On: on, Port: s.sshd, From: from}
	if err := remoteaccess.Save(s.rooms[name].dir, st); err != nil {
		s.t.Fatal(err)
	}
}

// heard waits for the hub to hold what the room said about its switch.
func (s *streamRig) heard(name string, on bool) {
	s.t.Helper()
	waitFor(s.t, 10*time.Second, func() bool {
		a, ok := s.rig.attachedAs(name)
		return ok && a.RemoteAccess != nil && a.RemoteAccess.On == on
	})
}

// door is the hub machine's operator asking for a stream.
func (s *streamRig) door(room string, headers ...string) (net.Conn, *bufio.Reader, *http.Response) {
	s.t.Helper()
	addr := strings.TrimPrefix(s.front.URL, "http://")
	c, err := net.Dial("tcp", addr)
	if err != nil {
		s.t.Fatal(err)
	}
	s.t.Cleanup(func() { c.Close() })
	req := fmt.Sprintf("POST /_hub/ssh/%s HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: %s\r\nX-Atrium-Claim: claude@sg4\r\n",
		room, addr, StreamUpgrade)
	for _, h := range headers {
		req += h + "\r\n"
	}
	_, _ = io.WriteString(c, req+"\r\n")
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodPost})
	if err != nil {
		s.t.Fatal(err)
	}
	return c, br, resp
}

func streamBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func readBanner(t *testing.T, br *bufio.Reader, c net.Conn) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	line, err := br.ReadString('\n')
	if err != nil || line != "SSH-2.0-fake\r\n" {
		t.Fatalf("the first bytes were %q, %v, and ssh expects sshd's banner", line, err)
	}
}

// THE WHOLE FEATURE: the hub machine's operator reaches sg3's sshd through the hub, sees the banner first, echoes
// bytes, half closes, and both ends audit it without a byte of content.
func TestAnSSHStreamReachesTheRoomsSSHDThroughTheHub(t *testing.T) {
	lightSlot(t)
	s := newStreamRig(t, 100*time.Millisecond, "sg3")
	s.set("sg3", true, "hub")
	s.heard("sg3", true)

	c, br, resp := s.door("sg3")
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status %d: %s", resp.StatusCode, streamBody(t, resp))
	}
	readBanner(t, br, c)
	if _, err := io.WriteString(c, "secret bytes"); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len("secret bytes"))
	if _, err := io.ReadFull(br, got); err != nil || string(got) != "secret bytes" {
		t.Fatalf("echo %q, %v", got, err)
	}
	// ssh's own shutdown: finish writing and still read to the end.
	c.(*net.TCPConn).CloseWrite()
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	if rest, err := io.ReadAll(br); err != nil {
		t.Fatalf("after the half close: %q, %v", rest, err)
	}

	waitFor(t, 10*time.Second, func() bool { return s.hasLine("ssh-close", "ended by") })
	if !s.hasLine("ssh-open", "hub -> sg3") || !s.hasLine("ssh-open", "claims claude@sg4") {
		t.Fatalf("the open was not recorded as the doc says: %v", s.lines())
	}
	for _, l := range s.lines() {
		if strings.Contains(l, "secret bytes") {
			t.Fatalf("an audit line carries content: %q", l)
		}
	}
	// The room's own record, joined to the hub's by the stream id.
	waitFor(t, 10*time.Second, func() bool {
		for _, l := range remoteaccess.RecentLog(s.rooms["sg3"].dir, 10) {
			if l.Event == "close" && l.Caller == "hub" && l.BytesIn == int64(len("secret bytes")) {
				return true
			}
		}
		return false
	})
}

// ANOTHER ROOM, by its certificate. The caller is the name the certificate carries.
func TestAnotherRoomOpensAStreamAndIsNamedByItsCertificate(t *testing.T) {
	lightSlot(t)
	s := newStreamRig(t, 100*time.Millisecond, "sg3", "m1mini")
	s.set("sg3", true, "m1mini")
	s.heard("sg3", true)

	conn, err := s.rooms["m1mini"].room.OpenStream(context.Background(), "sg3", "claude@m1mini")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	br := bufio.NewReader(conn)
	readBanner(t, br, conn)
	if !s.hasLine("ssh-open", "m1mini -> sg3") {
		t.Fatalf("the caller was not the room's name: %v", s.lines())
	}

	// The hub door is not on that list.
	_, _, resp := s.door("sg3")
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(streamBody(t, resp), "sg3 does not offer ssh to hub") {
		t.Fatalf("the hub got through a from list that does not name it: %d", resp.StatusCode)
	}
	if !s.hasLine("ssh-refused", "sg3 does not offer ssh to hub") {
		t.Fatalf("the refusal was not audited: %v", s.lines())
	}
}

// OFF IS OFF, said in the operator's words, and costs the room nothing.
func TestAStreamToARoomWithRemoteAccessOffIsRefusedInWords(t *testing.T) {
	lightSlot(t)
	s := newStreamRig(t, 100*time.Millisecond, "sg3", "m1mini")
	s.heard("sg3", false)
	_, _, resp := s.door("sg3")
	body := streamBody(t, resp)
	if resp.StatusCode != http.StatusForbidden ||
		!strings.Contains(body, "sg3 has remote access off. turn it on at that machine: atrium room remote-access on") {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	_, err := s.rooms["m1mini"].room.OpenStream(context.Background(), "sg3", "")
	if err == nil || !strings.Contains(err.Error(), "remote access off") {
		t.Fatalf("a room was told %v", err)
	}
}

// THE ROOM'S ANSWER IS THE ONE THAT COUNTS. A hub that still believes the switch is on, because the room has not said
// otherwise yet, can only refuse early. The room checks its own file at the moment of the request.
func TestTheRoomChecksItsOwnFileWhateverTheHubBelieves(t *testing.T) {
	lightSlot(t)
	// The watcher ticks every three seconds, so the hub keeps believing it is on for a moment after the file changes.
	s := newStreamRig(t, 3*time.Second, "sg3")
	s.set("sg3", true, "hub")
	s.heard("sg3", true)
	s.set("sg3", false, "hub")

	_, _, resp := s.door("sg3")
	body := streamBody(t, resp)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(body, "remote access off") {
		t.Fatalf("the room granted what its file no longer allows: %d %s", resp.StatusCode, body)
	}
	if !s.hasLine("ssh-refused", "remote access off") {
		t.Fatalf("not audited: %v", s.lines())
	}
	var refused bool
	for _, l := range remoteaccess.RecentLog(s.rooms["sg3"].dir, 10) {
		if l.Event == "refused" {
			refused = true
		}
	}
	if !refused {
		t.Fatal("the room did not record its own refusal")
	}
}

// TURNING IT OFF ends every open stream within Timings.Access.
func TestTurningRemoteAccessOffEndsAnOpenStream(t *testing.T) {
	lightSlot(t)
	s := newStreamRig(t, 100*time.Millisecond, "sg3")
	s.set("sg3", true, "hub")
	s.heard("sg3", true)

	c, br, resp := s.door("sg3")
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status %d: %s", resp.StatusCode, streamBody(t, resp))
	}
	readBanner(t, br, c)
	s.set("sg3", false, "hub")

	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadAll(br); err != nil {
		t.Fatalf("the stream was still open five seconds after remote access went off: %v", err)
	}
	// A real ssh closes when it sees the end. The hub then audits the target as the one that ended it.
	c.Close()
	// The room knows why it ended it.
	waitFor(t, 10*time.Second, func() bool {
		for _, l := range remoteaccess.RecentLog(s.rooms["sg3"].dir, 10) {
			if l.Event == "close" && l.EndedBy == "remote access being turned off" {
				return true
			}
		}
		return false
	})
	waitFor(t, 10*time.Second, func() bool { return s.hasLine("ssh-close", "ended by the target") })
	s.heard("sg3", false)
}

// A from list that drops a caller ends that caller's streams and leaves the rest.
func TestADroppedCallerLosesItsStreamsAndOthersKeepThem(t *testing.T) {
	lightSlot(t)
	s := newStreamRig(t, 100*time.Millisecond, "sg3", "m1mini")
	s.set("sg3", true, "hub", "m1mini")
	s.heard("sg3", true)

	hc, hbr, resp := s.door("sg3")
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status %d", resp.StatusCode)
	}
	readBanner(t, hbr, hc)
	mc, err := s.rooms["m1mini"].room.OpenStream(context.Background(), "sg3", "")
	if err != nil {
		t.Fatal(err)
	}
	defer mc.Close()
	mbr := bufio.NewReader(mc)
	readBanner(t, mbr, mc)

	s.set("sg3", true, "hub")
	_ = mc.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadAll(mbr); err != nil {
		t.Fatalf("m1mini's stream outlived its place on the list: %v", err)
	}
	// The hub's stream still echoes.
	if _, err := io.WriteString(hc, "x"); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 1)
	_ = hc.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(hbr, b); err != nil {
		t.Fatalf("the hub's stream was ended with it: %v", err)
	}
}

// SIXTEEN OPEN STREAMS PER TARGET ROOM, and the seventeenth is refused in words.
func TestTheSeventeenthStreamIsRefused(t *testing.T) {
	lightSlot(t)
	s := newStreamRig(t, 100*time.Millisecond, "sg3")
	s.set("sg3", true, "hub")
	s.heard("sg3", true)

	for i := 0; i < MaxStreams; i++ {
		c, br, resp := s.door("sg3")
		if resp.StatusCode != http.StatusSwitchingProtocols {
			t.Fatalf("stream %d: %d %s", i+1, resp.StatusCode, streamBody(t, resp))
		}
		readBanner(t, br, c)
	}
	_, _, resp := s.door("sg3")
	if body := streamBody(t, resp); resp.StatusCode != http.StatusTooManyRequests ||
		!strings.Contains(body, "sg3 already has 16 open ssh streams") {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
}

// sshd not answering is said as such, with the port.
func TestASSHDThatDoesNotAnswerIsSaidInWords(t *testing.T) {
	lightSlot(t)
	s := newStreamRig(t, 100*time.Millisecond, "sg3")
	// A port that was just free.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	st := remoteaccess.State{Version: remoteaccess.Version, On: true, Port: dead, From: []string{"hub"}}
	if err := remoteaccess.Save(s.rooms["sg3"].dir, st); err != nil {
		t.Fatal(err)
	}
	s.heard("sg3", true)
	_, _, resp := s.door("sg3")
	body := streamBody(t, resp)
	if resp.StatusCode != http.StatusBadGateway ||
		!strings.Contains(body, fmt.Sprintf("sg3 could not reach its sshd on 127.0.0.1:%d", dead)) {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
}

// THE PROXY GUARD. The room's route is not reachable from a board request, a page cannot open a stream, and a
// forwarding header takes the door away. In every case the room sees nothing.
func TestTheBoardCannotReachTheStreamRoute(t *testing.T) {
	lightSlot(t)
	s := newStreamRig(t, 100*time.Millisecond, "sg3")
	s.set("sg3", true, "*")
	s.heard("sg3", true)
	seen := s.rooms["sg3"].seen
	base := s.front.URL

	do := func(method, path string, h map[string]string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(method, base+path, nil)
		for k, v := range h {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	// A page open on the hub machine.
	for _, h := range []map[string]string{{"Origin": "http://127.0.0.1:7778"}, {"Sec-Fetch-Site": "same-origin"}} {
		h["Connection"] = "Upgrade"
		h["Upgrade"] = StreamUpgrade
		resp := do("POST", "/_hub/ssh/sg3", h)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("a page with %v got %d", h, resp.StatusCode)
		}
	}
	// A plain POST, as fetch would send, is not an upgrade.
	if resp := do("POST", "/_hub/ssh/sg3", nil); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a POST that was not an upgrade got %d", resp.StatusCode)
	}
	// A forwarding header says this did not start on the hub machine.
	resp := do("POST", "/_hub/ssh/sg3", map[string]string{"X-Forwarded-For": "10.0.0.9",
		"Connection": "Upgrade", "Upgrade": StreamUpgrade})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a forwarded request got %d", resp.StatusCode)
	}
	// The room's route, by every spelling a board could try.
	for _, p := range []string{"/_link/ssh", "/_link/", "/_link", "/_link/anything"} {
		resp := do("GET", p, map[string]string{"X-Atrium-Caller": "hub", "X-Atrium-Stream": "st-1",
			"Connection": "Upgrade", "Upgrade": StreamUpgrade})
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s got %d", p, resp.StatusCode)
		}
	}
	if n := seen.Load(); n != 0 {
		t.Fatalf("the room's handler was asked %d times by requests that must never reach it", n)
	}
	for _, l := range s.lines() {
		if strings.HasPrefix(l, "ssh-open") {
			t.Fatalf("a stream was opened: %s", l)
		}
	}

	// A board request that is forwarded carries neither header, whatever it sent.
	resp = do("GET", "/v1/anything", map[string]string{"X-Atrium-Caller": "hub", "X-Atrium-Stream": "st-1"})
	defer resp.Body.Close()
	if got := resp.Header.Get("X-Caller-Seen"); got != "" {
		t.Fatalf("the room saw X-Atrium-Caller %q from a board request", got)
	}
	if seen.Load() != 1 {
		t.Fatal("the forwarded request did not reach the room")
	}
}

// f-ssh-wslrelay-loopback: the room dials the address its switch names, not always 127.0.0.1. sshd answers on a second
// loopback address and nothing answers on 127.0.0.1 at that port, so only the right dial reaches the banner.
func TestTheRoomDialsTheAddrItsSwitchNames(t *testing.T) {
	lightSlot(t)
	ln, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Skipf("no second loopback address here: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.WriteString(c, "SSH-2.0-fake\r\n"); _, _ = io.Copy(io.Discard, c) }()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	if l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
		l.Close()
	} else {
		t.Skip("the port is taken on 127.0.0.1 too")
	}

	s := newStreamRig(t, 100*time.Millisecond, "sg3")
	dir := s.rooms["sg3"].dir
	save := func(addr string) {
		st := remoteaccess.State{Version: remoteaccess.Version, On: true, Addr: addr, Port: port, From: []string{"hub"}}
		if err := remoteaccess.Save(dir, st); err != nil {
			t.Fatal(err)
		}
	}
	save("")
	s.heard("sg3", true)
	_, _, resp := s.door("sg3")
	if body := streamBody(t, resp); resp.StatusCode != http.StatusBadGateway ||
		!strings.Contains(body, fmt.Sprintf("on 127.0.0.1:%d", port)) {
		t.Fatalf("with no addr the room dials 127.0.0.1: %d %s", resp.StatusCode, body)
	}

	save("127.0.0.2")
	c, br, resp := s.door("sg3")
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status %d: %s", resp.StatusCode, streamBody(t, resp))
	}
	readBanner(t, br, c)
}

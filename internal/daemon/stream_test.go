package daemon

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeStreamer struct {
	err    error
	target string
	claim  string
	up     net.Conn
}

func (f *fakeStreamer) OpenStream(_ context.Context, target, claim string) (net.Conn, error) {
	f.target, f.claim = target, claim
	if f.err != nil {
		return nil, f.err
	}
	return f.up, nil
}
func (f *fakeStreamer) OpenStreams() int { return 3 }

func streamReq(t *testing.T, srv *httptest.Server, room string) (net.Conn, *bufio.Reader, *http.Response) {
	t.Helper()
	c, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	fmt.Fprintf(c, "POST /stream HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: Upgrade\r\nUpgrade: atrium-stream\r\n"+
		"X-Atrium-Room: %s\r\nX-Atrium-Claim: claude@sg4\r\n\r\n", room)
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, &http.Request{Method: "POST"})
	if err != nil {
		t.Fatal(err)
	}
	return c, br, resp
}

// The room's door joins the caller to the hub's stream, byte for byte and with the half close passed through.
func TestTheRoomDoorJoinsTheCallerToTheHubStream(t *testing.T) {
	tcpA, tcpB := tcpPairT(t)
	// The hub's end echoes after the caller finishes writing, as sshd does.
	go func() {
		defer tcpB.Close()
		io.WriteString(tcpB, "SSH-2.0-fake\r\n")
		b, _ := io.ReadAll(tcpB)
		tcpB.Write(append([]byte("echo:"), b...))
	}()
	f := &fakeStreamer{up: tcpA}
	d := &Daemon{}
	d.initServices()
	d.SetStreamer(f)
	srv := httptest.NewServer(http.HandlerFunc(d.HandleStream))
	defer srv.Close()

	c, br, resp := streamReq(t, srv, "sg3")
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if f.target != "sg3" || f.claim != "claude@sg4" {
		t.Fatalf("asked for %q claiming %q", f.target, f.claim)
	}
	line, _ := br.ReadString('\n')
	if line != "SSH-2.0-fake\r\n" {
		t.Fatalf("first bytes %q", line)
	}
	io.WriteString(c, "hi")
	c.(*net.TCPConn).CloseWrite()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	rest, err := io.ReadAll(br)
	if err != nil || string(rest) != "echo:hi" {
		t.Fatalf("after the half close %q, %v", rest, err)
	}
}

func tcpPairT(t *testing.T) (a, b net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan net.Conn, 1)
	go func() { c, _ := ln.Accept(); got <- c }()
	a, err = net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return a, <-got
}

func TestTheRoomDoorSaysTheRefusalAsASentence(t *testing.T) {
	d := &Daemon{}
	d.initServices()
	d.SetStreamer(&fakeStreamer{err: errors.New("sg3 does not offer ssh to m1mini")})
	srv := httptest.NewServer(http.HandlerFunc(d.HandleStream))
	defer srv.Close()
	_, _, resp := streamReq(t, srv, "sg3")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(string(body), "sg3 does not offer ssh to m1mini") {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
}

func TestTheRoomDoorWithNoLinkSaysItIsNotAttached(t *testing.T) {
	d := &Daemon{}
	d.initServices()
	srv := httptest.NewServer(http.HandlerFunc(d.HandleStream))
	defer srv.Close()
	_, _, resp := streamReq(t, srv, "sg3")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(body), "not attached to a hub") {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
}

func TestStreamStatusCountsTheOpenStreams(t *testing.T) {
	d := &Daemon{}
	d.initServices()
	d.SetStreamer(&fakeStreamer{})
	rec := httptest.NewRecorder()
	d.HandleStreamStatus(rec, httptest.NewRequest("GET", "/stream/status", nil))
	if strings.TrimSpace(rec.Body.String()) != `{"open":3}` {
		t.Fatalf("%s", rec.Body.String())
	}
}

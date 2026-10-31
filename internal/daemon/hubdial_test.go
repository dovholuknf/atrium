package daemon

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeHubDialer struct {
	err   error
	up    net.Conn
	calls int
}

func (f *fakeHubDialer) Dial(context.Context) (net.Conn, error) {
	f.calls++
	return f.up, f.err
}

func hubDialReq(t *testing.T, srv *httptest.Server) (net.Conn, *http.Response) {
	t.Helper()
	c, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	io.WriteString(c, "POST /hubdial HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: Upgrade\r\nUpgrade: atrium-hubdial\r\n\r\n")
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, &http.Request{Method: "POST"})
	if err != nil {
		t.Fatal(err)
	}
	return &readerConn{Conn: c, br: br}, resp
}

func TestHubDialRelaysTheRoomsConnection(t *testing.T) {
	tcpA, tcpB := tcpPairT(t)
	go func() {
		defer tcpB.Close()
		b, _ := io.ReadAll(tcpB)
		tcpB.Write(append([]byte("echo:"), b...))
	}()
	f := &fakeHubDialer{up: tcpA}
	d := &Daemon{}
	d.initServices()
	d.SetHubDialer(f)
	srv := httptest.NewServer(http.HandlerFunc(d.handleHubDial))
	defer srv.Close()

	c, resp := hubDialReq(t, srv)
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status %d", resp.StatusCode)
	}
	io.WriteString(c, "hi")
	c.(*readerConn).Conn.(*net.TCPConn).CloseWrite()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	got, err := io.ReadAll(c)
	if err != nil || string(got) != "echo:hi" {
		t.Fatalf("relayed %q, %v", got, err)
	}
	if f.calls != 1 {
		t.Fatalf("dialled %d times", f.calls)
	}
}

func TestHubDialRefusesAPeerThatIsNotLoopback(t *testing.T) {
	f := &fakeHubDialer{}
	d := &Daemon{}
	d.initServices()
	d.SetHubDialer(f)
	req := httptest.NewRequest(http.MethodPost, "/hubdial", nil)
	req.RemoteAddr = "10.1.2.3:5555"
	req.Header.Set("Upgrade", "atrium-hubdial")
	req.Header.Set("Connection", "Upgrade")
	rec := httptest.NewRecorder()
	d.handleHubDial(rec, req)
	if rec.Code != http.StatusForbidden || f.calls != 0 {
		t.Fatalf("status %d, dials %d", rec.Code, f.calls)
	}
}

func TestHubDialSaysSoWhenTheRoomHasNoHub(t *testing.T) {
	d := &Daemon{}
	d.initServices()
	req := httptest.NewRequest(http.MethodPost, "/hubdial", nil)
	req.RemoteAddr = "127.0.0.1:5555"
	req.Header.Set("Upgrade", "atrium-hubdial")
	req.Header.Set("Connection", "Upgrade")
	rec := httptest.NewRecorder()
	d.handleHubDial(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestHubDialAnswersADialFailureWithItsSentence(t *testing.T) {
	d := &Daemon{}
	d.initServices()
	d.SetHubDialer(&fakeHubDialer{err: errors.New("no route to the hub")})
	req := httptest.NewRequest(http.MethodPost, "/hubdial", nil)
	req.RemoteAddr = "127.0.0.1:5555"
	req.Header.Set("Upgrade", "atrium-hubdial")
	req.Header.Set("Connection", "Upgrade")
	rec := httptest.NewRecorder()
	d.handleHubDial(rec, req)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "no route to the hub") {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

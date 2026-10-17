package cli

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/zitictx"
)

func TestRoomLinkForWithNoRoomRunningErrorsAndBuildsNoContext(t *testing.T) {
	d, err := newLocalRoomDialer(func() (daemon.Location, bool) { return daemon.Location{}, false })
	if err == nil || d != nil {
		t.Fatalf("got %v, %v, want an error", d, err)
	}
	if zitictx.Live() != 0 {
		t.Fatalf("%d ziti contexts live after a command with no room", zitictx.Live())
	}
}

func TestLocalRoomDialerCarriesTheRoomsConnectionAndTouchesNoContext(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		br := bufio.NewReader(c)
		req, err := http.ReadRequest(br)
		if err != nil || req.URL.Path != "/hubdial" || req.Header.Get("Upgrade") != "atrium-hubdial" {
			return
		}
		io.WriteString(c, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: atrium-hubdial\r\n\r\nhello")
	}()
	d, err := newLocalRoomDialer(func() (daemon.Location, bool) {
		return daemon.Location{Agent: "http://" + ln.Addr().String()}, true
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := d.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	buf := make([]byte, 5)
	if _, err := io.ReadFull(c, buf); err != nil || string(buf) != "hello" {
		t.Fatalf("read %q, %v", buf, err)
	}
	if zitictx.Live() != 0 {
		t.Fatalf("%d ziti contexts live", zitictx.Live())
	}
}

func TestLocalRoomDialerRelaysTheRoomsSentence(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = http.ReadRequest(bufio.NewReader(c))
		body := `{"error":"this room is not attached to a hub right now"}`
		io.WriteString(c, "HTTP/1.1 503 Service Unavailable\r\nContent-Length: "+
			string(rune('0'+len(body)/10))+string(rune('0'+len(body)%10))+"\r\n\r\n"+body)
	}()
	d := &localRoomDialer{agent: "http://" + ln.Addr().String()}
	_, err = d.Dial(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not attached to a hub") {
		t.Fatalf("got %v", err)
	}
}

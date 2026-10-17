package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon"
)

// The way a command on this machine reaches the hub: through the room that is running here.
//
// A command that built its own ladder would load the ziti identity and start a context per run, and each context keeps
// its refresh loop until the process exits. The room holds the one context, so the command asks the room to dial.

const hubDialUpgrade = "atrium-hubdial"

var errNoLocalRoom = errors.New("no room is running on this machine. start it with `atrium room`, which holds the link to the hub")

// localRoomDialer dials the hub through the room's agent listener.
type localRoomDialer struct{ agent string }

func newLocalRoomDialer(location func() (daemon.Location, bool)) (*localRoomDialer, error) {
	loc, ok := location()
	if !ok || strings.TrimSpace(loc.Agent) == "" {
		return nil, errNoLocalRoom
	}
	return &localRoomDialer{agent: loc.Agent}, nil
}

func (l *localRoomDialer) Describe() string { return "this machine's room" }

func (l *localRoomDialer) Dial(ctx context.Context) (net.Conn, error) {
	u, err := url.Parse(l.agent)
	if err != nil || u.Host == "" {
		return nil, errNoLocalRoom
	}
	conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, errNoLocalRoom
	}
	deadline := time.Now().Add(60 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	req := "POST /hubdial HTTP/1.1\r\nHost: " + u.Host + "\r\nConnection: Upgrade\r\nUpgrade: " + hubDialUpgrade + "\r\n\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		conn.Close()
		return nil, errNoLocalRoom
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodPost})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("the room did not answer the hub dial: %w", err)
	}
	if resp.StatusCode == http.StatusSwitchingProtocols {
		_ = conn.SetDeadline(time.Time{})
		return &bufferedConn{Conn: conn, br: br}, nil
	}
	defer conn.Close()
	var body struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&body)
	if body.Error == "" {
		return nil, errors.New("the room on this machine does not carry hub dials yet. update and restart it")
	}
	return nil, errors.New(body.Error)
}

type bufferedConn struct {
	net.Conn
	br *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.br.Read(p) }

func (c *bufferedConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}

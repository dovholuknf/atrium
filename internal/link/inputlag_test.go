package link

import (
	"net"
	"testing"
)

// Off is the default, and off must hand back the very connection it was given.
func TestLagWrapIsIdentityWhenOff(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if got := lagWrap(a, "alpha"); got != a {
		t.Fatalf("wrapped a connection with the logging off: %T", got)
	}
}

// Only an upgraded connection is timed, so a plain response must not arm it
// and a 101 must.
func TestLagConnArmsOnlyOnUpgrade(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	c := &lagConn{Conn: a, room: "alpha"}
	buf := make([]byte, 64)

	go func() { _, _ = b.Write([]byte("HTTP/1.1 200 OK\r\n\r\n")) }()
	if _, err := c.Read(buf); err != nil {
		t.Fatal(err)
	}
	if c.ws.Load() {
		t.Fatal("a 200 armed the timing")
	}

	go func() { _, _ = b.Write([]byte("HTTP/1.1 101 Switching Protocols\r\n\r\n")) }()
	if _, err := c.Read(buf); err != nil {
		t.Fatal(err)
	}
	if !c.ws.Load() {
		t.Fatal("a 101 did not arm the timing")
	}

	go func() { _, _ = b.Read(buf) }()
	if _, err := c.Write([]byte("k")); err != nil {
		t.Fatal(err)
	}
	if c.up.Load() == 0 {
		t.Fatal("a write after the upgrade did not start the echo clock")
	}
	go func() { _, _ = b.Write([]byte("k")) }()
	if _, err := c.Read(buf); err != nil {
		t.Fatal(err)
	}
	if c.up.Load() != 0 {
		t.Fatal("the echo did not close the clock")
	}
}

package link

import (
	"net"
	"testing"

	"github.com/dovholuknf/atrium/internal/inputlag"
)

// lagOnFor switches the logging on for one test, as the gear does.
func lagOnFor(t *testing.T) {
	t.Helper()
	if !inputlag.SetLive(true) {
		t.Skip(inputlag.Env + " is set, so the setting cannot switch the logging")
	}
	t.Cleanup(func() { inputlag.SetLive(false) })
}

// Off, an upgraded connection starts no clock, so nothing is timed and nothing
// is left behind to close as a stale hop once the logging comes on.
func TestLagConnTimesNothingWhenOff(t *testing.T) {
	inputlag.SetLive(false)
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	c := lagWrap(a, "alpha").(*lagConn)
	c.ws.Store(true)
	go func() { _, _ = b.Read(make([]byte, 8)) }()
	if _, err := c.Write([]byte("k")); err != nil {
		t.Fatal(err)
	}
	if c.up.Load() != 0 {
		t.Fatal("a write with the logging off started the echo clock")
	}
}

// Only an upgraded connection is timed, so a plain response must not arm it
// and a 101 must.
func TestLagConnArmsOnlyOnUpgrade(t *testing.T) {
	lagOnFor(t)
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

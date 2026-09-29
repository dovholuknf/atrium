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
	if !inputlag.SetLive(false) {
		t.Skip(inputlag.Env + " is set, so the setting cannot switch the logging off")
	}
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

var (
	pongUp   = []byte{0x8A, 0x80, 1, 2, 3, 4}          // masked empty pong, as the browser sends
	pongData = []byte{0x8A, 0x82, 1, 2, 3, 4, 9, 9}    // masked pong with a payload
	keyUp    = []byte{0x81, 0x81, 1, 2, 3, 4, 'k' ^ 1} // masked text frame, one key
	pingBack = []byte{0x89, 0x00}                      // the room's idle ping
)

func TestOnlyControl(t *testing.T) {
	cases := []struct {
		name string
		b    []byte
		want bool
	}{
		{"pong", pongUp, true},
		{"pong with payload", pongData, true},
		{"ping back", pingBack, true},
		{"close", []byte{0x88, 0x00}, true},
		{"empty", nil, true},
		{"data", keyUp, false},
		{"pong then data", append(append([]byte{}, pongUp...), keyUp...), false},
		{"data then pong", append(append([]byte{}, keyUp...), pongUp...), false},
		{"two pongs", append(append([]byte{}, pongUp...), pongUp...), true},
		{"partial pong", pongUp[:4], false},
		{"one byte", []byte{0x8A}, false},
	}
	for _, tc := range cases {
		if got := onlyControl(tc.b); got != tc.want {
			t.Errorf("%s: onlyControl = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The idle ping and pong: a pong up starts no clock, so the ping 45s later
// closes nothing and logs nothing. A key up still starts one, a ping back does
// not close it, and the first real bytes back do.
func TestLagConnIgnoresIdlePingPong(t *testing.T) {
	lagOnFor(t)
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	c := &lagConn{Conn: a, room: "alpha"}
	c.ws.Store(true)
	buf := make([]byte, 64)

	go func() { _, _ = b.Read(buf) }()
	if _, err := c.Write(pongUp); err != nil {
		t.Fatal(err)
	}
	if c.up.Load() != 0 {
		t.Fatal("a pong up started the echo clock")
	}

	go func() { _, _ = b.Read(buf) }()
	if _, err := c.Write(keyUp); err != nil {
		t.Fatal(err)
	}
	if c.up.Load() == 0 {
		t.Fatal("a data frame up did not start the echo clock")
	}

	go func() { _, _ = b.Write(pingBack) }()
	if _, err := c.Read(buf); err != nil {
		t.Fatal(err)
	}
	if c.up.Load() == 0 {
		t.Fatal("a ping back closed a clock a keystroke started")
	}

	go func() { _, _ = b.Write([]byte{0x82, 0x01, 'k'}) }()
	if _, err := c.Read(buf); err != nil {
		t.Fatal(err)
	}
	if c.up.Load() != 0 {
		t.Fatal("the echo did not close the clock")
	}
}

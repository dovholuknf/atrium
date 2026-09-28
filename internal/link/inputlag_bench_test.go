package link

import (
	"io"
	"log"
	"net"
	"testing"

	"github.com/dovholuknf/atrium/internal/inputlag"
)

// echoConn answers every read at once and takes every write, so a benchmark
// measures lagConn and not a socket.
type echoConn struct{ net.Conn }

func (echoConn) Write(b []byte) (int, error) { return len(b), nil }
func (echoConn) Read(b []byte) (int, error)  { b[0] = 'a'; return 1, nil }

// benchHubKeystroke is the hub's share of one keystroke on an upgraded attach:
// the frame written toward the room and the first bytes read back.
func benchHubKeystroke(b *testing.B, on bool) {
	if inputlag.Pinned() {
		b.Skip(inputlag.Env + " is set, so the switch cannot be flipped. Unset it and run again")
	}
	// The switch logs a line, which would land in the middle of the results.
	out := log.Writer()
	log.SetOutput(io.Discard)
	b.Cleanup(func() { log.SetOutput(out) })
	inputlag.SetLive(on)
	b.Cleanup(func() { inputlag.SetLive(false) })
	c := lagWrap(echoConn{}, "bench").(*lagConn)
	c.ws.Store(true)
	key, buf := []byte("a"), make([]byte, 64)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = c.Write(key)
		_, _ = c.Read(buf)
	}
}

func BenchmarkHubKeystrokeLagOff(b *testing.B) { benchHubKeystroke(b, false) }
func BenchmarkHubKeystrokeLagOn(b *testing.B)  { benchHubKeystroke(b, true) }

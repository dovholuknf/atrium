//go:build integration

package daemon

import (
	"net"
	"strconv"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/cardproc"
)

// previewBlockPort is a free 127.0.0.1 address inside the previews' block, other than skip.
func previewBlockPort(t *testing.T, skip ...string) string {
	t.Helper()
	for p := cardproc.Previews.Lo; p <= cardproc.Previews.Hi; p++ {
		addr := "127.0.0.1:" + strconv.Itoa(p)
		taken := false
		for _, s := range skip {
			taken = taken || s == addr
		}
		if taken {
			continue
		}
		if ln, err := net.Listen("tcp", addr); err == nil {
			ln.Close()
			return addr
		}
	}
	t.Skip("no free port in the previews' block")
	return ""
}

func portOf(t *testing.T, addr string) int {
	t.Helper()
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(p)
	return n
}

// A ROOM WHOSE OWN LISTENERS SIT IN THE PREVIEWS' BLOCK never lets the hub forward a browser to them, and still
// forwards to another port in the block.
func TestPreviewNeverForwardsToTheRoomsOwnListeners(t *testing.T) {
	d, _, cancel, errCh := startDaemonWith(t, func(o *Options) {
		o.AgentAddr = previewBlockPort(t)
		o.HumanAddr = previewBlockPort(t, o.AgentAddr)
	})
	defer func() { cancel(); <-errCh }()
	for name, addr := range map[string]string{"agent": d.opts.AgentAddr, "board": d.opts.HumanAddr} {
		if err := d.PreviewPort(portOf(t, addr)); err == nil {
			t.Errorf("the hub may forward a browser to this room's own %s listener on %s", name, addr)
		}
	}
	other := portOf(t, previewBlockPort(t, d.opts.AgentAddr, d.opts.HumanAddr))
	if err := d.PreviewPort(other); err != nil {
		t.Errorf("port %d in the block was refused: %v", other, err)
	}
}

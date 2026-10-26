//go:build integration

package conn

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/testwait"
)

type plain struct{ addr string }

func (p plain) Dial(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "tcp", p.addr)
}

func (p plain) Describe() string { return p.addr }

// lightSlots bounds how many of the tests that wait on a real link and hub go at once, so a loaded machine does not
// starve the heartbeats they run at a second's silence.
var lightSlots = make(chan struct{}, 4)

// lightSlot lets the calling test run beside the others, once, and holds one of the slots until it ends.
func lightSlot(t *testing.T) {
	t.Helper()
	if _, again := lighted.LoadOrStore(t, true); again {
		return
	}
	t.Cleanup(func() { lighted.Delete(t) })
	parallel(t)
	lightSlots <- struct{}{}
	t.Cleanup(func() { <-lightSlots })
}

var lighted, paralleled sync.Map

// parallel is t.Parallel for a test that may ask for it more than once, through more than one helper.
func parallel(t *testing.T) {
	t.Helper()
	if _, again := paralleled.LoadOrStore(t, true); again {
		return
	}
	t.Cleanup(func() { paralleled.Delete(t) })
	t.Parallel()
}

func waitFor(t *testing.T, limit time.Duration, ok func() bool) {
	t.Helper()
	testwait.Eventually(t, limit, "the condition", ok)
}

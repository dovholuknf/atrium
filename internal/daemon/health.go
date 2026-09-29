package daemon

import (
	"context"
	"fmt"
	"time"
)

// The `health` event: what `/v1/health` says, pushed when it changes, so the
// board can stop polling it (r-017, f-008).
//
// Two things in it change, and each has one place it changes. `halted` flips
// once, in `onHalt`, and a storage halt keeps the human listener and the stream
// up, so the board would otherwise never learn of it without asking. `settling`
// is worked out from a clock and a pending set (settling.go), so nothing fires
// when it ends: `watchSettle` watches for that edge and says so once.

// healthHalted is where the halt is read. A variable so a test can say halted
// without breaking a real database.
var healthHalted = func(d *Daemon) (bool, error) { return d.st.Halted() }

// settleWatchEvery is how often the settle watcher looks for the window closing.
const settleWatchEvery = 500 * time.Millisecond

// healthBody is the event's payload: the same `halted`, `cause` and `settling`
// `/v1/health` answers, without the build id, which does not change while this
// process runs.
func (d *Daemon) healthBody() map[string]any {
	halted, cause := healthHalted(d)
	body := map[string]any{"halted": halted, "settling": d.Settling()}
	if halted {
		body["cause"] = fmt.Sprint(cause)
	}
	return body
}

// publishHealth pushes the current health to every open stream.
func (d *Daemon) publishHealth() { d.ap.Broadcast("health", d.healthBody()) }

// watchSettle publishes `health` as the settle window opens, and again the
// moment it closes, then stops. Started once, right after `settle.begin`.
func (d *Daemon) watchSettle(ctx context.Context) {
	d.publishHealth()
	t := time.NewTicker(settleWatchEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if !d.Settling() {
			d.publishHealth()
			return
		}
	}
}

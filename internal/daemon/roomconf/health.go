package roomconf

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
var HealthHalted = func(d HaltReader) (bool, error) { return d.Halted() }

// settleWatchEvery is how often the settle watcher looks for the window closing.
const SettleWatchEvery = 500 * time.Millisecond

// healthBody is the event's payload: the same `halted`, `cause` and `settling`
// `/v1/health` answers, without the build id, which does not change while this
// process runs.
func (d *Service) HealthBody() map[string]any {
	halted, cause := d.HealthHalted(d.St)
	body := map[string]any{"halted": halted, "settling": d.Settling()}
	if halted {
		body["cause"] = fmt.Sprint(cause)
	}
	if l := d.LoginHealth(); l != nil {
		body["login_needed"] = l
	}
	if tc := d.ToolchainHealth(); tc != nil {
		body["toolchain_missing"] = tc
	}
	return body
}

// publishHealth pushes the current health to every open stream.
func (d *Service) PublishHealth() { d.Ap.Broadcast("health", d.HealthBody()) }

// watchSettle publishes `health` as the settle window opens, and again the
// moment it closes, then stops. Started once, right after `settle.begin`.
func (d *Service) WatchSettle(ctx context.Context) {
	d.PublishHealth()
	t := time.NewTicker(SettleWatchEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if !d.Settling() {
			d.PublishHealth()
			return
		}
	}
}

// healthHooks are the seams of the health report.
type HealthHooks struct {
	HealthHalted func(h HaltReader) (bool, error)
}

// haltReader is the part of the store healthHalted reads the halt from.
type HaltReader interface {
	Halted() (bool, error)
}

func DefaultHealthHooks() HealthHooks {
	return HealthHooks{HealthHalted: HealthHalted}
}

package daemon

import (
	"context"
	"log"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The reaper answers "is this session still there" without spending a token.
// A card carries the runner's process id, and whether that process exists is a
// question the operating system answers for free. Nothing is sent to the
// runner, nothing wakes its model, and a session that died in the night is
// marked dead rather than sitting in running forever.
//
// Cards with no known pid are left alone. Not knowing is not the same as being
// dead, and guessing would move work you are still doing.

// ReapEvery is how often liveness is checked. The check is a syscall per card,
// so this can be frequent without costing anything meaningful.
const ReapEvery = 20 * time.Second

func (d *Daemon) reapOnce() error {
	tasks, err := d.st.List(store.StatusRunning, store.StatusNeedsInput, store.StatusNeedsPermission)
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if t.PID <= 0 || processAlive(t.PID) {
			continue
		}
		if err := d.st.AppendEvent(t.ID, store.EventExited, map[string]any{
			"pid": t.PID, "by": "reaper", "detected": "process is gone",
		}); err != nil {
			return err
		}
		if err := d.st.SetStatus(t.ID, store.StatusDead); err != nil {
			return err
		}
		log.Printf("[atrium] %s is dead: pid %d is gone", t.DisplayTitle(), t.PID)
		d.publishTask(t.ID)
	}
	return nil
}

func (d *Daemon) reap(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = ReapEvery
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	var lastErr string
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if err := d.reapOnce(); err != nil {
			if msg := err.Error(); msg != lastErr {
				log.Printf("[atrium] liveness check: %v", err)
				lastErr = msg
			}
			continue
		}
		lastErr = ""
	}
}

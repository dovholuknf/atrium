package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/dovholuknf/atrium/internal/roomstats"
)

// The room pushes its own stats: a `room-stats` event on the board's stream
// every ten seconds, and `GET /v1/room/stats` for the first paint. Both come
// from one sampler's snapshot. See internal/roomstats and
// docs/backlog/runtime/r-010.md.
//
// On the BOARD listener only. Neither route is on the agent mux, and a lent
// session's stream never carries it: `guestHandler` refuses `/v1/events` and
// `/v1/room/stats` outright.
//
// BEST EFFORT. A failed sample marks its section stale and the room goes on.

// worktreeVolume is the path whose volume `disk` reports: the first provider
// worktree root, else the directory holding the database.
func (d *Daemon) worktreeVolume() string {
	if ps, err := d.st.Providers(); err == nil {
		for _, p := range ps {
			if p.Worktrees && p.WorktreeRoot != "" {
				return p.WorktreeRoot
			}
		}
	}
	return filepath.Dir(d.opts.DBPath)
}

func (d *Daemon) startRoomStats(ctx context.Context) {
	s := roomstats.New(roomstats.Sources{
		Room:         d.opts.Room,
		Started:      time.Now(),
		Usage:        d.st.UsageBuckets,
		ProcessTimes: roomstats.ProcessTimes,
		Machine:      roomstats.ReadMachine,
		Drift:        &roomstats.Drift{},
		Disk: func() (string, uint64, uint64, error) {
			return roomstats.DiskOf(d.worktreeVolume())
		},
		DBBytes: func() (int64, error) { return roomstats.DBBytes(d.opts.DBPath) },
		Worktrees: func() (int, error) {
			ts, err := d.st.List()
			if err != nil {
				return 0, err
			}
			paths := make([]string, 0, len(ts))
			for _, t := range ts {
				paths = append(paths, t.Worktree)
			}
			return roomstats.CountWorktrees(paths), nil
		},
		Runners: func() ([]roomstats.Runner, error) {
			hs, err := d.st.Harnesses()
			if err != nil {
				return nil, err
			}
			out := make([]roomstats.Runner, 0, len(hs))
			for _, h := range hs {
				out = append(out, roomstats.Runner{Kind: h.ID, Resolves: runnerFound(h) != ""})
			}
			return out, nil
		},
		Publish: func(b []byte) { d.ap.Broadcast("room-stats", json.RawMessage(b)) },
	})
	d.ap.RoomStats = s.Bytes
	stop := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(stop)
	}()
	go s.Run(stop)
}

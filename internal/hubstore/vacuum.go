package hubstore

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"
)

// Handing back the space the room cache leaves behind.
//
// The hub holds little, but `room_card` is rewritten wholesale every time a
// room announces what it is holding, and that churn frees pages inside the file
// that SQLite reuses rather than returns. A fresh database opens in incremental
// auto_vacuum mode (see Open), and PRAGMA incremental_vacuum hands those free
// pages back a bounded batch at a time while the hub stays live. On an older
// database not in that mode the call is a no-op.

// autoVacuumModeIncremental is the value PRAGMA auto_vacuum reports for a
// database in incremental mode. 0 is none, 1 is full, 2 is incremental.
const autoVacuumModeIncremental = 2

// autoVacuumIncremental reads back whether the open database is in incremental
// auto_vacuum mode.
func autoVacuumIncremental(db *sql.DB) (bool, error) {
	var mode int
	if err := db.QueryRow("PRAGMA auto_vacuum").Scan(&mode); err != nil {
		return false, err
	}
	return mode == autoVacuumModeIncremental, nil
}

// VacuumEvery is how often the hub hands freed pages back to disk.
//
// Modest on purpose. The hub is small and this only has to keep pace with the
// cache being rewritten, not with writes, and each tick reclaims a bounded
// batch so it never blocks a request for long.
const VacuumEvery = 15 * time.Minute

// VacuumPages bounds how many free pages one tick hands back. Small on purpose:
// the pragma takes the write lock while it runs, and this trims the file
// gradually rather than in one long reclaim.
const VacuumPages = 256

// VacuumLoop reclaims free pages on a timer until ctx is done. Started in the
// background beside BackUp. A no-op body on a database not in incremental mode.
func (s *Store) VacuumLoop(ctx context.Context) {
	if !s.incrementalVacuum {
		log.Printf("[hub] its store is not in incremental auto_vacuum mode, so freed pages will not be reclaimed")
		return
	}
	t := time.NewTicker(VacuumEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.vacuumOnce(); err != nil {
				log.Printf("[hub] incremental vacuum: %v", err)
			}
		}
	}
}

// vacuumOnce hands one bounded batch of free pages back, through guard like
// every other write here.
func (s *Store) vacuumOnce() error {
	return s.guard(func() error {
		_, err := s.db.Exec(fmt.Sprintf("PRAGMA incremental_vacuum(%d)", VacuumPages))
		return err
	})
}

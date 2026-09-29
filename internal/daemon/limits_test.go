package daemon

import (
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// An unchanged statusline writes nothing, and a changed figure writes a row.
func TestKeepLimitReadingsOnChange(t *testing.T) {
	f := newUsageFix(t)
	d := &Daemon{st: f.st}
	reset := time.Date(2026, 9, 29, 15, 0, 0, 0, time.UTC)
	post := func(pct int) {
		d.keepLimitReadings(f.task.ID, Telemetry{FiveHour: &Limit{Pct: pct, ResetsAt: reset}})
	}
	post(10)
	post(10)
	post(10)
	post(12)
	got, err := f.st.LimitReadings(time.Now().Add(-time.Hour))
	if err != nil || len(got) != 2 || got[0].Pct != 10 || got[1].Pct != 12 || got[1].Kind != store.LimitFiveHour {
		t.Fatalf("readings %+v (%v)", got, err)
	}
}

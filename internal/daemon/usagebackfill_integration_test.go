//go:build integration

package daemon

import (
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A transcript's turns become rows once, a second run adds nothing, a reply a
// row already covers is left alone, and it stops at UsageMaxBack.
func TestBackfillWritesTurnsOnce(t *testing.T) {
	f := newUsageFix(t)
	now := time.Now().UTC().Truncate(time.Second)
	old := now.Add(-store.UsageMaxBack - 24*time.Hour)
	f.line(old, "ancient", 0, 100, 0, 5, false)
	a := now.Add(-3 * time.Hour)
	f.line(a, "m1", 0, 100, 1000, 10, false)
	f.line(a.Add(time.Minute), "m2", 0, 100, 1000, 10, false)
	f.line(a.Add(30*time.Minute), "m3", 0, 100, 1000, 10, false)
	// A live row already covers this reply.
	b := now.Add(-time.Hour)
	f.line(b, "m4", 0, 100, 1000, 10, false)
	if err := f.st.AddSessionUsage(&store.SessionUsage{TaskID: f.task.ID, ResumeID: "s1", Started: b.Add(-time.Second),
		Ended: b.Add(time.Second), Cause: store.UsageOperator, Input: 2, LastMessage: "m4"}); err != nil {
		t.Fatal(err)
	}
	since := now.Add(-48 * time.Hour)
	res, err := BackfillUsage(f.st, since, false, f.u.transcript)
	if err != nil {
		t.Fatal(err)
	}
	if res.Rows != 2 || res.Replies != 3 {
		t.Fatalf("first run %+v", res)
	}
	rows, _ := f.st.SessionUsageOf(f.task.ID, 100)
	var backfilled, out int64
	n := 0
	for _, r := range rows {
		if r.Cause == store.UsageBackfill {
			n++
			backfilled += r.CacheWrite1h
			out += r.Output
		}
	}
	if n != 2 || backfilled != 300 || out != 30 {
		t.Fatalf("rows %d, cache_write_1h %d, output %d", n, backfilled, out)
	}
	res, err = BackfillUsage(f.st, since, false, f.u.transcript)
	if err != nil || res.Rows != 0 || res.Replies != 0 {
		t.Fatalf("second run %+v (%v)", res, err)
	}
	// Asking further back than UsageMaxBack still leaves the ancient reply out.
	res, err = BackfillUsage(f.st, old.Add(-time.Hour), false, f.u.transcript)
	if err != nil || res.Rows != 0 {
		t.Fatalf("far run %+v (%v)", res, err)
	}
}

// A dry run writes nothing.
func TestBackfillDryRunWritesNothing(t *testing.T) {
	f := newUsageFix(t)
	a := time.Now().UTC().Add(-time.Hour)
	f.line(a, "m1", 0, 100, 1000, 10, false)
	res, err := BackfillUsage(f.st, a.Add(-time.Hour), true, f.u.transcript)
	if err != nil || res.Rows != 1 {
		t.Fatalf("dry run %+v (%v)", res, err)
	}
	if rows, _ := f.st.SessionUsageOf(f.task.ID, 10); len(rows) != 0 {
		t.Fatalf("dry run wrote %d rows", len(rows))
	}
}

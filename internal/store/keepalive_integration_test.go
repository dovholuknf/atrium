//go:build integration

package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestKeepaliveTokensSinceCountsOnlyTheWindow(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	mid := time.Date(2026, 9, 29, 0, 0, 0, 0, time.Local)
	add := func(at time.Time, cost float64) {
		if err := st.AddKeepaliveRefresh(&KeepaliveRefresh{TaskID: "x", At: at, Outcome: "warmed",
			CacheRead: 100, CacheWrite: 10, Input: 2, Output: 3, Cost: cost}); err != nil {
			t.Fatal(err)
		}
	}
	add(mid.Add(-time.Minute), 0.1) // yesterday
	add(mid.Add(time.Minute), 0.1)
	add(mid.Add(time.Hour), 0.1)
	add(mid.Add(2*time.Hour), 0) // never reached the API
	n, tok, err := st.KeepaliveTokensSince(mid)
	if err != nil || n != 2 || tok != 230 {
		t.Fatalf("n=%d tokens=%d err=%v, want 2 and 230", n, tok, err)
	}
}

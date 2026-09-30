package api

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestSettingsKeepaliveTodayFigures(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "atrium.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now()
	mid := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for _, at := range []time.Time{mid.Add(-time.Hour), mid.Add(time.Second)} {
		if err := st.AddKeepaliveRefresh(&store.KeepaliveRefresh{TaskID: "x", At: at, Outcome: "warmed",
			CacheRead: 100, Input: 2, Output: 3, Cost: 0.1}); err != nil {
			t.Fatal(err)
		}
	}
	out := map[string]any{}
	keepaliveSettingsView(st, out)
	if out["cache_keepalive_today_refreshes"] != 1 || out["cache_keepalive_today_tokens"] != int64(105) {
		t.Fatalf("today = %v refreshes, %v tokens; want 1 and 105", out["cache_keepalive_today_refreshes"], out["cache_keepalive_today_tokens"])
	}
	if out["cache_keepalive_week_refreshes"] != 2 {
		t.Fatalf("week = %v, want 2", out["cache_keepalive_week_refreshes"])
	}
}

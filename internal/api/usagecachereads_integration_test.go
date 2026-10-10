//go:build integration

package api

import "testing"

// The usage tab's cache reads toggle: off until switched on, and it reads back.
func TestUsageCacheReadsSettingRoundTrip(t *testing.T) {
	srv, _, _ := fileServer(t)

	if got := settingsGet(t, srv)["usage_cache_reads"]; got != false {
		t.Fatalf("a new room reads usage_cache_reads=%v", got)
	}
	if rec := settingsPost(t, srv, `{"usage_cache_reads":true}`); rec.Code != 200 {
		t.Fatalf("switching it on answered %d: %s", rec.Code, rec.Body.String())
	}
	if got := settingsGet(t, srv)["usage_cache_reads"]; got != true {
		t.Fatalf("settings read back usage_cache_reads=%v", got)
	}
	if rec := settingsPost(t, srv, `{"usage_cache_reads":false}`); rec.Code != 200 {
		t.Fatalf("switching it off answered %d", rec.Code)
	}
	if got := settingsGet(t, srv)["usage_cache_reads"]; got != false {
		t.Fatalf("settings read back usage_cache_reads=%v after off", got)
	}
}

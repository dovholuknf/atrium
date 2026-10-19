//go:build integration

package api

import (
	"net/http"
	"testing"
)

// A room with no merged_cull_grace set, and one that had it reset, both read as a day. `off` is how a room opts out.
func TestMergedCullGraceDefaultsToADayAndResetsToIt(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real server with git worktrees and a database")
	}
	t.Parallel()
	srv, _, _ := fileServer(t)

	out := settingsGet(t, srv)
	if out["merged_cull_grace_default"] != "86400" {
		t.Fatalf("the default reads as %v, want 86400", out["merged_cull_grace_default"])
	}
	if got := out["merged_cull_grace"]; got != "" && got != "86400" {
		t.Fatalf("a new room has merged_cull_grace %v, want unset or 86400", got)
	}

	if rec := settingsPost(t, srv, `{"merged_cull_grace":"1800"}`); rec.Code != http.StatusOK {
		t.Fatalf("setting a grace answered %d: %s", rec.Code, rec.Body.String())
	}
	if rec := settingsPost(t, srv, `{"merged_cull_grace":""}`); rec.Code != http.StatusOK {
		t.Fatalf("resetting it answered %d: %s", rec.Code, rec.Body.String())
	}
	out = settingsGet(t, srv)
	if got := out["merged_cull_grace"]; got != "" && got != "86400" {
		t.Fatalf("a reset merged_cull_grace reads %v, want unset or 86400", got)
	}
	if out["merged_cull_grace_default"] != "86400" {
		t.Fatalf("the default reads as %v after a reset, want 86400", out["merged_cull_grace_default"])
	}
}

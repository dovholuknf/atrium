//go:build integration

package api

import (
	"net/http"
	"testing"
)

// A worker leaves only when somebody says so, so a room with no merged_cull_grace set, and one that had it reset,
// both read as off.
func TestMergedCullGraceDefaultsToOffAndResetsToOff(t *testing.T) {
	srv, _, _ := fileServer(t)

	out := settingsGet(t, srv)
	if out["merged_cull_grace_default"] != "off" {
		t.Fatalf("the default reads as %v, want off", out["merged_cull_grace_default"])
	}
	if got := out["merged_cull_grace"]; got != "" && got != "off" {
		t.Fatalf("a new room has merged_cull_grace %v, want unset or off", got)
	}

	if rec := settingsPost(t, srv, `{"merged_cull_grace":"1800"}`); rec.Code != http.StatusOK {
		t.Fatalf("setting a grace answered %d: %s", rec.Code, rec.Body.String())
	}
	if rec := settingsPost(t, srv, `{"merged_cull_grace":""}`); rec.Code != http.StatusOK {
		t.Fatalf("resetting it answered %d: %s", rec.Code, rec.Body.String())
	}
	out = settingsGet(t, srv)
	if got := out["merged_cull_grace"]; got != "" && got != "off" {
		t.Fatalf("a reset merged_cull_grace reads %v, want unset or off", got)
	}
	if out["merged_cull_grace_default"] != "off" {
		t.Fatalf("the default reads as %v after a reset, want off", out["merged_cull_grace_default"])
	}
}

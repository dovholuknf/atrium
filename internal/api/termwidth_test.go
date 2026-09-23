package api

import (
	"net/http"
	"testing"
)

// The width floor is refused out of range rather than clamped, reads back what
// was stored, and falls back to the default when cleared.
func TestTheWidthFloorRefusesOutOfRange(t *testing.T) {
	srv, _, _ := fileServer(t)

	if got := settingsGet(t, srv)["terminal_min_cols_now"]; got != float64(120) {
		t.Fatalf("the default floor reads %v, wanted 120", got)
	}
	for _, bad := range []string{`"39"`, `"401"`, `"wide"`, `"-5"`} {
		if rec := settingsPost(t, srv, `{"terminal_min_cols":`+bad+`}`); rec.Code != http.StatusBadRequest {
			t.Errorf("%s answered %d, wanted 400: %s", bad, rec.Code, rec.Body.String())
		}
	}
	if got := settingsGet(t, srv)["terminal_min_cols_now"]; got != float64(120) {
		t.Fatalf("a refused floor still moved it to %v", got)
	}

	if rec := settingsPost(t, srv, `{"terminal_min_cols":"150"}`); rec.Code != http.StatusOK {
		t.Fatalf("150 answered %d: %s", rec.Code, rec.Body.String())
	}
	got := settingsGet(t, srv)
	if got["terminal_min_cols"] != "150" || got["terminal_min_cols_now"] != float64(150) {
		t.Fatalf("150 read back as %v / %v", got["terminal_min_cols"], got["terminal_min_cols_now"])
	}

	if rec := settingsPost(t, srv, `{"terminal_min_cols":""}`); rec.Code != http.StatusOK {
		t.Fatalf("clearing answered %d: %s", rec.Code, rec.Body.String())
	}
	if got := settingsGet(t, srv)["terminal_min_cols_now"]; got != float64(120) {
		t.Fatalf("a cleared floor reads %v, wanted the default 120", got)
	}
}

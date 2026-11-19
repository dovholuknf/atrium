//go:build integration

package link

import (
	"net/http"
	"strings"
	"testing"
)

func TestARoomAlreadyProbingIsAConflictNotABadGateway(t *testing.T) {
	front, _, done := twoRoomsWith(t, func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/capabilities/run" {
				http.NotFound(w, r)
				return
			}
			if name == "alpha" {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"error":"a capability run is already going"}`))
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
		})
	})
	defer done()
	code, got := do(t, http.MethodPost, front.URL+"/_hub/capabilities?room=alpha", "", "")
	if code != http.StatusConflict {
		t.Fatalf("a busy room answered %d, not 409: %v", code, got)
	}
	if un, _ := got["unreached"].(map[string]any); !strings.Contains(un["alpha"].(string), "already going") {
		t.Errorf("the room's reason was not passed on: %v", got)
	}
	for _, room := range []string{"beta", "all"} {
		if code, got := do(t, http.MethodPost, front.URL+"/_hub/capabilities?room="+room, "", ""); code != http.StatusBadGateway {
			t.Errorf("room=%s answered %d, not 502: %v", room, code, got)
		}
	}
}

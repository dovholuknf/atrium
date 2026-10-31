//go:build integration

package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

// Two rooms each hold a live card with the alias `minion`. A bare alias with no
// room of the caller's own to settle it is refused naming both, and an explicit
// `minion@roomA` or `roomA~id` still resolves. A caller ON roomA keeps its own
// room's match for the bare name.
func TestAnAliasOnTwoRoomsIsRefusedUnlessNamed(t *testing.T) {
	rooms := map[string][]map[string]any{
		"roomA": {{"id": "ida", "wire_name": "x/minion-a", "alias": "minion", "status": "running",
			"created_at": "2026-09-28T10:00:00.000Z"}},
		"roomB": {{"id": "idb", "wire_name": "x/minion-b", "alias": "minion", "status": "running",
			"created_at": "2026-09-28T11:00:00.000Z"}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var tasks []map[string]any
		if room := r.Header.Get(wire.RoomHeader); room != "" {
			tasks = rooms[room]
		} else {
			for _, name := range []string{"roomA", "roomB"} {
				for _, t := range rooms[name] {
					tagged := map[string]any{}
					for k, v := range t {
						tagged[k] = v
					}
					tagged["id"] = wire.TagFor(name, t["id"].(string))
					tasks = append(tasks, tagged)
				}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": tasks})
	}))
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}
	ctx := context.Background()

	for _, who := range []string{"minion", "@minion", "MINION"} {
		_, id, _, err := c.resolveCard(ctx, "", who)
		if err == nil {
			t.Fatalf("resolveCard(%q) picked %q silently", who, id)
		}
		for _, want := range []string{"minion@roomA", "minion@roomB", "roomA~ida", "roomB~idb"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("resolveCard(%q) refusal %q does not list %s", who, err, want)
			}
		}
	}
	for who, want := range map[string]string{
		"minion@roomA": "ida", "minion@roomB": "idb", "roomA~ida": "ida", "roomB~idb": "idb",
	} {
		scope, id, _, err := c.resolveCard(ctx, "", who)
		if err != nil || id != want && id != wire.TagFor(scope, want) {
			t.Errorf("resolveCard(%q) = %q, %v; want %q", who, id, err, want)
		}
	}
	// The caller's own room settles a bare name.
	for room, want := range map[string]string{"roomA": "ida", "roomB": "idb"} {
		_, id, _, err := c.resolveCard(ctx, room, "minion")
		if err != nil || id != want {
			t.Errorf("a caller on %s resolved bare minion to %q, %v; want %q", room, id, err, want)
		}
	}
}

package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// A DRAG ON THE AGGREGATE BOARD SENDS TAGGED IDS, AND THE ORDER STILL HAS TO LAND.
//
// The strip posts the ids it drew, which are `room~id` on the hub's board. The
// hub untags the path and not the body, so the room saw ids its store has no
// row for, wrote no rank, and the next refresh put the pins back.
func TestPinOrderAcceptsRoomTaggedIDs(t *testing.T) {
	srv, st, dir := fileServer(t)
	var ids []string
	for _, name := range []string{"a", "b", "c"} {
		task, _, err := st.Register(store.Observed{
			WireName: name, Worktree: filepath.ToSlash(filepath.Join(dir, name)), Runner: "claude",
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, task.ID)
	}

	body := `{"ids":["sg4~` + ids[2] + `","sg4~` + ids[0] + `","` + ids[1] + `"]}`
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/tasks/pin-order",
		strings.NewReader(body)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("pin-order answered %d: %s", rec.Code, rec.Body.String())
	}

	want := map[string]float64{ids[2]: 0, ids[0]: 1, ids[1]: 2}
	for id, rank := range want {
		task, err := st.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if task.Rank != rank {
			t.Errorf("card %s has rank %v, wanted %v", id, task.Rank, rank)
		}
	}
}

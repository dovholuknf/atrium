package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// launcher_id is the BARE id of the launching card on this room, computed from the lineage, and
// absent for an operator launch and for a launcher on another room.
func TestLauncherIDOnTheTaskRow(t *testing.T) {
	srv, st, _ := fileServer(t)
	boss := exitCard(t, st, "boss")
	byID := exitCard(t, st, "by-id")
	byName := exitCard(t, st, "by-name")
	human := exitCard(t, st, "human-made")
	away := exitCard(t, st, "far-made")
	for _, l := range []struct{ id, by, byID string }{
		{byID.ID, "boss", boss.ID},
		{byName.ID, "boss", ""},
		{human.ID, "@human", ""},
		{away.ID, "boss@other", "other~abc"},
	} {
		if err := st.SetLineage(l.id, l.by, l.byID); err != nil {
			t.Fatal(err)
		}
	}
	want := map[string]string{byID.ID: boss.ID, byName.ID: boss.ID, human.ID: "", away.ID: "", boss.ID: ""}

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tasks", nil))
	var list struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	for _, row := range list.Tasks {
		id := row["id"].(string)
		got, present := row["launcher_id"]
		if w := want[id]; w == "" && present {
			t.Fatalf("%v: launcher_id = %v, want it omitted", row["wire_name"], got)
		} else if w != "" && got != w {
			t.Fatalf("%v: launcher_id = %v, want the bare id %s", row["wire_name"], got, w)
		}
	}

	// the single GET and the SSE row share it
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tasks/"+byID.ID, nil))
	var one map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &one)
	if one["launcher_id"] != boss.ID {
		t.Fatalf("GET one: launcher_id = %v, want %s", one["launcher_id"], boss.ID)
	}
	fresh, _ := st.Get(byName.ID)
	got, _ := json.Marshal(srv.taskEvent(fresh))
	var ev map[string]any
	_ = json.Unmarshal(got, &ev)
	if ev["launcher_id"] != boss.ID {
		t.Fatalf("task event: launcher_id = %v, want %s", ev["launcher_id"], boss.ID)
	}
}

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
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

// ?name= answers the one card a handle, alias or id means, with its launcher_id.
func TestListTasksByName(t *testing.T) {
	srv, st, _ := fileServer(t)
	boss, kid := exitCard(t, st, "boss"), exitCard(t, st, "kid")
	if err := st.SetAlias(boss.ID, "bo"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetLineage(kid.ID, "boss", boss.ID); err != nil {
		t.Fatal(err)
	}
	for q, want := range map[string]int{"kid": 1, "bo": 1, kid.ID: 1, "nobody": 0} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tasks?name="+q, nil))
		var out struct {
			Tasks []map[string]any `json:"tasks"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if len(out.Tasks) != want {
			t.Fatalf("name=%s: %d cards, want %d", q, len(out.Tasks), want)
		}
		if q == "kid" && out.Tasks[0]["launcher_id"] != boss.ID {
			t.Fatalf("launcher_id = %v", out.Tasks[0]["launcher_id"])
		}
	}
}

// 301 cards, all launched, to time the list (go test -run X -v).
func TestListTasksLauncherCostWithManyCards(t *testing.T) {
	srv, st, _ := fileServer(t)
	boss := exitCard(t, st, "boss")
	for i := 0; i < 300; i++ {
		c := exitCard(t, st, "w"+strconv.Itoa(i))
		_ = st.SetLineage(c.ID, "boss", boss.ID)
		_ = st.SetReportTo(c.ID, "boss")
	}
	best := time.Hour
	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		start := time.Now()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tasks", nil))
		if d := time.Since(start); d < best {
			best = d
		}
		if i == 0 && !strings.Contains(rec.Body.String(), `"launcher_id":"`+boss.ID+`"`) {
			t.Fatal("rows carry no launcher_id")
		}
	}
	t.Logf("GET /v1/tasks, 301 cards: best of 5 = %v", best)
}

//go:build integration

package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// GET /v1/says is every card's says: the same cursor and `more`, narrowed by from, to, state and since.
func TestSaysListsEveryCardAndFilters(t *testing.T) {
	d := testDaemon(t)
	a, b := peerCard(t, d, "sa1"), peerCard(t, d, "sa2")
	put := func(from, to *store.Task, state, at string) {
		t.Helper()
		_, err := d.st.RecordSay(store.Say{FromWire: from.WireName, FromTask: from.ID, ToTask: to.ID,
			ToWire: to.WireName, ToInput: to.WireName, Via: "handle", Door: "say", State: state, SentAt: at},
			"say at "+at)
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 25; i++ {
		put(a, b, store.SayDelivered, "2026-10-10T10:"+strconv.Itoa(10+i)+":00.000Z")
	}
	put(b, a, store.SayRefused, "2026-10-10T11:00:00.000Z")

	get := func(q string) ([]store.Say, bool, int) {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /v1/says", d.handleSays)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/v1/says"+q, nil))
		var body struct {
			Says []store.Say `json:"says"`
			More bool        `json:"more"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return body.Says, body.More, w.Code
	}
	first, more, _ := get("")
	if len(first) != 20 || !more || first[0].FromTask != b.ID {
		t.Fatalf("first page: %d rows, more %v", len(first), more)
	}
	rest, more, _ := get("?before=" + first[19].ID)
	if len(rest) != 6 || more {
		t.Fatalf("second page: %d rows, more %v", len(rest), more)
	}
	if rows, _, _ := get("?from=" + b.WireName); len(rows) != 1 || rows[0].ToTask != a.ID {
		t.Fatalf("from a wire name: %v", rows)
	}
	if rows, _, _ := get("?from=" + a.ID); len(rows) != 20 {
		t.Fatalf("from a card id: %d rows", len(rows))
	}
	if rows, _, _ := get("?to=" + a.ID); len(rows) != 1 || rows[0].FromTask != b.ID {
		t.Fatalf("to a card id: %v", rows)
	}
	if rows, more, _ := get("?agent=" + b.WireName); len(rows) != 20 || !more {
		t.Fatalf("agent as the receiver and the sender: %d rows, more %v", len(rows), more)
	}
	if rows, _, _ := get("?agent=" + a.ID + "&from=" + b.WireName); len(rows) != 1 {
		t.Fatalf("agent and from together: %d rows", len(rows))
	}
	if rows, _, _ := get("?state=refused"); len(rows) != 1 {
		t.Fatalf("state: %v", rows)
	}
	if rows, _, _ := get("?since=2026-10-10T10:30:00Z"); len(rows) != 6 {
		t.Fatalf("since: %d rows", len(rows))
	}
	if rows, more, _ := get("?since=2026-10-10T10:20:00Z"); len(rows) != 16 || more {
		t.Fatalf("since, narrow: %d rows, more %v", len(rows), more)
	}
	if _, _, code := get("?since=yesterday"); code != http.StatusBadRequest {
		t.Fatalf("a bad since answered %d", code)
	}
}

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

// GET /v1/tasks/{id}/says pages back by `before` and says whether another page is behind this one.
func TestTaskSaysPagesWithBefore(t *testing.T) {
	d := testDaemon(t)
	card := peerCard(t, d, "ts2")
	for i := 0; i < 25; i++ {
		at := "2026-10-10T10:" + strconv.Itoa(10+i) + ":00Z"
		_, err := d.st.RecordSay(store.Say{FromWire: "ts1", ToTask: card.ID, ToWire: "ts2", ToInput: "ts2",
			Via: "handle", Door: "say", State: store.SayDelivered, SentAt: at}, "full text "+strconv.Itoa(i))
		if err != nil {
			t.Fatal(err)
		}
	}
	get := func(q string) ([]store.Say, bool) {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /v1/tasks/{id}/says", d.handleTaskSays)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/v1/tasks/"+card.ID+"/says"+q, nil))
		var body struct {
			Says []store.Say `json:"says"`
			More bool        `json:"more"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.Says, body.More
	}
	first, more := get("")
	if len(first) != 20 || !more || first[0].Text != "full text 24" {
		t.Fatalf("first page: %d rows, more %v", len(first), more)
	}
	rest, more := get("?before=" + first[19].ID)
	if len(rest) != 5 || more || rest[4].Text != "full text 0" {
		t.Fatalf("second page: %d rows, more %v", len(rest), more)
	}
}

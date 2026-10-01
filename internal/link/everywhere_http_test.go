package link

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// card2Room is a room that streams what it is told and answers one card.
func card2Room(say <-chan string, title string) http.Handler {
	stream := streamer(say)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks/card2" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "card2", "display_title": title, "status": "running",
				"wire_name": "atrium-2", "alias": "orchestrator", "tags": []string{EverywhereTag}})
			return
		}
		if r.URL.Path == "/v1/tasks/gone" {
			http.NotFound(w, r)
			return
		}
		stream.ServeHTTP(w, r)
	})
}

func rigEverywhere(t *testing.T) (front string, hub *Hub, alpha, beta chan string, done func()) {
	t.Helper()
	alpha, beta = make(chan string, 64), make(chan string, 64)
	srv, hub, done := twoRoomsWith(t, func(name string) http.Handler {
		if name == "alpha" {
			return streamer(alpha)
		}
		return card2Room(beta, "the orchestrator")
	})
	return srv.URL, hub, alpha, beta, done
}

func getEverywhere(t *testing.T, front, scope string) []map[string]any {
	t.Helper()
	res, err := http.Get(front + "/_hub/everywhere?room=" + scope)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		Tasks []map[string]any `json:"tasks"`
	}
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || json.Unmarshal(raw, &body) != nil || body.Tasks == nil {
		t.Fatalf("%d %s", res.StatusCode, raw)
	}
	return body.Tasks
}

// FE7, the list: the everywhere card, fetched live, tagged, marked, and not on
// the room it belongs to.
func TestTheEverywhereListIsFetchedLiveAndTagged(t *testing.T) {
	front, hub, _, _, done := rigEverywhere(t)
	defer done()
	hub.IndexEverywhere("beta", []CardState{everyRow("card2", "running", "atrium-2", "orchestrator", EverywhereTag)})

	rows := getEverywhere(t, front, "alpha")
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	r := rows[0]
	if r["id"] != "beta~card2" || r["room"] != "beta" || r["everywhere"] != true ||
		r["display_title"] != "the orchestrator" || r["offline"] != nil {
		t.Fatalf("row = %+v", r)
	}
	if got := getEverywhere(t, front, "beta"); len(got) != 0 {
		t.Fatalf("a room was handed its own card: %+v", got)
	}
}

// A room that is not attached comes from the cache, marked offline.
func TestAnEverywhereCardOnARoomThatIsNotAttachedIsOffline(t *testing.T) {
	front, hub, _, _, done := rigEverywhere(t)
	defer done()
	hub.IndexEverywhere("gamma", []CardState{everyRow("g1", "running", "atrium-9", "", EverywhereTag)})

	rows := getEverywhere(t, front, "alpha")
	if len(rows) != 1 || rows[0]["id"] != "gamma~g1" || rows[0]["offline"] != true || rows[0]["everywhere"] != true {
		t.Fatalf("rows = %+v", rows)
	}
}

// A card its room no longer has is left out rather than drawn from the cache.
func TestAnEverywhereCardItsRoomNoLongerHasIsLeftOut(t *testing.T) {
	front, hub, _, _, done := rigEverywhere(t)
	defer done()
	hub.IndexEverywhere("beta", []CardState{everyRow("gone", "running", "atrium-3", "", EverywhereTag)})
	if rows := getEverywhere(t, front, "alpha"); len(rows) != 0 {
		t.Fatalf("rows = %+v", rows)
	}
}

// FE7 and FE8, the stream: a foreign card's events arrive tagged, other cards of
// that room's do not, and an index change is told.
func TestTheScopedStreamCarriesEverywhereCardsWhenAsked(t *testing.T) {
	front, hub, alpha, beta, done := rigEverywhere(t)
	defer done()
	hub.IndexEverywhere("beta", []CardState{everyRow("card2", "running", "atrium-2", "orchestrator", EverywhereTag)})

	ch, shut := listen(t, front+"/v1/events/room/alpha?everywhere=1")
	defer shut()
	go func() {
		for i := 0; i < 60; i++ {
			beta <- sse("task", `{"id":"other","title":"not indexed"}`)
			beta <- sse("task", `{"id":"card2","title":"indexed"}`)
			beta <- sse("activity", `{"task_id":"card2","what":"x"}`)
			alpha <- sse("task", `{"id":"own"}`)
			time.Sleep(100 * time.Millisecond)
		}
	}()
	seen := map[string]bool{}
	deadline := time.After(10 * time.Second)
	for !seen["beta~card2 task"] || !seen["beta~card2 activity"] || !seen["own"] {
		select {
		case e := <-ch:
			obj := fields(t, e.Data)
			switch {
			case e.Kind == "task" && obj["id"] == "own":
				seen["own"] = true
			case e.Kind == "task" && obj["id"] == "beta~card2":
				if obj["room"] != "beta" {
					t.Fatalf("not marked with its room: %s", e.Data)
				}
				seen["beta~card2 task"] = true
			case e.Kind == "activity" && obj["task_id"] == "beta~card2":
				seen["beta~card2 activity"] = true
			case e.Kind == "task":
				t.Fatalf("a card that is not indexed came through: %s", e.Data)
			}
		case <-deadline:
			t.Fatalf("saw %v", seen)
		}
	}
	// FE8: the tag removed, and the board is told to fetch again.
	hub.IndexEverywhere("beta", nil)
	waitEvent(t, ch, "everywhere")
}

// FE9. Without the parameter the stream is what it was: the room's own events
// exactly as it sent them, nothing from another room, and no `everywhere` event.
func TestTheScopedStreamWithoutTheParameterIsUnchanged(t *testing.T) {
	front, hub, alpha, beta, done := rigEverywhere(t)
	defer done()
	hub.IndexEverywhere("beta", []CardState{everyRow("card2", "running", "atrium-2", "orchestrator", EverywhereTag)})

	req, _ := http.NewRequest(http.MethodGet, front+"/v1/events/room/alpha", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	own := `{"id":"own","title":"mine"}`
	go func() {
		for i := 0; i < 20; i++ {
			beta <- sse("task", `{"id":"card2","title":"indexed"}`)
			alpha <- sse("task", own)
			time.Sleep(100 * time.Millisecond)
		}
		hub.IndexEverywhere("beta", nil)
	}()
	var got strings.Builder
	buf := make([]byte, 4096)
	end := time.Now().Add(3 * time.Second)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for time.Now().Before(end) {
			n, err := res.Body.Read(buf)
			got.Write(buf[:n])
			if err != nil {
				return
			}
		}
	}()
	time.Sleep(3200 * time.Millisecond)
	res.Body.Close()
	<-readDone
	raw := got.String()
	if !strings.Contains(raw, sse("task", own)) {
		t.Fatalf("the room's own event is not there byte for byte: %q", raw)
	}
	// The rooms event is every window's news and names both rooms. Everything
	// else is the room's own.
	blocks := strings.Split(raw, "\n\n")
	// The read was cut off wherever it was, so the last block may be half of one.
	blocks = blocks[:len(blocks)-1]
	for _, block := range blocks {
		if strings.HasPrefix(block, "event: rooms") || block == "" {
			continue
		}
		if block != strings.TrimSuffix(sse("task", own), "\n\n") {
			t.Fatalf("the stream carries something that is not the room's own: %q", block)
		}
	}
}

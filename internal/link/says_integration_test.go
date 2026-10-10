//go:build integration

package link

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

// roomSays is a room that holds `n` says, newest first, sent a minute apart from `base` minutes, and pages them the way
// the real endpoint does: 20 a page, `before` an id, `more` when another page is behind.
func roomSays(room string, n, base int, seen *[]url.Values) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v1/says" {
			fmt.Fprint(w, `{}`)
			return
		}
		q := r.URL.Query()
		*seen = append(*seen, q)
		var rows []map[string]any
		started := q.Get("before") == ""
		for i := n - 1; i >= 0 && len(rows) <= 20 && (q.Get("from") == "" || q.Get("from") == "card0"); i-- {
			id := fmt.Sprintf("%s-%02d", room, i)
			if !started {
				started = q.Get("before") == id
				continue
			}
			rows = append(rows, map[string]any{"id": id, "from_task": "card0", "to_task": "card1",
				"sent_at": fmt.Sprintf("2026-10-10T10:%02d:00.000Z", base+i), "text": id})
		}
		more := len(rows) > 20
		if more {
			rows = rows[:20]
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"says": rows, "more": more})
	})
}

type mergedSays struct {
	Says []struct {
		ID, Room string
		FromTask string `json:"from_task"`
		ToTask   string `json:"to_task"`
		SentAt   string `json:"sent_at"`
	} `json:"says"`
	More    bool     `json:"more"`
	Cursor  string   `json:"cursor"`
	Missing []string `json:"missing"`
}

func getSays(t *testing.T, base, q string) mergedSays {
	t.Helper()
	res, err := http.Get(base + "/v1/says" + q)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out mergedSays
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// Two rooms merge by sent_at, the rows carry their room and `room~id` tags, and the cursor pages back through both
// without a row shown twice or lost.
func TestSaysMergeAcrossRoomsAndPageWithTheCursor(t *testing.T) {
	var a, b []url.Values
	// alpha: 30 says at minutes 0..29. beta: 5 says at minutes 50..54, all newer than alpha's.
	front, _, done := two(t, roomSays("alpha", 30, 0, &a), roomSays("beta", 5, 50, &b))
	defer done()

	first := getSays(t, front.URL, "")
	if len(first.Says) != 20 || !first.More || first.Cursor == "" || len(first.Missing) != 0 {
		t.Fatalf("first page: %d rows, more %v, cursor %q, missing %v", len(first.Says), first.More, first.Cursor,
			first.Missing)
	}
	// beta's five are newest, then alpha's newest fifteen.
	for i, s := range first.Says {
		want := "alpha"
		if i < 5 {
			want = "beta"
		}
		if s.Room != want {
			t.Fatalf("row %d came from %q, want %q", i, s.Room, want)
		}
		if i > 0 && s.SentAt > first.Says[i-1].SentAt {
			t.Fatalf("row %d is newer than the one before it", i)
		}
	}
	if got := first.Says[0]; got.ID != "beta~beta-04" || got.FromTask != "beta~card0" || got.ToTask != "beta~card1" {
		t.Fatalf("a row is not tagged: %+v", got)
	}

	second := getSays(t, front.URL, "?before="+url.QueryEscape(first.Cursor))
	if len(second.Says) != 15 || second.More || second.Cursor != "" {
		t.Fatalf("second page: %d rows, more %v", len(second.Says), second.More)
	}
	seen := map[string]bool{}
	for _, s := range append(first.Says, second.Says...) {
		if seen[s.ID] {
			t.Fatalf("%s was shown twice", s.ID)
		}
		seen[s.ID] = true
	}
	if len(seen) != 35 {
		t.Fatalf("%d of 35 says were shown", len(seen))
	}
	// beta had nothing further back after the first page, so it is not asked again.
	if len(b) != 1 {
		t.Fatalf("beta was asked %d times, want once", len(b))
	}
}

// A room that does not answer is named, never an error, and the rest still draw.
func TestSaysNameAQuietRoomAndKeepGoing(t *testing.T) {
	var a, b []url.Values
	quiet := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "busy", http.StatusServiceUnavailable)
	})
	front, _, done := two(t, roomSays("alpha", 3, 0, &a), quiet)
	defer done()
	_ = b

	got := getSays(t, front.URL, "")
	if len(got.Says) != 3 || got.More || len(got.Missing) != 1 || got.Missing[0] != "beta" {
		t.Fatalf("quiet room: %d rows, more %v, missing %v", len(got.Says), got.More, got.Missing)
	}
}

// Filters reach the rooms, a `room~id` filter asks only that room and arrives bare, and `room` narrows the rooms.
func TestSaysFiltersReachTheRooms(t *testing.T) {
	var a, b []url.Values
	front, _, done := two(t, roomSays("alpha", 2, 0, &a), roomSays("beta", 2, 10, &b))
	defer done()

	got := getSays(t, front.URL, "?from="+url.QueryEscape("beta~card0")+"&state=held&since=2026-10-10T10:00:00Z")
	if len(a) != 0 || len(b) != 1 {
		t.Fatalf("asked alpha %d and beta %d times", len(a), len(b))
	}
	if q := b[0]; q.Get("from") != "card0" || q.Get("state") != "held" || q.Get("since") != "2026-10-10T10:00:00Z" {
		t.Fatalf("beta was asked %v", q)
	}
	if len(got.Says) != 2 || got.Says[0].Room != "beta" {
		t.Fatalf("rows: %+v", got.Says)
	}

	a, b = nil, nil
	_ = getSays(t, front.URL, "?room=alpha")
	if len(a) != 1 || len(b) != 0 {
		t.Fatalf("room=alpha asked alpha %d and beta %d times", len(a), len(b))
	}
	if a[0].Get("room") != "" {
		t.Fatalf("the room filter leaked to the room: %v", a[0])
	}
}

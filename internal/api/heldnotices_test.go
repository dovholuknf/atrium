package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// The row carries held_notices and oldest_held_at, and reading them clears the count.
func TestNoticesReadClearsTheHeldCountOnTheRow(t *testing.T) {
	srv, st, dir := fileServer(t)
	card := cardIn(t, st, dir)

	old := HeldNoticesOf
	t.Cleanup(func() { HeldNoticesOf = old })
	HeldNoticesOf = func(t *store.Task) (int, string) {
		n, at, _ := st.HeldNoticeStats(t.ID)
		if n == 0 {
			return 0, ""
		}
		return n, at.UTC().Format(store.TimeFormat)
	}

	row := func() view {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tasks/"+card.ID, nil))
		var v view
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
			t.Fatalf("%v: %s", err, rec.Body)
		}
		return v
	}
	if v := row(); v.HeldNotices != 0 || v.OldestHeldAt != "" {
		t.Fatalf("a fresh row: %+v", v)
	}
	if err := st.AppendEvent(card.ID, store.EventNotified, store.HeldNoticePayload("fyi", "w", "w1", "hi")); err != nil {
		t.Fatal(err)
	}
	if v := row(); v.HeldNotices != 1 || v.OldestHeldAt == "" {
		t.Fatalf("one held: %+v", v)
	}

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/tasks/"+card.ID+"/notices-read", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("notices-read answered %d: %s", rec.Code, rec.Body)
	}
	if v := row(); v.HeldNotices != 0 || v.OldestHeldAt != "" {
		t.Fatalf("after read: %+v", v)
	}

	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost,
		"/v1/tasks/01a0ffff-ffff-7fff-bfff-ffffffffffff/notices-read", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown card answered %d, want 404", rec.Code)
	}
}

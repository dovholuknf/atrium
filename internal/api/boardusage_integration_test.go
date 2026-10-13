//go:build integration

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The board posts counts, the room adds them per day, and what is malformed or too big is dropped without an error.
func TestBoardUsageIsCountedAndBounded(t *testing.T) {
	srv, st, _ := fileServer(t)
	post := func(body string) {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/usage", strings.NewReader(body)))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%.40s answered %d, want 204", body, rec.Code)
		}
	}
	post(`{"counts":{"menu:arrange tabs":2,"tab:cards":1}}`)
	post(`{"counts":{"menu:arrange tabs":3}}`)
	post(`not json`)
	post(`{"counts":"nope"}`)
	post(`{"counts":{"":4,"zero":0,"neg":-3,"` + strings.Repeat("x", 201) + `":1}}`)
	post(`{"counts":{"pad":1},"junk":"` + strings.Repeat("y", 70<<10) + `"}`)

	got, err := st.BoardUsageTotals()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Control != "menu:arrange tabs" || got[0].N != 5 || got[1].Control != "tab:cards" || got[1].N != 1 {
		t.Fatalf("totals are %+v, want arrange tabs 5 then cards 1 and nothing else", got)
	}
	if got[0].Last == "" {
		t.Fatal("the last used day is empty")
	}

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/usage/board", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"menu:arrange tabs"`) {
		t.Fatalf("GET /v1/usage answered %d %s", rec.Code, rec.Body.String())
	}
}

// A second day adds a row and the total spans both.
func TestBoardUsageUpsertSpansDays(t *testing.T) {
	_, st, _ := fileServer(t)
	for _, d := range []string{"2026-10-09", "2026-10-10", "2026-10-10"} {
		if err := st.AddBoardUsage("tab:cards", d, 1); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := st.BoardUsageTotals()
	if len(got) != 1 || got[0].N != 3 || got[0].Last != "2026-10-10" {
		t.Fatalf("totals are %+v", got)
	}
}

// The totals are read from a snapshot that a write drops, so a count posted after a read still shows on the next one.
func TestBoardUsageSnapshotFollowsWrites(t *testing.T) {
	srv, st, _ := fileServer(t)
	read := func() string {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/usage/board", nil))
		return rec.Body.String()
	}
	post := func(body string) {
		srv.Handler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/usage", strings.NewReader(body)))
	}
	post(`{"counts":{"tab:cards":2}}`)
	if first := read(); !strings.Contains(first, `"n":2`) {
		t.Fatalf("first read %s", first)
	}
	// A write behind the snapshot's back is not seen: the read is the snapshot and not the table.
	if err := st.AddBoardUsage("tab:cards", "2026-10-10", 5); err != nil {
		t.Fatal(err)
	}
	if again := read(); !strings.Contains(again, `"n":2`) {
		t.Fatalf("second read %s is not the snapshot", again)
	}
	post(`{"counts":{"tab:cards":1}}`)
	if after := read(); !strings.Contains(after, `"n":8`) {
		t.Fatalf("read after a post %s, want the total 8", after)
	}
}

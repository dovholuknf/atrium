//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The row carries owed, and a dismiss from the card the item is kept on closes it. Reading does not.
func TestOwedOnTheRowAndADismissFromTheHostClosesIt(t *testing.T) {
	srv, st, dir := fileServer(t)
	host := cardIn(t, st, dir)

	old := OwedOf
	t.Cleanup(func() { OwedOf = old })
	OwedOf = func(t *store.Task) (int, string, bool) {
		items, _ := st.OpenOwedItems()
		n := 0
		for _, it := range items {
			if it.Host == t.ID {
				n++
			}
		}
		return n, "", false
	}
	row := func() view {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tasks/"+host.ID, nil))
		var v view
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
			t.Fatalf("%v: %s", err, rec.Body)
		}
		return v
	}
	if err := st.PutOwedItem(store.OwedItem{Worker: "w1", Host: host.ID, Launcher: host.ID, Since: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if v := row(); v.Owed != 1 {
		t.Fatalf("the row shows %d owed", v.Owed)
	}
	post := func(id, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/tasks/"+id+"/owed-dismiss",
			strings.NewReader(body)))
		return rec
	}
	// Reading the notices closes nothing.
	through := time.Now().Add(time.Minute).UTC().Format(store.TimeFormat)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/tasks/"+host.ID+"/notices-read",
		strings.NewReader(`{"through":"`+through+`"}`)))
	if v := row(); v.Owed != 1 {
		t.Fatalf("reading closed it: %d", v.Owed)
	}
	// A card the item is not kept on cannot close it.
	other, _, err := st.Register(store.Observed{WireName: "stranger", Worktree: "/tmp/stranger", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if rec := post(other.ID, `{"worker":"w1"}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"closed":false`) {
		t.Fatalf("another card's dismiss: %d %s", rec.Code, rec.Body)
	}
	if v := row(); v.Owed != 1 {
		t.Fatalf("a stranger closed it: %d", v.Owed)
	}
	if rec := post(host.ID, `{"worker":"w1"}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"closed":true`) {
		t.Fatalf("the dismiss: %d %s", rec.Code, rec.Body)
	}
	if v := row(); v.Owed != 0 {
		t.Fatalf("after the dismiss: %d", v.Owed)
	}
	if rec := post("01a0ffff-ffff-7fff-bfff-ffffffffffff", `{"worker":"w1"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown card answered %d", rec.Code)
	}
}

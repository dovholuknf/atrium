//go:build integration

package shepherd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTheHubEdgeRoundTripsARowAndFindsAnItemByCard(t *testing.T) {
	var row State
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/_hub/shepherds/") && r.Method == http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&row)
		case strings.HasPrefix(r.URL.Path, "/_hub/shepherds/"):
			if row.ItemID == "" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"no shepherd for that item"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(row)
		case r.URL.Path == "/_hub/items":
			_, _ = w.Write([]byte(`{"items":[{"item_id":"b","card":"other","state":"running"},{"item_id":"a","card":"c1","state":"finished"}]}`))
		}
	}))
	defer srv.Close()
	h := &HubHTTP{Board: strings.TrimPrefix(srv.URL, "http://")}
	ctx := context.Background()
	if _, ok, err := h.Load(ctx, "bug-x"); ok || err != nil {
		t.Fatalf("before a row: %v %v", ok, err)
	}
	if err := h.Save(ctx, State{ItemID: "bug-x", Phase: PhaseWait, Card: "c1"}); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := h.Load(ctx, "bug-x"); !ok || err != nil || got.Phase != PhaseWait || got.Card != "c1" {
		t.Fatalf("%+v %v %v", got, ok, err)
	}
	if it, ok, err := h.ItemForCard(ctx, "c1"); !ok || err != nil || it.ID != "a" || it.State != "finished" {
		t.Fatalf("%+v %v %v", it, ok, err)
	}
}

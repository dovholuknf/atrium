//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestACardsArtifactsComeBackAsAListAndAMissingCardIsRefused(t *testing.T) {
	t.Parallel()
	srv, st, _ := fileServer(t)
	card := exitCard(t, st, "maker")
	if err := st.RecordArtifact(store.Artifact{TaskID: card.ID, Kind: store.ArtifactFile, Ref: "a/b.go", Source: "hook"}); err != nil {
		t.Fatal(err)
	}
	get := func(id string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tasks/"+id+"/artifacts", nil))
		return rec
	}
	var list []store.Artifact
	if rec := get(card.ID); rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &list) != nil || len(list) != 1 || list[0].Ref != "a/b.go" {
		t.Fatalf("the list came back as %d %s", rec.Code, rec.Body.String())
	}
	if rec := get("nope"); rec.Code != 404 {
		t.Fatalf("a card that is not here answered %d", rec.Code)
	}
}

package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// aliasBoard is one card the tool reads and patches, and a second holding
// `taken`, so a clash comes back the way the room words it.
type aliasBoard struct {
	card    map[string]any
	patched []string
}

func (b *aliasBoard) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{b.card}})
		case r.URL.Path == "/v1/tasks/me" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(b.card)
		case r.URL.Path == "/v1/tasks/me" && r.Method == http.MethodPatch:
			var body struct {
				Alias *string `json:"alias"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Alias == nil {
				http.Error(w, "no alias in the patch", http.StatusBadRequest)
				return
			}
			b.patched = append(b.patched, *body.Alias)
			if strings.TrimPrefix(*body.Alias, "@") == "taken" {
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": "@taken is already the alias of other (x), card other"})
				return
			}
			b.card["alias"] = strings.TrimPrefix(*body.Alias, "@")
			b.card["alias_note"] = ""
			_ = json.NewEncoder(w).Encode(map[string]any{"task": b.card})
		default:
			http.NotFound(w, r)
		}
	})
}

// With no card it reads and sets the caller's own, a clash names the holder,
// and the note on a card whose default was taken is passed on.
func TestAliasToolReadsAndSetsTheCallersOwnCard(t *testing.T) {
	board := &aliasBoard{card: map[string]any{
		"id": "me", "wire_name": "saorch-2", "display_title": "saorch: merger", "status": "running",
		"alias_note": "no alias: its default is taken. @saorch is already the alias of sa84",
	}}
	srv := httptest.NewServer(board.handler())
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}
	ctx := context.Background()

	_, got, err := c.aliasHandler(ctx, ctlReq("saorch-2", ""), aliasInput{})
	if err != nil || got.Card != "me" || got.Alias != "" || !strings.Contains(got.Note, "@saorch") {
		t.Fatalf("a read = %+v, %v", got, err)
	}
	if len(board.patched) != 0 {
		t.Fatalf("a read patched the card: %v", board.patched)
	}

	_, got, err = c.aliasHandler(ctx, ctlReq("saorch-2", ""), aliasInput{Alias: "@orch"})
	if err != nil || got.Alias != "orch" || got.Was != "(none)" || got.Note != "" {
		t.Fatalf("a set = %+v, %v", got, err)
	}

	_, _, err = c.aliasHandler(ctx, ctlReq("saorch-2", ""), aliasInput{Alias: "taken"})
	if err == nil || !strings.Contains(err.Error(), "card other") {
		t.Fatalf("a clash did not name the holder: %v", err)
	}

	_, got, err = c.aliasHandler(ctx, ctlReq("", ""), aliasInput{Card: "@orch", Clear: true})
	if err != nil || got.Alias != "" || got.Was != "orch" {
		t.Fatalf("a clear by alias = %+v, %v", got, err)
	}
	if last := board.patched[len(board.patched)-1]; last != "" {
		t.Fatalf("a clear sent %q, not empty", last)
	}

	if _, _, err := c.aliasHandler(ctx, ctlReq("", ""), aliasInput{}); err == nil {
		t.Fatal("no card and no caller was not refused")
	}
	if _, _, err := c.aliasHandler(ctx, ctlReq("saorch-2", ""), aliasInput{Alias: "x", Clear: true}); err == nil {
		t.Fatal("alias and clear together were not refused")
	}
}

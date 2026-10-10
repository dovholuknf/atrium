//go:build integration

package api

import "testing"

func cardColorsOf(t *testing.T, srv *Server) map[string]any {
	t.Helper()
	cc, _ := settingsGet(t, srv)["card_colors"].(map[string]any)
	if cc == nil {
		t.Fatal("no card_colors in the settings answer")
	}
	return cc
}

func TestCardColorsSeededOnceAndEmptyStaysEmpty(t *testing.T) {
	srv, _, _ := fileServer(t)
	if n := len(cardColorsOf(t, srv)["repos"].(map[string]any)); n != 17 {
		t.Fatalf("seeded %d entries", n)
	}
	if rec := settingsPost(t, srv, `{"card_colors":{"default":"","repos":{}}}`); rec.Code != 200 {
		t.Fatalf("save answered %d: %s", rec.Code, rec.Body.String())
	}
	if n := len(cardColorsOf(t, srv)["repos"].(map[string]any)); n != 0 {
		t.Fatalf("emptied list came back with %d entries", n)
	}
	if rec := settingsPost(t, srv, `{"card_colors":{"default":"","repos":{"Ziti":"x"}}}`); rec.Code != 400 {
		t.Fatalf("a bad key answered %d", rec.Code)
	}
}

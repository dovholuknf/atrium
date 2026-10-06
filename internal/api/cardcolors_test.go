package api

import "testing"

func TestValidCardColors(t *testing.T) {
	ok := CardColors{Default: "nord", Repos: map[string]string{"github/openziti/ziti": "teal-dusk"}}
	if err := validCardColors(ok); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"ziti", "github/OpenZiti/ziti", "github/openziti", "a/b/c/d"} {
		if validCardColors(CardColors{Repos: map[string]string{k: "x"}}) == nil {
			t.Errorf("%q accepted", k)
		}
	}
	if validCardColors(CardColors{Repos: map[string]string{"github/a/b": ""}}) == nil {
		t.Error("empty name accepted")
	}
}

func TestSeededCardColorsValid(t *testing.T) {
	if len(seededCardColors) != 17 {
		t.Fatalf("seed has %d entries", len(seededCardColors))
	}
	if err := validCardColors(CardColors{Repos: seededCardColors}); err != nil {
		t.Fatal(err)
	}
}

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

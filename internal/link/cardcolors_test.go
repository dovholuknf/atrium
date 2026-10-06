package link

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

// The card colours are the board's, so the hub holds them: the ALL view answers the hub's table whatever
// the borrowed room says (an old room has none), a save lands on the hub, and a room-scoped view keeps the
// room's own.

func hubColors(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	cc, _ := body["card_colors"].(map[string]any)
	if cc == nil {
		t.Fatalf("no card_colors in %v", body)
	}
	return cc["repos"].(map[string]any)
}

func postSettings(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	res, err := http.Post(url+"/v1/settings", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func TestAllViewServesSeededHubCardColorsAndADeletedEntryStaysDeleted(t *testing.T) {
	front, _, done := two(t, settingsRoom("alpha", "moss"), settingsRoom("beta", "ember"))
	defer done()
	front.Config.Handler.(*Proxy).SetInventory(&remembering{})

	if n := len(hubColors(t, getJSON(t, front.URL+"/v1/settings"))); n != 17 {
		t.Fatalf("seeded %d entries, wanted 17", n)
	}
	code, out := postSettings(t, front.URL, `{"card_colors":{"default":"","repos":{"github/openziti/ziti":"nord"}}}`)
	if code != 200 {
		t.Fatalf("a valid save answered %d", code)
	}
	if got := hubColors(t, out); len(got) != 1 || got["github/openziti/ziti"] != "nord" {
		t.Fatalf("the save answered %v", got)
	}
	if got := hubColors(t, getJSON(t, front.URL+"/v1/settings")); len(got) != 1 {
		t.Fatalf("after the save the hub has %v, a deleted entry came back", got)
	}
}

func TestCardColorsSaveRefusesABadKey(t *testing.T) {
	front, _, done := two(t, settingsRoom("alpha", "moss"), settingsRoom("beta", "ember"))
	defer done()
	front.Config.Handler.(*Proxy).SetInventory(&remembering{})
	if code, _ := postSettings(t, front.URL, `{"card_colors":{"default":"","repos":{"Ziti":"nord"}}}`); code != 400 {
		t.Fatalf("a bad key answered %d, wanted 400", code)
	}
	if n := len(hubColors(t, getJSON(t, front.URL+"/v1/settings"))); n != 17 {
		t.Fatalf("a refused save changed the table to %d entries", n)
	}
}

func TestARoomScopedGetKeepsTheRoomsOwnCardColors(t *testing.T) {
	front, _, done := two(t, settingsRoom("alpha", "moss"), settingsRoom("beta", "ember"))
	defer done()
	front.Config.Handler.(*Proxy).SetInventory(&remembering{})
	req, _ := http.NewRequest(http.MethodGet, front.URL+"/v1/settings", nil)
	req.Header.Set("X-Atrium-Room", "beta")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(res.Body).Decode(&body)
	if _, has := body["card_colors"]; has {
		t.Fatalf("the hub's colours leaked into a room-scoped view: %v", body["card_colors"])
	}
}

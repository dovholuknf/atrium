package link

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
)

// The board skin follows the room-picker scope. In the ALL view it is the HUB's
// own, held on the hub, served by the hub, saved to the hub. In a room's view it
// is that room's. The bug this closes: with two rooms attached the ALL view used
// to borrow the alphabetically-first room's skin, so a second room attaching
// swapped it, and a skin saved from ALL got the `needsARoom` 409.

// settingsRoom answers `/v1/settings` with its own skin and the shared list, and
// says which room served anything else. A skin write reaching a room lands here
// so a test can prove it did NOT.
func settingsRoom(room, skin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/settings" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"board_skin":     skin,
				"board_skins":    []string{"harbour", "moss", "noir", "ember", "vapor"},
				"editor_command": room + "-vi",
			})
			return
		}
		fmt.Fprintf(w, `{"served_by":%q}`, room)
	})
}

func getJSON(t *testing.T, url string) map[string]any {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return body
}

// THE ALL VIEW WEARS THE DEFAULT, NOT A ROOM'S, before any hub skin is saved.
// alpha sorts first and wears "moss"; a borrow would show that. The hub has no
// skin yet, so the answer is the default, which the board reports as the first
// entry of board_skins.
func TestAllViewSkinIsTheDefaultNotABorrowedRoomSkin(t *testing.T) {
	front, _, done := two(t, settingsRoom("alpha", "moss"), settingsRoom("beta", "ember"))
	defer done()
	front.Config.Handler.(*Proxy).SetInventory(&remembering{})

	body := getJSON(t, front.URL+"/v1/settings")
	if got := body["board_skin"]; got != "harbour" {
		t.Fatalf("the ALL view served skin %q, wanted the default harbour and not a room's", got)
	}
	// The rest of the settings still come from the borrow, untouched.
	if body["editor_command"] == nil {
		t.Fatal("the ALL settings lost the borrowed fields; only the skin should change")
	}
	// The skin list is carried through so the picker still has its options.
	if raw, _ := body["board_skins"].([]any); len(raw) != 5 {
		t.Fatalf("the skin list did not survive: %v", body["board_skins"])
	}
}

// THE HUB'S OWN SKIN WINS, and it is independent of which rooms are attached.
// This is the no-clobber property: the served skin is the hub's whatever the
// rooms wear, so a second room mid-attach cannot swap it.
func TestAllViewServesTheHubSkin(t *testing.T) {
	front, _, done := two(t, settingsRoom("alpha", "moss"), settingsRoom("beta", "ember"))
	defer done()
	front.Config.Handler.(*Proxy).SetInventory(&remembering{skin: "noir"})

	body := getJSON(t, front.URL+"/v1/settings")
	if got := body["board_skin"]; got != "noir" {
		t.Fatalf("the ALL view served skin %q, wanted the hub's noir", got)
	}
}

// A HUB SKIN THAT IS NO LONGER SHIPPED reads as the default rather than as a
// board wearing a name nothing matches. A skin dropped in a later build must not
// leave the ALL view looking like the setting was ignored.
func TestAnUnknownHubSkinClampsToTheDefault(t *testing.T) {
	front, _, done := two(t, settingsRoom("alpha", "moss"), settingsRoom("beta", "ember"))
	defer done()
	front.Config.Handler.(*Proxy).SetInventory(&remembering{skin: "gone-in-a-later-build"})

	body := getJSON(t, front.URL+"/v1/settings")
	if got := body["board_skin"]; got != "harbour" {
		t.Fatalf("an unknown hub skin served as %q, wanted the default harbour", got)
	}
}

// A SKIN SAVED FROM THE ALL VIEW LANDS ON THE HUB, not the 409 it used to get.
func TestSavingASkinInTheAllViewWritesTheHub(t *testing.T) {
	front, _, done := two(t, settingsRoom("alpha", "moss"), settingsRoom("beta", "ember"))
	defer done()
	stock := &remembering{}
	front.Config.Handler.(*Proxy).SetInventory(stock)

	res, err := http.Post(front.URL+"/v1/settings", "application/json",
		bytes.NewReader([]byte(`{"board_skin":"vapor"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("saving a skin from ALL answered %d, wanted 200: %s", res.StatusCode, raw)
	}
	if stock.skin != "vapor" {
		t.Fatalf("the hub skin is %q after the save, wanted vapor", stock.skin)
	}
	// And the answer carries the new skin back, so the board's cached prefs pick
	// it up without a second fetch.
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode save answer: %v", err)
	}
	if body["board_skin"] != "vapor" {
		t.Fatalf("the save answer reported skin %q, wanted vapor", body["board_skin"])
	}
}

// AN UNKNOWN SKIN IS REFUSED AT THE HUB, the same refusal a room makes, so a
// name that would silently wear the default is told at the boundary.
func TestSavingAnUnknownSkinInTheAllViewIsRefused(t *testing.T) {
	front, _, done := two(t, settingsRoom("alpha", "moss"), settingsRoom("beta", "ember"))
	defer done()
	stock := &remembering{}
	front.Config.Handler.(*Proxy).SetInventory(stock)

	res, err := http.Post(front.URL+"/v1/settings", "application/json",
		bytes.NewReader([]byte(`{"board_skin":"chartreuse"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("an unknown skin answered %d, wanted 400", res.StatusCode)
	}
	if stock.skin != "" {
		t.Fatalf("an unknown skin was stored as %q; nothing should have been written", stock.skin)
	}
}

// ONLY THE SKIN GETS THIS TREATMENT. A save that also names a machine-shaped
// setting still has no room to land in and is refused with the same 409, so the
// escape hatch is the skin alone and not a way past `needsARoom`.
func TestASkinSaveMixedWithAnotherSettingStillNeedsARoom(t *testing.T) {
	front, _, done := two(t, settingsRoom("alpha", "moss"), settingsRoom("beta", "ember"))
	defer done()
	stock := &remembering{}
	front.Config.Handler.(*Proxy).SetInventory(stock)

	res, err := http.Post(front.URL+"/v1/settings", "application/json",
		bytes.NewReader([]byte(`{"board_skin":"vapor","editor_command":"vi"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("a mixed save answered %d, wanted the 409 that asks for a room", res.StatusCode)
	}
	if stock.skin != "" {
		t.Fatalf("a mixed save wrote the hub skin %q; it should have been refused whole", stock.skin)
	}
}

// A ROOM-SCOPED REQUEST IS UNTOUCHED. Scoping to a room is a byte pipe: its own
// skin comes back and a save goes to it, with the hub not involved.
func TestARoomScopedSkinRequestGoesToThatRoom(t *testing.T) {
	front, _, done := two(t, settingsRoom("alpha", "moss"), settingsRoom("beta", "ember"))
	defer done()
	front.Config.Handler.(*Proxy).SetInventory(&remembering{skin: "noir"})

	req, _ := http.NewRequest(http.MethodGet, front.URL+"/v1/settings", nil)
	req.Header.Set("X-Atrium-Room", "beta")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if got := body["board_skin"]; got != "ember" {
		t.Fatalf("scoped to beta the skin was %q, wanted beta's own ember, not the hub's", got)
	}
}

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

// mutableRoom answers `/v1/settings` with a skin it will CHANGE on a save, so a
// test can prove a scoped write reached this room and reads back off it. The
// static settingsRoom above proves a read; this one proves a round trip. Every
// other path says which room served it, exactly as settingsRoom does.
type mutableRoom struct {
	name string
	skin string
}

func (m *mutableRoom) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path != "/v1/settings" {
		fmt.Fprintf(w, `{"served_by":%q}`, m.name)
		return
	}
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		var body struct {
			BoardSkin *string `json:"board_skin"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body)
		if body.BoardSkin != nil {
			m.skin = *body.BoardSkin
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"board_skin":     m.skin,
		"board_skins":    []string{"harbour", "moss", "noir", "ember", "vapor", "sandstone"},
		"editor_command": m.name + "-vi",
	})
}

// skinIn reads the skin one scope wears: no room header is the ALL view, a name
// is that room.
func skinIn(t *testing.T, base, room string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, base+"/v1/settings", nil)
	if room != "" {
		req.Header.Set("X-Atrium-Room", room)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode settings for %q: %v", room, err)
	}
	got, _ := body["board_skin"].(string)
	return got
}

// saveSkinIn saves a skin in one scope: no room header lands on the hub, a name
// lands on that room. Fails the test on anything but a 200.
func saveSkinIn(t *testing.T, base, room, skin string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, base+"/v1/settings",
		bytes.NewReader([]byte(fmt.Sprintf(`{"board_skin":%q}`, skin))))
	req.Header.Set("Content-Type", "application/json")
	if room != "" {
		req.Header.Set("X-Atrium-Room", room)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("saving %q in scope %q answered %d, wanted 200: %s", skin, room, res.StatusCode, raw)
	}
	_, _ = io.Copy(io.Discard, res.Body)
}

// THREE SCOPES, THREE SKINS, HELD AT ONCE AND WITHOUT LEAKING. This is the
// property clint asked for: the ALL view, and each of two rooms, wear their own
// skin, a save in one reaches only that scope, and switching scope reads each
// back independently. The ALL skin is the hub's (noir), alpha wears moss, beta
// wears ember, and no save into one is ever seen by the others.
func TestThreeScopesHoldThreeIndependentSkins(t *testing.T) {
	alpha := &mutableRoom{name: "alpha", skin: "moss"}
	beta := &mutableRoom{name: "beta", skin: "ember"}
	front, _, done := two(t, alpha, beta)
	defer done()
	stock := &remembering{skin: "noir"}
	front.Config.Handler.(*Proxy).SetInventory(stock)

	// All three at once, and all three different: the hub's, and each room's own.
	if got := skinIn(t, front.URL, ""); got != "noir" {
		t.Fatalf("the ALL view wore %q, wanted the hub's noir", got)
	}
	if got := skinIn(t, front.URL, "alpha"); got != "moss" {
		t.Fatalf("alpha wore %q, wanted its own moss", got)
	}
	if got := skinIn(t, front.URL, "beta"); got != "ember" {
		t.Fatalf("beta wore %q, wanted its own ember", got)
	}

	// A save scoped to alpha reaches alpha, and no other scope moves.
	saveSkinIn(t, front.URL, "alpha", "vapor")
	if alpha.skin != "vapor" {
		t.Fatalf("saving alpha's skin left it %q, wanted vapor", alpha.skin)
	}
	if stock.skin != "noir" {
		t.Fatalf("saving alpha's skin changed the hub to %q, wanted the untouched noir", stock.skin)
	}
	if beta.skin != "ember" {
		t.Fatalf("saving alpha's skin changed beta to %q, wanted the untouched ember", beta.skin)
	}
	if got := skinIn(t, front.URL, ""); got != "noir" {
		t.Fatalf("after alpha's save the ALL view wore %q, wanted noir", got)
	}
	if got := skinIn(t, front.URL, "beta"); got != "ember" {
		t.Fatalf("after alpha's save beta wore %q, wanted ember", got)
	}
	if got := skinIn(t, front.URL, "alpha"); got != "vapor" {
		t.Fatalf("after alpha's save alpha wore %q, wanted vapor", got)
	}

	// A save in the ALL view reaches the hub, and no room moves.
	saveSkinIn(t, front.URL, "", "sandstone")
	if stock.skin != "sandstone" {
		t.Fatalf("saving the ALL skin left the hub %q, wanted sandstone", stock.skin)
	}
	if alpha.skin != "vapor" {
		t.Fatalf("saving the ALL skin changed alpha to %q, wanted the untouched vapor", alpha.skin)
	}
	if beta.skin != "ember" {
		t.Fatalf("saving the ALL skin changed beta to %q, wanted the untouched ember", beta.skin)
	}
	if got := skinIn(t, front.URL, ""); got != "sandstone" {
		t.Fatalf("after the ALL save the ALL view wore %q, wanted sandstone", got)
	}
	if got := skinIn(t, front.URL, "alpha"); got != "vapor" {
		t.Fatalf("after the ALL save alpha wore %q, wanted vapor", got)
	}
	if got := skinIn(t, front.URL, "beta"); got != "ember" {
		t.Fatalf("after the ALL save beta wore %q, wanted ember", got)
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

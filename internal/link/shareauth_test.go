package link

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// The public-share login is a board-owned setting, held on the hub the same way
// the skin is. It reaches the board through `/v1/settings` in the ALL view, and
// a save lands on the hub rather than getting the `needsARoom` 409. The one rule
// that is not the skin's: the password is never sent back to the board it
// guards, only whether one is set.

// postJSON posts a body to `/v1/settings` and returns the status and decoded
// answer.
func postJSON(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	res, err := http.Post(url, "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

// THE ALL VIEW REPORTS THE HUB'S SHARE LOGIN, and never the password. A stored
// updb credential shows as its scheme, username and a set flag, with the
// password absent from the payload entirely.
func TestAllViewReportsShareAuthWithoutThePassword(t *testing.T) {
	front, _, done := two(t, settingsRoom("alpha", "moss"), settingsRoom("beta", "ember"))
	defer done()
	front.Config.Handler.(*Proxy).SetInventory(&remembering{
		shareAuth: ShareAuth{Scheme: "updb", User: "clint", Pass: "hunter2"},
	})

	body := getJSON(t, front.URL+"/v1/settings")
	if body["share_auth"] != "updb" {
		t.Fatalf("share_auth was %q, wanted updb", body["share_auth"])
	}
	if body["share_user"] != "clint" {
		t.Fatalf("share_user was %q, wanted clint", body["share_user"])
	}
	if set, _ := body["share_pass_set"].(bool); !set {
		t.Fatal("share_pass_set was false with a password stored")
	}
	if _, leaked := body["share_pass"]; leaked {
		t.Fatal("the password was sent to the board; only whether one is set may leave")
	}
}

// SAVING THE UPDB CREDENTIAL FROM ALL LANDS ON THE HUB, not the 409 a
// machine-shaped write gets.
func TestSavingShareUpdbInTheAllViewWritesTheHub(t *testing.T) {
	front, _, done := two(t, settingsRoom("alpha", "moss"), settingsRoom("beta", "ember"))
	defer done()
	stock := &remembering{}
	front.Config.Handler.(*Proxy).SetInventory(stock)

	code, out := postJSON(t, front.URL+"/v1/settings",
		`{"share_auth":"updb","share_user":"clint","share_pass":"hunter2"}`)
	if code != http.StatusOK {
		t.Fatalf("saving the share login answered %d, wanted 200: %v", code, out)
	}
	if stock.shareAuth.Scheme != "updb" || stock.shareAuth.User != "clint" || stock.shareAuth.Pass != "hunter2" {
		t.Fatalf("the hub stored %+v, wanted updb/clint/hunter2", stock.shareAuth)
	}
	if _, leaked := out["share_pass"]; leaked {
		t.Fatal("the save answer echoed the password back")
	}
}

// A BLANK PASSWORD BOX KEEPS THE STORED PASSWORD. Re-saving the username without
// retyping the password does not blank it: blank means "leave it".
func TestSavingShareWithBlankPasswordKeepsStored(t *testing.T) {
	front, _, done := two(t, settingsRoom("alpha", "moss"), settingsRoom("beta", "ember"))
	defer done()
	stock := &remembering{shareAuth: ShareAuth{Scheme: "updb", User: "clint", Pass: "hunter2"}}
	front.Config.Handler.(*Proxy).SetInventory(stock)

	code, out := postJSON(t, front.URL+"/v1/settings",
		`{"share_auth":"updb","share_user":"clint2","share_pass":""}`)
	if code != http.StatusOK {
		t.Fatalf("answered %d, wanted 200: %v", code, out)
	}
	if stock.shareAuth.User != "clint2" {
		t.Fatalf("username did not update: %q", stock.shareAuth.User)
	}
	if stock.shareAuth.Pass != "hunter2" {
		t.Fatalf("a blank password box blanked the stored password: %q", stock.shareAuth.Pass)
	}
}

// UPDB WITH NO PASSWORD AT ALL IS REFUSED, so a public share is never left with
// an empty credential. This is the design's "refuse at configuration time".
func TestSavingShareUpdbWithNoPasswordIsRefused(t *testing.T) {
	front, _, done := two(t, settingsRoom("alpha", "moss"), settingsRoom("beta", "ember"))
	defer done()
	stock := &remembering{}
	front.Config.Handler.(*Proxy).SetInventory(stock)

	code, _ := postJSON(t, front.URL+"/v1/settings",
		`{"share_auth":"updb","share_user":"clint"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("updb with no password answered %d, wanted 400", code)
	}
	if stock.shareAuth.Scheme != "" {
		t.Fatalf("a refused save still wrote %+v", stock.shareAuth)
	}
}

// OIDC NEEDS A PROVIDER, and a save that names none is refused.
func TestSavingShareOIDCWithNoProviderIsRefused(t *testing.T) {
	front, _, done := two(t, settingsRoom("alpha", "moss"), settingsRoom("beta", "ember"))
	defer done()
	stock := &remembering{}
	front.Config.Handler.(*Proxy).SetInventory(stock)

	code, _ := postJSON(t, front.URL+"/v1/settings", `{"share_auth":"oidc"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("oidc with no provider answered %d, wanted 400", code)
	}
}

// AN UNKNOWN SCHEME IS REFUSED at the boundary, the same as an unknown skin.
func TestSavingAnUnknownShareSchemeIsRefused(t *testing.T) {
	front, _, done := two(t, settingsRoom("alpha", "moss"), settingsRoom("beta", "ember"))
	defer done()
	stock := &remembering{}
	front.Config.Handler.(*Proxy).SetInventory(stock)

	code, _ := postJSON(t, front.URL+"/v1/settings", `{"share_auth":"magic"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("an unknown scheme answered %d, wanted 400", code)
	}
	if stock.shareAuth.Scheme != "" {
		t.Fatalf("an unknown scheme was stored as %q", stock.shareAuth.Scheme)
	}
}

// TURNING THE LOGIN OFF (empty scheme) IS ALLOWED to save. The refusal that
// matters is at the point a PUBLIC share is created, not here.
func TestSavingAnEmptyShareSchemeIsAllowed(t *testing.T) {
	front, _, done := two(t, settingsRoom("alpha", "moss"), settingsRoom("beta", "ember"))
	defer done()
	stock := &remembering{shareAuth: ShareAuth{Scheme: "updb", User: "clint", Pass: "hunter2"}}
	front.Config.Handler.(*Proxy).SetInventory(stock)

	code, _ := postJSON(t, front.URL+"/v1/settings", `{"share_auth":""}`)
	if code != http.StatusOK {
		t.Fatalf("clearing the scheme answered %d, wanted 200", code)
	}
	if stock.shareAuth.Scheme != "" {
		t.Fatalf("the scheme is %q after clearing, wanted empty", stock.shareAuth.Scheme)
	}
}

// A SHARE-LOGIN SAVE MIXED WITH A MACHINE-SHAPED SETTING still needs a room, the
// same guard the skin save has: the share login alone is the escape hatch, not a
// way past `needsARoom`.
func TestShareSaveMixedWithAnotherSettingStillNeedsARoom(t *testing.T) {
	front, _, done := two(t, settingsRoom("alpha", "moss"), settingsRoom("beta", "ember"))
	defer done()
	stock := &remembering{}
	front.Config.Handler.(*Proxy).SetInventory(stock)

	res, err := http.Post(front.URL+"/v1/settings", "application/json",
		bytes.NewReader([]byte(`{"share_auth":"updb","share_user":"clint","share_pass":"p","editor_command":"vi"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("a mixed save answered %d, wanted the 409 that asks for a room", res.StatusCode)
	}
	if stock.shareAuth.Scheme != "" {
		t.Fatalf("a mixed save wrote %+v; it should have been refused whole", stock.shareAuth)
	}
}

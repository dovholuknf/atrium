//go:build integration

package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The hub's atrium_launch sends superlean with lean, keeps lean as the default, refuses superlean with lean: false
// before posting anything, and names superlean in the answer when the card that comes back lacks the tag.
func TestHubLaunchSendsSuperleanAndRefusesItWithLeanOff(t *testing.T) {
	type sent struct {
		Lean      bool `json:"lean"`
		Superlean bool `json:"superlean"`
	}
	var got sent
	posts := 0
	applied := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = sent{}
		_ = json.NewDecoder(r.Body).Decode(&got)
		posts++
		card := map[string]any{"id": "card1", "wire_name": "kid", "tags": []string{"atrium:lean"}}
		if applied {
			card["tags"] = []string{"atrium:lean", "atrium:superlean"}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(card)
	}))
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}
	off := false

	for _, tc := range []struct {
		name string
		in   launchInput
		want sent
	}{
		{"default", launchInput{Item: "f-test", Cwd: "/w"}, sent{Lean: true}},
		{"superlean", launchInput{Item: "f-test", Cwd: "/w", Superlean: relaySuperlean(true)}, sent{Lean: true, Superlean: true}},
	} {
		_, out, err := c.launchHandler(context.Background(), ctlReq("a", "beta"), tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: room got %+v, want %+v", tc.name, got, tc.want)
		}
		if strings.Contains(out.Note, "superlean") {
			t.Errorf("%s: the tag came back and it still warned: %q", tc.name, out.Note)
		}
	}

	before := posts
	_, _, err := c.launchHandler(context.Background(), ctlReq("a", "beta"),
		launchInput{Item: "f-test", Cwd: "/w", Superlean: relaySuperlean(true), Lean: &off})
	if err == nil || !strings.Contains(err.Error(), "lean: false") {
		t.Fatalf("superlean with lean: false: err %v", err)
	}
	if posts != before {
		t.Errorf("the refusal posted to the room anyway")
	}

	applied = false
	_, out, err := c.launchHandler(context.Background(), ctlReq("a", "beta"),
		launchInput{Item: "f-test", Cwd: "/w", Superlean: relaySuperlean(true)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Note, "superlean was NOT applied") {
		t.Fatalf("an older room's silence was passed on as success: %q", out.Note)
	}
}

func TestSuperleanDropped(t *testing.T) {
	if SuperleanDropped(false, nil) {
		t.Error("not asked, nothing dropped")
	}
	if SuperleanDropped(true, []string{"atrium:lean", " atrium:superlean "}) {
		t.Error("the tag is there")
	}
	if !SuperleanDropped(true, []string{"atrium:lean"}) {
		t.Error("lean without the tag is a room that ignored the field")
	}
}

func TestLaunchInputSchemaOffersSuperlean(t *testing.T) {
	raw, _ := json.Marshal(launchInput{Superlean: relaySuperlean(true)})
	if !strings.Contains(string(raw), `"superlean":true`) {
		t.Fatalf("launchInput marshals as %s", raw)
	}
}

func TestSuperleanLaunchDefaultsOnForItemWorkersOnly(t *testing.T) {
	off, on := false, true
	for _, tc := range []struct {
		name string
		in   launchInput
		want bool
	}{
		{"plain launch", launchInput{}, false},
		{"item worker", launchInput{Isolated: true}, true},
		{"item worker, superlean false", launchInput{Isolated: true, Superlean: &off}, false},
		{"item worker, lean false", launchInput{Isolated: true, Lean: &off}, false},
		{"item review", launchInput{Isolated: true, Kind: KindReview}, false},
		{"plain, superlean true", launchInput{Superlean: &on}, true},
	} {
		if got := superleanLaunch(tc.in, "claude"); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
	if superleanLaunch(launchInput{Isolated: true}, "codex") {
		t.Error("a codex item worker went superlean")
	}
}

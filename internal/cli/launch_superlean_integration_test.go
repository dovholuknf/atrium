//go:build integration

package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `atrium launch --lean` and `--superlean` reach /v1/launch as lean and superlean, and a launch with neither posts
// neither.
func TestLaunchLeanFlagsReachTheEndpoint(t *testing.T) {
	c := newLaunch()
	for _, f := range []string{"lean", "superlean"} {
		if c.Flags().Lookup(f) == nil {
			t.Fatalf("atrium launch has no --%s", f)
		}
	}
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		got = nil
		_ = json.Unmarshal(raw, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","display_title":"x"}`))
	}))
	defer srv.Close()
	for _, tc := range []struct {
		name        string
		o           launchOpts
		lean, super any
	}{
		{"neither", launchOpts{}, nil, nil},
		{"lean", launchOpts{lean: true}, true, nil},
		{"superlean implies lean", launchOpts{superlean: true}, true, true},
	} {
		tc.o.boardURL, tc.o.harness, tc.o.cwd, tc.o.item, tc.o.quiet = srv.URL, "claude", t.TempDir(), "f-test", true
		if err := launchAgent(tc.o); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got["lean"] != tc.lean || got["superlean"] != tc.super {
			t.Errorf("%s: body carried lean %v superlean %v, want %v %v", tc.name, got["lean"], got["superlean"], tc.lean, tc.super)
		}
	}
}

// The stdio atrium_launch passes superlean to the room as the kit is, and names it in the warning when the card
// that comes back lacks atrium:superlean.
func TestStdioLaunchSendsSuperleanAndWarnsWhenARoomDropsIt(t *testing.T) {
	applied := true
	var sent any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got map[string]any
		_ = json.NewDecoder(r.Body).Decode(&got)
		sent = got["superlean"]
		card := map[string]any{"id": "card1", "wire_name": "kid", "tags": []string{"atrium:lean"}}
		if applied {
			card["tags"] = []string{"atrium:lean", "atrium:superlean"}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(card)
	}))
	defer srv.Close()
	loc := filepath.Join(t.TempDir(), "daemon.json")
	raw, _ := json.Marshal(map[string]any{"board": srv.URL, "pid": os.Getpid()})
	if err := os.WriteFile(loc, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATRIUM_LOCATION", loc)
	t.Setenv("ATRIUM_SHARED_LOCATION", "-")
	in := LaunchInput{Item: "f-test", Cwd: "/w", Superlean: true}

	_, out, err := launchHandler(context.Background(), nil, in)
	if err != nil {
		t.Fatal(err)
	}
	if sent != true || strings.Contains(out.Note, "WARNING") {
		t.Fatalf("sent %v, note %q", sent, out.Note)
	}
	applied = false
	_, out, err = launchHandler(context.Background(), nil, in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Note, "superlean was NOT applied") {
		t.Fatalf("an older room's silence was passed on as success: %q", out.Note)
	}
	// A launch that did not ask sends nothing and warns of nothing.
	sent = "unset"
	_, out, err = launchHandler(context.Background(), nil, LaunchInput{Item: "f-test", Cwd: "/w"})
	if err != nil {
		t.Fatal(err)
	}
	if sent != nil || strings.Contains(out.Note, "superlean") {
		t.Fatalf("an ordinary launch sent %v, note %q", sent, out.Note)
	}
}

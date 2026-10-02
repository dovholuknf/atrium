package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The stdio atrium_launch with `room`, against a fake room board: it goes to this room's
// /v1/peers/launch and touches nothing on this disk. See control_launch_room.go.

type launchBoardFake struct {
	mu sync.Mutex
	// across is the bodies POSTed to /v1/peers/launch, and local the ones to /v1/launch.
	across, local []map[string]any
	// answer is what /v1/peers/launch says, with code, or bare for a room that has no such route.
	answer map[string]any
	code   int
	bare   bool
}

func (b *launchBoardFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	switch r.URL.Path {
	case "/v1/peers/launch":
		if b.bare {
			http.NotFound(w, r)
			return
		}
		b.across = append(b.across, body)
		if b.code != 0 {
			w.WriteHeader(b.code)
		}
		_ = json.NewEncoder(w).Encode(b.answer)
	case "/v1/launch":
		b.local = append(b.local, body)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "L1", "wire_name": "kid", "status": "running", "display_title": "t"})
	default:
		http.NotFound(w, r)
	}
}

func launchAgainst(t *testing.T, b *launchBoardFake, room string) {
	t.Helper()
	srv := httptest.NewServer(b)
	t.Cleanup(srv.Close)
	loc := filepath.Join(t.TempDir(), "daemon.json")
	raw, _ := json.Marshal(map[string]any{"board": srv.URL, "pid": os.Getpid()})
	if err := os.WriteFile(loc, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATRIUM_LOCATION", loc)
	t.Setenv("ATRIUM_SHARED_LOCATION", "-")
	t.Setenv("ATRIUM_AGENT_NAME", "sa1")
	t.Setenv("ATRIUM_ROOM", room)
}

func farLaunch() map[string]any {
	return map[string]any{"card": "claude-sg4~K1", "handle": "kid@claude-sg4", "title": "rev", "status": "running",
		"watch": "http://hub/#term=K1", "brief": "/srv/repo/BRIEF.md", "model": "opus"}
}

// A launch with a room goes to /v1/peers/launch and not /v1/launch, carries the brief and the
// launcher, and looks at nothing here: the directory does not exist on this machine, and
// the working directory it is given is left without a BRIEF.md.
func TestStdioLaunchOnAnotherRoomGoesToThePeersRouteAndTouchesNoDisk(t *testing.T) {
	b := &launchBoardFake{answer: farLaunch()}
	launchAgainst(t, b, "m1mini")
	here := t.TempDir()

	for _, cwd := range []string{"/srv/not/on/this/machine", here} {
		_, out, err := launchHandler(context.Background(), nil, LaunchInput{Cwd: cwd, Room: "claude-sg4",
			Brief: "the brief", Prompt: "go", Title: "rev", Model: "opus", Env: map[string]string{"A": "b"}})
		if err != nil {
			t.Fatalf("launch on %s: %v", cwd, err)
		}
		if out.Card != "claude-sg4~K1" || out.Handle != "kid@claude-sg4" || out.Watch != "http://hub/#term=K1" ||
			out.Brief != "/srv/repo/BRIEF.md" || out.Status != "running" || out.Model != "opus" {
			t.Fatalf("out = %+v", out)
		}
	}
	if len(b.local) != 0 || len(b.across) != 2 {
		t.Fatalf("/v1/launch got %d, /v1/peers/launch got %d", len(b.local), len(b.across))
	}
	got := b.across[0]
	if got["room"] != "claude-sg4" || got["from"] != "sa1" || got["cwd"] != "/srv/not/on/this/machine" ||
		got["brief"] != "the brief" || got["prompt"] != "go" || got["runner"] != "claude" ||
		got["env"].(map[string]any)["A"] != "b" {
		t.Fatalf("the room was sent %+v", got)
	}
	if _, err := os.Stat(filepath.Join(here, "BRIEF.md")); err == nil {
		t.Fatal("a BRIEF.md was written on this machine for a launch on another room")
	}
}

// The dropped-options sentence the hub found comes back ahead of the note.
func TestStdioLaunchOnAnotherRoomCarriesTheHubsWarning(t *testing.T) {
	a := farLaunch()
	a["warning"] = "WARNING: the room is older than launch options, so model was NOT applied. "
	b := &launchBoardFake{answer: a}
	launchAgainst(t, b, "m1mini")
	_, out, err := launchHandler(context.Background(), nil, LaunchInput{Cwd: "/w", Room: "claude-sg4", Model: "opus"})
	if err != nil || !strings.HasPrefix(out.Note, "WARNING: the room is older") || !strings.Contains(out.Note, "started.") {
		t.Fatalf("note = %q, %v", out.Note, err)
	}
}

// This room's own name, or none, is today's path: /v1/launch, and the briefing written
// here before the runner starts.
func TestStdioLaunchOnThisRoomIsTheLocalPathUntouched(t *testing.T) {
	for _, room := range []string{"", "m1mini", "M1MINI"} {
		b := &launchBoardFake{}
		launchAgainst(t, b, "m1mini")
		dir := t.TempDir()
		_, out, err := launchHandler(context.Background(), nil, LaunchInput{Cwd: dir, Room: room, Brief: "hello"})
		if err != nil || out.Card != "L1" {
			t.Fatalf("room %q: %+v, %v", room, out, err)
		}
		if len(b.across) != 0 || len(b.local) != 1 {
			t.Fatalf("room %q: /v1/launch got %d, /v1/peers/launch got %d", room, len(b.local), len(b.across))
		}
		if raw, err := os.ReadFile(filepath.Join(dir, "BRIEF.md")); err != nil || !strings.HasPrefix(string(raw), "hello") {
			t.Fatalf("room %q: the local brief was not written: %v", room, err)
		}
	}
}

// A room that does not know its own name from the environment still has the daemon say
// the room is this one, and the launch carries on locally, with its briefing.
func TestStdioLaunchTheDaemonCallsLocalCarriesOnHere(t *testing.T) {
	b := &launchBoardFake{answer: map[string]any{"local": true}}
	launchAgainst(t, b, "")
	dir := t.TempDir()
	_, out, err := launchHandler(context.Background(), nil, LaunchInput{Cwd: dir, Room: "m1mini", Brief: "hello"})
	if err != nil || out.Card != "L1" || len(b.across) != 1 || len(b.local) != 1 {
		t.Fatalf("out = %+v, %v, across %d local %d", out, err, len(b.across), len(b.local))
	}
	if _, err := os.Stat(filepath.Join(dir, "BRIEF.md")); err != nil {
		t.Fatalf("the local brief was not written: %v", err)
	}
}

// An unknown room is the hub's sentence with the rooms it knows, a known room that is down says
// it is not answering, and nothing was written here.
func TestStdioLaunchRefusalsReachTheCallerInTheirWords(t *testing.T) {
	for _, c := range []struct {
		code int
		msg  string
		want []string
	}{
		{404, `this hub knows no room called "atlantis". rooms: claude-sg4, m1mini`, []string{"atlantis", "claude-sg4", "m1mini"}},
		{503, "room claude-sg4 is not answering (it is not attached), so nothing was started", []string{"not answering"}},
		{504, "it may or may not have started on claude-sg4. look at the peers on that room before launching again", []string{"may or may not"}},
		{502, "the hub is older than launching on another room, so it cannot carry this. update the hub", []string{"older than launching on another room", "update the hub"}},
	} {
		b := &launchBoardFake{code: c.code, answer: map[string]any{"error": c.msg}}
		launchAgainst(t, b, "m1mini")
		here := t.TempDir()
		_, _, err := launchHandler(context.Background(), nil, LaunchInput{Cwd: here, Room: "atlantis", Brief: "b"})
		if err == nil {
			t.Fatalf("%d: no error", c.code)
		}
		for _, w := range c.want {
			if !strings.Contains(err.Error(), w) {
				t.Fatalf("%d: err = %q, want %q in it", c.code, err, w)
			}
		}
		if _, serr := os.Stat(filepath.Join(here, "BRIEF.md")); serr == nil || len(b.local) != 0 {
			t.Fatalf("%d: a refused launch touched this room (local %d)", c.code, len(b.local))
		}
	}
}

// This room predates the route: it says so and does not launch here by mistake.
func TestStdioLaunchOnARoomOlderThanTheRouteSaysSo(t *testing.T) {
	b := &launchBoardFake{bare: true}
	launchAgainst(t, b, "m1mini")
	_, _, err := launchHandler(context.Background(), nil, LaunchInput{Cwd: "/w", Room: "claude-sg4"})
	if err == nil || !strings.Contains(err.Error(), "this room is older than launching on another room") ||
		!strings.Contains(err.Error(), "claude-sg4") || len(b.local) != 0 {
		t.Fatalf("err = %v, local %d", err, len(b.local))
	}
}

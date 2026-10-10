//go:build integration

package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func roomLaunch(t *testing.T, d *Daemon, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	d.handleRoomLaunch(rec, httptest.NewRequest(http.MethodPost, "/v1/peers/launch", bytes.NewReader(raw)))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func launchBody(room string) map[string]any {
	return map[string]any{"room": room, "from": "sa1", "cwd": "/srv/repo", "title": "review", "prompt": "do it",
		"brief": "the brief", "runner": "claude", "tags": []string{"atrium:subagent"}, "model": "opus",
		"env": map[string]string{"A": "b"}, "lean_agents": []string{"x"}}
}

// The launch goes to the hub whole, from the launcher, for the room it named, and the answer
// names the card across. The brief is carried and the cwd is not looked at.
func TestARoomLaunchGoesToTheHubWholeAndNamesTheCardAcross(t *testing.T) {
	d, f := roomDaemon(t)
	code, out := roomLaunch(t, d, launchBody("claude-sg4"))
	if code != http.StatusOK || out["card"] != "claude-sg4~kid" || out["handle"] != "kid@claude-sg4" ||
		out["watch"] != "http://hub/#term=kid" || out["status"] != "running" || out["title"] != "review" ||
		out["model"] != "opus" {
		t.Fatalf("answer = %d %+v", code, out)
	}
	got := f.launched()
	if len(got) != 1 {
		t.Fatalf("the hub was asked %d times", len(got))
	}
	l := got[0]
	if l.From != "sa1" || l.Room != "claude-sg4" || l.Cwd != "/srv/repo" || l.Brief != "the brief" ||
		l.Prompt != "do it" || l.Runner != "claude" || l.Model != "opus" || l.Env["A"] != "b" ||
		len(l.Tags) != 1 || len(l.LeanAgents) != 1 {
		t.Fatalf("the hub got %+v", l)
	}
}

// This room's own name, in any case, is no launch across: nothing reaches the hub, and
// the caller carries on locally.
func TestARoomLaunchOnThisRoomIsLocalAndAsksNobody(t *testing.T) {
	d, f := roomDaemon(t)
	for _, room := range []string{"m1mini", "M1MINI", " m1mini "} {
		code, out := roomLaunch(t, d, launchBody(room))
		if code != http.StatusOK || out["local"] != true || out["card"] != nil {
			t.Fatalf("%q = %d %+v", room, code, out)
		}
	}
	if n := len(f.launched()); n != 0 {
		t.Fatalf("the hub was asked %d times for a launch on this room", n)
	}
}

func TestARoomLaunchNeedsADirectoryAndALink(t *testing.T) {
	d, f := roomDaemon(t)
	b := launchBody("claude-sg4")
	b["cwd"] = " "
	if code, _ := roomLaunch(t, d, b); code != http.StatusBadRequest || len(f.launched()) != 0 {
		t.Fatalf("no cwd = %d, the hub asked %d times", code, len(f.launched()))
	}
	d.SetRelay(nil)
	code, out := roomLaunch(t, d, launchBody("claude-sg4"))
	if code != http.StatusServiceUnavailable || !strings.Contains(out["error"].(string), "claude-sg4") {
		t.Fatalf("no link = %d %+v", code, out)
	}
}

// A refusal keeps the target's code and sentence, so an unknown room's list of rooms and the
// cap's reason reach the launcher as they were written.
func TestARoomLaunchRefusalKeepsItsCodeAndWords(t *testing.T) {
	for _, c := range []struct {
		code int
		msg  string
	}{
		{404, `this hub knows no room called "atlantis". rooms: claude-sg4, m1mini`},
		{409, "at the launch cap of 10 running workers on room claude-sg4. wait for one to finish"},
		{400, "is not a directory"},
	} {
		d, f := roomDaemon(t)
		f.launch = func(RelayLaunch) (RelayResult, error) { return RelayResult{Code: c.code, Error: c.msg}, nil }
		code, out := roomLaunch(t, d, launchBody("atlantis"))
		if code != c.code || out["error"] != c.msg {
			t.Fatalf("%d: got %d %+v", c.code, code, out)
		}
		if len(owed(t, d)) != 0 {
			t.Fatal("a refused launch was held")
		}
	}
}

// A room the hub knows and cannot reach, and a hub that does not answer, are refused now
// and never held: sent late, a launch starts a session nobody is waiting for.
func TestARoomLaunchThatCannotBeTakenIsRefusedNotHeld(t *testing.T) {
	d, f := roomDaemon(t)
	f.launch = func(RelayLaunch) (RelayResult, error) {
		return RelayResult{Unreachable: true, Code: 503, Error: "the room claude-sg4 is not attached to the hub right now"}, nil
	}
	code, out := roomLaunch(t, d, launchBody("claude-sg4"))
	msg, _ := out["error"].(string)
	if code != http.StatusServiceUnavailable || !strings.Contains(msg, "not answering") ||
		!strings.Contains(msg, "claude-sg4") || !strings.Contains(msg, "nothing is held") {
		t.Fatalf("down room = %d %q", code, msg)
	}
	f.launch = func(RelayLaunch) (RelayResult, error) { return RelayResult{}, ErrRelayDown }
	code, out = roomLaunch(t, d, launchBody("claude-sg4"))
	msg, _ = out["error"].(string)
	if code != http.StatusServiceUnavailable || !strings.Contains(msg, "hub is not answering") {
		t.Fatalf("down hub = %d %q", code, msg)
	}
	if len(owed(t, d)) != 0 {
		t.Fatal("a launch was held for later")
	}
	if n := len(f.launched()); n != 2 {
		t.Fatalf("the hub was asked %d times, want once per call and no retry", n)
	}
}

// A launch that may have started is unconfirmed, never retried, and says to look first.
func TestARoomLaunchThatMayHaveStartedIsUnconfirmedAndNotRetried(t *testing.T) {
	for name, fn := range map[string]func(RelayLaunch) (RelayResult, error){
		"answer": func(RelayLaunch) (RelayResult, error) {
			return RelayResult{Unconfirmed: true, Code: 504, Error: "the room timed out"}, nil
		},
		"error": func(RelayLaunch) (RelayResult, error) { return RelayResult{}, ErrRelayUnconfirmed },
	} {
		d, f := roomDaemon(t)
		f.launch = fn
		code, out := roomLaunch(t, d, launchBody("claude-sg4"))
		msg, _ := out["error"].(string)
		if code != http.StatusGatewayTimeout || !strings.Contains(msg, "may or may not have started") ||
			!strings.Contains(msg, "before launching again") {
			t.Fatalf("%s: %d %q", name, code, msg)
		}
		settle(d)
		if n := len(f.launched()); n != 1 || len(owed(t, d)) != 0 {
			t.Fatalf("%s: the hub was asked %d times, %d owed", name, n, len(owed(t, d)))
		}
	}
}

// An old hub, found out either way, is told to the launcher as too old to launch on
// another room, naming what to do.
func TestARoomLaunchThroughAnOldHubSaysToUpdateIt(t *testing.T) {
	for name, fn := range map[string]func(RelayLaunch) (RelayResult, error){
		"refused at hello": func(RelayLaunch) (RelayResult, error) { return RelayResult{}, ErrRelayOld },
		"unknown op": func(RelayLaunch) (RelayResult, error) {
			return RelayResult{Code: 400, Error: `this hub does not know the relay op "launch"`}, nil
		},
	} {
		d, f := roomDaemon(t)
		f.launch = fn
		code, out := roomLaunch(t, d, launchBody("claude-sg4"))
		msg, _ := out["error"].(string)
		if code != http.StatusBadGateway || !strings.Contains(msg, "older than launching on another room") ||
			!strings.Contains(msg, "update the hub") {
			t.Fatalf("%s: %d %q", name, code, msg)
		}
	}
	// Anything else that goes wrong is a refusal in its own words.
	d, f := roomDaemon(t)
	f.launch = func(RelayLaunch) (RelayResult, error) { return RelayResult{}, errors.New("boom") }
	if code, out := roomLaunch(t, d, launchBody("claude-sg4")); code != http.StatusBadGateway || out["error"] != "boom" {
		t.Fatalf("other error = %d %+v", code, out)
	}
}

//go:build integration

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

type stdioBoard struct {
	mu      sync.Mutex
	noSay   bool
	said    []map[string]string
	message []map[string]string
	report  map[string]string
	// noAcross is a room older than /v1/peers/card and /v1/peers/exit. across
	// is the addresses they were asked, and exited the local card ids exited.
	noAcross bool
	across   []string
	exited   []string
	// peersAsked is the queries /v1/peers/rooms got, and rooms what it answers.
	peersAsked []string
	rooms      []map[string]any
}

// acrossAnswer is what the fake room says to an address: local when it names
// m1mini, reached across otherwise.
func acrossAnswer(to string) map[string]any {
	name, room := to, ""
	if i := strings.LastIndexAny(to, "@~"); i >= 0 {
		if to[i] == '@' {
			name, room = to[:i], to[i+1:]
		} else {
			name, room = to[i+1:], to[:i]
		}
	}
	if strings.EqualFold(room, "m1mini") {
		return map[string]any{"local": name}
	}
	return nil
}

func (b *stdioBoard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/v1/say" && !b.noSay:
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		b.said = append(b.said, body)
		_ = json.NewEncoder(w).Encode(map[string]any{"delivered": "held", "to": body["to"]})
	case r.URL.Path == "/v1/peers/rooms" && b.rooms != nil:
		b.peersAsked = append(b.peersAsked, r.URL.RawQuery)
		_ = json.NewEncoder(w).Encode(map[string]any{"peers": b.rooms})
	case r.URL.Path == "/v1/tasks":
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{
			{"id": "c1", "wire_name": "sa1", "status": "working"},
			{"id": "c2", "wire_name": "sa2", "status": "working"},
		}})
	case r.URL.Path == "/v1/tasks/c2/message":
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		b.message = append(b.message, body)
		_ = json.NewEncoder(w).Encode(map[string]any{"delivered": "queued", "when": "immediate"})
	case r.URL.Path == "/v1/peers/card" && !b.noAcross:
		to := r.URL.Query().Get("to")
		b.across = append(b.across, "card "+to+" events="+r.URL.Query().Get("events"))
		if local := acrossAnswer(to); local != nil {
			_ = json.NewEncoder(w).Encode(local)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"task": map[string]any{
			"card": "claude-sg4~L1", "handle": "orch@claude-sg4", "status": "working",
			"events": []map[string]any{{"at": "t", "kind": "launched"}},
		}})
	case r.URL.Path == "/v1/peers/exit" && !b.noAcross:
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		b.across = append(b.across, "exit "+body["to"])
		if local := acrossAnswer(body["to"]); local != nil {
			_ = json.NewEncoder(w).Encode(local)
			return
		}
		if strings.HasPrefix(body["to"], "nobody@") {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "no session called \"nobody\". these would have worked: orch"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"asked": true, "card": "claude-sg4~L1", "handle": "orch@claude-sg4"})
	case r.URL.Path == "/v1/tasks/c2/exit":
		b.exited = append(b.exited, "c2")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	case r.URL.Path == "/v1/tasks/c2":
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "c2", "wire_name": "sa2", "status": "working"})
	case r.URL.Path == "/v1/tasks/c1/report":
		_ = json.NewDecoder(r.Body).Decode(&b.report)
		_ = json.NewEncoder(w).Encode(map[string]any{"recorded": true, "status": "done", "launcher_told": true})
	default:
		w.Header().Set("Content-Type", "text/plain")
		http.NotFound(w, r)
	}
}

func stdioAgainst(t *testing.T, b *stdioBoard, room string) {
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

// RULE 3 AND RULE 2. The stdio say names its sender, so it is never typed as
// the operator, and passes `name@room` to the room to relay.
func TestStdioSayNamesTheSenderAndTakesAnotherRoom(t *testing.T) {
	b := &stdioBoard{}
	stdioAgainst(t, b, "m1mini")

	_, out, err := sayHandler(context.Background(), nil, SayInput{To: "atrium-87300@claude-sg4", Text: "hi"})
	if err != nil {
		t.Fatalf("say: %v", err)
	}
	if len(b.said) != 1 || b.said[0]["from"] != "sa1" || b.said[0]["to"] != "atrium-87300@claude-sg4" {
		t.Fatalf("the room got %+v", b.said)
	}
	if out.Delivered != "held" || !strings.Contains(out.Note, "held") {
		t.Fatalf("out = %+v", out)
	}
}

// A room older than /v1/say still takes a bare name, now with the sender, and
// refuses another room with a sentence.
func TestStdioSayAgainstAnOlderRoom(t *testing.T) {
	b := &stdioBoard{noSay: true}
	stdioAgainst(t, b, "m1mini")

	_, out, err := sayHandler(context.Background(), nil, SayInput{To: "sa2", Text: "hi"})
	if err != nil || out.ToCard != "c2" {
		t.Fatalf("say = %+v, %v", out, err)
	}
	if len(b.message) != 1 || b.message[0]["from"] != "sa1" {
		t.Fatalf("the message went as %+v, want it from sa1", b.message)
	}
	_, _, err = sayHandler(context.Background(), nil, SayInput{To: "x@claude-sg4", Text: "hi"})
	if err == nil || !strings.Contains(err.Error(), "older") {
		t.Fatalf("err = %v, want the room called older", err)
	}
}

// atrium_report on the stdio server files on the caller's own card.
func TestStdioReportFilesOnTheCallersCard(t *testing.T) {
	b := &stdioBoard{}
	stdioAgainst(t, b, "m1mini")

	_, out, err := reportHandler(context.Background(), nil, ReportInput{Status: "done", Summary: "did it", SHA: "abc"})
	if err != nil || !out.Recorded || !out.LauncherTold {
		t.Fatalf("report = %+v, %v", out, err)
	}
	if b.report["status"] != "done" || b.report["recap"] != "did it" || b.report["sha"] != "abc" {
		t.Fatalf("the room got %+v", b.report)
	}
}

// ITEM 68. The stdio atrium_task and atrium_exit take a card on another room,
// asked of this room, which asks its hub. A bare name never goes that way.
func TestStdioTaskAndExitReachAnotherRoom(t *testing.T) {
	b := &stdioBoard{}
	stdioAgainst(t, b, "m1mini")

	_, task, err := taskHandler(context.Background(), nil, TaskInput{Card: "orch@claude-sg4", Events: true})
	if err != nil || task.Card != "claude-sg4~L1" || task.Handle != "orch@claude-sg4" || len(task.Events) != 1 ||
		task.Note == "" {
		t.Fatalf("task = %+v, %v", task, err)
	}
	_, out, err := exitHandler(context.Background(), nil, ExitInput{Card: "claude-sg4~01ABC"})
	if err != nil || !out.Asked || out.Card != "claude-sg4~L1" || out.Handle != "orch@claude-sg4" {
		t.Fatalf("exit = %+v, %v", out, err)
	}
	if len(b.across) != 2 || b.across[0] != "card orch@claude-sg4 events=1" || b.across[1] != "exit claude-sg4~01ABC" {
		t.Fatalf("the room was asked %v", b.across)
	}
	_, _, err = exitHandler(context.Background(), nil, ExitInput{Card: "nobody@claude-sg4"})
	if err == nil || !strings.Contains(err.Error(), "orch") {
		t.Fatalf("err = %v, want the far room's refusal", err)
	}

	// This room's own name, and a bare name, are found here.
	b.across = nil
	for _, card := range []string{"sa2@M1MINI", "sa2"} {
		_, task, err = taskHandler(context.Background(), nil, TaskInput{Card: card})
		if err != nil || task.Card != "c2" {
			t.Fatalf("task %q = %+v, %v", card, task, err)
		}
		if _, out, err = exitHandler(context.Background(), nil, ExitInput{Card: card}); err != nil || out.Card != "c2" {
			t.Fatalf("exit %q = %+v, %v", card, out, err)
		}
	}
	if len(b.exited) != 2 || len(b.across) != 2 {
		t.Fatalf("exited %v, asked across %v: want two local exits, and only the addressed ones asked", b.exited, b.across)
	}
}

// A room older than this still finds its own name here, and says why it
// cannot reach another room.
func TestStdioTaskAndExitAgainstAnOlderRoom(t *testing.T) {
	b := &stdioBoard{noAcross: true}
	stdioAgainst(t, b, "m1mini")

	if _, out, err := exitHandler(context.Background(), nil, ExitInput{Card: "sa2@m1mini"}); err != nil || out.Card != "c2" {
		t.Fatalf("exit on own room = %+v, %v", out, err)
	}
	_, _, err := taskHandler(context.Background(), nil, TaskInput{Card: "orch@claude-sg4"})
	if err == nil || !strings.Contains(err.Error(), "older") {
		t.Fatalf("err = %v, want the room called older", err)
	}
}

// atrium_peers without `rooms` appends the everywhere cards its room lists, and
// a room that predates them leaves the list as it is.
func TestStdioPeersAppendTheEverywhereCards(t *testing.T) {
	b := &stdioBoard{rooms: []map[string]any{{"handle": "atrium-87300@claude-sg4", "card": "claude-sg4~s1",
		"room": "claude-sg4", "status": "running", "everywhere": true}}}
	stdioAgainst(t, b, "m1mini")

	_, out, err := peersHandler(context.Background(), nil, PeersInput{})
	if err != nil {
		t.Fatal(err)
	}
	last := out.Peers[len(out.Peers)-1]
	if last.Handle != "atrium-87300@claude-sg4" || !last.Everywhere || len(out.Peers) != 2 {
		t.Fatalf("peers = %+v", out.Peers)
	}
	if len(b.peersAsked) != 1 || b.peersAsked[0] != "everywhere=1" {
		t.Fatalf("the room was asked %v", b.peersAsked)
	}

	old := &stdioBoard{}
	stdioAgainst(t, old, "m1mini")
	_, out, err = peersHandler(context.Background(), nil, PeersInput{})
	if err != nil || len(out.Peers) != 1 {
		t.Fatalf("an older room's list = %+v, %v", out.Peers, err)
	}
}

// atrium_exit with no card exits this session, by the name the room launched it
// with. And a say reply names the recipient as to_card, never card.
func TestStdioExitWithNoCardExitsTheCaller(t *testing.T) {
	b := &stdioBoard{}
	stdioAgainst(t, b, "m1mini")
	t.Setenv("ATRIUM_AGENT_NAME", "sa2")
	_, out, err := exitHandler(context.Background(), nil, ExitInput{})
	if err != nil || !out.Asked || out.Card != "c2" {
		t.Fatalf("exit with no card = %+v, %v", out, err)
	}
	if len(b.exited) != 1 || b.exited[0] != "c2" {
		t.Fatalf("exited %v, want only the caller's c2", b.exited)
	}
	t.Setenv("ATRIUM_AGENT_NAME", "")
	if _, _, err := exitHandler(context.Background(), nil, ExitInput{}); err == nil {
		t.Fatal("an exit with no card and no session name was accepted")
	}
	raw, _ := json.Marshal(SayOutput{To: "sa2", ToCard: "c2"})
	if strings.Contains(string(raw), `"card"`) || !strings.Contains(string(raw), `"to_card"`) {
		t.Fatalf("a say reply is %s: the recipient must be to_card and there must be no card", raw)
	}
}

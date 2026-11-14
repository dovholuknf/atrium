//go:build integration

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/testwait"
	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/link"
	"github.com/dovholuknf/atrium/internal/link/conn"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

type handRoom struct {
	d     *daemon.Daemon
	agent string
	human string
}

// handRoomOn starts a real room daemon on scratch ports and attaches it to the hub at hubAddr.
func handRoomOn(t *testing.T, ctx context.Context, name, hubAddr string, tm wire.Timings) *handRoom {
	t.Helper()
	agent, human := freeAddrs(t)
	dir := t.TempDir()
	d, err := daemon.New(daemon.Options{AgentAddr: agent, HumanAddr: human,
		DBPath: filepath.ToSlash(filepath.Join(dir, "atrium.db")), LongPoll: 2 * time.Second,
		LocationFile: filepath.Join(dir, "daemon.json")})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	rctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { _ = d.Run(rctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done; d.Close() })
	testwait.EventuallyEvery(t, 20*time.Millisecond, 5*time.Second, name+" to answer", func() bool {
		res, err := http.Get("http://" + human + "/v1/tasks")
		if err != nil {
			return false
		}
		res.Body.Close()
		return res.StatusCode == http.StatusOK
	})
	room := &conn.Room{Name: name, Version: "b", Dial: tcpDial{hubAddr}, Handler: d.BoardHandler(), T: tm}
	d.SetRelay(linkRelay{room: room})
	go func() { _ = room.Run(ctx) }()
	testwait.EventuallyEvery(t, 20*time.Millisecond, 5*time.Second, name+" to attach", func() bool { return room.State().Up })
	return &handRoom{d: d, agent: agent, human: human}
}

func (r *handRoom) hook(t *testing.T, path string, body map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	res, err := http.Post("http://"+r.agent+path, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
}

type handCard struct {
	ID     string `json:"id"`
	Name   string `json:"wire_name"`
	Status string `json:"status"`
	PID    int    `json:"pid"`
	Act    *struct {
		Tool string `json:"tool"`
	} `json:"activity"`
}

func (r *handRoom) cards(t *testing.T) []handCard {
	t.Helper()
	res, err := http.Get("http://" + r.human + "/v1/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		Tasks []handCard `json:"tasks"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body.Tasks
}

func (r *handRoom) waitCard(t *testing.T, what string, ok func(handCard) bool) handCard {
	t.Helper()
	var got handCard
	testwait.EventuallyEvery(t, 20*time.Millisecond, 5*time.Second, what, func() bool {
		for _, c := range r.cards(t) {
			if ok(c) {
				got = c
				return true
			}
		}
		return false
	})
	return got
}

// A hand `claude --resume` of room B's dead card, whose hooks reach room A, revives B's card with the new pid, and its
// end marks that card dead. Room A never gets a card of its own for it.
func TestAHandResumeOfAnotherRoomsCardReachesItsCard(t *testing.T) {

	log.SetOutput(io.Discard)
	defer log.SetOutput(os.Stderr)
	tm := wire.Timings{Beat: 200 * time.Millisecond, Silence: time.Second, Warm: 1, Backoff: 50 * time.Millisecond}
	hub := link.NewHub(tm)
	proxy := link.NewProxy(hub, nil, "", nil)
	boardLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer boardLn.Close()
	go func() { _ = http.Serve(boardLn, proxy) }()
	proxy.SetControl(boardLn.Addr().String())
	hubLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer hubLn.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = hub.Serve(ctx, hubLn) }()

	a := handRoomOn(t, ctx, "roomA", hubLn.Addr().String(), tm)
	b := handRoomOn(t, ctx, "roomB", hubLn.Addr().String(), tm)

	// B's card: a session that started and ended, in a folder that is gone, so it is dead and holds its conversation.
	gone := filepath.ToSlash(filepath.Join(t.TempDir(), "gone"))
	b.hook(t, "/session", map[string]any{"agent": "bcard", "event": "start", "resume": "conv-1", "pid": 999999,
		"cwd": gone, "resumable": true})
	b.hook(t, "/session", map[string]any{"agent": "bcard", "event": "end", "reason": "other", "pid": 999999,
		"cwd": gone, "resume": "conv-1"})
	card := b.waitCard(t, "B's card to be dead", func(c handCard) bool { return c.Name == "bcard" && c.Status == "dead" })

	// The hand session: its hooks reach A.
	pid := os.Getpid()
	a.hook(t, "/session", map[string]any{"agent": "hand-x", "event": "start", "resume": "conv-1", "pid": pid,
		"cwd": gone, "resumable": true, "source": "resume"})
	revived := b.waitCard(t, "B's card to take the new pid", func(c handCard) bool { return c.ID == card.ID && c.PID == pid })
	if revived.Status == "dead" {
		t.Fatalf("B's card is still %s", revived.Status)
	}
	if n := len(a.cards(t)); n != 0 {
		t.Fatalf("room A made %d card(s) of its own for a session B's card holds", n)
	}

	a.hook(t, "/activity", map[string]any{"agent": "hand-x", "event": "tool-start", "tool": "Bash", "pid": pid})
	b.waitCard(t, "B's card to be running", func(c handCard) bool { return c.ID == card.ID && c.Status == "running" })

	a.hook(t, "/session", map[string]any{"agent": "hand-x", "event": "end", "reason": "other", "pid": pid, "cwd": gone,
		"resume": "conv-1"})
	b.waitCard(t, "B's card to be dead again", func(c handCard) bool { return c.ID == card.ID && c.Status == "dead" })
	if n := len(a.cards(t)); n != 0 {
		t.Fatalf("room A holds %d card(s) after the end", n)
	}
}

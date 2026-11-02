//go:build integration

package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// keyedRoom is a room that files the card late: the first launch loses its answer and the card is not on the list
// yet. It honours `launch_key` the way the real room does, answering a repeat with the card filed under it.
type keyedRoom struct {
	mu    sync.Mutex
	keys  []string
	filed map[string]string
	made  int
}

func (k *keyedRoom) serve(t *testing.T) *httptest.Server {
	k.filed = map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		k.mu.Lock()
		defer k.mu.Unlock()
		switch {
		case r.URL.Path == "/v1/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/launch":
			var body struct {
				Key string `json:"launch_key"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			k.keys = append(k.keys, body.Key)
			if id, ok := k.filed[body.Key]; ok && body.Key != "" {
				_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "launch_replay": true})
				return
			}
			k.made++
			k.filed[body.Key] = "c-" + body.Key[:4]
			w.WriteHeader(http.StatusBadGateway) // the answer is lost, and the card is filed behind it
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// THE WINDOW THE SNAPSHOT CANNOT CLOSE: the card is not on the list when the answer is lost, so looking finds
// nothing. The retry sends the same key and the room hands back the card it filed, so there is one worker.
func TestARetryAfterALostAnswerSendsTheSameKeyAndGetsTheSameCard(t *testing.T) {
	room := &keyedRoom{}
	srv := room.serve(t)
	c := &controlMCP{board: srv.URL, client: srv.Client()}
	in := launchInput{Item: "f-test", Cwd: "c:/work/keyed-one"}

	if _, err := c.LaunchOnRoom(context.Background(), in, "claude", "", "", "", "", ""); err == nil {
		t.Fatal("a lost answer with no card to find succeeded")
	}
	out, err := c.LaunchOnRoom(context.Background(), in, "claude", "", "", "", "", "")
	if err != nil {
		t.Fatalf("the retry failed: %v", err)
	}
	if room.made != 1 {
		t.Fatalf("the room made %d cards, want 1", room.made)
	}
	if len(room.keys) != 2 || room.keys[0] == "" || room.keys[0] != room.keys[1] {
		t.Fatalf("the keys sent were %v, want one key twice", room.keys)
	}
	if out.Card != "c-"+room.keys[0][:4] || !out.Recovered {
		t.Fatalf("out = %+v", out)
	}
}

// An answered launch is finished: the next launch in that directory is a new one, with a new key.
func TestAnAnsweredLaunchDoesNotKeepItsKey(t *testing.T) {
	room := &keyedRoom{}
	srv := room.serve(t)
	c := &controlMCP{board: srv.URL, client: srv.Client()}
	in := launchInput{Item: "f-test", Cwd: "c:/work/keyed-two"}

	_, _ = c.LaunchOnRoom(context.Background(), in, "claude", "", "", "", "", "") // lost, key kept
	if _, err := c.LaunchOnRoom(context.Background(), in, "claude", "", "", "", "", ""); err != nil {
		t.Fatal(err) // answered by replay, key let go
	}
	_, _ = c.LaunchOnRoom(context.Background(), in, "claude", "", "", "", "", "")
	if len(room.keys) != 3 || room.keys[2] == room.keys[0] {
		t.Fatalf("a finished launch kept its key: %v", room.keys)
	}
	// And another directory never shares one.
	_, _ = c.LaunchOnRoom(context.Background(), launchInput{Item: "f-test", Cwd: "c:/work/keyed-three"}, "claude", "", "", "", "", "")
	if last := room.keys[len(room.keys)-1]; last == room.keys[0] || last == room.keys[2] {
		t.Fatalf("two directories shared a key: %v", room.keys)
	}
}

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
	"time"
)

// slowRoom is a board whose launch outlasts the caller's bound. `lateCard` is when the card turns up on the
// list: with the post (false) or only afterwards (true, set by the test).
type slowRoom struct {
	mu    sync.Mutex
	cards []map[string]any
	posts int
	hang  time.Duration
	cwd   string
}

func (s *slowRoom) add() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cards = append(s.cards, map[string]any{"id": "c9", "wire_name": "kid", "worktree": s.cwd,
		"created_at": "1999-01-01T00:00:00Z"}) // a clock nowhere near the caller's
}

func (s *slowRoom) serve(t *testing.T, started bool) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tasks":
			s.mu.Lock()
			defer s.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": s.cards})
		case r.URL.Path == "/v1/launch":
			s.mu.Lock()
			s.posts++
			s.mu.Unlock()
			if started {
				s.add()
			}
			time.Sleep(s.hang) // wait: a room slow to answer the launch is what is under test
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "c9"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	loc := filepath.Join(t.TempDir(), "daemon.json")
	raw, _ := json.Marshal(map[string]any{"board": srv.URL, "pid": os.Getpid()})
	if err := os.WriteFile(loc, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATRIUM_LOCATION", loc)
	t.Setenv("ATRIUM_SHARED_LOCATION", "-")
}

func shortLaunchWait(t *testing.T) { t.Setenv("ATRIUM_LAUNCH_WAIT", "100ms") }

// The room starts the card and answers after the caller's bound: the launch returns that card.
func TestAStdioLaunchThatOutlastsItsBoundReturnsTheCardItStarted(t *testing.T) {
	shortLaunchWait(t)
	room := &slowRoom{hang: 400 * time.Millisecond, cwd: "/w/dir0"}
	room.serve(t, true)

	_, out, err := launchHandler(context.Background(), nil, LaunchInput{Item: "f-test", Cwd: room.cwd})
	if err != nil {
		t.Fatalf("a launch that started was reported as failed: %v", err)
	}
	if out.Card != "c9" || !strings.Contains(out.Note, "do NOT launch it again") {
		t.Fatalf("out = %+v", out)
	}
	if room.posts != 1 {
		t.Fatalf("posted %d times, want 1", room.posts)
	}
}

// The card shows up only after the caller gave up: the failure stays, and the retry returns the card
// without posting again.
func TestARetryAfterATimedOutLaunchReturnsTheCardThatAppearedLate(t *testing.T) {
	shortLaunchWait(t)
	room := &slowRoom{hang: 400 * time.Millisecond, cwd: "/w/dir1"}
	room.serve(t, false)

	if _, _, err := launchHandler(context.Background(), nil, LaunchInput{Item: "f-test", Cwd: room.cwd}); err == nil {
		t.Fatal("a launch with no card and no answer succeeded")
	}
	room.add() // the room finishes behind the caller

	_, out, err := launchHandler(context.Background(), nil, LaunchInput{Item: "f-test", Cwd: room.cwd})
	if err != nil || out.Card != "c9" {
		t.Fatalf("retry = %+v, %v", out, err)
	}
	if room.posts != 1 {
		t.Fatalf("the retry posted again: %d posts", room.posts)
	}
}

// A card that was already in the directory is not mistaken for the launch.
func TestACardAlreadyThereIsNotTakenForTheLaunch(t *testing.T) {
	shortLaunchWait(t)
	room := &slowRoom{hang: 400 * time.Millisecond, cwd: "/w/dir2"}
	room.serve(t, false)
	room.add()

	if _, _, err := launchHandler(context.Background(), nil, LaunchInput{Item: "f-test", Cwd: room.cwd}); err == nil {
		t.Fatal("an old card in the directory was taken for a launch that never made one")
	}
}

// THE KEY CLOSES WHAT THE SNAPSHOT CANNOT. The card is not on the list when the answer is lost, so looking finds
// nothing, and the retry sends the key it made the first time. The room that files by key hands back that card.
func TestARetryAfterALostAnswerSendsTheKeyAgain(t *testing.T) {
	shortLaunchWait(t)
	var mu sync.Mutex
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/v1/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []any{}})
		case "/v1/launch":
			var body struct {
				Key string `json:"launch_key"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			keys = append(keys, body.Key)
			if len(keys) == 1 {
				time.Sleep(400 * time.Millisecond) // wait: a first answer lost to the launch wait is what is under test
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "c9", "launch_replay": len(keys) > 1})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	loc := filepath.Join(t.TempDir(), "daemon.json")
	raw, _ := json.Marshal(map[string]any{"board": srv.URL, "pid": os.Getpid()})
	if err := os.WriteFile(loc, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATRIUM_LOCATION", loc)
	t.Setenv("ATRIUM_SHARED_LOCATION", "-")

	cwd := "/w/keyed-dir"
	if _, _, err := launchHandler(context.Background(), nil, LaunchInput{Item: "f-test", Cwd: cwd}); err == nil {
		t.Fatal("a launch that outlasted its bound succeeded")
	}
	_, out, err := launchHandler(context.Background(), nil, LaunchInput{Item: "f-test", Cwd: cwd})
	if err != nil || out.Card != "c9" || !strings.Contains(out.Note, "do NOT launch it again") {
		t.Fatalf("retry = %+v, %v", out, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(keys) != 2 || keys[0] == "" || keys[0] != keys[1] {
		t.Fatalf("keys sent = %v, want one key twice", keys)
	}
}

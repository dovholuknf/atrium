//go:build integration

package link

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

// THE WHOLE THING, over a real link: a hub mirroring a checkout, a room that says Git, git
// itself on both ends. Sync makes the clone, a moved claude/main reaches it, a worker's
// branch on the room is collected into the hub's checkout as refs/remotes/<room>/claude/*.
func TestGitSyncAndCollectEndToEndOverTheLink(t *testing.T) {
	root := t.TempDir()
	checkout := filepath.Join(root, "checkout")
	gitRoot := filepath.Join(root, "roomgit")
	for _, d := range []string{checkout, gitRoot, filepath.Join(root, "hub")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	run(t, checkout, "init", "-q", "-b", "claude/main")
	commitIn := func(dir, file, msg string) string {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(msg), 0o644); err != nil {
			t.Fatal(err)
		}
		run(t, dir, "add", "-A")
		run(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "-q", "-m", msg)
		return run(t, dir, "rev-parse", "HEAD")
	}
	one := commitIn(checkout, "a.txt", "one")

	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: time.Second, Warm: 1})
	g := &gitsync.Hub{
		Dir: filepath.Join(root, "hub"), Rooms: hub.GitRooms(), Runner: gitsync.NewRunner(),
		Repos: func() ([]gitsync.Repo, error) {
			return []gitsync.Repo{{Name: "github/o/r", Checkout: filepath.ToSlash(checkout), Branch: "claude/main"}}, nil
		},
	}
	hub.Git = g.Backend()
	hubLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); hubLn.Close() })
	go func() { _ = hub.Serve(ctx, hubLn) }()

	room := &Room{Name: "SG3", Dial: plain{addr: hubLn.Addr().String()}, Git: true,
		T: Timings{Beat: 200 * time.Millisecond, Warm: 1, Backoff: 50 * time.Millisecond}}
	rh := &gitsync.RoomHandler{
		Syncer: &gitsync.Syncer{Runner: gitsync.NewRunner(), Root: func() string { return gitRoot }},
		Hub: func() (http.RoundTripper, error) {
			if !room.HubServesGit() {
				return nil, ErrNoGit
			}
			return room.GitTransport(), nil
		},
	}
	room.Handler = rh.Handler()
	go func() { _ = room.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return hub.Has("sg3") && room.State().Up && room.HubServesGit() })
	if !hub.RoomSaysGit("sg3") {
		t.Fatal("the hub did not record the room's Git")
	}

	// No clone, no init: absent, and nothing is made.
	res, err := g.Sync(ctx, "sg3", "github/o/r", false)
	if err != nil || len(res) != 1 || res[0].State != "absent" {
		t.Fatalf("absent: %+v err=%v", res, err)
	}
	if _, err := os.Stat(filepath.Join(gitRoot, "github")); err == nil {
		t.Fatal("a clone was made without init")
	}
	// init: made, and both branches land on the hub's sha.
	res, err = g.Sync(ctx, "sg3", "github/o/r", true)
	if err != nil || res[0].State != "ok" || res[0].SHA != one {
		t.Fatalf("init: %+v err=%v", res, err)
	}
	clone := filepath.Join(gitRoot, "github", "o", "r")
	for _, b := range []string{"claude/main", "hub-main"} {
		if got := run(t, clone, "rev-parse", "refs/heads/"+b); got != one {
			t.Fatalf("%s = %s want %s", b, got, one)
		}
	}
	// claude/main moves on the hub, re-signed so it is not a fast-forward, and the room follows.
	run(t, checkout, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "-q", "--amend", "-m", "resigned")
	two := run(t, checkout, "rev-parse", "HEAD")
	res, err = g.Sync(ctx, "sg3", "github/o/r", false)
	if err != nil || res[0].State != "ok" || res[0].SHA != two {
		t.Fatalf("moved: %+v err=%v", res, err)
	}
	if got := run(t, clone, "rev-parse", "refs/heads/claude/main"); got != two {
		t.Fatalf("claude/main = %s want %s", got, two)
	}

	// A worker's branch on the room, and a collect brings it to the hub's checkout.
	run(t, clone, "checkout", "-q", "-b", "claude/w1")
	w1 := commitIn(clone, "w.txt", "worker one")
	run(t, clone, "checkout", "-q", "hub-main")
	col, err := g.Collect(ctx, "sg3")
	if err != nil || len(col.Repos) != 1 || col.Repos[0].Error != "" || !col.Repos[0].Delivered {
		t.Fatalf("collect: %+v err=%v", col, err)
	}
	if got := run(t, checkout, "rev-parse", "refs/remotes/sg3/claude/w1"); got != w1 {
		t.Fatalf("the checkout has %s for the worker branch, want %s", got, w1)
	}
	if out := run(t, checkout, "for-each-ref", "refs/remotes/sg3/claude/main"); out != "" {
		t.Fatalf("claude/main was collected: %s", out)
	}
	if strings.Contains(run(t, checkout, "for-each-ref", "refs/heads"), "w1") {
		t.Fatal("a branch was written under refs/heads in the checkout")
	}
	// The hub's own view of the room.
	if st := g.Status().Rooms["sg3"]; st == nil || st.State != "ok" {
		t.Fatalf("status = %+v", st)
	}
}

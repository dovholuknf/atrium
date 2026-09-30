package link

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

func TestTheGitKindIsKnown(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go func() { _ = writeJSON(a, hello{V: Version, Kind: gitKind, Room: "x"}) }()
	if _, err := hearHello(b, bufio.NewReader(b)); err != nil {
		t.Fatalf("hearHello refused the git kind: %v", err)
	}
}

// A new hub's refusal of a kind it does not know names git among the ones it does.
func TestANewHubsRefusalNamesGit(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go func() { _ = writeJSON(a, hello{V: Version, Kind: "teleport", Room: "x"}) }()
	var w welcome
	done := make(chan struct{})
	go func() { _ = readJSON(bufio.NewReader(a), &w); close(done) }()
	if _, err := hearHello(b, bufio.NewReader(b)); err == nil {
		t.Fatal("an unknown kind was accepted")
	}
	<-done
	want := "a connection is control, data, enrol, upgrade, announce, relay or git"
	if w.Error != want {
		t.Fatalf("refusal = %q, want %q", w.Error, want)
	}
}

// gitFixture is a bare repository behind a hub's Git handler, with a room attached.
type gitFixture struct {
	hub  *Hub
	room *Room
	sha  string
	bare string
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitsync.Default.Git(context.Background(), dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(out)
}

func newGitFixture(t *testing.T, hubSaysGit, roomSaysGit bool, roomHandler http.Handler) *gitFixture {
	t.Helper()
	root := t.TempDir()
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, work, "init", "-q", "-b", "claude/main")
	if err := os.WriteFile(filepath.Join(work, "a"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, work, "add", "-A")
	run(t, work, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "one")
	bare := filepath.Join(root, "git", "github", "o", "r.git")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, bare, "init", "-q", "--bare")
	run(t, work, "push", "-q", bare, "claude/main:refs/heads/claude/main")

	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: time.Second, Warm: 1})
	if hubSaysGit {
		hub.Git = &gitsync.Backend{
			Hide: []string{"refs/rooms"},
			Resolve: func(name string) (string, bool) {
				return bare, name == "github/o/r"
			},
		}
	}
	hubLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); hubLn.Close() })
	go func() { _ = hub.Serve(ctx, hubLn) }()
	if roomHandler == nil {
		roomHandler = http.NotFoundHandler()
	}
	room := &Room{Name: "sg3", Dial: plain{addr: hubLn.Addr().String()}, Handler: roomHandler, Git: roomSaysGit,
		T: Timings{Beat: 200 * time.Millisecond, Warm: 1, Backoff: 50 * time.Millisecond}}
	go func() { _ = room.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return hub.Has("sg3") && room.State().Up })
	return &gitFixture{hub: hub, room: room, sha: run(t, bare, "rev-parse", "refs/heads/claude/main"), bare: bare}
}

func TestARoomFetchesFromItsHubOverTheGitKind(t *testing.T) {
	x := newGitFixture(t, true, true, nil)
	if !x.room.HubServesGit() {
		t.Fatal("the room did not learn the hub serves git")
	}
	f, err := gitsync.NewForwarder(x.room.GitTransport(), "hub.internal", "")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dst := t.TempDir()
	run(t, dst, "init", "-q")
	run(t, dst, "fetch", "-q", "--no-tags", f.URL+"/github/o/r.git", "+refs/heads/claude/main:refs/remotes/hub/claude/main")
	if got := run(t, dst, "rev-parse", "refs/remotes/hub/claude/main"); got != x.sha {
		t.Fatalf("got %s want %s", got, x.sha)
	}
	// A name the hub does not list is a 404 through the link as well.
	if _, err := gitsync.Default.Git(context.Background(), dst, "ls-remote", f.URL+"/github/o/other.git"); err == nil {
		t.Fatal("an unlisted repository was served")
	}
}

// A room never dials the git kind at a hub that did not say Git, and a hub never counts a
// room that did not say it.
func TestNothingIsDialledWithoutTheCapability(t *testing.T) {
	x := newGitFixture(t, false, false, nil)
	if x.room.HubServesGit() {
		t.Fatal("a hub with no Git handler said it serves git")
	}
	if _, err := x.room.DialGit(context.Background()); !errors.Is(err, ErrNoGit) {
		t.Fatalf("DialGit = %v, want ErrNoGit", err)
	}
	if x.hub.RoomSaysGit("sg3") {
		t.Fatal("the hub counted a room that did not say Git")
	}
	for _, a := range x.hub.Rooms() {
		if a.Git {
			t.Fatal("Attached reports Git for a room that did not say it")
		}
	}
}

func TestAHubKnowsWhichRoomsSaidGit(t *testing.T) {
	x := newGitFixture(t, true, true, nil)
	if !x.hub.RoomSaysGit("SG3") {
		t.Fatal("the hub did not record the room's Git")
	}
}

// The hub fetches from a room's handler over the data pool the room dialled.
func TestTheHubFetchesFromARoomOverThePool(t *testing.T) {
	// A room serving a repository at /v1/git.
	root := t.TempDir()
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, work, "init", "-q", "-b", "claude/w")
	if err := os.WriteFile(filepath.Join(work, "a"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, work, "add", "-A")
	run(t, work, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "w")
	sha := run(t, work, "rev-parse", "HEAD")
	roomGit := &gitsync.Backend{
		Prefix:  "/v1/git",
		Resolve: func(name string) (string, bool) { return filepath.Join(work, ".git"), name == "github/o/r" },
	}
	x := newGitFixture(t, true, true, roomGit)
	f, err := gitsync.NewForwarder(x.hub.Transport("sg3"), "sg3.room.atrium.internal", "/v1/git")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dst := t.TempDir()
	run(t, dst, "init", "-q")
	run(t, dst, "fetch", "-q", "--no-tags", f.URL+"/github/o/r.git", "+refs/heads/claude/*:refs/rooms/sg3/claude/*")
	if got := run(t, dst, "rev-parse", "refs/rooms/sg3/claude/w"); got != sha {
		t.Fatalf("got %s want %s", got, sha)
	}
}

// A want list past git's post buffer floor is sent chunked, and has to survive the
// forwarder, the link and cgi together.
func TestAChunkedPostGoesThroughForwarderLinkAndCgi(t *testing.T) {
	x := newGitFixture(t, true, true, nil)
	var stream strings.Builder
	for i := 0; i < 1500; i++ {
		fmt.Fprintf(&stream, "commit refs/heads/b%d\ncommitter t <t@t> %d +0000\ndata 1\nx\n\n", i, 1000+i)
	}
	cmd := exec.Command("git", "-C", x.bare, "fast-import", "--quiet")
	cmd.Env = gitsync.CleanEnv()
	cmd.Stdin = strings.NewReader(stream.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fast-import: %v %s", err, out)
	}
	f, err := gitsync.NewForwarder(x.room.GitTransport(), "hub.internal", "")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dst := t.TempDir()
	run(t, dst, "init", "-q")
	run(t, dst, "-c", "http.postBuffer=1024", "fetch", "-q", "--no-tags",
		f.URL+"/github/o/r.git", "+refs/heads/*:refs/remotes/hub/*")
	if n := strings.Count(run(t, dst, "for-each-ref", "refs/remotes/hub"), "\n"); n < 1500 {
		t.Fatalf("only %d refs arrived", n)
	}
}

// One-conn listener: Serve returns when the connection closes, so no goroutine is left.
func TestOneConnServeReturnsWhenTheConnectionCloses(t *testing.T) {
	a, b := net.Pipe()
	one := newOneConn(b)
	srv := &http.Server{Handler: http.NotFoundHandler(), ConnState: func(_ net.Conn, s http.ConnState) {
		if s == http.StateClosed {
			one.finish()
		}
	}}
	done := make(chan struct{})
	go func() { _ = srv.Serve(one); close(done) }()
	a.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after the connection closed")
	}
}

package link

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/edge"
	"github.com/dovholuknf/atrium/internal/gitsync"
)

// A fetch passed through the hub to a room, over a real link: a hub, a room that says Git with a real clone
// behind its git route, the board's own proxy in front, and git itself as the reader. See
// docs/fabric/hub-forge-design.md 3.3 and the stage 3 row of 7.

type passRig struct {
	hub    *Hub
	g      *gitsync.Hub
	room   *Room
	cancel context.CancelFunc
	proxy  *Proxy
	srv    *httptest.Server
	root   string
	clone  string
	hubDir string
	// posts counts upload-pack POSTs that reached the room.
	posts atomic.Int32
	// advs counts ref advertisements that reached the room.
	advs atomic.Int32

	mu    sync.Mutex
	audit []string
	live  []string
	// remote is what the room says its hub forwarder is, at GET /v1/hub-remote. Empty is a room that predates it (404).
	remote string

	claudeW1, stash, notes, secret, liveSHA string
}

func (x *passRig) setRemote(base string) { x.mu.Lock(); x.remote = base; x.mu.Unlock() }

func (x *passRig) setLive(b ...string) { x.mu.Lock(); x.live = b; x.mu.Unlock() }

func newPassRig(t *testing.T) *passRig {
	t.Helper()
	x := &passRig{root: t.TempDir()}
	checkout := filepath.Join(x.root, "checkout")
	gitRoot := filepath.Join(x.root, "roomgit")
	x.hubDir = filepath.Join(x.root, "hub")
	for _, d := range []string{checkout, gitRoot, x.hubDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	commitIn := func(dir, file, msg string) string {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(msg), 0o644); err != nil {
			t.Fatal(err)
		}
		run(t, dir, "add", "-A")
		run(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "-q", "-m", msg)
		return run(t, dir, "rev-parse", "HEAD")
	}
	run(t, checkout, "init", "-q", "-b", "claude/main")
	commitIn(checkout, "a.txt", "one")

	x.hub = NewHub(Timings{Beat: 200 * time.Millisecond, Silence: time.Second, Warm: 1})
	x.g = &gitsync.Hub{
		Dir: x.hubDir, Rooms: x.hub.GitRooms(), Runner: gitsync.NewRunner(),
		Repos: func() ([]gitsync.Repo, error) {
			return []gitsync.Repo{{Name: "github/o/r", Checkout: filepath.ToSlash(checkout), Branch: "claude/main"}}, nil
		},
		Audit: func(room, kind, detail string) {
			x.mu.Lock()
			x.audit = append(x.audit, room+"|"+kind+"|"+detail)
			x.mu.Unlock()
		},
	}
	x.hub.Git = x.g.Backend()
	hubLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	x.cancel = cancel
	t.Cleanup(func() { cancel(); hubLn.Close() })
	go func() { _ = x.hub.Serve(ctx, hubLn) }()

	x.room = &Room{Name: "SG3", Dial: plain{addr: hubLn.Addr().String()}, Git: true,
		T: Timings{Beat: 200 * time.Millisecond, Warm: 1, Backoff: 50 * time.Millisecond}}
	rh := &gitsync.RoomHandler{
		Syncer: &gitsync.Syncer{Runner: gitsync.NewRunner(), Root: func() string { return gitRoot }},
		Hub: func() (http.RoundTripper, error) {
			if !x.room.HubServesGit() {
				return nil, ErrNoGit
			}
			return x.room.GitTransport(), nil
		},
		Live: func(string) []string { x.mu.Lock(); defer x.mu.Unlock(); return x.live },
	}
	inner := rh.Handler()
	x.room.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/hub-remote" {
			x.mu.Lock()
			base := x.remote
			x.mu.Unlock()
			if base == "" {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"base": base})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/git-upload-pack") {
			x.posts.Add(1)
		}
		if strings.HasSuffix(r.URL.Path, "/info/refs") {
			x.advs.Add(1)
		}
		inner.ServeHTTP(w, r)
	})
	go func() { _ = x.room.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return x.hub.Has("sg3") && x.room.State().Up && x.room.HubServesGit() })
	if res, err := x.g.Sync(ctx, "sg3", "github/o/r", true); err != nil || res[0].State != "ok" {
		t.Fatalf("sync: %+v %v", res, err)
	}

	// The card's work on the room: a claude/* branch, committed and NOT pushed anywhere, and what must not be
	// served: a stash, a note and a branch with no live card.
	x.clone = filepath.Join(gitRoot, "github", "o", "r")
	run(t, x.clone, "checkout", "-q", "-b", "claude/w1")
	x.claudeW1 = commitIn(x.clone, "w.txt", "worker one")
	run(t, x.clone, "checkout", "-q", "-b", "fix/live", "hub-main")
	x.liveSHA = commitIn(x.clone, "l.txt", "live")
	run(t, x.clone, "checkout", "-q", "-b", "secret/x", "hub-main")
	x.secret = commitIn(x.clone, "s.txt", "secret")
	run(t, x.clone, "checkout", "-q", "hub-main")
	if err := os.WriteFile(filepath.Join(x.clone, "a.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, x.clone, "-c", "user.name=t", "-c", "user.email=t@t", "stash")
	x.stash = run(t, x.clone, "rev-parse", "refs/stash")
	run(t, x.clone, "-c", "user.name=t", "-c", "user.email=t@t", "notes", "add", "-m", "private", "HEAD")
	x.notes = run(t, x.clone, "rev-parse", "refs/notes/commits")

	x.proxy = NewProxy(x.hub, nil, "", nil)
	x.proxy.SetGit(x.g)
	// Generous, so one test's many fetches are not the rate's business. The rate has its own test.
	x.g.PassHandler().FetchesPerMinute, x.g.PassHandler().RoundsPerMinute = 1000, 1000
	x.srv = httptest.NewServer(x.proxy)
	t.Cleanup(x.srv.Close)
	return x
}

func (x *passRig) url(room string) string {
	return x.srv.URL + "/git/room/" + room + "/github/o/r.git"
}

// reader is an empty repository to fetch into.
func reader(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	run(t, d, "init", "-q")
	return d
}

// treeDigest is every path under dir with its mode, size and content hash, so a change of any kind shows.
func treeDigest(t *testing.T, dir string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		sum := ""
		if info.Mode().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			sum = fmt.Sprintf("%x", sha256.Sum256(b))
		}
		rel, _ := filepath.Rel(dir, p)
		lines = append(lines, fmt.Sprintf("%s %v %d %s", filepath.ToSlash(rel), info.Mode(), info.Size(), sum))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func TestAFetchOfAClaudeBranchPassesThroughAndTheHubStoresNothing(t *testing.T) {
	x := newPassRig(t)
	before := treeDigest(t, x.root+"/hub")
	hubObjects := run(t, x.root+"/checkout", "count-objects", "-v")

	dst := reader(t)
	run(t, dst, "fetch", "-q", x.url("sg3"), "refs/heads/claude/w1:refs/remotes/sg3/claude/w1")
	if got := run(t, dst, "rev-parse", "refs/remotes/sg3/claude/w1"); got != x.claudeW1 {
		t.Fatalf("fetched %s want %s", got, x.claudeW1)
	}
	// The diff is there to be reviewed.
	if out := run(t, dst, "show", "--stat", "--format=", "refs/remotes/sg3/claude/w1"); !strings.Contains(out, "w.txt") {
		t.Fatalf("diff: %q", out)
	}
	// The name is case-folded like the hub's, and the short form is the same repository.
	run(t, dst, "fetch", "-q", strings.Replace(x.url("SG3"), "github/o/r", "o/r", 1), "refs/heads/claude/w1:refs/remotes/short/w1")

	if after := treeDigest(t, x.root+"/hub"); after != before {
		t.Fatalf("the hub's directory changed by a fetch:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if got := run(t, x.root+"/checkout", "count-objects", "-v"); got != hubObjects {
		t.Fatalf("the hub's checkout gained objects:\n%s\n%s", hubObjects, got)
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	var logged bool
	for _, a := range x.audit {
		if strings.Contains(a, "git-passed") && strings.Contains(a, "github/o/r via loopback") && strings.HasPrefix(a, "SG3|") {
			logged = true
		}
	}
	if !logged {
		t.Fatalf("the fetch was not logged with room, repo and reach: %q", x.audit)
	}
}

func TestStashNotesAndAnUnservedBranchAreRefusedThroughTheHub(t *testing.T) {
	x := newPassRig(t)
	for _, c := range []struct{ name, ref string }{
		{"stash", "refs/stash"},
		{"notes", "refs/notes/commits"},
		{"an unserved branch", "refs/heads/secret/x"},
		{"a branch whose card is not live", "refs/heads/fix/live"},
		{"claude/main", "refs/heads/claude/main"},
		{"hub-main", "refs/heads/hub-main"},
	} {
		dst := reader(t)
		if out, err := gitsync.Default.Git(context.Background(), dst, "fetch", x.url("sg3"), c.ref+":refs/heads/got"); err == nil {
			t.Errorf("%s: went through: %s", c.name, out)
		}
		if out := run(t, dst, "for-each-ref"); out != "" {
			t.Errorf("%s: wrote refs: %s", c.name, out)
		}
	}
	// A want by sha, for what the room did not advertise, reaches the room and is refused by its git.
	for name, sha := range map[string]string{"stash": x.stash, "notes": x.notes, "secret": x.secret} {
		body := string(pkt("want "+sha+"\n")) + "0000" + string(pkt("done\n"))
		resp := post(t, x.url("sg3")+"/git-upload-pack", body)
		if !strings.Contains(resp, "ERR") || strings.Contains(resp, "PACK") {
			t.Errorf("%s: a want by sha was answered %q", name, resp)
		}
	}
	// With a live card, its branch is served, and with the card gone it is not.
	x.setLive("fix/live")
	dst := reader(t)
	run(t, dst, "fetch", "-q", x.url("sg3"), "refs/heads/fix/live:refs/heads/live")
	if got := run(t, dst, "rev-parse", "refs/heads/live"); got != x.liveSHA {
		t.Fatalf("live branch at %s", got)
	}
	x.setLive()
	if _, err := gitsync.Default.Git(context.Background(), dst, "fetch", x.url("sg3"), "refs/heads/fix/live:refs/heads/again"); err == nil {
		t.Fatal("a branch whose card ended is still served")
	}
}

func pkt(s string) []byte { return []byte(fmt.Sprintf("%04x%s", len(s)+4, s)) }

func post(t *testing.T, url, body string) string {
	t.Helper()
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-git-upload-pack-request")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func TestAShallowOrFilteredFetchIsRefusedBeforeItIsPassedOn(t *testing.T) {
	x := newPassRig(t)
	for _, args := range [][]string{
		{"--depth", "1"},
		{"--filter=blob:none"},
		{"--shallow-since=2000-01-01"},
		{"--deepen", "3"},
	} {
		dst := reader(t)
		a := append([]string{"fetch"}, args...)
		a = append(a, x.url("sg3"), "refs/heads/claude/w1:refs/heads/got")
		out, err := gitsync.Default.Git(context.Background(), dst, a...)
		if err == nil {
			t.Errorf("%v went through: %s", args, out)
		}
		if got := run(t, dst, "for-each-ref"); got != "" {
			t.Errorf("%v wrote refs: %s", args, got)
		}
	}
	// And by hand, with each line a client could send, none of which may reach the room.
	for _, line := range []string{"shallow " + x.claudeW1 + "\n", "deepen 1\n", "deepen-since 1\n", "deepen-not x\n", "filter blob:none\n"} {
		body := string(pkt("want "+x.claudeW1+"\n")) + string(pkt(line)) + "0000" + string(pkt("done\n"))
		if resp := post(t, x.url("sg3")+"/git-upload-pack", body); !strings.Contains(resp, "ERR atrium") {
			t.Errorf("%q answered %q", line, resp)
		}
	}
	if n := x.posts.Load(); n != 0 {
		t.Fatalf("%d refused requests were passed on to the room", n)
	}
}

func TestADetachedOrUnknownRoomAnswers503(t *testing.T) {
	x := newPassRig(t)
	get := func(room string) (int, string) {
		resp, err := http.Get(x.url(room) + "/info/refs?service=git-upload-pack")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, strings.TrimSpace(string(b))
	}
	if code, body := get("sg3"); code != 200 {
		t.Fatalf("attached: %d %s", code, body)
	}
	if code, body := get("nowhere"); code != http.StatusServiceUnavailable || body != "nowhere is not connected" {
		t.Fatalf("unknown: %d %q", code, body)
	}
	x.cancel()
	waitFor(t, 5*time.Second, func() bool { return !x.hub.Has("sg3") })
	code, body := get("sg3")
	if code != http.StatusServiceUnavailable || body != "sg3 is not connected" {
		t.Fatalf("detached: %d %q", code, body)
	}
	dst := reader(t)
	out, err := gitsync.Default.Git(context.Background(), dst, "fetch", x.url("sg3"), "refs/heads/claude/w1")
	if err == nil || !strings.Contains(out+err.Error(), "sg3 is not connected") {
		t.Fatalf("git was not shown the 503: %v %s", err, out)
	}
}

func TestOnlyTheOperatorsReachesMayFetchThroughTheHub(t *testing.T) {
	x := newPassRig(t)
	do := func(h http.Handler, host, remote string) int {
		req := httptest.NewRequest("GET", "http://"+host+"/git/room/sg3/github/o/r.git/info/refs?service=git-upload-pack", nil)
		req.RemoteAddr = remote
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := do(x.proxy, "127.0.0.1:7778", "127.0.0.1:5555"); code != 200 {
		t.Errorf("loopback = %d", code)
	}
	for _, r := range []edge.Reach{edge.ReachOverlay, edge.ReachZrokPrivate} {
		if code := do(edge.MarkReach(x.proxy, r), "atrium.ziti", offLoopback); code != 200 {
			t.Errorf("%s = %d", r, code)
		}
	}
	pub := edge.MarkReach(x.proxy, edge.ReachZrokPublic)
	if code := do(pub, "atrium.ziti", offLoopback); code != http.StatusNotFound {
		t.Errorf("a zrok public share = %d, want 404", code)
	}
	if code := do(pub, "127.0.0.1:7778", "127.0.0.1:5555"); code != http.StatusNotFound {
		t.Errorf("a zrok public share that looks like loopback = %d, want 404", code)
	}
	if code := do(x.proxy, "atrium.ziti", offLoopback); code != http.StatusForbidden {
		t.Errorf("an unmarked off-machine caller = %d, want 403", code)
	}
	if n := x.posts.Load(); n != 0 {
		t.Fatalf("%d", n)
	}
}

func TestACardOnARoomDoesNotReachThePassThroughOverTheLink(t *testing.T) {
	x := newPassRig(t)
	other := &Room{Name: "m1mini", Dial: plain{addr: x.hubAddr(t)}, Git: true,
		T: Timings{Beat: 200 * time.Millisecond, Warm: 1, Backoff: 50 * time.Millisecond}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = other.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return x.hub.Has("m1mini") && other.HubServesGit() })
	req, _ := http.NewRequest("GET", "http://hub/git/room/sg3/github/o/r.git/info/refs?service=git-upload-pack", nil)
	resp, err := other.GitTransport().RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a room's request for another room's pass-through = %d, want 404 (stage 1's job, not this one)", resp.StatusCode)
	}
}

func (x *passRig) hubAddr(t *testing.T) string {
	t.Helper()
	return x.room.Dial.(plain).addr
}

func TestSevenFetchesInAMinuteFromOneReaderAreTooMany(t *testing.T) {
	x := newPassRig(t)
	x.g.PassHandler().FetchesPerMinute, x.g.PassHandler().RoundsPerMinute = 0, 0
	codes := map[int]int{}
	for i := 0; i < 8; i++ {
		resp, err := http.Get(x.url("sg3") + "/info/refs?service=git-upload-pack")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		codes[resp.StatusCode]++
		if resp.StatusCode == http.StatusTooManyRequests && resp.Header.Get("Retry-After") == "" {
			t.Error("no Retry-After")
		}
	}
	if codes[200] != 6 || codes[http.StatusTooManyRequests] != 2 {
		t.Fatalf("answers: %v, want six 200 and two 429", codes)
	}
}

// A ziti service's peers have no IP: RemoteAddr is one string per connection. They share one budget, so opening a new
// connection is not a new reader.
func TestReadersWithNoAddressShareTheReachsBudgetAndPeersWithOneDoNot(t *testing.T) {
	if got := readerKey("overlay", "ziti-edge-router connId=7, logical=er1"); got != "overlay" {
		t.Errorf("a ziti peer is keyed %q", got)
	}
	if readerKey("overlay", "ziti-edge-router connId=7, logical=er1") != readerKey("overlay", "ziti-edge-router connId=8, logical=er2") {
		t.Error("two connections of the overlay are two readers")
	}
	if a, b := readerKey("loopback", "127.0.0.1:5555"), readerKey("loopback", "127.0.0.1:6666"); a != b || a != "loopback:127.0.0.1" {
		t.Errorf("one address on two ports: %q %q", a, b)
	}
	if readerKey("zrok-private", "192.0.2.7:1") == readerKey("zrok-private", "192.0.2.8:1") {
		t.Error("two addresses are one reader")
	}

	x := newPassRig(t)
	x.g.PassHandler().FetchesPerMinute = 0
	overlay := edge.MarkReach(x.proxy, edge.ReachOverlay)
	codes := map[int]int{}
	for i := 0; i < 8; i++ {
		req := httptest.NewRequest("GET", "http://atrium.ziti/git/room/sg3/github/o/r.git/info/refs?service=git-upload-pack", nil)
		req.RemoteAddr = fmt.Sprintf("ziti-edge-router connId=%d, logical=er1", i)
		rec := httptest.NewRecorder()
		overlay.ServeHTTP(rec, req)
		codes[rec.Code]++
	}
	if codes[200] != 6 || codes[http.StatusTooManyRequests] != 2 {
		t.Fatalf("eight connections of the overlay: %v, want six 200 and two 429", codes)
	}
}

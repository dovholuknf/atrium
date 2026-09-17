package link

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// A hub and a room, with the hub offering whatever it is running.
// THE OFFER IS SET BEFORE THE ROOM STARTS, deliberately. A hub says what it is
// running once, when a room attaches, and never mentions it again: a hub that
// chased rooms with offers would be a hub with a plan for them. So a hub that
// learns of a build after a room attached says nothing until that room comes
// back, which it does on every hub restart, which is when a hub's binary can
// have changed anyway.
func offering(t *testing.T, up *Upgrades, o *Offer) (*Hub, *Room, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: 5 * time.Second, Warm: 1})
	hub.Offers(o)
	ctx, stop := context.WithCancel(context.Background())
	go func() { _ = hub.Serve(ctx, ln) }()

	r := &Room{
		Name: "leaf", Dial: plain{addr: ln.Addr().String()}, Handler: http.NotFoundHandler(),
		Version: "v1", Upgrades: up,
		T: Timings{Beat: 200 * time.Millisecond, Warm: 1, Backoff: 50 * time.Millisecond},
	}
	go func() { _ = r.Run(ctx) }()
	return hub, r, func() { stop(); ln.Close() }
}

// selfSum is what the test binary hashes to, which is what the hub will offer:
// `Offered` describes the RUNNING executable, and under `go test` that is the
// test binary.
func selfSum(t *testing.T) (string, int64) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), int64(len(raw))
}

// THE WHOLE POINT, END TO END: the hub says what it has, the room decides it
// wants it, dials for it, checks what arrived, and hands over a file that is
// byte for byte what was offered.
func TestARoomTakesTheBuildItWasOffered(t *testing.T) {
	dir := t.TempDir()
	var (
		mu     sync.Mutex
		landed string
		got    Offer
	)
	up := &Upgrades{
		Accept: true, Version: "v1", Dir: dir,
		Install: func(path string, o Offer) error {
			mu.Lock()
			defer mu.Unlock()
			landed, got = path, o
			return nil
		},
	}
	sum, size := selfSum(t)
	hub, _, done := offering(t, up, &Offer{
		Version: "v2", SHA256: sum, Size: size,
		OS: runtime.GOOS, Arch: runtime.GOARCH,
	})
	defer done()
	waitFor(t, 5*time.Second, func() bool { return hub.Has("leaf") })

	waitFor(t, 30*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return landed != ""
	})

	mu.Lock()
	defer mu.Unlock()
	if got.Version != "v2" {
		t.Errorf("the room installed %q", got.Version)
	}
	// IN THE DIRECTORY IT WAS TOLD TO USE, because the last step is a rename
	// and a rename across volumes is a copy that can fail with the old binary
	// already moved aside.
	if filepath.Dir(landed) != dir {
		t.Errorf("the download landed in %s, not %s", filepath.Dir(landed), dir)
	}
	raw, err := os.ReadFile(landed)
	if err != nil {
		t.Fatal(err)
	}
	check := sha256.Sum256(raw)
	if hex.EncodeToString(check[:]) != sum {
		t.Error("what was installed is not what was offered")
	}
}

// A HUB CANNOT MAKE THIS HAPPEN. The offer arrives, the room has not opted in,
// and nothing is fetched. This is the check the whole design exists for: a hub
// that can write a binary a room will execute owns that room.
func TestAHubCannotUpgradeARoomThatDidNotAgree(t *testing.T) {
	var asked bool
	var mu sync.Mutex
	up := &Upgrades{
		Accept: false, Version: "v1", Dir: t.TempDir(),
		Install: func(string, Offer) error {
			mu.Lock()
			asked = true
			mu.Unlock()
			return nil
		},
	}
	sum, size := selfSum(t)
	hub, _, done := offering(t, up, &Offer{Version: "v2", SHA256: sum, Size: size,
		OS: runtime.GOOS, Arch: runtime.GOARCH})
	defer done()
	waitFor(t, 5*time.Second, func() bool { return hub.Has("leaf") })

	// Long enough that a fetch would have finished.
	time.Sleep(2 * time.Second)
	mu.Lock()
	defer mu.Unlock()
	if asked {
		t.Fatal("a room that never agreed to upgrades installed one")
	}
}

// A BINARY FOR ANOTHER PLATFORM IS NOT INSTALLED, even if the room agreed to
// upgrades. Finding that out after the swap is the worst possible moment.
func TestAnotherPlatformsBuildIsRefused(t *testing.T) {
	var asked bool
	var mu sync.Mutex
	up := &Upgrades{
		Accept: true, Version: "v1", Dir: t.TempDir(),
		Install: func(string, Offer) error {
			mu.Lock()
			asked = true
			mu.Unlock()
			return nil
		},
	}
	sum, size := selfSum(t)
	hub, _, done := offering(t, up, &Offer{Version: "v2", SHA256: sum, Size: size,
		OS: "plan9", Arch: "sparc64"})
	defer done()
	waitFor(t, 5*time.Second, func() bool { return hub.Has("leaf") })

	time.Sleep(2 * time.Second)
	mu.Lock()
	defer mu.Unlock()
	if asked {
		t.Fatal("a build for another platform was installed")
	}
}

// WHAT ARRIVED MUST BE WHAT WAS DESCRIBED. The hash is the check that makes
// fetching over the link defensible, so a wrong one installs nothing.
func TestABuildThatDoesNotMatchItsHashIsNotInstalled(t *testing.T) {
	dir := t.TempDir()
	var asked bool
	var mu sync.Mutex
	up := &Upgrades{
		Accept: true, Version: "v1", Dir: dir,
		Install: func(string, Offer) error {
			mu.Lock()
			asked = true
			mu.Unlock()
			return nil
		},
	}
	_, size := selfSum(t)
	hub, _, done := offering(t, up, &Offer{
		Version: "v2", Size: size,
		SHA256: "0000000000000000000000000000000000000000000000000000000000000000",
		OS:     runtime.GOOS, Arch: runtime.GOARCH,
	})
	defer done()
	waitFor(t, 5*time.Second, func() bool { return hub.Has("leaf") })

	time.Sleep(3 * time.Second)
	mu.Lock()
	defer mu.Unlock()
	if asked {
		t.Fatal("a binary that did not match its hash was installed")
	}
	// AND NOTHING IS LEFT BEHIND. A half written binary beside the real one is
	// the kind of litter somebody eventually runs by accident.
	left, err := filepath.Glob(filepath.Join(dir, "atrium-upgrade-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) > 0 {
		t.Errorf("a rejected download was left on disk: %v", left)
	}
}

// One offer, acted on once, however many times a reconnecting hub repeats it.
func TestAnOfferIsTakenOnce(t *testing.T) {
	var tk taker
	if !tk.start("abc") {
		t.Fatal("the first attempt was refused")
	}
	if tk.start("abc") {
		t.Error("a second attempt started while the first was running")
	}
	tk.finish("abc", true)
	if tk.start("abc") {
		t.Error("the same build was taken twice")
	}
	if !tk.start("def") {
		t.Error("a different build was refused after a successful one")
	}
}

// The hub does not bother a room that has nothing to gain, which keeps the log
// quiet and the offer meaningful.
func TestAHubOnlySaysSomethingWorthSaying(t *testing.T) {
	o := &Offer{Version: "v2", OS: "linux", Arch: "amd64"}
	cases := []struct {
		why  string
		hi   hello
		want bool
	}{
		{"a room that did not ask", hello{Version: "v1", OS: "linux", Arch: "amd64"}, false},
		{"another os", hello{Upgrades: true, Version: "v1", OS: "windows", Arch: "amd64"}, false},
		{"another arch", hello{Upgrades: true, Version: "v1", OS: "linux", Arch: "arm64"}, false},
		{"the same version", hello{Upgrades: true, Version: "v2", OS: "linux", Arch: "amd64"}, false},
		{"a room that wants it", hello{Upgrades: true, Version: "v1", OS: "linux", Arch: "amd64"}, true},
	}
	for _, c := range cases {
		if got := worthOffering(o, c.hi); got != c.want {
			t.Errorf("%s: offered=%v, expected %v", c.why, got, c.want)
		}
	}
	// TWO `dev` BUILDS ARE DIFFERENT BINARIES most of the time, and handing a
	// room the build you just made is the entire point during development.
	dev := &Offer{Version: "dev", OS: "linux", Arch: "amd64"}
	if !worthOffering(dev, hello{Upgrades: true, Version: "dev", OS: "linux", Arch: "amd64"}) {
		t.Error("one dev build was not offered to a room running another")
	}
}

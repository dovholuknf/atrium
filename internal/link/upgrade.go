package link

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"runtime"
	"sync"
	"time"
)

// A room taking a newer binary from its hub.
//
// ── the shape, and why it is this way round ──────────────
//
// THE HUB OFFERS. THE ROOM DECIDES, FETCHES AND CHECKS.
//
// The obvious design is a push: the hub already makes HTTP requests down the
// link, so `PUT /v1/upgrade` would have worked in an afternoon. It is the wrong
// design, and the reason is written down elsewhere in this package. The hub is
// refused `/v1/shutdown` because once something else can reach a room, loopback
// stops meaning "this machine". Shipping an executable is strictly more
// powerful than stopping one: a hub that can write a binary a room will execute
// owns that room completely and nothing about the room's own configuration can
// disagree.
//
// So the hub may do exactly one thing: SAY what it is running, on the control
// connection, as a `note`. That is a notice board. Everything else happens on
// the room's side and at the room's choice:
//
//	1. the room was started with `--accept-upgrades`, or it ignores the offer
//	2. the platform matches, or it ignores the offer
//	3. the version differs from what it is running, or it ignores the offer
//	4. the room DIALS a connection and asks for the bytes
//	5. the room hashes what arrives and checks it against the offer
//	6. the room writes it aside and restarts itself
//
// A room that wants none of this never says so and never has to: a hub with
// nothing to offer and a room that declines everything look identical from the
// hub, which is the correct amount for a hub to know.
//
// ── why the room dials for the bytes ─────────────────────
//
// It cannot pull over HTTP, because on this link the HUB is the client: it
// sends requests down connections the room opened. The room has no way to make
// a request in the other direction, and a room behind a firewall may have no
// route to the hub other than the one it dialled.
//
// So the bytes come down a connection of the same kind as every other one, with
// `kind: "upgrade"` in the hello. The room dials, the hub writes, both ends are
// using the machinery that already exists.

// upgradeKind is the third kind of connection, after control and data.
const upgradeKind = "upgrade"

// upgradeLimit bounds what a room will read as a binary.
//
// Atrium is around fifty megabytes. Two hundred is far past generous, and a
// room that reads without a limit is a room a hub can exhaust by answering an
// upgrade request with an endless stream.
const upgradeLimit = 200 << 20

// Build is one binary a hub can hand out, and where it keeps it.
//
// ── ONE HUB, SEVERAL PLATFORMS ───────────────────────────
//
// A hub can only ever describe binaries it has, and the one it is certain to
// have is its own. That is enough for a fleet of one kind of machine and
// useless for any other: a Windows hub with a Linux room can say nothing that
// room could run, so the feature would quietly do nothing for exactly the
// people who need it most. Rooms are on other machines, which is the entire
// reason they are rooms.
//
// So a hub holds a SET, keyed by what it runs on, and answers each room with
// the one that matches. Its own binary is simply the entry for its own
// platform, and a hub given nothing else still works for rooms like itself.
//
// `Path` never goes on the wire. A room is told what a build IS, not where the
// hub keeps it.
type Build struct {
	Offer
	Path string
}

// Offered describes the binary this hub is running.
//
// Hashed once at startup, because hashing fifty megabytes per room attach
// would be fifty megabytes per reconnect and the answer cannot change: a
// running process cannot have its own image swapped under it on either
// platform without the swap producing a different file.
func Offered(version string) (Build, error) {
	self, err := os.Executable()
	if err != nil {
		return Build{}, err
	}
	return Describe(self, version, runtime.GOOS, runtime.GOARCH)
}

// Describe hashes a binary so it can be offered.
//
// The platform is passed in rather than guessed at, because the whole point is
// binaries for machines this one is not.
func Describe(path, version, goos, arch string) (Build, error) {
	f, err := os.Open(path)
	if err != nil {
		return Build{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Build{}, err
	}
	if st.IsDir() {
		return Build{}, errors.New(path + " is a directory")
	}
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return Build{}, err
	}
	return Build{
		Offer: Offer{
			Version: version,
			SHA256:  hex.EncodeToString(sum.Sum(nil)),
			Size:    st.Size(),
			OS:      goos,
			Arch:    arch,
		},
		Path: path,
	}, nil
}

// forRoom picks the build a room could actually run, or nothing.
//
// A ROOM THAT SAYS NOTHING ABOUT ITS PLATFORM IS OFFERED NOTHING. An older
// room predates these fields, and guessing that it must be like the hub is the
// guess that ends with a Linux machine holding a Windows binary. Silence is
// the safe answer and costs that room an upgrade it never asked for.
func forRoom(builds []Build, hi hello) *Build {
	if !hi.Upgrades || hi.OS == "" || hi.Arch == "" {
		return nil
	}
	for i := range builds {
		b := &builds[i]
		if b.OS != hi.OS || b.Arch != hi.Arch {
			continue
		}
		// Same version, nothing to say. `dev` is deliberately excluded from
		// that shortcut: two `dev` builds are different binaries most of the
		// time, and during development handing a room the build you just made
		// is the entire point.
		if hi.Version == b.Version && b.Version != "dev" {
			return nil
		}
		return b
	}
	return nil
}

// serveUpgrade writes a binary down a connection a room dialled asking for it.
//
// WHICH binary is decided here, from what the room says it runs on, and not by
// the room naming a file. A room asking for a path would be a room reading the
// hub's disk.
func (h *Hub) serveUpgrade(conn net.Conn, hi hello) {
	defer conn.Close()
	h.mu.Lock()
	builds := h.builds
	h.mu.Unlock()

	b := forRoom(builds, hello{Upgrades: true, OS: hi.OS, Arch: hi.Arch})
	if b == nil {
		_ = writeJSON(conn, welcome{OK: false,
			Error: "this hub has no " + hi.OS + "/" + hi.Arch + " build"})
		return
	}
	f, err := os.Open(b.Path)
	if err != nil {
		_ = writeJSON(conn, welcome{OK: false, Error: err.Error()})
		return
	}
	defer f.Close()
	// STILL THE FILE THAT WAS DESCRIBED? Hashed at startup, and a release
	// directory is a place somebody drops new files into while the hub runs.
	// The room's own check is what makes this safe, so this exists to SAY so:
	// without it the symptom is a room rejecting every download with nothing
	// on the hub explaining why.
	if st, err := f.Stat(); err == nil && st.Size() != b.Size {
		log.Printf("[hub] %s is %d bytes now and %d when this hub started. "+
			"the room will reject it. restart this hub to offer what is there",
			b.Path, st.Size(), b.Size)
	}
	if err := writeJSON(conn, welcome{OK: true}); err != nil {
		return
	}
	// NO DEADLINE ON THE COPY. Fifty megabytes over an overlay on a bad
	// network is minutes, and a room that gave up half way would simply ask
	// again, which costs more than waiting.
	n, err := io.Copy(conn, f)
	if err != nil {
		log.Printf("[hub] sending the binary stopped after %d bytes: %v", n, err)
	}
}

// ── the room's side ──────────────────────────────────────

// Upgrades is how a room is told it may take one, and what to do when it has.
//
// `Accept` is the operator's decision, made once when the room was started.
// `Install` is handed the verified file and owns everything after: where it
// goes, how the running binary is replaced, and when to restart. This package
// deliberately does not know how atrium restarts itself.
type Upgrades struct {
	Accept  bool
	Version string
	// SHA256 is what the room is running RIGHT NOW.
	//
	// THE CHECK THAT STOPS A ROOM UPGRADING TO ITSELF. Versions are a poor
	// comparison during development, where every build says `dev`, so `dev` is
	// deliberately always offered. On a machine where the hub and the room are
	// the same binary -- which is exactly how anybody first tries this -- that
	// meant fetching it, installing it over itself, and stopping. The room
	// went down and nothing had changed.
	//
	// Bytes are the honest question: same hash, same program, nothing to do.
	SHA256 string
	// Dir is where the download lands, which should be the directory the
	// binary itself lives in. The last step is a rename, and a rename across
	// volumes is a copy that can fail with the old binary already moved aside.
	Dir     string
	Install func(path string, o Offer) error
}

// take is a room acting on an offer, once.
//
// ONCE, AND THAT IS WHAT `busy` IS FOR. The hub repeats its offer on every
// reconnect, and a room that reconnects three times while downloading would
// otherwise be downloading three times.
type taker struct {
	mu   sync.Mutex
	busy bool
	// settled is every hash this room has finished with, whether it installed
	// it or rejected it.
	//
	// A FAILURE COUNTS AS SETTLED, and that is not giving up quietly. The hub
	// repeats its offer on every reconnect, so a build that does not match its
	// hash would otherwise be fetched again on every reconnect: tens of
	// megabytes across the network, every time, for a result that cannot
	// change until the offer does. The mismatch is logged loudly once, which
	// is the part somebody needs to see.
	//
	// It is keyed by hash, so a hub that fixes the problem and offers a
	// different build is tried immediately.
	settled map[string]bool
}

func (t *taker) start(sha string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.busy || t.settled[sha] {
		return false
	}
	t.busy = true
	return true
}

func (t *taker) finish(sha string, _ bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.busy = false
	if t.settled == nil {
		t.settled = map[string]bool{}
	}
	t.settled[sha] = true
}

// consider is the room deciding what to do about an offer.
func (r *Room) consider(ctx context.Context, o *Offer) {
	if o == nil || r.Upgrades == nil || !r.Upgrades.Accept {
		return
	}
	// THE ROOM ASKS THE SAME QUESTIONS THE HUB ASKED, and these are the
	// answers that matter. The hub's version of this check is a courtesy to
	// avoid saying something pointless; a hub that skipped it, or lied, still
	// gets nowhere.
	if o.OS != runtime.GOOS || o.Arch != runtime.GOARCH {
		log.Printf("[link] the hub offered a %s/%s build and this is %s/%s. ignoring it",
			o.OS, o.Arch, runtime.GOOS, runtime.GOARCH)
		return
	}
	if o.SHA256 == "" || o.Size <= 0 || o.Size > upgradeLimit {
		log.Printf("[link] the hub offered something that does not describe a binary. ignoring it")
		return
	}
	// ALREADY RUNNING THESE BYTES. Checked before the version, because it is
	// the question the version was standing in for, and it is the one that is
	// right during development where every build is called `dev`.
	if r.Upgrades.SHA256 != "" && o.SHA256 == r.Upgrades.SHA256 {
		return
	}
	if o.Version == r.Upgrades.Version && o.Version != "dev" {
		return
	}
	if !r.taking.start(o.SHA256) {
		return
	}
	go func() {
		err := r.fetchUpgrade(ctx, *o)
		r.taking.finish(o.SHA256, err == nil)
		if err != nil {
			// LOUDLY, AND ONCE. This build will not be tried again until the
			// hub offers a different one, so this line is the only record
			// that something was wrong with it.
			log.Printf("[link] REFUSED the hub's build %s: %v", o.Version, err)
			log.Printf("[link] it will not be tried again unless the hub offers a different one")
		}
	}()
}

// fetchUpgrade dials for the bytes, checks them, and hands the file over.
func (r *Room) fetchUpgrade(ctx context.Context, o Offer) error {
	log.Printf("[link] the hub is running %s and this room is %s. fetching it",
		o.Version, r.Upgrades.Version)

	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	conn, err := r.Dial.Dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	br := bufio.NewReader(conn)
	if _, err := sayHello(conn, br, hello{
		Kind: upgradeKind, Room: r.Name, Version: r.Version,
		OS: runtime.GOOS, Arch: runtime.GOARCH,
	}); err != nil {
		return err
	}

	// Beside where it will end up rather than in the system temp directory:
	// the last step is a rename, and a rename across volumes is a copy that
	// can fail with the old binary already moved aside.
	dir := os.TempDir()
	if r.Upgrades.Dir != "" {
		dir = r.Upgrades.Dir
	}
	f, err := os.CreateTemp(dir, "atrium-upgrade-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	// Removed on every failure path. A half written binary left beside the
	// real one is the kind of litter somebody eventually runs by accident.
	defer func() {
		f.Close()
		if _, err := os.Stat(tmp); err == nil {
			_ = os.Remove(tmp)
		}
	}()

	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, sum), io.LimitReader(br, o.Size))
	if err != nil {
		return fmt.Errorf("after %d of %d bytes: %w", n, o.Size, err)
	}
	if n != o.Size {
		return fmt.Errorf("the hub sent %d bytes and offered %d", n, o.Size)
	}
	got := hex.EncodeToString(sum.Sum(nil))
	if got != o.SHA256 {
		// NOT INSTALLED, AND SAID PLAINLY. This is the check that makes the
		// whole feature defensible: what arrived is not what was described,
		// and the only safe thing to do with it is delete it.
		return errors.New("what arrived hashes to " + got + " and the offer said " + o.SHA256)
	}
	if err := f.Chmod(0o755); err != nil && runtime.GOOS != "windows" {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if r.Upgrades.Install == nil {
		return errors.New("this room has no way to install one")
	}
	if err := r.Upgrades.Install(tmp, o); err != nil {
		return err
	}
	// Installed. The file is no longer this function's to remove.
	tmp = ""
	return nil
}

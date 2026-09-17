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

// Offered is what a hub is running, for a room to compare itself against.
//
// Built once when the hub starts, because hashing fifty megabytes on every
// room attach would be fifty megabytes per reconnect and the answer cannot
// change: a running process cannot have its own binary swapped under it on
// either platform without the swap being a different file.
func Offered(version string) (*Offer, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(self)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return nil, err
	}
	return &Offer{
		Version: version,
		SHA256:  hex.EncodeToString(sum.Sum(nil)),
		Size:    st.Size(),
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}, nil
}

// worthOffering reports whether a room would have any use for this.
//
// Asked on the HUB only to avoid saying something pointless. The room asks the
// same questions again and its answers are the ones that count.
func worthOffering(o *Offer, hi hello) bool {
	if o == nil || !hi.Upgrades {
		return false
	}
	if hi.OS != "" && hi.OS != o.OS {
		return false
	}
	if hi.Arch != "" && hi.Arch != o.Arch {
		return false
	}
	// Same version, nothing to say. `dev` is deliberately excluded from that
	// shortcut: two `dev` builds are different binaries most of the time, and
	// during development being able to hand a room the build you just made is
	// the entire point.
	return hi.Version != o.Version || o.Version == "dev"
}

// serveUpgrade writes the hub's own binary down a connection a room dialled
// asking for it.
func (h *Hub) serveUpgrade(conn net.Conn) {
	defer conn.Close()
	h.mu.Lock()
	o := h.offer
	h.mu.Unlock()
	if o == nil {
		_ = writeJSON(conn, welcome{OK: false, Error: "this hub has nothing to offer"})
		return
	}
	self, err := os.Executable()
	if err != nil {
		_ = writeJSON(conn, welcome{OK: false, Error: err.Error()})
		return
	}
	f, err := os.Open(self)
	if err != nil {
		_ = writeJSON(conn, welcome{OK: false, Error: err.Error()})
		return
	}
	defer f.Close()
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
	// done is the hash it has already installed, so a hub that keeps offering
	// the same build after a restart is answered with silence rather than an
	// upgrade loop.
	done string
}

func (t *taker) start(sha string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.busy || t.done == sha {
		return false
	}
	t.busy = true
	return true
}

func (t *taker) finish(sha string, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.busy = false
	if ok {
		t.done = sha
	}
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
			log.Printf("[link] could not take the hub's build: %v", err)
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

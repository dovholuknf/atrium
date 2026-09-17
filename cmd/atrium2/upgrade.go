package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dovholuknf/atrium/internal/link"
)

// Installing a build a room took from its hub.
//
// `internal/link/upgrade.go` has the design and the reason it is a pull. This
// file is the last two steps: put the verified file where the running one is,
// and stop so that whatever starts this room starts the new one.
//
// ── the swap ─────────────────────────────────────────────
//
// The same rename-aside that `internal/cli/control.go` uses, and for the same
// platform reason: Windows refuses to delete a file that is open, and an
// executable is open for as long as anything is running it. It permits a
// rename WITHIN THE SAME DIRECTORY, so the running image keeps its handle, the
// name is freed, and the new file takes it.
//
// ── and then it stops ────────────────────────────────────
//
// A process cannot become a different program. Something has to start the new
// binary, and a room is started by a service, a task, a terminal or a person.
// So this exits cleanly, the way `atrium2 room` exits on ctrl-c, and whatever
// starts rooms starts it again on the new binary. A room started by hand stays
// down, which is correct: nobody should discover that their room restarted
// itself because a hub said so.

// installUpgrade puts the fetched binary in place of the running one.
func installUpgrade(path string, o link.Offer) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return err
	}
	if filepath.Dir(path) != filepath.Dir(self) {
		// The fetch is told to land beside the binary precisely so this cannot
		// happen. A rename across volumes is a copy, and a copy can fail with
		// the old binary already moved aside.
		return fmt.Errorf("the download landed in %s and the binary is in %s",
			filepath.Dir(path), filepath.Dir(self))
	}

	dir := filepath.Dir(self)
	base := strings.TrimSuffix(filepath.Base(self), exeSuffix())
	aside := filepath.Join(dir, base+".old"+exeSuffix())

	// LAST TIME'S, NOW THAT NOTHING IS RUNNING IT. On Windows a file that is
	// still open cannot be deleted, and if that happens the rename below fails
	// and the upgrade is refused with the old binary untouched, which is the
	// safe outcome and an opaque one. So it is said.
	if err := os.Remove(aside); err != nil && !os.IsNotExist(err) {
		log.Printf("[link] %s is still there and could not be removed: %v", aside, err)
		log.Printf("[link] something is probably still running it. this upgrade will not go in")
	}
	if err := os.Rename(self, aside); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(path, self); err != nil {
		// Put it back. Failing here with the old one moved aside would leave
		// nothing at that name, which is an outage rather than a
		// disappointment.
		_ = os.Rename(aside, self)
		return err
	}
	log.Printf("[link] installed %s at %s. the previous one is %s",
		o.Version, self, filepath.Base(aside))
	log.Printf("[link] stopping so it can be started again on the new binary")

	// NOT os.Exit. The room has agents, pseudo terminals and a database, and
	// the wind-down that closes them tidily is the one the daemon already runs
	// on ctrl-c. Asking for it here means a swap costs exactly what a stop
	// costs and nothing more.
	askToStop()
	return nil
}

// acceptUpgrades is the operator's standing answer, set by `--accept-upgrades`.
//
// OFF BY DEFAULT AND NOT NEGOTIABLE FROM THE HUB. A hub that could turn this
// on would be a hub that can install a binary, which is the one thing this
// whole design refuses.
var acceptUpgrades bool

// selfDir is where the running binary lives, which is where a download has to
// land for the swap to be a rename rather than a copy.
func selfDir() string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(self); err == nil {
		self = real
	}
	return filepath.Dir(self)
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

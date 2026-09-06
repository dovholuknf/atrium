package daemon

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
)

// Where atrium is, written down for anything that needs to reach it.
//
// A hook runs in a session atrium did not start, in a shell atrium did not
// configure, and has to find the daemon without being told. Defaulting to
// localhost:7777 covers that and nothing else: a second daemon, a port already
// taken, or anyone who passed --agent-addr is invisible to every caller not
// given the same flag.
//
// Staleness is not guarded against, on purpose. A daemon killed outright
// leaves the file behind, a hook posts to a port nobody is listening on, and
// the connection is refused in milliseconds, which is the fail-open path every
// hook already handles. Locking or heartbeating would add a failure mode this
// does not have.

// Location is what a caller needs to reach a running daemon.
type Location struct {
	// Agent is the listener hooks and runners post to.
	Agent string `json:"agent"`
	// Board is the human side, for anything printing a link.
	Board string `json:"board"`
	PID   int    `json:"pid"`
	// Since dates the file, so a stale one can be recognized by a human
	// reading it even though nothing acts on it.
	Since string `json:"since"`
	// DB is which database this daemon opened. Two daemons on one machine is
	// a mistake worth being able to see.
	DB string `json:"db"`
	// Exe is the binary this daemon is running, and it is what any hook must
	// name to be considered wired.
	//
	// Here because this file is already the answer to "where is the daemon",
	// and which binary it is running is the same question one level down.
	// `atrium hook install` run from a freshly built binary used to write that
	// binary's path into settings.json, so every hook pointed at a build
	// directory while the daemon ran from an installed copy, and the board
	// reported them all as pointing elsewhere. Correctly: they were.
	//
	// See `internal/claudeconf/whichexe.go`, which reads it.
	Exe string `json:"exe,omitempty"`
}

// LocationPath is the file, in whatever this operating system calls the place
// for local runtime state.
//
// Not beside the database: a caller that has to be told where the database is
// has the problem this exists to solve. Not a dotfile in the home directory
// either, since every platform has a place for this and none of them is there.
//
// What it must NOT be is anything that roams. On Windows that rules out
// `%AppData%`, which is what `os.UserConfigDir` returns: a localhost port
// synced to another machine is worse than no file at all, because it points a
// hook somewhere confidently wrong. `os.UserCacheDir` is `%LocalAppData%`,
// which stays put.
//
// On Linux `XDG_RUNTIME_DIR` is exactly this: per-user, local, and emptied
// when the session ends, which cleans up after a daemon that was killed. State
// is the fallback for a system that does not set it.
func LocationPath() (string, error) {
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
			return filepath.Join(d, "atrium", "daemon.json"), nil
		}
		if d := os.Getenv("XDG_STATE_HOME"); d != "" {
			return filepath.Join(d, "atrium", "daemon.json"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "state", "atrium", "daemon.json"), nil
	}
	// Windows: %LocalAppData%. macOS: ~/Library/Caches. Both local to the
	// machine, which is the requirement.
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "atrium", "daemon.json"), nil
}

// SharedLocationPath is the SECOND place the address is written, for callers
// running as somebody else.
//
// `LocationPath` is per-user by design and that design has a hole in it: the
// daemon runs as one account and the things that call it do not. A script
// running as the human cannot read the daemon account's `%LocalAppData%`, so
// discovery fails and the caller is left hardcoding a path into somebody's
// profile, which is the thing the file existed to prevent.
//
// `%WORKTREE_ROOT%` is the answer because it is already the shared ground
// between those accounts: it is where the worktrees a caller is launching
// against live, so a caller that has one has this. NOT a fixed path, and not a
// second guess if the variable is unset: no shared file is written, and the
// per-user one is still there for anything running as the daemon's account.
//
// `ATRIUM_SHARED_LOCATION` names the file outright, for a machine that shares
// something other than a worktree root.
func SharedLocationPath() string {
	if p := strings.TrimSpace(os.Getenv("ATRIUM_SHARED_LOCATION")); p != "" {
		return p
	}
	root := strings.TrimSpace(os.Getenv("WORKTREE_ROOT"))
	if root == "" {
		return ""
	}
	return filepath.Join(root, "atrium", "daemon.json")
}

// sharedLocation is where THIS daemon publishes its address for other
// accounts.
//
// THE SETTING WINS over the environment. The variable is the right answer for
// a caller, which is a script somebody just ran in a configured shell, and the
// wrong one for the daemon, which is a long-lived process started by a logon
// task or a service and inherits nothing anybody exported. So the answer an
// operator can give from the machine that is already running takes precedence.
// See `api.SettingSharedLocation`.
func (d *Daemon) sharedLocation() string {
	// THE STORE MAY BE GONE. This runs from `clearLocation` on the way out,
	// and a daemon that halted or was never fully built has no store to ask.
	// Reaching into it there is a nil dereference during shutdown, which is
	// the worst moment to panic: the address file is exactly what gets left
	// behind pointing at a process that is no longer listening.
	if d.st != nil {
		if v, err := d.st.Setting(api.SettingSharedLocation); err == nil {
			if p := strings.TrimSpace(v); p != "" {
				return p
			}
		}
	}
	return SharedLocationPath()
}

// ReadLocation finds a running daemon, wherever it wrote itself down.
//
// THE PER-USER FILE FIRST, then the shared one. Order matters on a machine
// where both exist: the per-user file is written by the daemon this account is
// running, and preferring the shared copy would send a caller to somebody
// else's daemon on a box where two people each have one.
//
// Every failure is an absent Location rather than an error to report. Nothing
// that calls this is entitled to fail because atrium is not running: the whole
// posture of the hooks is that an unreachable daemon is normal.
func ReadLocation() (Location, bool) {
	tries := make([]string, 0, 2)
	if p, err := LocationPath(); err == nil {
		tries = append(tries, p)
	}
	if p := SharedLocationPath(); p != "" {
		tries = append(tries, p)
	}
	for _, p := range tries {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var loc Location
		if err := json.Unmarshal(raw, &loc); err != nil {
			continue
		}
		if strings.TrimSpace(loc.Agent) == "" && strings.TrimSpace(loc.Board) == "" {
			continue
		}
		return loc, true
	}
	return Location{}, false
}

// locationPath is where THIS daemon records its address.
//
// The machine's one true place unless the options name another. A test names
// another, because Run writes this file and deletes it again, and a test doing
// that to the real file removes the address of whatever daemon is actually
// running on the machine at the time.
func (d *Daemon) locationPath() (string, error) {
	if d.opts.LocationFile != "" {
		return d.opts.LocationFile, nil
	}
	return LocationPath()
}

// writeLocation records where this daemon is listening.
//
// Failure is logged and otherwise ignored. Callers fall back to the default
// address, which is what they did before this existed.
func (d *Daemon) writeLocation() {
	path, err := d.locationPath()
	if err != nil {
		log.Printf("[atrium] could not work out where to record my address: %v", err)
		return
	}
	loc := Location{
		Agent: "http://localhost" + d.opts.AgentAddr,
		Board: "http://localhost" + d.opts.HumanAddr,
		PID:   os.Getpid(),
		Since: time.Now().Format(time.RFC3339),
		DB:    d.opts.DBPath,
		Exe:   daemonBinary(),
	}
	// Taking the file off another daemon is allowed and is said out loud. Two
	// daemons on one machine is a mistake worth being able to see, and the
	// symptom without this line is every hook in every session quietly
	// arriving at the wrong one.
	if raw, err := os.ReadFile(path); err == nil {
		if prev, taking := takingOver(raw, os.Getpid(), processAlive); taking {
			log.Printf("[atrium] WARNING: pid %d is already listening on %s and every hook "+
				"was aimed at it. they will now arrive here instead.", prev.PID, prev.Agent)
		}
	}

	body, err := json.MarshalIndent(loc, "", "  ")
	if err != nil {
		return
	}
	if err := putLocation(path, body, 0o600); err != nil {
		log.Printf("[atrium] could not record my address: %v", err)
		return
	}
	log.Printf("[atrium] address  -> %s", filepath.ToSlash(path))

	// AND WHERE SOMEBODY ELSE CAN READ IT. See `SharedLocationPath`. Only when
	// this daemon is on the machine's real location file: a test pointing
	// `LocationFile` somewhere else must not also write a real shared one and
	// send every caller on the box to a daemon that is about to stop.
	shared := d.sharedLocation()
	if shared == "" || d.opts.LocationFile != "" {
		return
	}
	// READABLE, unlike the one above. The whole point is another account, and
	// a mode nobody else can open is the bug this is fixing. There is nothing
	// secret in it: a loopback port, a pid, and two paths. On Windows the mode
	// is mostly advisory and the directory's own permissions decide, which is
	// why the shared root is the operator's choice rather than a path atrium
	// invents.
	if err := putLocation(shared, body, 0o644); err != nil {
		log.Printf("[atrium] could not record my address where others can read it: %v", err)
		return
	}
	log.Printf("[atrium] shared   -> %s", filepath.ToSlash(shared))
}

// putLocation writes one copy of the address file.
//
// THROUGH A TEMPORARY FILE AND A RENAME, because a caller may read this at any
// moment and a half-written file parses as nothing. A rename is atomic on both
// filesystems atrium runs on, so a reader sees the old address or the new one
// and never a truncated document.
func putLocation(path string, body []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, append(body, '\n'), perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// daemonBinary is this process's own path, resolved.
//
// `os.Executable()` is the right call HERE and nowhere else: this is the
// daemon, so its own binary is the answer by definition. Everything outside
// the daemon reads it back out of the location file rather than asking itself.
func daemonBinary() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return filepath.ToSlash(exe)
}

// takingOver reports whether writing this file would take it off a daemon
// that is still running.
//
// Three things all have to be true, and each of the other cases is a normal
// one that must stay silent: a file this process already owns is a restart in
// place, a file naming a process that has gone is what a daemon killed outright
// leaves behind, and a file that does not parse says nothing at all.
//
// The liveness check is a parameter so this can be tested without arranging
// for a second live process, which is not a thing a test can portably do.
func takingOver(raw []byte, myPID int, alive func(int) bool) (Location, bool) {
	var prev Location
	if err := json.Unmarshal(raw, &prev); err != nil {
		return prev, false
	}
	if prev.PID == 0 || prev.PID == myPID {
		return prev, false
	}
	return prev, alive(prev.PID)
}

// handleHooksChanged says a board should re-read settings.json now.
//
// Nothing here reads the file or decides anything. `atrium hook install`
// already made the edit and the board already knows how to inspect it: this
// only removes the wait, which is otherwise up to one poll and only while the
// runners tab happens to be open.
//
// Answered before doing anything, like every other hook endpoint. A caller
// that has already written the file has nothing to do with a failure here, and
// the poll is still behind it either way.
func (d *Daemon) handleHooksChanged(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
	d.ap.Broadcast("hooks", map[string]any{"at": time.Now().Format(time.RFC3339)})
}

// clearLocation removes the file on the way out, so the next hook to run does
// not aim at a daemon that has stopped.
func (d *Daemon) clearLocation() {
	path, err := d.locationPath()
	if err != nil {
		return
	}
	removeIfMine(path)
	// The shared copy goes with it, under the same rule, or a caller running
	// as somebody else keeps being sent to a daemon that has stopped. Guarded
	// the same way as the write: a test with its own location file never wrote
	// a shared one, so it must not delete the real one either.
	if shared := d.sharedLocation(); shared != "" && d.opts.LocationFile == "" {
		removeIfMine(shared)
	}
}

// removeIfMine deletes an address file only while it still describes this
// process. A second daemon that started while this one was winding down owns
// the file now, and taking it would unhook every session on the machine.
func removeIfMine(path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var loc Location
	if err := json.Unmarshal(raw, &loc); err == nil && loc.PID != os.Getpid() {
		return
	}
	_ = os.Remove(path)
}

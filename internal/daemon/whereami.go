package daemon

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Where atrium is, written down for anything that needs to reach it.
//
// A hook runs in a session atrium did not start, in a shell atrium did not
// configure, and it has to find the daemon without being told. Defaulting to
// localhost:7777 covers the common case and nothing else: a second daemon, a
// port already taken, or anyone who passed --agent-addr is invisible to every
// caller that was not also given the same flag.
//
// A file in a known place is what makes the flag unnecessary. The daemon knows
// its own address, so it writes it, and the callers read it.
//
// Staleness is not guarded against, on purpose. A daemon killed outright
// leaves the file behind, a hook posts to a port nobody is listening on, and
// the connection is refused in milliseconds. That is the same fail-open path
// as atrium simply being down, which every hook already handles. Locking or
// heartbeating this would add a way for it to go wrong that the current
// version does not have.

// Location is what a caller needs to reach a running daemon.
type Location struct {
	// Agent is the listener hooks and runners post to.
	Agent string `json:"agent"`
	// Board is the human side, for anything printing a link.
	Board string `json:"board"`
	PID   int    `json:"pid"`
	// Since dates the file, so a stale one can be recognised by a human
	// reading it even though nothing acts on it.
	Since string `json:"since"`
	// DB is which database this daemon opened. Two daemons on one machine is
	// a mistake worth being able to see.
	DB string `json:"db"`
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

// writeLocation records where this daemon is listening.
//
// Failure is logged and otherwise ignored. Callers fall back to the default
// address, which is what they did before this existed.
func (d *Daemon) writeLocation() {
	path, err := LocationPath()
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
	}
	body, err := json.MarshalIndent(loc, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		log.Printf("[atrium] could not record my address: %v", err)
		return
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
		log.Printf("[atrium] could not record my address: %v", err)
		return
	}
	log.Printf("[atrium] address  -> %s", filepath.ToSlash(path))
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
	path, err := LocationPath()
	if err != nil {
		return
	}
	// Only if it is still describing this process. A second daemon that
	// started while this one was winding down owns the file now.
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

// Package addrfile finds the daemon: the address file it writes and the paths it is written to. It imports nothing of
// atrium so the statusline and the hooks can read it.
package addrfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	// AgentListen and BoardListen are the addresses this daemon BOUND, as it
	// was given them (`127.0.0.1:7778`, `0.0.0.0:7778`). Agent and Board above
	// are where to REACH it and lose the host (`:7778` becomes localhost), so
	// a restart that rebuilt its bind from them could not tell a loopback
	// daemon from one open to the LAN.
	AgentListen string `json:"agent_listen,omitempty"`
	BoardListen string `json:"board_listen,omitempty"`
	// Room is set when this process is a room (`atrium room`) and empty for a
	// plain `atrium daemon`. A restart reads it to come back as the same mode.
	Room string `json:"room,omitempty"`
	// RoomDir is the room's --dir, so a room started outside the default
	// directory restarts in the same one.
	RoomDir string `json:"room_dir,omitempty"`
	// Service is set when the SCM runs this room, so a restart goes through the SCM and never a detached respawn.
	Service bool `json:"service,omitempty"`
	PID     int  `json:"pid"`
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
	// See `internal/base/claudeconf/whichexe.go`, which reads it.
	Exe string `json:"exe,omitempty"`
	// HubDialToken lets a command of this user use the room's hub door. Never in the shared file.
	HubDialToken string `json:"hubdial_token,omitempty"`
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
	// `ATRIUM_LOCATION` NAMES THE FILE OUTRIGHT, and it exists so a second
	// atrium on one machine cannot steal the first one's hooks.
	//
	// The address file is how every hook, every CLI call and every MCP tool
	// finds the daemon. Two daemons under one account both writing this path
	// means the second one silently takes every hook on the machine, and the
	// symptom is not an error: it is activity arriving at a board nobody is
	// looking at. `docs/ui/preview-design.md` says what that costs.
	//
	// A room sets this before it opens anything, and a second room started
	// with `--isolated` names a private file, so the machine's room keeps its
	// own file and its own hooks. A daemon spawns its
	// runners with its own environment, so the hooks those agents fire inherit
	// the variable and report to the room that started them, which is the
	// behaviour that makes two atriums on one machine coherent rather than
	// merely non-colliding.
	if p := strings.TrimSpace(os.Getenv("ATRIUM_LOCATION")); p != "" {
		return p, nil
	}
	return DefaultLocationPath()
}

// DefaultLocationPath is the machine's one place for the address file, the
// answer `LocationPath` gives when `ATRIUM_LOCATION` names nothing. A room that
// is the machine's only daemon records itself here, so a hook in a session
// atrium did not start finds it without being told.
func DefaultLocationPath() (string, error) {
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
		// `-` MEANS NONE, and it is here so a second atrium can refuse to
		// publish itself as the machine's. Unsetting the variable would not do
		// it: the fallback below writes into `%WORKTREE_ROOT%`, which on a
		// machine that has one is exactly where the first atrium already wrote,
		// and the second would quietly take the address over.
		if p == "-" {
			return ""
		}
		return p
	}
	root := strings.TrimSpace(os.Getenv("WORKTREE_ROOT"))
	if root == "" {
		return ""
	}
	return filepath.Join(root, "atrium", "daemon.json")
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

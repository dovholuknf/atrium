package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/daemon"
)

// The temporary `atrium2` command line, for this machine's live scripts.
//
// TEMPORARY, AND FOR ONE MACHINE. The deploy scripts in `~/.atrium2/scripts`
// run `atrium2.exe hub ...` and `atrium2.exe room ...` with every path spelled
// out, and they must keep working between the merge and the cutover. So
// `cmd/atrium2` stays as a ten-line main over `ExecuteAtrium2`, which answers
// those exact lines the way the old binary did: the old command names, the old
// flag names, the old defaults, and the old address file. It is deleted in
// stage 4 of `docs/one-atrium-plan.md`. See `docs/one-atrium-cutover.md`.
//
// Everything else is the one binary: `atrium2 hook ...` works too, which is
// what makes the board's "install hooks" safe while the room still runs from
// `atrium2.exe`.

// legacy is true in the atrium2 shim, and only there. It picks the old
// defaults and the old address file. Set once, before the root is built,
// because cobra reads flag defaults when a command is constructed.
var legacy bool

// ExecuteAtrium2 runs the shim's command line. Returns the exit code.
func ExecuteAtrium2() int {
	legacy = true
	return runRoot(newAtrium2Root(), os.Args[1:])
}

// newAtrium2Root is the one binary's root with the three names that collide
// put back the way `atrium2` had them: `hub` is the atrium, `join` is a room's
// first join, and `room` runs the room.
func newAtrium2Root() *cobra.Command {
	root := newRoot()
	root.Use = "atrium2"
	// As atrium2 had it: a failure is its one line, never the usage after it.
	root.SilenceUsage = true
	for _, c := range root.Commands() {
		// `atrium join` is the session command. In atrium2 the word meant a
		// room's first join, and a script that typed it meant that.
		if c.Name() == "join" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(joinCmd(), hubCmd())
	return root
}

// atriumCmd is how a hint names a command, so the shim's hints name the
// command the shim has. `sub` is the one binary's spelling.
func atriumCmd(sub string) string {
	if !legacy {
		return "atrium " + sub
	}
	switch {
	case strings.HasPrefix(sub, "rooms"):
		return "atrium2 hub room" + strings.TrimPrefix(sub, "rooms")
	case sub == "room join":
		return "atrium2 join"
	case sub == "backups restore":
		return "atrium2 hub restore"
	case sub == "backups":
		return "atrium2 hub backups"
	case strings.HasPrefix(sub, "run"):
		return "atrium2 hub"
	}
	return "atrium2 " + sub
}

// The defaults, which differ between the one binary and the shim.
//
// The one binary's are this machine's layout (clint, 2026-09-25): the board on
// 7778, the link on 7779, the room's own board on 7781 and its agent listener
// on 7777, which is also where a hook with no address file posts. Directories
// sit beside the database atrium has always used. The shim's are what atrium2
// shipped with, unchanged, because a line that leans on one must behave as it
// did the day it was written.

func defaultBoardAddr() string { return pick(":7800", "127.0.0.1:7778") }
func defaultLinkAddr() string  { return pick(":7801", "127.0.0.1:7779") }
func defaultRoomHTTP() string  { return pick("127.0.0.1:7810", "127.0.0.1:7781") }
func defaultRoomAgent() string { return pick("127.0.0.1:7811", "127.0.0.1:7777") }

func pick(old, now string) string {
	if legacy {
		return old
	}
	return now
}

// hubDir is where the atrium keeps its certificates and its store.
//
// NEVER THE ROOM'S DIRECTORY, so running both on one machine cannot have either
// overwrite the other's keys.
func hubDir() string {
	if legacy {
		return atrium2Dir("hub")
	}
	return filepath.Join(daemon.StateDir(), "hub")
}

// roomDir is where a room keeps its key, its certificate and room.json.
func roomDir() string {
	if legacy {
		return atrium2Dir("room")
	}
	return filepath.Join(daemon.StateDir(), "room")
}

// defaultRoomDB is the room's database.
//
// The one binary's is the database `atrium daemon` has always opened, because a
// room is the daemon now and there is one of them per machine. The shim keeps
// atrium2's clean slate, which was deliberately NOT that file.
func defaultRoomDB() string {
	if legacy {
		return filepath.Join(atrium2Dir("room"), "atrium2.db")
	}
	return daemon.DefaultDBPath()
}

func atrium2Dir(leaf string) string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "atrium2", leaf)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".atrium2", leaf)
}

// roomLocation is the room's own address file.
//
// The one binary's room is the machine's daemon, so it records itself in the
// machine's one place and every hook finds it untold. The shim's room records
// itself where atrium2 always did, which is the file every session it launched
// was told about in ATRIUM_LOCATION, and moving it before the cutover would
// leave all of those reading a file nobody writes.
func roomLocation() string {
	if !legacy {
		if p, err := daemon.DefaultLocationPath(); err == nil {
			return p
		}
	}
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "atrium2", "room", "daemon.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".atrium2", "room", "daemon.json")
}

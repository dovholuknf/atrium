package cli

import (
	"path/filepath"

	"github.com/dovholuknf/atrium/internal/daemon"
)

// atriumCmd is how a hint names a command. `sub` is the subcommand.
func atriumCmd(sub string) string {
	return "atrium " + sub
}

// The defaults are this machine's layout (clint, 2026-09-25): the board on
// 7778, the link on 7779, the room's own board on 7781 and its agent listener
// on 7777, which is also where a hook with no address file posts. Directories
// sit beside the database atrium has always used.

func defaultBoardAddr() string { return "127.0.0.1:7778" }
func defaultLinkAddr() string  { return "127.0.0.1:7779" }
func defaultRoomHTTP() string  { return "127.0.0.1:7781" }
func defaultRoomAgent() string { return "127.0.0.1:7777" }

// hubDir is where the atrium keeps its certificates and its store.
//
// NEVER THE ROOM'S DIRECTORY, so running both on one machine cannot have either
// overwrite the other's keys.
func hubDir() string {
	return filepath.Join(daemon.StateDir(), "hub")
}

// roomDir is where a room keeps its key, its certificate and room.json.
func roomDir() string {
	return filepath.Join(daemon.StateDir(), "room")
}

// defaultRoomDB is the room's database: the one `atrium daemon` has always
// opened, because a room is the daemon now and there is one of them per machine.
func defaultRoomDB() string {
	return daemon.DefaultDBPath()
}

// roomLocation is the room's own address file. The room is the machine's
// daemon, so it records itself in the machine's one place and every hook finds
// it untold.
func roomLocation() string {
	if p, err := daemon.DefaultLocationPath(); err == nil {
		return p
	}
	return filepath.Join(daemon.StateDir(), "daemon.json")
}

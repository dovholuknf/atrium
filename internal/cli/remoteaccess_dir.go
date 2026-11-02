package cli

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/link/transport"
)

// runningRoomDir is the directory the room running on this machine keeps its certificate in, as that room recorded it
// in its address file. "" when no room has recorded itself. A room that did not record a directory is on the default.
func runningRoomDir() string {
	loc, ok := daemon.ReadLocation()
	if !ok || loc.Room == "" {
		return ""
	}
	if loc.RoomDir != "" {
		return loc.RoomDir
	}
	return roomDir()
}

func sameDir(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func holdsRoomCert(dir string) bool {
	return dir != "" && transport.Keys{Dir: dir}.HasRoomCert()
}

// pickRoomDir is the directory remote-access reads and writes, and a line saying which room is running here.
//
// A --dir that holds no room certificate is REFUSED: the file is only read by a room that lives in that directory, so
// writing it anywhere else answered "on" for a door nothing would ever open (sg3, 2026-10-14). Without --dir the
// directory the running room recorded is taken first, then the default, and an error says where it looked.
func pickRoomDir(flag string, running string) (dir, note string, err error) {
	if running != "" {
		note = "the running room on this machine uses " + running
	}
	if flag != "" {
		if !holdsRoomCert(flag) {
			msg := fmt.Sprintf("%s holds no room certificate, so no room lives there", flag)
			if running != "" {
				msg += ". " + note + ". pass --dir " + running + " or leave --dir off"
			}
			return "", "", fmt.Errorf("%s", msg)
		}
		return flag, note, nil
	}
	def := roomDir()
	if holdsRoomCert(running) {
		return running, note, nil
	}
	if holdsRoomCert(def) {
		return def, note, nil
	}
	looked := def
	if running != "" && !sameDir(running, def) {
		looked += " and " + running + " (the running room's)"
	}
	return "", "", fmt.Errorf("no room certificate found: looked in %s. this account has no room here, or it keeps one elsewhere: pass --dir", looked)
}

// dirLine is what an answer says about where it read or wrote, with a warning when it is not the running room's.
func dirLine(dir, running string) string {
	s := "room directory: " + dir
	if running == "" {
		return s
	}
	if sameDir(dir, running) {
		return s + " (the running room's)"
	}
	return s + "\nnote: the running room on this machine uses " + running + ", so this change does not reach it"
}

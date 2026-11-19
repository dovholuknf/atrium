package roomconf

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/base/addrfile"
)

// What the address file holds and where it lives are in addrfile, so the statusline can read it without this package.
type Location = addrfile.Location

var (
	LocationPath        = addrfile.LocationPath
	DefaultLocationPath = addrfile.DefaultLocationPath
	SharedLocationPath  = addrfile.SharedLocationPath
	ReadLocation        = addrfile.ReadLocation
)

// sharedLocation is where THIS daemon publishes its address for other
// accounts.
//
// THE SETTING WINS over the environment. The variable is the right answer for
// a caller, which is a script somebody just ran in a configured shell, and the
// wrong one for the daemon, which is a long-lived process started by a logon
// task or a service and inherits nothing anybody exported. So the answer an
// operator can give from the machine that is already running takes precedence.
// See `api.SettingSharedLocation`.
func (d *Service) SharedLocation() string {
	// THE STORE MAY BE GONE. This runs from `clearLocation` on the way out,
	// and a daemon that halted or was never fully built has no store to ask.
	// Reaching into it there is a nil dereference during shutdown, which is
	// the worst moment to panic: the address file is exactly what gets left
	// behind pointing at a process that is no longer listening.
	if d.St != nil {
		if v, err := d.St.Setting(api.SettingSharedLocation); err == nil {
			if p := strings.TrimSpace(v); p != "" {
				return p
			}
		}
	}
	return SharedLocationPath()
}

// locationPath is where THIS daemon records its address.
//
// The machine's one true place unless the options name another. A test names
// another, because Run writes this file and deletes it again, and a test doing
// that to the real file removes the address of whatever daemon is actually
// running on the machine at the time.
func (d *Service) LocationPath() (string, error) {
	if d.Opts().LocationFile != "" {
		return d.Opts().LocationFile, nil
	}
	return LocationPath()
}

// writeLocation records where this daemon is listening.
//
// Failure is logged and otherwise ignored. Callers fall back to the default
// address, which is what they did before this existed.
func (d *Service) WriteLocation() {
	path, err := d.LocationPath()
	if err != nil {
		log.Printf("[atrium] could not work out where to record my address: %v", err)
		return
	}
	loc := Location{
		Agent:       AddressOf(d.Opts().AgentAddr),
		Board:       AddressOf(d.Opts().HumanAddr),
		AgentListen: strings.TrimSpace(d.Opts().AgentAddr),
		BoardListen: strings.TrimSpace(d.Opts().HumanAddr),
		Room:        d.Opts().Room,
		RoomDir:     d.Opts().RoomDir,
		Service:     d.Opts().Service,
		PID:         os.Getpid(),
		Since:       time.Now().Format(time.RFC3339),
		DB:          d.Opts().DBPath,
		Exe:         DaemonBinary(),
	}
	// A LIVE ROOM'S FILE IS NOT TAKEN. A throwaway room or a second daemon that
	// writes the machine's file aims every session's hook at itself, and the
	// permission hook then waits on a process nobody is watching. So a daemon
	// that would write the machine's own place, and finds another live process
	// already recorded there, leaves it alone and says so. A daemon that names
	// its own `LocationFile` (a test, a preview) is not asking for the machine's
	// place and may take its own file over, which is only announced.
	if raw, err := os.ReadFile(path); err == nil {
		if prev, taking := TakingOver(raw, os.Getpid(), d.PidAlive); taking {
			if d.Opts().LocationFile == "" {
				log.Printf("[atrium] NOT recording my address: pid %d is already listening on %s "+
					"and every hook is aimed at it. start a second or throwaway room with "+
					"--isolated to keep them apart.", prev.PID, prev.Agent)
				return
			}
			log.Printf("[atrium] WARNING: pid %d is already listening on %s and every hook "+
				"was aimed at it. they will now arrive here instead.", prev.PID, prev.Agent)
		}
	}

	shareable, err := json.MarshalIndent(loc, "", "  ")
	if err != nil {
		return
	}
	loc.HubDialToken = d.Opts().HubDialToken
	body, err := json.MarshalIndent(loc, "", "  ")
	if err != nil {
		return
	}
	if err := PutLocation(path, body, 0o600); err != nil {
		log.Printf("[atrium] could not record my address: %v", err)
		return
	}
	log.Printf("[atrium] address  -> %s", filepath.ToSlash(path))

	// AND WHERE SOMEBODY ELSE CAN READ IT. See `SharedLocationPath`. Only when
	// this daemon is on the machine's real location file: a test pointing
	// `LocationFile` somewhere else must not also write a real shared one and
	// send every caller on the box to a daemon that is about to stop.
	shared := d.SharedLocation()
	if shared == "" || d.Opts().LocationFile != "" {
		return
	}
	// READABLE, unlike the one above. The whole point is another account, and
	// a mode nobody else can open is the bug this is fixing. There is nothing
	// secret in it: a loopback port, a pid, and two paths. On Windows the mode
	// is mostly advisory and the directory's own permissions decide, which is
	// why the shared root is the operator's choice rather than a path atrium
	// invents.
	if err := PutLocation(shared, shareable, 0o644); err != nil {
		log.Printf("[atrium] could not record my address where others can read it: %v", err)
		return
	}
	log.Printf("[atrium] shared   -> %s", filepath.ToSlash(shared))
}

// addressOf turns a listen address into one somebody can open.
//
// `:7778` means every interface, so the host to reach it on is this machine,
// and gluing `localhost` to the front is right. `127.0.0.1:53895`, which is
// what `atrium preview` passes, ALREADY HAS A HOST, and gluing produced
// `http://localhost127.0.0.1:53895` in the log and in the address file: an
// address that is not wrong so much as unparseable.
func AddressOf(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if strings.HasPrefix(addr, ":") {
		return "http://localhost" + addr
	}
	return "http://" + addr
}

// putLocation writes one copy of the address file.
//
// THROUGH A TEMPORARY FILE AND A RENAME, because a caller may read this at any
// moment and a half-written file parses as nothing. A rename is atomic on both
// filesystems atrium runs on, so a reader sees the old address or the new one
// and never a truncated document.
func PutLocation(path string, body []byte, perm os.FileMode) error {
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
func DaemonBinary() string {
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
func TakingOver(raw []byte, myPID int, alive func(int) bool) (Location, bool) {
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
func (d *Service) HandleHooksChanged(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
	d.Ap.Broadcast("hooks", map[string]any{"at": time.Now().Format(time.RFC3339)})
}

// clearLocation removes the file on the way out, so the next hook to run does
// not aim at a daemon that has stopped.
//
// ONCE, AND WHILE THE STORE IS STILL OPEN. The shared path is a setting, so
// finding it reads the store. The shutdown path calls this just before it
// closes the store, and Run's deferred call afterwards is then a no-op. Reading
// a closed store is a failure the store halts on, which is how a clean stop used
// to end in `HALTED: sql: database is closed`.
func (d *Service) ClearLocation() {
	d.ClearOnce.Do(d.RemoveLocation)
}

func (d *Service) RemoveLocation() {
	path, err := d.LocationPath()
	if err != nil {
		return
	}
	RemoveIfMine(path)
	// The shared copy goes with it, under the same rule, or a caller running
	// as somebody else keeps being sent to a daemon that has stopped. Guarded
	// the same way as the write: a test with its own location file never wrote
	// a shared one, so it must not delete the real one either.
	if shared := d.SharedLocation(); shared != "" && d.Opts().LocationFile == "" {
		RemoveIfMine(shared)
	}
}

// removeIfMine deletes an address file only while it still describes this
// process. A second daemon that started while this one was winding down owns
// the file now, and taking it would unhook every session on the machine.
func RemoveIfMine(path string) {
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

// tellWhereIAm puts this daemon's own address file in a child's environment
// when it keeps one somewhere other than where `LocationPath` would look.
//
// THE HOOKS A RUNNER FIRES FIND THEIR DAEMON THROUGH ATRIUM_LOCATION, or else
// the machine's shared file. A daemon given its own `LocationFile`, a throwaway
// room or a test, wrote its address there and never told its children, so their
// hooks read the shared file and reported to whatever room owns it. That is how
// a test's runner put cards on the live board. Told, they report here, and to
// nothing once this daemon is gone.
func (d *Service) TellWhereIAm(env map[string]string) {
	if p := strings.TrimSpace(d.Opts().LocationFile); p != "" {
		env["ATRIUM_LOCATION"] = p
	}
}

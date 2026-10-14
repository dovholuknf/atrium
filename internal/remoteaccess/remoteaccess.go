// Package remoteaccess is a room's switch for letting its own sshd be reached through the hub, the file
// `remote_access.json` beside the room's key. See docs/rnd/ssh-via-hub.md.
//
// ATRIUM CARRIES BYTES AND HOLDS NO CREDENTIAL. This file says whether the room's own sshd (127.0.0.1, one port) may
// be reached over the link, and by whom. The login is sshd's own.
//
// ONLY THE CLI ON THE ROOM'S MACHINE WRITES IT. Nothing on the link, the agent listener, the room's api or its board
// calls Save. The room reads it at every stream open, and the environment can only turn it off.
package remoteaccess

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// File is the switch's name inside the room's key directory, and LogFile the room's record of streams.
const (
	File    = "remote_access.json"
	LogFile = "remote_access.log"
)

// EnvForce is the one environment variable. `off` forces the switch off whatever the file says. No value turns it on.
const EnvForce = "ATRIUM_REMOTE_ACCESS"

// Version is the only file version this reads, and DefaultPort the sshd port when none is given.
const (
	Version     = 1
	DefaultPort = 22
)

// State is the file. Host is not here: it is always 127.0.0.1.
type State struct {
	Version int      `json:"version"`
	On      bool     `json:"on"`
	Port    int      `json:"port"`
	From    []string `json:"from"`
	SetAt   string   `json:"set_at,omitempty"`
	SetBy   string   `json:"set_by,omitempty"`
}

var fromRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ParseFrom reads `hub,m1mini` or `*`. Every entry is `hub`, `*` or a room name.
func ParseFrom(s string) ([]string, error) {
	var out []string
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f == "" {
			continue
		}
		if f != "*" && !fromRE.MatchString(f) {
			return nil, fmt.Errorf("from entry %q must be hub, * or a room name", f)
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		return nil, errors.New("from must name hub, a room, or *")
	}
	return out, nil
}

// Path is where the file lives under dir.
func Path(dir string) string { return filepath.Join(dir, File) }

// Load reads the file. A missing file is off.
func Load(dir string) (State, error) {
	b, err := os.ReadFile(Path(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return State{Version: Version, Port: DefaultPort}, nil
	}
	if err != nil {
		return State{}, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return State{}, fmt.Errorf("%s is not valid: %w", Path(dir), err)
	}
	if s.Version != Version {
		return State{}, fmt.Errorf("%s has version %d and this reads version %d", Path(dir), s.Version, Version)
	}
	if s.Port == 0 {
		s.Port = DefaultPort
	}
	if s.Port < 1 || s.Port > 65535 {
		return State{}, fmt.Errorf("%s has a port that is not 1 to 65535", Path(dir))
	}
	for _, f := range s.From {
		if f != "*" && !fromRE.MatchString(f) {
			return State{}, fmt.Errorf("%s: from entry %q must be hub, * or a room name", Path(dir), f)
		}
	}
	return s, nil
}

// Save writes the file through a temp file and a rename, mode 0600, stamping set_at.
func Save(dir string, s State) error {
	s.Version = Version
	if s.Port == 0 {
		s.Port = DefaultPort
	}
	if s.From == nil {
		s.From = []string{}
	}
	if s.SetAt == "" {
		s.SetAt = time.Now().UTC().Format(time.RFC3339)
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, File+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(append(b, '\n'))
	cerr := f.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp, 0o600)
	}
	if werr == nil {
		werr = os.Rename(tmp, Path(dir))
	}
	if werr != nil {
		os.Remove(tmp)
	}
	return werr
}

// Allows says whether caller may open the room's sshd: the switch is on and the list holds * or the name.
func (s State) Allows(caller string) bool {
	if !s.On || caller == "" {
		return false
	}
	for _, f := range s.From {
		if f == "*" || strings.EqualFold(f, caller) {
			return true
		}
	}
	return false
}

// Status is the file as it applies right now: the environment's veto folded in, and the reason a file could not be read.
type Status struct {
	State
	// Forced is ATRIUM_REMOTE_ACCESS=off in the room's environment.
	Forced bool
	// Err is why the file could not be read. The status is off then.
	Err error
}

// Effective is what a room acts on. Off whenever the file is missing, broken, or the environment says off.
func Effective(dir string) Status {
	s, err := Load(dir)
	st := Status{State: s}
	if err != nil {
		st.State = State{Version: Version, Port: DefaultPort}
		st.Err = err
		return st
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv(EnvForce)), "off") {
		st.On, st.Forced = false, true
	}
	return st
}

// Describe is one line for `atrium room remote-access`.
func (s Status) Describe() string {
	switch {
	case s.Forced:
		return "off (forced by " + EnvForce + ")"
	case s.Err != nil:
		return "off (" + s.Err.Error() + ")"
	case !s.On:
		return "off"
	}
	return fmt.Sprintf("on, port %d, from %s", s.Port, strings.Join(s.From, ", "))
}

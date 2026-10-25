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
	"net"
	"net/netip"
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

// DefaultUser is the login other rooms use when the room has not said otherwise.
const DefaultUser = "claude"

// LoginUser is the account other rooms log in as.
func (s State) LoginUser() string {
	if s.User == "" {
		return DefaultUser
	}
	return s.User
}

var userRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$`)

// ValidUser says whether s is a login name safe to write into an ssh config and an authorized keys file.
func ValidUser(s string) bool { return userRE.MatchString(s) }

// DefaultAddr is the address of sshd when the file names none.
const DefaultAddr = "127.0.0.1"

// State is the file. Addr is where this machine's own sshd answers: 127.0.0.1 unless the which-sshd probe found that
// loopback belongs to something else (wslrelay).
type State struct {
	Version int      `json:"version"`
	On      bool     `json:"on"`
	Addr    string   `json:"addr,omitempty"`
	Port    int      `json:"port"`
	From    []string `json:"from"`
	// User is the account other rooms log in as on this machine. The room's own say, published to the hub. Empty is
	// DefaultUser.
	User  string `json:"user,omitempty"`
	SetAt string `json:"set_at,omitempty"`
	SetBy string `json:"set_by,omitempty"`
}

var hostRE = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]{0,253}[A-Za-z0-9])?$`)

// ValidAddr says whether s is an IP address or a host name. An IPv6 address is written without brackets.
func ValidAddr(s string) bool {
	if _, err := netip.ParseAddr(s); err == nil {
		return true
	}
	return hostRE.MatchString(s)
}

// Host is the address to dial, the default when the file names none.
func (s State) Host() string {
	if s.Addr == "" {
		return DefaultAddr
	}
	return s.Addr
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
	if s.Addr != "" && !ValidAddr(s.Addr) {
		return State{}, fmt.Errorf("%s has an addr that is not an IP address or a host name", Path(dir))
	}
	if s.User != "" && !ValidUser(s.User) {
		return State{}, fmt.Errorf("%s has a user that is not a login name", Path(dir))
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
		werr = renameRetrying(tmp, Path(dir))
	}
	if werr != nil {
		os.Remove(tmp)
	}
	return werr
}

// renameRetrying retries a refusal because Windows denies a replace while a reader of the switch has it open.
func renameRetrying(tmp, target string) error {
	var err error
	for i := 0; i < 10; i++ {
		if err = os.Rename(tmp, target); err == nil {
			return nil
		}
		time.Sleep(time.Duration(i+1) * 20 * time.Millisecond)
	}
	return err
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
	where := fmt.Sprintf("port %d", s.Port)
	if s.Host() != DefaultAddr {
		where = net.JoinHostPort(s.Host(), fmt.Sprint(s.Port))
	}
	return fmt.Sprintf("on, %s, from %s", where, strings.Join(s.From, ", "))
}

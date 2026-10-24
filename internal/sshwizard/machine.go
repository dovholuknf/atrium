package sshwizard

import (
	"context"
	"errors"
	"github.com/dovholuknf/atrium/internal/base/nowindow"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dovholuknf/atrium/internal/remoteaccess"
	"github.com/dovholuknf/atrium/internal/sshcfg"
)

// ErrUnavailable is what a Machine answers for something that cannot be asked yet, such as what the hub holds for this
// room before the hub has an ssh kind. A step that gets it says so and is skipped, so the list stays honest.
var ErrUnavailable = errors.New("not available yet")

// Joined is what the room saved when it joined a hub.
type Joined struct {
	Room   string
	Hub    string
	Proven bool
	// Err is why there is no join at all.
	Err error
	// Rejoin is the sentence to show when the room has no proven name.
	Rejoin string
}

// Account is who the room runs as.
type Account struct {
	Name     string
	Elevated bool
}

// Published is what the hub holds for this room.
type Published struct {
	User        string
	Port        int
	Fingerprint string
	Enabled     bool
	// On is the switch the room published with its key.
	On bool
}

// PeerKey is another room's published key and login.
type PeerKey struct {
	Room        string
	User        string
	Fingerprint string
	// Key is the public key line, type and body.
	Key string
	// Online says the room is attached, and SSHOn that it has ssh on.
	Online bool
	SSHOn  bool
}

// Machine is everything the steps ask of this machine and this room. The checks only read. The fix half (fix.go) is
// the only caller of the methods that write: WriteFile, MkdirAll, Publish and SaveUser, and Run for ssh-keygen,
// icacls and chown.
type Machine interface {
	Env
	// Run runs a read only command and returns its output. An error is a failure to run or a non zero exit.
	Run(ctx context.Context, name string, args ...string) (string, error)
	LookPath(name string) bool
	Exists(path string) bool
	Home() string
	Joined() Joined
	Account() Account
	UserExists(name string) bool
	// Switch is the saved remote_access.json, whatever the environment says.
	Switch() (remoteaccess.State, error)
	SSHConfig() (sshcfg.Shown, error)
	// Published is this room's record on the hub, nil when there is none. ErrUnavailable when the hub cannot say.
	Published(ctx context.Context) (*Published, error)
	// Peers is every other room the hub knows with a published key. ErrUnavailable when the hub cannot say.
	Peers(ctx context.Context) ([]PeerKey, error)

	// UserHome is the home directory of an account on this machine, the room's own Home when it cannot be found.
	UserHome(name string) string
	MkdirAll(path string, perm os.FileMode) error
	// WriteFile writes a file, making its directory, and replaces what was there.
	WriteFile(path string, data []byte, perm os.FileMode) error
	// Publish sends this room's login user, public key, port and switch to the hub.
	Publish(ctx context.Context, p PublishRecord) error
	// SaveUser writes the login user into the room's own switch file, the room's say and nobody else's.
	SaveUser(user string) error
}

// PublishRecord is what a room tells the hub about its own ssh.
type PublishRecord struct {
	User string
	Key  string
	Port int
	On   bool
}

// LocalMachine is the real machine. The hub's side is injected, because the hub has no ssh kind yet and because
// reaching it is the CLI's seam, not this package's.
type LocalMachine struct {
	OSEnv
	// KeyDir is the room's key directory, where remote_access.json lives.
	KeyDir    string
	JoinedFn  func() Joined
	PublishFn func(context.Context) (*Published, error)
	PeersFn   func(context.Context) ([]PeerKey, error)
	// SendFn publishes this room's record. Nil is ErrUnavailable.
	SendFn     func(context.Context, PublishRecord) error
	HomeDir    string
	ElevatedFn func() bool
}

func (m LocalMachine) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	nowindow.Hide(cmd)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (LocalMachine) LookPath(name string) bool { _, err := exec.LookPath(name); return err == nil }

func (LocalMachine) Exists(path string) bool { _, err := os.Stat(path); return err == nil }

func (m LocalMachine) Home() string {
	if m.HomeDir != "" {
		return m.HomeDir
	}
	h, _ := os.UserHomeDir()
	return h
}

func (m LocalMachine) Joined() Joined {
	if m.JoinedFn == nil {
		return Joined{Err: errors.New("no room is known here")}
	}
	return m.JoinedFn()
}

func (m LocalMachine) Account() Account {
	a := Account{}
	if u, err := user.Current(); err == nil {
		a.Name = u.Username
		if i := strings.LastIndex(a.Name, `\`); i >= 0 {
			a.Name = a.Name[i+1:]
		}
	}
	if m.ElevatedFn != nil {
		a.Elevated = m.ElevatedFn()
	} else if runtime.GOOS != "windows" {
		a.Elevated = os.Geteuid() == 0
	} else {
		// `net session` needs an elevated token and changes nothing.
		net := exec.Command("net", "session")
		nowindow.Hide(net)
		a.Elevated = net.Run() == nil
	}
	return a
}

func (LocalMachine) UserExists(name string) bool { _, err := user.Lookup(name); return err == nil }

func (m LocalMachine) Switch() (remoteaccess.State, error) { return remoteaccess.Load(m.KeyDir) }

func (m LocalMachine) SSHConfig() (sshcfg.Shown, error) { return sshcfg.Show(m.Home()) }

func (m LocalMachine) Published(ctx context.Context) (*Published, error) {
	if m.PublishFn == nil {
		return nil, ErrUnavailable
	}
	return m.PublishFn(ctx)
}

func (m LocalMachine) Peers(ctx context.Context) ([]PeerKey, error) {
	if m.PeersFn == nil {
		return nil, ErrUnavailable
	}
	return m.PeersFn(ctx)
}

func (m LocalMachine) UserHome(name string) string {
	if u, err := user.Lookup(name); err == nil && u.HomeDir != "" {
		return u.HomeDir
	}
	return m.Home()
}

func (LocalMachine) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }

func (LocalMachine) WriteFile(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(name, perm)
	}
	if werr == nil {
		werr = os.Rename(name, path)
	}
	if werr != nil {
		os.Remove(name)
	}
	return werr
}

func (m LocalMachine) Publish(ctx context.Context, p PublishRecord) error {
	if m.SendFn == nil {
		return ErrUnavailable
	}
	return m.SendFn(ctx, p)
}

func (m LocalMachine) SaveUser(user string) error {
	st, err := remoteaccess.Load(m.KeyDir)
	if err != nil {
		st = remoteaccess.State{}
	}
	st.User = user
	return remoteaccess.Save(m.KeyDir, st)
}

// KeyPath is the room's dedicated key, never the user's default.
func KeyPath(home string) string { return filepath.Join(home, ".ssh", "atrium_ed25519") }

// keyPathFor is KeyPath for the OS the machine reports, so a test on one OS can stand in for another.
func keyPathFor(goos, home string) string { return joinFor(goos, home, ".ssh", "atrium_ed25519") }

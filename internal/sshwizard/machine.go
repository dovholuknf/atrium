package sshwizard

import (
	"context"
	"errors"
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

// Machine is everything the steps ask of this machine and this room. Every method only reads.
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
}

// LocalMachine is the real machine. The hub's side is injected, because the hub has no ssh kind yet and because
// reaching it is the CLI's seam, not this package's.
type LocalMachine struct {
	OSEnv
	// KeyDir is the room's key directory, where remote_access.json lives.
	KeyDir     string
	JoinedFn   func() Joined
	PublishFn  func(context.Context) (*Published, error)
	PeersFn    func(context.Context) ([]PeerKey, error)
	HomeDir    string
	ElevatedFn func() bool
}

func (m LocalMachine) Run(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
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
		a.Elevated = exec.Command("net", "session").Run() == nil
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

// KeyPath is the room's dedicated key, never the user's default.
func KeyPath(home string) string { return filepath.Join(home, ".ssh", "atrium_ed25519") }

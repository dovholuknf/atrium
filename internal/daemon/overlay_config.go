package daemon

import (
	"encoding/json"
	"fmt"
	"strings"
)

// What each overlay needs to be told, and how that becomes a command line.
//
// Kept as one JSON blob per overlay in the settings table rather than as
// columns. These are somebody else's options and they change on somebody
// else's schedule, so a migration per field would be a migration per release
// of a tool atrium does not own.

// SettingOverlayZrok and SettingOverlayZiti are where each config lives.
const (
	SettingOverlayZrok = "overlay_zrok"
	SettingOverlayZiti = "overlay_ziti"
)

// ZrokConfig is what a zrok share needs.
type ZrokConfig struct {
	// Mode is public or private. Public gives anyone with the link the board;
	// private needs zrok access on the other end, which is the safer default
	// for something with no login.
	Mode string `json:"mode"`
	// Reserved is a share token from `zrok reserve`, so the address survives
	// a restart. Empty means an ephemeral share and a new address every time.
	Reserved string `json:"reserved"`
	// Backend is what zrok is told to publish. Defaults to the board's own
	// address, which is the only thing worth sharing here.
	Backend string `json:"backend"`
	// Extra is anything else, split on spaces, for options atrium has never
	// heard of rather than a release that needs a new field here.
	Extra string `json:"extra"`
}

// ZitiConfig is what hosting the board on an OpenZiti network needs.
//
// One field, because one field is all `ziti tunnel host` takes. Which services
// this hosts, and what each points at on this machine, are configured on the
// network and reached through the identity's bind policies. There was a
// service name and a backend address here and neither could be applied: they
// asked for something atrium had no way to send, which is worse than not
// asking, because a form that ignores what you type is one you stop trusting.
type ZitiConfig struct {
	// Identity is the path to an enrolled identity JSON. Atrium never reads
	// it: it is passed to the tunneler, which owns the private key.
	Identity string `json:"identity"`
	Extra    string `json:"extra"`
}

// defaultBackend is the board's own address, which is the thing being shared.
func (d *Daemon) defaultBackend() string {
	addr := d.opts.HumanAddr
	if strings.HasPrefix(addr, ":") {
		return "localhost" + addr
	}
	return addr
}

func (d *Daemon) zrokConfig() ZrokConfig {
	var c ZrokConfig
	raw, err := d.st.Setting(SettingOverlayZrok)
	if err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &c)
	}
	if c.Mode == "" {
		c.Mode = "private"
	}
	if c.Backend == "" {
		c.Backend = d.defaultBackend()
	}
	return c
}

func (d *Daemon) zitiConfig() ZitiConfig {
	var c ZitiConfig
	raw, err := d.st.Setting(SettingOverlayZiti)
	if err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &c)
	}
	return c
}

func (d *Daemon) saveOverlayConfig(key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return d.st.SetSetting(key, string(b))
}

// zrokArgs builds the share command.
//
// `--headless` because there is no terminal to draw in: atrium runs this as a
// child and reads its output. Without it zrok paints a full-screen interface
// into a pipe and nothing useful can be read back.
func (c ZrokConfig) zrokArgs() ([]string, error) {
	backend := strings.TrimSpace(c.Backend)
	if backend == "" {
		return nil, fmt.Errorf("nothing to share: set what zrok should publish")
	}
	var args []string
	if t := strings.TrimSpace(c.Reserved); t != "" {
		// A reserved share already knows its mode and address, so the token
		// is the whole instruction.
		args = []string{"share", "reserved", t, "--headless", "--override-endpoint", backend}
	} else {
		mode := strings.TrimSpace(c.Mode)
		if mode != "public" && mode != "private" {
			return nil, fmt.Errorf("share mode must be public or private, not %q", mode)
		}
		args = []string{"share", mode, backend, "--headless"}
	}
	return append(args, splitExtra(c.Extra)...), nil
}

// zitiArgs builds the hosting command.
//
// `ziti tunnel host` binds every service this identity is allowed to bind and
// forwards each to whatever its own configuration says. Atrium passes the
// identity path through and never opens the file: the key inside it is the
// tunneler's business.
func (c ZitiConfig) zitiArgs() ([]string, error) {
	id := strings.TrimSpace(c.Identity)
	if id == "" {
		return nil, fmt.Errorf("no identity: point atrium at an enrolled identity file")
	}
	args := []string{"tunnel", "host", "--identity", id}
	return append(args, splitExtra(c.Extra)...), nil
}

// splitExtra turns a free-text options field into arguments. Whitespace only,
// with no quote handling: an option that needs a quoted value belongs in a
// field of its own rather than in a catch-all.
func splitExtra(s string) []string {
	return strings.Fields(s)
}

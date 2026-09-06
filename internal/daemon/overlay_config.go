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
//
// The fields follow zrok v2, which is what the CLI actually accepts.
// `zrok share reserved` is gone: a stable address now comes from reusing a
// share token on a private share, or from a reserved name on a public one, and
// those are two different flags rather than one subcommand.
type ZrokConfig struct {
	// Mode is public or private. Public gives anyone with the link the board;
	// private needs zrok access on the other end, which is the safer default
	// for something with no login.
	//
	// STILL THE FIELD EVERYTHING READS, and now derived rather than typed. See
	// `Public` and `Private` below and `normalise`, which reconciles them.
	// Keeping one string as the answer to "what does starting a share do"
	// means the start path did not have to learn about a second axis.
	Mode string `json:"mode"`

	// Public and Private are what the operator ticked, and they are NOT
	// exclusive.
	//
	// A machine may reasonably want a public link for the board and a private
	// share for a lent session, or the reverse, or both. The single `mode`
	// select could not say that: it was a leftover from when a share was one
	// thing, and it forced a choice that the two features do not actually
	// share.
	//
	// `Mode` remains what the BOARD's own share starts as, because a listener
	// is one thing and has to be one of the two. These decide what is OFFERED,
	// and the board's share follows public when both are on, since that is the
	// one somebody enabling both is reaching for.
	Public  bool `json:"public"`
	Private bool `json:"private"`
	// ShareToken reuses an existing private share, from `zrok create share`,
	// so the address survives a restart. Private only: `share public` has no
	// such flag.
	ShareToken string `json:"share_token"`
	// Name is a reserved name from `zrok create name`, which is how a public
	// share keeps one address. Public only.
	Name string `json:"name"`
	// Backend is what zrok is told to publish. Defaults to the board's own
	// address, which is the only thing worth sharing here.
	Backend string `json:"backend"`
	// Extra is anything else, split on spaces, for options atrium has never
	// heard of rather than a release that needs a new field here.
	Extra string `json:"extra"`

	// OwnEnvironment puts atrium on its own zrok environment rather than the
	// machine's, so it can be enabled against a different instance while the
	// `zrok` command and everything else on the machine carry on unchanged.
	//
	// See `overlay_root.go`, which is where the consequence lives: every zrok
	// call goes through one loader so there is no path that reaches the
	// machine's environment by forgetting to ask which one to use.
	OwnEnvironment bool `json:"own_environment"`
	// ApiEndpoint is the zrok instance that environment talks to. Empty means
	// zrok's own default, which is the hosted service.
	//
	// Recorded here as well as in zrok's config file, which is not redundant:
	// this is what the operator TYPED, and zrok's copy is what is in effect.
	// They differ exactly when a change was refused, and a board that showed
	// only the second would silently drop what was asked for.
	ApiEndpoint string `json:"api_endpoint"`
}

// ZitiConfig is what serving the board on an OpenZiti network needs.
//
// Service is back after being removed. Under `ziti tunnel host` it could not
// be applied, because that command hosts whatever the identity's policies
// allow and takes no service argument, so the field asked for something atrium
// had no way to send. Serving natively, `Context.Listen(service)` takes
// exactly that, so the field now does what it says.
//
// There is no backend address. Nothing is forwarded anywhere: the board
// answers the ziti listener in this process.
type ZitiConfig struct {
	// Identity is the path to an enrolled identity JSON. The SDK opens it and
	// owns the key inside; atrium reads only the controller address out of it,
	// to say which network this is.
	Identity string `json:"identity"`
	// Service is the ziti service this board answers. It has to already exist
	// with a bind policy this identity satisfies.
	Service string `json:"service"`
	Extra   string `json:"extra"`
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
	c.normalise()
	if c.Backend == "" {
		c.Backend = d.defaultBackend()
	}
	return c
}

// normalise reconciles the two checkboxes with the one mode, in both
// directions.
//
// THE UPGRADE PATH IS THE POINT. Every install that exists was written before
// the booleans, so it holds a `mode` string and neither flag. Reading that as
// "nothing is enabled" would silently switch off sharing on every machine that
// already had it configured, and the operator would find out by pressing start
// and being refused.
//
// So: no flags and a legacy mode means derive the flags from it. Flags and no
// usable mode means derive the mode from them. Both present and agreeing is
// the ordinary case and nothing moves.
//
// Public wins when both are ticked, because the board's own share is one
// listener and has to pick, and somebody who ticked both is reaching for the
// link rather than the command.
func (c *ZrokConfig) normalise() {
	mode := strings.TrimSpace(c.Mode)
	known := mode == "public" || mode == "private"

	if !c.Public && !c.Private {
		if known {
			// An install from before the flags existed.
			c.Public, c.Private = mode == "public", mode == "private"
		} else {
			// A fresh one. Private is the safer default for a board with no
			// login in front of it, and it stays the default.
			c.Private = true
		}
	}
	switch {
	case c.Public:
		c.Mode = "public"
	case c.Private:
		c.Mode = "private"
	}
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
	mode := strings.TrimSpace(c.Mode)
	if mode != "public" && mode != "private" {
		return nil, fmt.Errorf("share mode must be public or private, not %q", mode)
	}
	args := []string{"share", mode, backend, "--headless"}

	// Each mode keeps a stable address a different way, and neither flag
	// exists on the other subcommand. Sending the wrong one is an unknown
	// flag error from zrok rather than anything atrium could explain.
	switch mode {
	case "private":
		if t := strings.TrimSpace(c.ShareToken); t != "" {
			args = append(args, "--share-token", t)
		}
	case "public":
		if n := strings.TrimSpace(c.Name); n != "" {
			args = append(args, "--name-selection", n)
		}
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

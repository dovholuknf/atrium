package daemon

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/openziti/zrok/v2/environment"
	"github.com/openziti/zrok/v2/environment/env_core"
)

// WHICH ZROK ENVIRONMENT ATRIUM USES.
//
// zrok keeps an environment in a directory, `~/.zrok2` by default, holding the
// account token, the ziti identity it was issued, and which instance the two
// belong to. The SDK finds that directory through a PACKAGE-LEVEL GLOBAL
// rather than a parameter, `environment.SetRootDirName`, and that is the whole
// mechanism available. Everything here is shaped by it.
//
// Two roots are possible and the difference is the point:
//
//   - THE MACHINE'S, `~/.zrok2`, which is what the `zrok` command uses. Atrium
//     shares from the same account as everything else on the machine, and an
//     environment enabled at a terminal is one atrium can use immediately.
//   - ATRIUM'S OWN, under the atrium home. A second environment, enabled with
//     its own token, usually against a DIFFERENT instance, while the machine's
//     stays exactly as it was.
//
// The second exists because the two answer to different people. A hosted
// account is one you keep, and the instance you stand up to try something is
// not. Switching the machine between them means disabling and re-enabling the
// environment every other tool on that machine depends on, which is a large
// price for atrium wanting to talk somewhere else.
//
// THE MACHINE'S ENVIRONMENT IS NEVER WRITTEN TO when atrium has its own. Every
// path that enables, disables or reserves goes through `zrokRoot`, so there is
// no route that reaches `~/.zrok2` by forgetting to ask.

// zrokDefaultRootDir is what the SDK uses when nothing has moved it. Restored
// explicitly rather than assumed, because the global is shared and the last
// caller wins.
const zrokDefaultRootDir = ".zrok2"

// zrokRootMu guards the SDK's global while a root is being loaded.
//
// `SetRootDirName` writes an unsynchronised package variable and `LoadRoot`
// reads it. Two goroutines reaching for different roots at once would
// otherwise be able to load each other's, which on this data means atrium
// enabling one instance's token into the other instance's environment.
//
// Held only across the load. The `Root` that comes back carries its own path,
// so everything after that is free of the global.
var zrokRootMu sync.Mutex

// zrokRootDir is the directory this daemon's zrok environment lives in.
//
// Empty means the machine's, spelled as the SDK's own relative default so it
// resolves against whatever home the process has. An absolute path is used as
// given: `rootDir` in the SDK returns it unchanged when it is absolute, which
// is the documented way to put an environment somewhere specific.
// Beside the DATABASE rather than at a fixed path, which is deliberate: this
// environment belongs to this daemon's state. A second daemon on a second
// database gets a second environment, and neither can enable over the other,
// which is the same rule the icons and the scrap directory already follow.
func (d *Daemon) zrokRootDir() string {
	cfg := d.zrokConfig()
	if !cfg.OwnEnvironment {
		return ""
	}
	dir := filepath.Dir(d.opts.DBPath)
	if dir == "" || dir == "." {
		return ""
	}
	abs, err := filepath.Abs(filepath.Join(dir, "zrok"))
	if err != nil {
		// A relative path would be resolved against the HOME directory by the
		// SDK rather than against the working directory, which would put the
		// environment somewhere nobody asked for. The machine's is the safe
		// answer, and it is loud in the log rather than silent.
		log.Printf("[atrium] could not place atrium's own zrok environment: %v", err)
		return ""
	}
	return abs
}

// zrokRoot loads whichever environment this daemon is configured to use.
//
// EVERY zrok call in atrium comes through here. A direct `environment.LoadRoot`
// reads whatever the global happens to say, which is correct exactly until
// something else moves it, and the failure is silent: the call succeeds
// against the wrong account.
func (d *Daemon) zrokRoot() (env_core.Root, error) {
	dir := d.zrokRootDir()

	zrokRootMu.Lock()
	defer zrokRootMu.Unlock()
	if dir == "" {
		environment.SetRootDirName(zrokDefaultRootDir)
	} else {
		// Created here rather than at enable time. `SetConfig` writes a file
		// into this directory and does not make it, and the first thing that
		// happens to a new own-environment is a config write.
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("could not make atrium's own zrok directory at %s: %w", dir, err)
		}
		environment.SetRootDirName(dir)
	}
	root, err := environment.LoadRoot()
	// Put it back whatever happened, so a failure here cannot leave every
	// later caller pointed at a directory this one was told to use.
	environment.SetRootDirName(zrokDefaultRootDir)
	if err != nil {
		return nil, err
	}
	return root, nil
}

// zrokWhere describes the environment in one line, for the board and the log.
//
// Names the DIRECTORY as well as the instance, because the question this
// answers is "which of the two am I looking at", and two environments against
// the same instance are told apart by nothing else.
func (d *Daemon) zrokWhere() string {
	cfg := d.zrokConfig()
	root, err := d.zrokRoot()
	if err != nil {
		return "unreadable"
	}
	api, _ := root.ApiEndpoint()
	where := "this machine's zrok environment"
	if cfg.OwnEnvironment {
		where = "atrium's own zrok environment"
	}
	if api != "" {
		where += " on " + api
	}
	if !root.IsEnabled() {
		where += ", not enabled yet"
	}
	return where
}

// describeEnvironment names this machine to a zrok account.
//
// Two values, matching what the controller wants: the host detail, which is
// the long form nobody reads, and the description, which is what shows up in
// `zrok overview` and is the only thing anybody does read.
//
// An empty description becomes `atrium@<host>` rather than `<user>@<host>`,
// which is the CLI's default. Two environments from one machine sit next to
// each other in that list, and the one somebody just made needs to be
// identifiable without counting rows.
func (d *Daemon) describeEnvironment(description string) (string, string) {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown-host"
	}
	user := os.Getenv("USERNAME")
	if user == "" {
		user = os.Getenv("USER")
	}
	if user == "" {
		user = "unknown"
	}
	detail := fmt.Sprintf("%s; %s; %s", user, host, runtime.GOOS)
	description = strings.TrimSpace(description)
	if description == "" {
		description = "atrium@" + host
	}
	return detail, description
}

// sameZrokInstance answers whether two endpoint strings name the same zrok
// instance, rather than whether they are the same bytes.
//
// CONFIRMED BUG THIS EXISTS FOR: an environment enabled against
// "https://api-v2.zrok.io/", exactly as the CLI wrote it, refused a request
// naming "https://api-v2.zrok.io", which is the same instance read back out of
// the binary's own compiled-in default with no trailing slash. Nobody asked to
// change the address; the board sent whatever the instance box already held,
// and that text differed from what was on disk by one character neither side
// meant. A byte comparison cannot tell "moved" from "spelled differently", so
// judging identity needs the structure of a URL rather than its string.
//
// A trailing slash, a host's letter case, and a default port spelled out
// explicitly are the three ways two people (or a CLI default and a compiled-in
// one) write the same address differently. All three fold away here.
func sameZrokInstance(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == b {
		// Also covers "both empty", which is not a same-instance question at
		// all but must not be reported as a mismatch either.
		return true
	}
	na, oka := normalizeZrokEndpoint(a)
	nb, okb := normalizeZrokEndpoint(b)
	if !oka || !okb {
		// A string that will not parse as a URL is not provably the same
		// instance as anything. Falling through to "different" here is the
		// safe direction: it means an unparseable address still gets the
		// refusal-and-explain path rather than a silent switch.
		return false
	}
	return na == nb
}

// normalizeZrokEndpoint reduces one endpoint to the form its identity is
// judged by: scheme and host lower-cased, a default port omitted rather than
// spelled out, and no trailing slash on the path. The bool says whether `s`
// parsed as a URL with a host at all, because "" and "not a URL" must not
// normalize to the same string and then compare equal by accident.
func normalizeZrokEndpoint(s string) (string, bool) {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if port := u.Port(); port != "" && port != defaultPortForScheme(scheme) {
		host = host + ":" + port
	}
	path := strings.TrimRight(u.Path, "/")
	return scheme + "://" + host + path, true
}

// defaultPortForScheme is the port a scheme implies when none is written, so
// ":443" on an "https" address normalizes the same as no port at all.
func defaultPortForScheme(scheme string) string {
	switch scheme {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

// SetZrokEnvironment chooses between the two roots and points one at an
// instance.
//
// Both in one call because they are one decision. Turning atrium's own
// environment on and leaving it pointed at the public instance is a second
// account against the same service, which is a thing somebody might want and
// never the thing they meant when they typed an address.
//
// The endpoint is written to whichever root is now selected, and only when
// that root is NOT enabled. Moving an enabled environment leaves a token
// issued by one instance being sent to another, which fails in a way that
// reads as a broken token rather than a wrong address.
func (d *Daemon) SetZrokEnvironment(own bool, endpoint string) error {
	endpoint = strings.TrimSpace(endpoint)

	// WHICH ENVIRONMENT IS SAVED FIRST AND ON ITS OWN. Switching between the
	// two is always legal, and `zrokRoot` reads this setting to decide which
	// one to load, so the address below has to be checked against the root that
	// is now selected rather than the one that was.
	cfg := d.zrokConfig()
	cfg.OwnEnvironment = own
	if err := d.saveOverlayConfig(SettingOverlayZrok, cfg); err != nil {
		return err
	}

	root, err := d.zrokRoot()
	if err != nil {
		return fmt.Errorf("could not read that zrok environment: %w", err)
	}

	// AN ENABLED ENVIRONMENT OWNS ITS OWN ADDRESS, so the setting follows it
	// rather than the other way round.
	//
	// The address used to be written before any of this was checked, which
	// meant a refused change was stored anyway and a no-op change cleared it.
	// That is what made the instance toggle look dead: pressing the public zrok
	// on an enabled environment recorded the public zrok, the environment went
	// on answering the instance it was enabled against, and the panel redrew
	// from the environment showing no change at all.
	if root.IsEnabled() {
		current, _ := root.ApiEndpoint()
		// Compared NORMALISED, not byte for byte. `current` is read off disk
		// (or is the SDK's own compiled-in default when nothing was ever
		// written) and `endpoint` is whatever text a caller had on hand; a
		// trailing slash or a default port spelled out differently between
		// the two is not a request to move anywhere. See `sameZrokInstance`.
		if endpoint != "" && !sameZrokInstance(current, endpoint) {
			label := "this machine's zrok environment"
			if cfg.OwnEnvironment {
				label = "atrium's own zrok environment"
			}
			// Naming FROM and TO, unnormalised, so a one-character difference
			// that somehow still trips this is visible rather than a message
			// that only ever repeats "already enabled".
			return fmt.Errorf("%s is enabled against %s, not %s. disable it first, "+
				"then set the address, then enable again", label, current, endpoint)
		}
		cfg.ApiEndpoint = current
		if err := d.saveOverlayConfig(SettingOverlayZrok, cfg); err != nil {
			return err
		}
		log.Printf("[atrium] zrok: using %s", d.zrokWhere())
		return nil
	}

	// Nothing is enabled here yet, so the address is atrium's to set.
	cfg.ApiEndpoint = endpoint
	if err := d.saveOverlayConfig(SettingOverlayZrok, cfg); err != nil {
		return err
	}
	if endpoint == "" && !own {
		// Back on the machine's environment with nothing to say about where it
		// points. Its config is the machine's business and atrium does not
		// clear it on the way past.
		log.Printf("[atrium] zrok: using %s", d.zrokWhere())
		return nil
	}

	c := root.Config()
	if c == nil {
		c = &env_core.Config{}
	}
	c.ApiEndpoint = endpoint
	if err := root.SetConfig(c); err != nil {
		return fmt.Errorf("could not save that address: %w", err)
	}
	log.Printf("[atrium] zrok: using %s", d.zrokWhere())
	return nil
}

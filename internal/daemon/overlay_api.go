package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"

	"github.com/openziti/zrok/v2/environment"
	"github.com/openziti/zrok/v2/environment/env_core"
)

// What the board asks about overlays, answered.
//
// The commands are resolved against this process's PATH, the same lookup
// launching a runner does, so what the board reports as installed is what
// starting a share will actually find.

// OverlayView is one overlay, its configuration and its state, in the shape
// the board draws.
type OverlayView struct {
	OverlayState
	// Label and Blurb are what the board calls it. Here rather than in the
	// page so a new overlay is one file.
	Label string `json:"label"`
	Blurb string `json:"blurb"`
	// Install is where to get it, shown when it is not installed.
	Install string `json:"install"`
	// Config is the saved settings for this overlay, whatever shape it has.
	Config any `json:"config"`
	// Setup is how far this machine has got towards being able to share at
	// all. Shape differs per overlay: an environment for zrok, an identity
	// for ziti.
	Setup any `json:"setup"`
	// Ready is whether sharing can be attempted. False means the panel shows
	// the setup step instead of the share button, which is the whole point of
	// reporting any of this.
	Ready bool `json:"ready"`
	// SignUp is where somebody with nothing gets what they need.
	SignUp string `json:"signup,omitempty"`
}

// overlayCommand is the CLI each kind still uses for SETUP only.
//
// Sharing is native and needs no binary at all. Enabling a zrok environment
// and enrolling a ziti identity still shell out, because both write files and
// register with a controller in ways their SDKs do not expose as one call, and
// re-implementing that would mean owning credential handling this project has
// said twice it will not own.
func overlayCommand(kind overlayKind) string {
	if kind == OverlayZrok {
		return "zrok"
	}
	return "ziti"
}

// overlayViews is what GET /v1/overlays answers.
func (d *Daemon) overlayViews() any {
	zrokCfg := d.zrokConfig()
	zitiCfg := d.zitiConfig()
	env := zrokEnv()
	// An enabled environment records the instance it was enabled against. A
	// machine that is not enabled yet has none, and that is exactly when
	// somebody needs to see and change where enabling will point, so the
	// configured endpoint is filled in from zrok's own resolution instead.
	if env.ApiEndpoint == "" {
		env.ApiEndpoint, env.ApiEndpointFrom = d.ZrokApiEndpoint()
	}
	id := zitiEnrolment(zitiCfg.Identity)

	return []OverlayView{
		{
			OverlayState: d.nat(OverlayZrok).state(lookPath(overlayCommand(OverlayZrok))),
			Label:        "zrok",
			Blurb: "Publishes the board at a zrok address. A private share needs zrok on the " +
				"other end; a public one is a link anyone can open, and this board has no login.",
			Install: "https://zrok.io",
			SignUp:  "https://api.zrok.io",
			Config:  zrokCfg,
			Setup:   env,
			// An environment is the whole precondition. Without one every
			// share command fails with zrok's own words about a thing that
			// was never set up.
			Ready: env.Enabled,
		},
		{
			OverlayState: d.nat(OverlayZiti).state(lookPath(overlayCommand(OverlayZiti))),
			Label:        "OpenZiti",
			Blurb: "Hosts whatever services this identity is allowed to bind, on your own network. " +
				"Which services, and where each points, are configured there rather than here. " +
				"Who may reach it is a policy on that network, so nothing is exposed to the internet.",
			Install: "https://openziti.io",
			SignUp:  "https://openziti.io/docs/learn/quickstarts/network/",
			Config:  zitiCfg,
			Setup:   id,
			// A path that points at nothing is not readiness. The tunneler
			// would start, fail to load it, and exit.
			Ready: id.Present,
		},
	}
}

// SetupRequest is what the board sends to get this machine ready.
type SetupRequest struct {
	// Token is a zrok account token or a ziti enrollment JWT.
	Token string `json:"token"`
	// Name describes this machine to zrok, or names the identity for ziti.
	Name string `json:"name"`
}

// setupOverlay does the one thing standing between this machine and being able
// to share, and returns whatever the tool said either way.
//
// Output is returned on failure as well as success on purpose. These commands
// fail for reasons only they can explain, and swallowing that leaves somebody
// with a red box and no next step.
func (d *Daemon) setupOverlay(kind string, req SetupRequest) (string, error) {
	switch overlayKind(kind) {
	case OverlayZrok:
		return d.EnableZrok(req.Token, req.Name)
	case OverlayZiti:
		path, out, err := d.EnrollZiti(req.Token, req.Name)
		if err != nil {
			return out, err
		}
		// Enrolling and then making somebody paste the path back in would be
		// most of the work with none of the point.
		cfg := d.zitiConfig()
		cfg.Identity = path
		if err := d.saveOverlayConfig(SettingOverlayZiti, cfg); err != nil {
			return out, err
		}
		return "enrolled, and this identity is now the one atrium will use:\n" + path, nil
	}
	return "", fmt.Errorf("no overlay called %q", kind)
}

// teardownOverlay undoes the setup step.
func (d *Daemon) teardownOverlay(kind string) (string, error) {
	switch overlayKind(kind) {
	case OverlayZrok:
		return d.DisableZrok()
	case OverlayZiti:
		// The identity file is not deleted. It may be the only copy, and
		// re-enrolling needs a token that has already been spent.
		d.nat(OverlayZiti).stop(nil)
		cfg := d.zitiConfig()
		cfg.Identity = ""
		if err := d.saveOverlayConfig(SettingOverlayZiti, cfg); err != nil {
			return "", err
		}
		return "atrium has forgotten that identity. the file is still on disk.", nil
	}
	return "", fmt.Errorf("no overlay called %q", kind)
}

// InspectToken reads an enrollment token so the board can show which network it
// is for, and whether it has already expired, before anything is done with it.
func (d *Daemon) InspectToken(token string) (any, error) {
	return readJWT(token)
}

// lookPath resolves a command the same way starting one does. Empty when it is
// not installed, which is the only thing the board needs to know.
func lookPath(cmd string) string {
	p, err := exec.LookPath(cmd)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(p)
}

// saveOverlay stores one overlay's configuration.
func (d *Daemon) saveOverlay(kind string, body []byte) error {
	switch overlayKind(kind) {
	case OverlayZrok:
		var c ZrokConfig
		if err := json.Unmarshal(body, &c); err != nil {
			return err
		}
		return d.saveOverlayConfig(SettingOverlayZrok, c)
	case OverlayZiti:
		var c ZitiConfig
		if err := json.Unmarshal(body, &c); err != nil {
			return err
		}
		return d.saveOverlayConfig(SettingOverlayZiti, c)
	}
	return fmt.Errorf("no overlay called %q", kind)
}

// startOverlay serves the board on an overlay listener.
//
// Refusals here are the operator's to fix, and each one names the field that
// is missing rather than reporting that something failed somewhere. A share
// that will not start because nothing said what to publish should not read
// like a bug.
func (d *Daemon) startOverlay(kind string) error {
	switch overlayKind(kind) {
	case OverlayZrok:
		if d.nat(OverlayZrok).running() {
			return fmt.Errorf("the board is already on a zrok share")
		}
		return d.startZrokNative(d.zrokConfig())
	case OverlayZiti:
		if d.nat(OverlayZiti).running() {
			return fmt.Errorf("the board is already on a ziti service")
		}
		return d.startZitiNative(d.zitiConfig())
	}
	return fmt.Errorf("no overlay called %q", kind)
}

func (d *Daemon) stopOverlay(kind string) error {
	k := overlayKind(kind)
	if k != OverlayZrok && k != OverlayZiti {
		return fmt.Errorf("no overlay called %q", kind)
	}
	log.Printf("[atrium] closing the %s listener", k)
	d.nat(k).stop(d.zrokRoot(k))
	return nil
}

// zrokRoot is the environment a share has to be released against, and nil for
// anything that is not zrok. A ziti listener has nothing to release.
func (d *Daemon) zrokRoot(k overlayKind) env_core.Root {
	if k != OverlayZrok {
		return nil
	}
	root, err := environment.LoadRoot()
	if err != nil {
		return nil
	}
	return root
}

// sharing reports whether the board is being served on an overlay right now.
//
// Read by the shutdown endpoint. The reason survives going native: a request
// arriving over an overlay is accepted by this process, so its source address
// is whatever the overlay presents rather than whoever sent it.
func (d *Daemon) sharing() bool {
	for _, k := range []overlayKind{OverlayZrok, OverlayZiti} {
		if d.nat(k).running() {
			return true
		}
	}
	return false
}

// closeOverlays stops serving on every overlay on the way out. A listener
// outliving the board it answers for would be an address that hangs.
func (d *Daemon) closeOverlays() {
	for _, k := range []overlayKind{OverlayZrok, OverlayZiti} {
		d.nat(k).stop(d.zrokRoot(k))
	}
}

package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
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
}

// overlayCommand is the binary each kind runs.
func overlayCommand(kind overlayKind) string {
	if kind == OverlayZrok {
		return "zrok"
	}
	return "ziti"
}

// overlayViews is what GET /v1/overlays answers.
func (d *Daemon) overlayViews() any {
	return []OverlayView{
		{
			OverlayState: d.ovl.get(OverlayZrok).state(lookPath(overlayCommand(OverlayZrok))),
			Label:        "zrok",
			Blurb: "Publishes the board at a zrok address. A private share needs zrok on the " +
				"other end; a public one is a link anyone can open, and this board has no login.",
			Install: "https://zrok.io",
			Config:  d.zrokConfig(),
		},
		{
			OverlayState: d.ovl.get(OverlayZiti).state(lookPath(overlayCommand(OverlayZiti))),
			Label:        "OpenZiti",
			Blurb: "Hosts whatever services this identity is allowed to bind, on your own network. " +
				"Which services, and where each points, are configured there rather than here. " +
				"Who may reach it is a policy on that network, so nothing is exposed to the internet.",
			Install: "https://openziti.io",
			Config:  d.zitiConfig(),
		},
	}
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

// startOverlay opens a share.
//
// Refusals here are the operator's to fix, and each one names the field that
// is missing rather than reporting that a command failed. A share that will
// not start because nothing said what to publish should not read like a bug.
func (d *Daemon) startOverlay(kind string) error {
	k := overlayKind(kind)
	name := lookPath(overlayCommand(k))
	if name == "" {
		return fmt.Errorf("%s is not installed, or not on the daemon's PATH", overlayCommand(k))
	}

	var args []string
	var err error
	switch k {
	case OverlayZrok:
		args, err = d.zrokConfig().zrokArgs()
	case OverlayZiti:
		args, err = d.zitiConfig().zitiArgs()
	default:
		return fmt.Errorf("no overlay called %q", kind)
	}
	if err != nil {
		return err
	}

	log.Printf("[atrium] opening a %s share: %s %s", k, name, strings.Join(args, " "))
	return d.ovl.get(k).start(name, args, nil)
}

func (d *Daemon) stopOverlay(kind string) error {
	k := overlayKind(kind)
	if k != OverlayZrok && k != OverlayZiti {
		return fmt.Errorf("no overlay called %q", kind)
	}
	log.Printf("[atrium] closing the %s share", k)
	d.ovl.get(k).stop()
	return nil
}

// sharing reports whether any overlay is publishing the board right now.
//
// Read by the shutdown endpoint, which cannot trust a loopback source address
// while a tunneler on this machine is terminating connections from anywhere.
func (d *Daemon) sharing() bool {
	for _, k := range []overlayKind{OverlayZrok, OverlayZiti} {
		if d.ovl.get(k).state("").Running {
			return true
		}
	}
	return false
}

// closeOverlays ends every share on the way out. A share outliving the board
// it publishes would be an address that answers with nothing.
func (d *Daemon) closeOverlays() {
	for _, k := range []overlayKind{OverlayZrok, OverlayZiti} {
		d.ovl.get(k).stop()
	}
}

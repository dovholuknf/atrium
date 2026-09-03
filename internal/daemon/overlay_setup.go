package daemon

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/openziti/zrok/v2/environment"
	"github.com/openziti/zrok/v2/environment/env_core"
)

// The setup half: getting to the point where a share can start.
//
// Sharing runs forever and is watched. These run once and are waited on, so
// they are a different shape: bounded, and the whole output is the answer.

// setupTimeout bounds a setup command. Enrolling and enabling both talk to a
// controller over the network, so this is generous compared to anything on the
// hot path, and still short enough that a hung command does not hold a browser
// request open indefinitely.
const setupTimeout = 90 * time.Second

// runSetup runs one command to completion and returns what it said.
//
// Both streams are combined, because these tools narrate progress on stderr
// and the useful line is as likely to be there as on stdout, and the caller is
// showing all of it either way.
func runSetup(name string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	out := strings.TrimSpace(buf.String())

	if ctx.Err() != nil {
		return out, fmt.Errorf("%s did not finish in %s", filepath.Base(name), setupTimeout)
	}
	if err != nil {
		// The command's own words, when it had any. Its exit status alone
		// says nothing somebody can act on.
		if out != "" {
			return out, fmt.Errorf("%s", firstLines(out, 4))
		}
		return out, err
	}
	return out, nil
}

// firstLines keeps the top of an error message. A tool that dumps its usage on
// failure would otherwise bury the one line that says what went wrong.
func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// SetZrokApiEndpoint points this machine at a zrok instance.
//
// Written before enabling, not after, because enabling talks to whichever API
// this names: doing it the other way round would enable against the public
// service and then claim to be pointed somewhere else.
//
// Empty clears it, which puts the machine back on zrok's own default. That is
// a real choice rather than an omission, so it is not treated as "no change".
func (d *Daemon) SetZrokApiEndpoint(endpoint string) error {
	endpoint = strings.TrimSpace(endpoint)
	root, err := environment.LoadRoot()
	if err != nil {
		return fmt.Errorf("could not read the zrok environment: %w", err)
	}
	if root.IsEnabled() && endpoint != "" {
		// Changing where an enabled environment points leaves an account token
		// issued by one instance being sent to another, which fails in a way
		// that reads as a broken token rather than a wrong address.
		return fmt.Errorf("this machine is already enabled against a zrok instance. " +
			"disable it first, then set the endpoint, then enable again")
	}
	cfg := root.Config()
	if cfg == nil {
		cfg = &env_core.Config{}
	}
	cfg.ApiEndpoint = endpoint
	if err := root.SetConfig(cfg); err != nil {
		return fmt.Errorf("could not save that endpoint: %w", err)
	}
	if endpoint == "" {
		log.Printf("[atrium] zrok is back on its default api endpoint")
	} else {
		log.Printf("[atrium] zrok will talk to %s", endpoint)
	}
	return nil
}

// ZrokApiEndpoint is where this machine talks to zrok, and where that came
// from. The second value is zrok's own account of it: a config file, an
// environment variable, or the built-in default.
func (d *Daemon) ZrokApiEndpoint() (string, string) {
	root, err := environment.LoadRoot()
	if err != nil {
		return "", ""
	}
	return root.ApiEndpoint()
}

// EnableZrok turns an account token into a zrok environment on this machine.
func (d *Daemon) EnableZrok(token, description string) (string, error) {
	if strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("no token: paste the one from your zrok account")
	}
	name := lookPath(overlayCommand(OverlayZrok))
	if name == "" {
		return "", fmt.Errorf("zrok is not installed, or not on the daemon's PATH")
	}
	if zrokEnv().Enabled {
		return "", fmt.Errorf("this machine already has a zrok environment. disable it first")
	}
	log.Printf("[atrium] enabling a zrok environment")
	return runSetup(name, zrokEnableArgs(token, description))
}

// DisableZrok removes the environment. zrok's own command cleans up the
// account side too, which is why this runs the command rather than deleting
// the directory.
func (d *Daemon) DisableZrok() (string, error) {
	name := lookPath(overlayCommand(OverlayZrok))
	if name == "" {
		return "", fmt.Errorf("zrok is not installed, or not on the daemon's PATH")
	}
	// A listener bound against an environment that is about to be removed
	// would keep answering nothing and report itself as working. Released
	// first, while the environment that owns the share still exists.
	d.nat(OverlayZrok).stop(d.zrokRoot(OverlayZrok))
	log.Printf("[atrium] disabling the zrok environment")
	return runSetup(name, []string{"disable"})
}

// EnrollZiti turns an enrollment token into an identity file.
//
// Returns the path it wrote, so the caller can store it as the configured
// identity without somebody having to find it.
func (d *Daemon) EnrollZiti(token, name string) (string, string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", "", fmt.Errorf("no token: paste the enrollment JWT")
	}
	// The token is checked before the binary. Both can be wrong at once, and
	// "install ziti" is unhelpful advice for a token that is already dead: it
	// sends somebody to do ten minutes of work to earn a second refusal. This
	// one is free to check and is about what they just typed.
	if claims, err := readJWT(token); err == nil && claims.Expired {
		return "", "", fmt.Errorf("that token expired at %s. ask for a new one", claims.Expires)
	}
	exe := lookPath(overlayCommand(OverlayZiti))
	if exe == "" {
		return "", "", fmt.Errorf("ziti is not installed, or not on the daemon's PATH")
	}

	dir := d.zitiIdentityDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	base := safeName(name)
	jwtPath := filepath.Join(dir, base+".jwt")
	outPath := filepath.Join(dir, base+".json")

	// The token goes to a file, not an argument. An argument is visible to
	// anything on this machine that can list processes.
	if err := os.WriteFile(jwtPath, []byte(token), 0o600); err != nil {
		return "", "", err
	}
	// Gone either way. It is one use, and a spent credential lying around is
	// still a credential lying around.
	defer os.Remove(jwtPath)

	log.Printf("[atrium] enrolling a ziti identity as %s", base)
	out, err := runSetup(exe, zitiEnrollArgs(jwtPath, outPath))
	if err != nil {
		return "", out, err
	}
	if _, statErr := os.Stat(outPath); statErr != nil {
		return "", out, fmt.Errorf("enrollment reported success but wrote no identity to %s", outPath)
	}
	return filepath.ToSlash(outPath), out, nil
}

// safeName turns whatever was typed into something usable as a filename.
//
// A name reaches this from a text box and is about to become a path, so
// anything that is not plainly a name is dropped rather than escaped.
func safeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), "-_")
	if out == "" {
		return "atrium"
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}

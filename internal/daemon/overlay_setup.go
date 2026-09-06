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

	httptransport "github.com/go-openapi/runtime/client"
	"github.com/openziti/zrok/v2/environment/env_core"
	restEnvironment "github.com/openziti/zrok/v2/rest_client_zrok/environment"
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
	root, err := d.zrokRoot()
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
	root, err := d.zrokRoot()
	if err != nil {
		return "", ""
	}
	return root.ApiEndpoint()
}

// EnableZrok turns an account token into a zrok environment.
//
// NATIVE, not the `zrok` command, and that is what makes a second environment
// possible at all. The CLI writes to whichever root ITS global says, which is
// always the machine's `~/.zrok2`: driving it could never enable atrium's own.
//
// It is also four calls, which is the whole of `zrok enable` minus its
// terminal drawing. See `cmd/zrok2/enable.go` in the zrok source: ask the
// controller, write the environment, write the identity it issued. Enabling no
// longer needs zrok on the PATH.
func (d *Daemon) EnableZrok(token, description string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", fmt.Errorf("no token: paste the one from your zrok account")
	}
	root, err := d.zrokRoot()
	if err != nil {
		return "", fmt.Errorf("could not read that zrok environment: %w", err)
	}
	if root.IsEnabled() {
		return "", fmt.Errorf("%s is already enabled. disable it first", d.zrokWhere())
	}

	// What the account will call this environment.
	//
	// Built here rather than with `zrok/util.GetHostDetails`, which is two
	// lines of the same thing behind a package that also imports a profanity
	// filter. A dependency for a hostname is a dependency to keep updated
	// forever.
	//
	// The description says ATRIUM. Two environments from one machine appear
	// side by side in `zrok overview`, and telling them apart afterwards is
	// the whole reason somebody made the second one.
	hostDetail, description := d.describeEnvironment(description)

	zrok, err := root.Client()
	if err != nil {
		return "", zrokSays("could not reach the zrok api", err)
	}
	auth := httptransport.APIKeyAuth("X-TOKEN", "header", token)
	req := restEnvironment.NewEnableParams()
	req.Body.Description = description
	req.Body.Host = hostDetail

	api, _ := root.ApiEndpoint()
	log.Printf("[atrium] enabling %s", d.zrokWhere())
	resp, err := zrok.Environment.Enable(req, auth)
	if err != nil {
		return "", zrokSays("could not enable an environment on "+api, err)
	}

	// The token is written down as well as the identity. It is what every
	// later call authenticates with, and losing it means the environment
	// exists on the account and cannot be used from here.
	if err := root.SetEnvironment(&env_core.Environment{
		AccountToken: token, ZitiIdentity: resp.Payload.Identity, ApiEndpoint: api,
	}); err != nil {
		return "", fmt.Errorf("the environment was created on %s but could not be "+
			"saved here: %w", api, err)
	}
	if err := root.SaveZitiIdentityNamed(root.EnvironmentIdentityName(), resp.Payload.Cfg); err != nil {
		return "", fmt.Errorf("the environment was created on %s but its identity "+
			"could not be written: %w", api, err)
	}
	return "enabled against " + api + ".\nthis is " + d.zrokWhere() + ".", nil
}

// DisableZrok removes the environment, from the account as well as from disk.
//
// Native for the same reason as enabling: the CLI only ever reaches the
// machine's environment. The two steps are in this order deliberately. The
// account is told first, because that is the call that needs the token, and
// deleting the local copy first would leave an environment on the account with
// nothing here able to authenticate against it.
func (d *Daemon) DisableZrok() (string, error) {
	root, err := d.zrokRoot()
	if err != nil {
		return "", fmt.Errorf("could not read that zrok environment: %w", err)
	}
	if !root.IsEnabled() {
		return "", fmt.Errorf("%s is not enabled", d.zrokWhere())
	}
	// A listener bound against an environment that is about to be removed
	// would keep answering nothing and report itself as working. Released
	// first, while the environment that owns the share still exists.
	d.nat(OverlayZrok).stop(d.rootToReleaseAgainst(OverlayZrok))

	api, _ := root.ApiEndpoint()
	log.Printf("[atrium] disabling %s", d.zrokWhere())
	zrok, err := root.Client()
	if err != nil {
		return "", zrokSays("could not reach the zrok api", err)
	}
	auth := httptransport.APIKeyAuth("X-TOKEN", "header", root.Environment().AccountToken)
	req := restEnvironment.NewDisableParams()
	// WHICH ENVIRONMENT, and it is not optional.
	//
	// The account token says who you are and this says which of that account's
	// environments to remove. Sending the request without it answers `401
	// disableUnauthorized`, which reads as a revoked token and is not: the
	// controller could not find an environment to check the token against.
	req.Body.Identity = root.Environment().ZitiIdentity

	// A REFUSAL FROM THE ACCOUNT DOES NOT STOP THE LOCAL REMOVAL, which is what
	// the zrok command does and it is right. An environment the account will
	// not take off is one that cannot be used from here either, and stopping
	// would mean it can never be got rid of: every retry sends the same
	// credential to the same refusal.
	//
	// So it is reported and stepped over. What is left on the account is
	// visible in `zrok overview` and removable from the console.
	note := ""
	if _, err := zrok.Environment.Disable(req, auth); err != nil {
		log.Printf("[atrium] %s would not remove this environment: %v", api, err)
		note = "\n\n" + api + " would not remove it: " + err.Error() +
			"\nit is gone from this machine either way. if it is still listed on the " +
			"account, remove it from the zrok console."
	}
	if err := root.DeleteEnvironment(); err != nil {
		return "", fmt.Errorf("the environment could not be removed from disk: %w", err)
	}
	// The identity goes too. Leaving it behind means the next enable writes a
	// second one beside a key that authenticates as an environment that no
	// longer exists.
	if err := root.DeleteZitiIdentityNamed(root.EnvironmentIdentityName()); err != nil {
		log.Printf("[atrium] could not remove the zrok backend identity: %v", err)
	}
	return "disabled, and removed from this machine." + note, nil
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

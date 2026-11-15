package transport

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"

	httptransport "github.com/go-openapi/runtime/client"
	"github.com/openziti/zrok/v2/environment"
	"github.com/openziti/zrok/v2/environment/env_core"
	restEnvironment "github.com/openziti/zrok/v2/rest_client_zrok/environment"
)

// ZrokDefaultRootDir is the SDK's own default, the machine's `~/.zrok2`.
const ZrokDefaultRootDir = ".zrok2"

// ZrokRootMu guards the SDK's package-level root directory while a root loads, since two loads of different roots
// at once could each read the other's.
var ZrokRootMu sync.Mutex

// LoadZrokRoot loads the environment in dir, or the machine's when dir is empty. Every zrok root atrium reads comes
// through here, so the SDK's global is always put back.
func LoadZrokRoot(dir string) (env_core.Root, error) {
	ZrokRootMu.Lock()
	defer ZrokRootMu.Unlock()
	if dir == "" {
		environment.SetRootDirName(ZrokDefaultRootDir)
	} else {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("could not make the zrok directory at %s: %w", dir, err)
		}
		environment.SetRootDirName(dir)
	}
	root, err := environment.LoadRoot()
	environment.SetRootDirName(ZrokDefaultRootDir)
	return root, err
}

// EnableZrokRoot is `zrok enable` in the binary: ask the controller for an environment, then write it and the
// identity it issued into root. It answers the api endpoint it enabled against.
func EnableZrokRoot(root env_core.Root, token, host, description string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("no token: paste the one from your zrok account")
	}
	if root.IsEnabled() {
		return "", fmt.Errorf("that zrok environment is already enabled. disable it first")
	}
	zrok, err := root.Client()
	if err != nil {
		return "", fmt.Errorf("could not reach the zrok api: %w", err)
	}
	req := restEnvironment.NewEnableParams()
	req.Body.Description = description
	req.Body.Host = host
	api, _ := root.ApiEndpoint()
	resp, err := zrok.Environment.Enable(req, httptransport.APIKeyAuth("X-TOKEN", "header", token))
	if err != nil {
		return api, fmt.Errorf("could not enable an environment on %s: %w", api, err)
	}
	if err := root.SetEnvironment(&env_core.Environment{
		AccountToken: token, ZitiIdentity: resp.Payload.Identity, ApiEndpoint: api,
	}); err != nil {
		return api, fmt.Errorf("the environment was created on %s but could not be saved here: %w", api, err)
	}
	if err := root.SaveZitiIdentityNamed(root.EnvironmentIdentityName(), resp.Payload.Cfg); err != nil {
		return api, fmt.Errorf("the environment was created on %s but its identity could not be written: %w", api, err)
	}
	return api, nil
}

// EnableZrokFromFile enables root with the account token held in a file, so the token is never an argument or a
// setting. The environment then holds it, as `zrok enable` leaves it.
func EnableZrokFromFile(root env_core.Root, tokenFile string) error {
	raw, err := os.ReadFile(tokenFile)
	if err != nil {
		return fmt.Errorf("could not read the zrok account token file: %w", err)
	}
	host, _ := os.Hostname()
	_, err = EnableZrokRoot(root, strings.TrimSpace(string(raw)), "atrium; "+host+"; "+runtime.GOOS, "atrium@"+host)
	return err
}

// DisableZrokRoot is `zrok disable` in the binary. The account is told first, since that call needs the token on
// disk. A refusal from the account is returned as refused and the local copy goes anyway, or it could never go.
func DisableZrokRoot(root env_core.Root) (refused error, err error) {
	if !root.IsEnabled() {
		return nil, fmt.Errorf("that zrok environment is not enabled")
	}
	zrok, err := root.Client()
	if err != nil {
		return nil, fmt.Errorf("could not reach the zrok api: %w", err)
	}
	req := restEnvironment.NewDisableParams()
	// Which environment: without it the controller answers 401, which reads as a revoked token.
	req.Body.Identity = root.Environment().ZitiIdentity
	_, refused = zrok.Environment.Disable(req, httptransport.APIKeyAuth("X-TOKEN", "header", root.Environment().AccountToken))
	if err := root.DeleteEnvironment(); err != nil {
		return refused, fmt.Errorf("the environment could not be removed from disk: %w", err)
	}
	_ = root.DeleteZitiIdentityNamed(root.EnvironmentIdentityName())
	return refused, nil
}

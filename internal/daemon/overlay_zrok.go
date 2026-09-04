package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Whether this machine has a zrok environment, and how to get one.
//
// Read off disk rather than out of `zrok status`, which prints boxed tables to
// stdout for a person to look at. Parsing that would tie atrium to somebody
// else's table layout, and it would break on a release that widened a column.
//
// The file is the same thing the CLI itself checks. `IsEnabled` in
// `environment/env_v0_4/api.go` is "did environment.json load", so its presence
// is the whole answer.

// zrokRootNames are the directories a zrok environment lives in, newest first.
//
// v2 moved from `.zrok` to `.zrok2` and both can be on one machine, so the
// newer wins and the older is still recognized rather than reported as
// nothing being set up.
var zrokRootNames = []string{".zrok2", ".zrok"}

// ZrokEnv is what atrium can tell about the local zrok environment.
type ZrokEnv struct {
	// Enabled is whether this machine has an environment at all. Everything
	// else is only meaningful when it is true.
	Enabled bool `json:"enabled"`
	// Root is the directory that answered, for saying which one when there
	// are two.
	Root string `json:"root,omitempty"`
	// ApiEndpoint is the zrok instance this environment belongs to. Shown
	// because a token from one instance does not work on another, and that is
	// otherwise an opaque failure.
	ApiEndpoint string `json:"api_endpoint,omitempty"`
	// ApiEndpointFrom is where that value came from when this machine is not
	// enabled: a config file, an environment variable, or zrok's built-in
	// default. Worth showing, because "the public service, because nobody said
	// otherwise" and "the instance you configured" look identical otherwise.
	ApiEndpointFrom string `json:"api_endpoint_from,omitempty"`
	// HasAccountToken reports that a token is present WITHOUT reading it out.
	// The value is a credential and the board has no reason to hold one.
	HasAccountToken bool `json:"has_account_token"`
}

// zrokEnvFile is the shape on disk. Only the fields atrium reports are named:
// the rest of the file is zrok's business.
//
// The token's key is `zrok_token`, which is not what the Go field beside it is
// called. See `environment/env_v0_4/root.go:297`, where `AccountToken` carries
// that tag. Guessing from the field name reports every enabled environment as
// having no token.
type zrokEnvFile struct {
	AccountToken string `json:"zrok_token"`
	ApiEndpoint  string `json:"api_endpoint"`
	ZitiIdentity string `json:"ziti_identity"`
}

// zrokEnv reports the local environment.
//
// Every failure reads as "not enabled". A file atrium cannot read is one it
// cannot vouch for, and claiming an environment that does not work would be
// worse than reporting none.
func zrokEnv() ZrokEnv {
	home, err := os.UserHomeDir()
	if err != nil {
		return ZrokEnv{}
	}
	for _, name := range zrokRootNames {
		root := filepath.Join(home, name)
		raw, err := os.ReadFile(filepath.Join(root, "environment.json"))
		if err != nil {
			continue
		}
		out := ZrokEnv{Enabled: true, Root: filepath.ToSlash(root)}
		var f zrokEnvFile
		if err := json.Unmarshal(raw, &f); err == nil {
			out.ApiEndpoint = f.ApiEndpoint
			out.HasAccountToken = strings.TrimSpace(f.AccountToken) != ""
		}
		return out
	}
	return ZrokEnv{}
}

// zrokEnableArgs builds the command that turns a token into an environment.
//
// `--headless` for the same reason sharing needs it: atrium runs this as a
// child and reads its output, and the interactive form paints a spinner into
// a pipe.
//
// The description is what the zrok console lists this machine as. Left to
// zrok's own default when empty, which is user@hostname.
func zrokEnableArgs(token, description string) []string {
	args := []string{"enable", strings.TrimSpace(token), "--headless"}
	if d := strings.TrimSpace(description); d != "" {
		args = append(args, "--description", d)
	}
	return args
}

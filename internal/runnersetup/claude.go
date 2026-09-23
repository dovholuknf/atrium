package runnersetup

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/claudeconf"
)

// Claude Code. Two checks, both over things atrium already reads, so the
// framework runs on two runners rather than one.
//
// Claude's folder trust (`hasTrustDialogAccepted` in `~/.claude.json`) is not
// checked. Claude walks up from the folder with a bound atrium could not confirm
// from the shipped binary, and every running session rewrites that file, so a
// check could be wrong and a fix would race. See the design's open questions.
var Claude = &Adapter{
	ID: "claude", Label: "claude code", Cmds: []string{"claude"}, Package: "@anthropic-ai/claude-code",
	Checks: []Check{
		{ID: "auth", Label: "signed in", Run: claudeAuthCheck},
		{ID: "hooks", Label: "atrium hooks", Run: claudeHooksCheck, Apply: claudeHooksApply},
	},
}

func claudeAuthCheck(env Env) Result {
	creds := filepath.Join(env.Home, ".claude", ".credentials.json")
	r := Result{Path: filepath.ToSlash(creds)}
	for _, k := range []string{"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN"} {
		if strings.TrimSpace(env.RowEnv[k]) != "" {
			r.State, r.Fix = Warn, FixExplain
			r.Detail = k + " is in this runner's env in atrium, and atrium does not hold credentials. " +
				"put it in your own environment, or sign in with claude and /login, and remove it from " +
				"the runner."
			r.Command = "claude"
			return r
		}
	}
	if env.inherited("ANTHROPIC_API_KEY") != "" || env.inherited("CLAUDE_CODE_OAUTH_TOKEN") != "" {
		r.State, r.Detail = OK, "a key is in the environment the runner starts with."
		return r
	}
	if exists(creds) {
		r.State, r.Detail = OK, "signed in."
		return r
	}
	if env.goos() == "darwin" {
		r.State, r.Detail = NA, "claude keeps its sign-in in the macOS keychain, which atrium does not read."
		return r
	}
	r.State, r.Fix, r.Command = Fail, FixExplain, "claude"
	r.Detail = "claude has no saved sign-in, so its first launch stops to ask. run claude in a terminal " +
		"and type /login."
	return r
}

func claudeHooksCheck(env Env) Result {
	rep, err := claudeconf.InspectTarget(claudeconf.Claude, env.AtriumExe)
	if err != nil {
		return Result{State: Warn, Detail: "atrium could not read claude's settings: " + err.Error()}
	}
	r := Result{Path: rep.Path}
	switch {
	case rep.Unreadable != "":
		r.State, r.Fix = Fail, FixExplain
		r.Detail = "claude's settings.json is not valid json, so atrium will not write to it. fix it by hand."
	case rep.Refused != "":
		r.State, r.Fix, r.Detail = Warn, FixExplain, rep.Refused
	case rep.Missing == 0:
		r.State, r.Detail = OK, "every hook atrium uses is wired."
	default:
		r.State, r.Fix, r.FixLabel = Fail, FixApply, "wire them"
		r.Detail = fmt.Sprintf("%d hook(s) are missing or point at another binary, so atrium cannot see "+
			"what these sessions are doing.", rep.Missing)
	}
	return r
}

func claudeHooksApply(env Env, _ string) (Applied, error) {
	rep, res, err := claudeconf.InstallOnlyTarget(claudeconf.Claude, env.AtriumExe, nil)
	if err != nil {
		return Applied{}, err
	}
	return Applied{Changed: res.Changed, Path: rep.Path, Backup: res.Backup}, nil
}

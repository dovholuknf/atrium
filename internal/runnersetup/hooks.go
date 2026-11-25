package runnersetup

import (
	"fmt"
	"path/filepath"

	"github.com/dovholuknf/atrium/internal/base/claudeconf"
)

// hooksTarget is a hooks target aimed at the settings file the runner will read, which GEMINI_CLI_HOME or QWEN_HOME in
// the row's env moves.
func hooksTarget(t claudeconf.Target, env Env) claudeconf.Target {
	switch t.ID {
	case claudeconf.Gemini.ID:
		t.Path = func() (string, error) { return filepath.Join(geminiDir(env), "settings.json"), nil }
	case claudeconf.Qwen.ID:
		dir := env.lookup("QWEN_HOME")
		if dir == "" {
			dir = filepath.Join(env.Home, ".qwen")
		}
		t.Path = func() (string, error) { return filepath.Join(dir, "settings.json"), nil }
	}
	return t
}

// allEvents names every hook of the target, the optional turn end included: it is what moves a card to needs-input.
func allEvents(t claudeconf.Target) []string {
	var out []string
	for _, w := range t.Wanted {
		out = append(out, w.Event)
	}
	return out
}

func hooksCheck(t claudeconf.Target) func(Env) Result {
	return func(env Env) Result {
		rep, err := claudeconf.InspectTarget(hooksTarget(t, env), env.AtriumExe)
		if err != nil {
			return Result{State: Warn, Detail: "atrium could not read " + t.Label + "'s settings: " + err.Error()}
		}
		r := Result{Path: rep.Path}
		switch {
		case rep.Unreadable != "":
			r.State, r.Fix = Fail, FixExplain
			r.Detail = t.Label + "'s settings.json is not valid json, so atrium will not write to it. fix it by hand."
		case rep.Refused != "":
			r.State, r.Fix, r.Detail = Warn, FixExplain, rep.Refused
		default:
			missing := 0
			for _, h := range rep.Hooks {
				if !h.Installed || h.Stale || h.TimeoutShort {
					missing++
				}
			}
			if missing == 0 {
				r.State, r.Detail = OK, "every hook atrium uses is wired."
				return r
			}
			r.State, r.Fix, r.FixLabel = Fail, FixApply, "wire them"
			r.Detail = fmt.Sprintf("%d hook(s) are missing or point at another binary, so atrium cannot see "+
				"what these sessions are doing.", missing)
		}
		return r
	}
}

func hooksApply(t claudeconf.Target) func(Env, string) (Applied, error) {
	return func(env Env, _ string) (Applied, error) {
		rep, res, err := claudeconf.InstallOnlyTarget(hooksTarget(t, env), env.AtriumExe, allEvents(t))
		if err != nil {
			return Applied{}, err
		}
		return Applied{Changed: res.Changed, Path: rep.Path, Backup: res.Backup}, nil
	}
}

// hooksLaunch wires the hooks that are missing before the runner starts, so a card launched here reports itself with no
// hand setup. A hook naming another room's atrium binary counts as there: two rooms on one machine share the settings
// file, and each binary reports to the room its card's ATRIUM_LOCATION names.
func hooksLaunch(t claudeconf.Target) func(Env, string) (string, error) {
	return func(env Env, _ string) (string, error) {
		if env.AtriumExe == "" {
			return "", nil
		}
		t := hooksTarget(t, env)
		rep, err := claudeconf.InspectTarget(t, env.AtriumExe)
		if err != nil || rep.Unreadable != "" || rep.Refused != "" {
			if err == nil {
				err = fmt.Errorf("%s hooks at %s left alone: %s%s", t.Label, rep.Path, rep.Unreadable, rep.Refused)
			}
			return "", err
		}
		var missing []string
		for _, h := range rep.Hooks {
			if !h.Installed {
				missing = append(missing, h.Event)
			}
		}
		if len(missing) == 0 {
			return "", nil
		}
		if _, _, err := claudeconf.InstallOnlyTarget(t, env.AtriumExe, missing); err != nil {
			return "", err
		}
		return "wired atrium's hooks for " + t.Label + " in " + rep.Path, nil
	}
}

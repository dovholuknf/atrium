package runnersetup

import (
	"path/filepath"
	"testing"
)

func TestClaudeAuthStates(t *testing.T) {
	env := Env{Home: t.TempDir(), Getenv: func(string) string { return "" }, GOOS: "windows"}
	if r := claudeAuthCheck(env); r.State != Fail || r.Fix != FixExplain {
		t.Fatalf("no sign-in should fail with an explained fix, got %+v", r)
	}
	writeFile(t, filepath.Join(env.Home, ".claude", ".credentials.json"), `{}`)
	if r := claudeAuthCheck(env); r.State != OK {
		t.Fatalf("a saved sign-in should be ok, got %+v", r)
	}
	env.RowEnv = map[string]string{"ANTHROPIC_API_KEY": "sk"}
	if r := claudeAuthCheck(env); r.State != Warn {
		t.Fatalf("a key in the harness env should warn, got %+v", r)
	}

	mac := Env{Home: t.TempDir(), Getenv: func(string) string { return "" }, GOOS: "darwin"}
	if r := claudeAuthCheck(mac); r.State != NA {
		t.Fatalf("macOS keeps the sign-in in the keychain, so n/a, got %+v", r)
	}
}

// The hooks check reads the home directory through claudeconf, which uses
// os.UserHomeDir, so HOME and USERPROFILE both point at the temp directory.
func TestClaudeHooksCheckAndApply(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	exe := filepath.ToSlash(filepath.Join(home, "bin", "atrium.exe"))
	env := Env{Home: home, AtriumExe: exe}

	r := claudeHooksCheck(env)
	if r.State != Fail || r.Fix != FixApply {
		t.Fatalf("no settings file should be a fail with an apply fix, got %+v", r)
	}
	res, err := Fix(Claude, env, "hooks", "")
	if err != nil || !res.Changed {
		t.Fatalf("wiring should change the file, got %+v %v", res, err)
	}
	if r := claudeHooksCheck(env); r.State != OK {
		t.Fatalf("after wiring the check should be ok, got %+v", r)
	}
	writeFile(t, filepath.Join(home, ".claude", "settings.json"), `{not json`)
	if r := claudeHooksCheck(env); r.State != Fail || r.Fix != FixExplain {
		t.Fatalf("an unreadable file should be explained, got %+v", r)
	}
}

func TestInspectCountsOnlyFailures(t *testing.T) {
	env, _ := geminiHome(t)
	rep := Inspect(Gemini, env)
	// trust fails (untrusted root), auth fails (nothing chosen), hooks is n/a.
	if rep.Failing != 2 || len(rep.Checks) != 3 || rep.Installed {
		t.Fatalf("got %+v", rep)
	}
	for _, c := range rep.Checks {
		if c.State == NA && c.Fix != "" {
			t.Fatalf("an n/a check carries a fix: %+v", c)
		}
	}
}

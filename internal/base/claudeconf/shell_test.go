package claudeconf

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A target writing into a temp file, with the Git Bash answer faked.
func shellTarget(t *testing.T, needs bool) (Target, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	tg := Claude
	tg.Path = func() (string, error) { return path, nil }
	tg.NeedsPowerShell = func() bool { return needs }
	return tg, path
}

func firstCommand(t *testing.T, path, hook string) map[string]any {
	t.Helper()
	doc := readSettingsFile(t, path)
	hooks := doc["hooks"].(map[string]any)[hook].([]any)
	return hooks[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
}

func readSettingsFile(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestBashFoundWritesNoShellKeyAndTheOldCommand(t *testing.T) {
	tg, path := shellTarget(t, false)
	exe := "C:/Program Files/atrium/atrium.exe"
	if _, _, err := InstallOnlyTarget(tg, exe, []string{"tool-end"}); err != nil {
		t.Fatal(err)
	}
	h := firstCommand(t, path, "PostToolUse")
	if _, has := h["shell"]; has {
		t.Fatalf("a shell key was written where bash is found: %v", h)
	}
	if want := `"C:/Program Files/atrium/atrium.exe" hook --event tool-end`; h["command"] != want {
		t.Fatalf("command %q, want %q", h["command"], want)
	}
}

func TestNoBashWritesPowerShellShapeAndCallOperatorForAQuotedPath(t *testing.T) {
	tg, path := shellTarget(t, true)
	if _, _, err := InstallOnlyTarget(tg, "C:/Program Files/atrium/atrium.exe", []string{"tool-end"}); err != nil {
		t.Fatal(err)
	}
	h := firstCommand(t, path, "PostToolUse")
	if h["shell"] != "powershell" {
		t.Fatalf("shell key is %v, want powershell", h["shell"])
	}
	if want := `& "C:/Program Files/atrium/atrium.exe" hook --event tool-end`; h["command"] != want {
		t.Fatalf("command %q, want %q", h["command"], want)
	}

	// No space in the path, so no quotes and no call operator.
	if got := HookCommandForTarget(tg, "C:/x/atrium.exe", "tool-end"); got != "C:/x/atrium.exe hook --event tool-end" {
		t.Fatalf("unquoted path got %q", got)
	}
	// Codex is never given the shell key or the operator.
	cx := Codex
	cx.NeedsPowerShell = func() bool { return true }
	if got := HookCommandForTarget(cx, "C:/x/atrium.exe", "tool-end"); strings.HasPrefix(got, "& ") {
		t.Fatalf("codex got a call operator: %q", got)
	}
}

func TestStatusRecognisesThePowerShellFormAndFlagsAMissingShell(t *testing.T) {
	tg, path := shellTarget(t, true)
	exe := "C:/Program Files/atrium/atrium.exe"
	if _, _, err := InstallOnlyTarget(tg, exe, nil); err != nil {
		t.Fatal(err)
	}
	rep, err := InspectTarget(tg, exe)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Missing != 0 {
		t.Fatalf("a fresh powershell install reports %d missing", rep.Missing)
	}
	for _, h := range rep.Hooks {
		if !h.Optional && (!h.Installed || h.Stale) {
			t.Fatalf("%s not recognised: installed=%v stale=%v found=%q", h.Event, h.Installed, h.Stale, h.Found)
		}
	}
	// Installing again changes nothing.
	if _, res, err := InstallOnlyTarget(tg, exe, nil); err != nil || res.Changed {
		t.Fatalf("second install changed=%v err=%v", res.Changed, err)
	}
	// The same file read where bash is found: still recognised as installed.
	bash := tg
	bash.NeedsPowerShell = func() bool { return false }
	rep, _ = InspectTarget(bash, exe)
	for _, h := range rep.Hooks {
		if !h.Optional && !h.Installed {
			t.Fatalf("%s lost when read as bash", h.Event)
		}
	}
	// An old entry with no shell key is stale on a machine without bash, and
	// the install repairs it in place.
	old := `{"hooks":{"PostToolUse":[{"matcher":"","hooks":[{"type":"command","command":"` +
		`\"C:/Program Files/atrium/atrium.exe\" hook --event tool-end"}]}]}}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, _ = InspectTarget(tg, exe)
	for _, h := range rep.Hooks {
		if h.Event == "tool-end" && !(h.Installed && h.Stale) {
			t.Fatalf("shell-less entry: installed=%v stale=%v", h.Installed, h.Stale)
		}
	}
	if _, _, err := InstallOnlyTarget(tg, exe, []string{"tool-end"}); err != nil {
		t.Fatal(err)
	}
	if h := firstCommand(t, path, "PostToolUse"); h["shell"] != "powershell" ||
		!strings.HasPrefix(h["command"].(string), "& ") {
		t.Fatalf("entry not repaired: %v", h)
	}
}

func TestFindGitBashLooksWhereClaudeCodeLooks(t *testing.T) {
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	have := map[string]bool{}
	exists := func(p string) bool { return have[filepath.ToSlash(p)] }
	git := ""
	look := func(string) (string, error) {
		if git == "" {
			return "", errors.New("not found")
		}
		return git, nil
	}

	if got := findGitBash(getenv, look, exists); got != "" {
		t.Fatalf("nothing there, found %q", got)
	}
	have[`C:/Program Files/Git/bin/bash.exe`] = true
	env["ProgramFiles"] = "C:/Program Files"
	if got := findGitBash(getenv, look, exists); got == "" {
		t.Fatal("Program Files bash missed")
	}
	delete(have, `C:/Program Files/Git/bin/bash.exe`)
	git = "D:/tools/git/cmd/git.exe"
	have["D:/tools/git/bin/bash.exe"] = true
	if got := findGitBash(getenv, look, exists); filepath.ToSlash(got) != "D:/tools/git/bin/bash.exe" {
		t.Fatalf("git.exe sibling missed, got %q", got)
	}
	delete(have, "D:/tools/git/bin/bash.exe")
	env[gitBashEnv] = "E:/bash.exe"
	have["E:/bash.exe"] = true
	if got := findGitBash(getenv, look, exists); got != "E:/bash.exe" {
		t.Fatalf("env override missed, got %q", got)
	}
}

package claudeconf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Both Inspect and Install read the home directory, so a test has to move it
// or it would report on whoever is running the suite.
func withHome(t *testing.T, contents string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	path := filepath.Join(home, ".claude", "settings.json")
	if contents != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func readSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("what was written is not valid json: %v", err)
	}
	return doc
}

// The whole point of the feature: say which hooks are missing rather than
// leaving the operator to compare a documentation page against their own file.
func TestInspectReportsEveryHookMissingOnAFreshMachine(t *testing.T) {
	withHome(t, "")

	rep, err := Inspect("C:/tools/atrium.exe")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Exists {
		t.Fatal("a settings file that is not there was reported as present")
	}
	if rep.Missing != wantedCount() {
		t.Fatalf("missing is %d, wanted %d", rep.Missing, wantedCount())
	}
	for _, h := range rep.Hooks {
		if h.Installed {
			t.Fatalf("%s reported installed with no settings file", h.Hook)
		}
		// The command has to be one this binary would recognise as reporting
		// that event, which is not the same as containing its name: the
		// session subcommand spells its own argument differently.
		if !reportsEvent(h.Want, h.Event) {
			t.Fatalf("%s would write %q, which does not report its event", h.Hook, h.Want)
		}
	}
}

// Installing has to be safe to run against a file full of settings atrium
// knows nothing about. Losing one is worse than never installing the hooks.
func TestInstallKeepsEverythingElseInTheFile(t *testing.T) {
	path := withHome(t, `{
  "permissions": { "allow": ["Bash(git status)"], "deny": [] },
  "model": "opus",
  "somethingAtriumHasNeverHeardOf": { "deep": [1, 2, 3] }
}`)

	if _, _, err := Install("C:/tools/atrium.exe"); err != nil {
		t.Fatal(err)
	}

	doc := readSettings(t, path)
	if doc["model"] != "opus" {
		t.Fatalf("model became %v", doc["model"])
	}
	if _, ok := doc["somethingAtriumHasNeverHeardOf"]; !ok {
		t.Fatal("a key atrium does not understand was dropped")
	}
	perms, _ := doc["permissions"].(map[string]any)
	allow, _ := perms["allow"].([]any)
	if len(allow) != 1 || allow[0] != "Bash(git status)" {
		t.Fatalf("the permission list came back as %v", perms)
	}
}

// A second run must not add a second copy. Two commands reporting the same
// event would double every subagent count.
func TestInstallIsSafeToRunTwice(t *testing.T) {
	path := withHome(t, "")

	if _, _, err := Install("C:/tools/atrium.exe"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Install("C:/tools/atrium.exe"); err != nil {
		t.Fatal(err)
	}

	doc := readSettings(t, path)
	hooks, _ := doc["hooks"].(map[string]any)
	for name, v := range hooks {
		entries, _ := v.([]any)
		seen := 0
		for _, e := range entries {
			for _, c := range commandsIn(e) {
				if strings.Contains(c, "atrium") {
					seen++
				}
			}
		}
		if seen != 1 {
			t.Fatalf("%s has %d atrium commands after two installs, wanted 1", name, seen)
		}
	}

	rep, err := Inspect("C:/tools/atrium.exe")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Missing != 0 {
		t.Fatalf("%d hooks still reported missing after installing", rep.Missing)
	}
}

// Wiring one at a time is the point of the per-row buttons and of
// `hook install --event`. Naming one must not quietly write the rest.
func TestInstallOnlyWritesWhatWasNamed(t *testing.T) {
	withHome(t, "")

	rep, _, err := InstallOnly("C:/tools/atrium.exe", []string{"tool-start"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Missing != wantedCount()-1 {
		t.Fatalf("missing is %d after wiring one, wanted %d", rep.Missing, wantedCount()-1)
	}
	for _, h := range rep.Hooks {
		want := h.Event == "tool-start"
		if h.Installed != want {
			t.Fatalf("%s installed=%v, wanted %v", h.Hook, h.Installed, want)
		}
	}

	// The next one joins it rather than replacing it, which is what working
	// through the list a row at a time does.
	rep, _, err = InstallOnly("C:/tools/atrium.exe", []string{"prompt"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Missing != wantedCount()-2 {
		t.Fatalf("missing is %d after wiring two, wanted %d", rep.Missing, wantedCount()-2)
	}
}

// Working through the list a few at a time means running install repeatedly,
// and a backup per run buries the one worth having. Nothing to change means
// nothing written.
func TestInstallWritesNothingWhenAlreadyCorrect(t *testing.T) {
	path := withHome(t, "")

	if _, _, err := Install("C:/tools/atrium.exe"); err != nil {
		t.Fatal(err)
	}
	first, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	rep, res, err := Install("C:/tools/atrium.exe")
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("a second install reported that it changed something")
	}
	if res.Backup != "" {
		t.Fatalf("a second install kept a backup of an unchanged file: %s", res.Backup)
	}
	if rep.Missing != 0 {
		t.Fatalf("the report from a no-op install says %d missing", rep.Missing)
	}

	second, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !second.ModTime().Equal(first.ModTime()) {
		t.Fatal("the file was rewritten even though nothing changed")
	}

	// And no stray backups anywhere in the directory.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".bak") {
			t.Fatalf("a backup was left behind: %s", e.Name())
		}
	}
}

// Session events ride a different subcommand with a different argument, and
// the round trip has to survive that: what Install writes is what Inspect
// recognises.
func TestSessionHooksRoundTrip(t *testing.T) {
	withHome(t, "")

	rep, _, err := InstallOnly("C:/tools/atrium.exe", []string{"session-start"})
	if err != nil {
		t.Fatal(err)
	}
	var got HookStatus
	for _, h := range rep.Hooks {
		if h.Hook == "SessionStart" {
			got = h
		}
	}
	if !got.Installed {
		t.Fatalf("SessionStart did not read back as installed: %+v", got)
	}
	if !strings.Contains(got.Want, " session --event start") {
		t.Fatalf("SessionStart would run %q, not the session subcommand", got.Want)
	}
	// And the tool hooks are untouched by it.
	for _, h := range rep.Hooks {
		if h.Hook != "SessionStart" && h.Installed {
			t.Fatalf("%s was installed when only session-start was asked for", h.Hook)
		}
	}
}

// The script the session subcommand replaces reports the same events, so it
// has to be recognised and replaced rather than left alongside.
func TestSessionHookReplacesTheOldScript(t *testing.T) {
	path := withHome(t, `{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "",
        "hooks": [
          { "type": "command", "command": "pwsh -NoProfile -File D:/dotfiles/atrium-session-hook.ps1 -Event start" },
          { "type": "command", "command": "pwsh -NoProfile -File D:/dotfiles/session-bootstrap.ps1 -Phase start" }
        ]
      }
    ]
  }
}`)

	if _, _, err := InstallOnly("C:/tools/atrium.exe", []string{"session-start"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "atrium-session-hook.ps1") {
		t.Fatal("the old session script is still registered alongside the subcommand")
	}
	// Somebody else's SessionStart hook is not atrium's to remove.
	if !strings.Contains(string(raw), "session-bootstrap.ps1") {
		t.Fatal("an unrelated SessionStart hook was removed")
	}
}

// An event nobody knows is a typo, and writing nothing while reporting success
// would look like it worked.
func TestInstallOnlyRefusesAnUnknownEvent(t *testing.T) {
	withHome(t, "")

	if _, _, err := InstallOnly("C:/tools/atrium.exe", []string{"not-an-event"}); err == nil {
		t.Fatal("an event that does not exist was reported as installed")
	}
}

// The old dotfiles script reports the same events. It has to be replaced
// rather than joined, or every event is posted twice.
func TestInstallReplacesTheOldScript(t *testing.T) {
	path := withHome(t, `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "",
        "hooks": [
          { "type": "command", "command": "pwsh -NoProfile -File D:/dotfiles/atrium-perm-hook.ps1" },
          { "type": "command", "command": "pwsh -NoProfile -File D:/dotfiles/atrium-activity-hook.ps1 -Event tool-start", "timeout": 5 }
        ]
      }
    ]
  }
}`)

	before, err := Inspect("C:/tools/atrium.exe")
	if err != nil {
		t.Fatal(err)
	}
	var pre HookStatus
	for _, h := range before.Hooks {
		if h.Hook == "PreToolUse" {
			pre = h
		}
	}
	if !pre.Installed || !pre.Stale {
		t.Fatalf("the old script should read as installed but stale, got %+v", pre)
	}

	if _, _, err := Install("C:/tools/atrium.exe"); err != nil {
		t.Fatal(err)
	}

	doc := readSettings(t, path)
	hooks, _ := doc["hooks"].(map[string]any)
	entries, _ := hooks["PreToolUse"].([]any)
	var cmds []string
	for _, e := range entries {
		cmds = append(cmds, commandsIn(e)...)
	}
	joined := strings.Join(cmds, "\n")
	if strings.Contains(joined, "atrium-activity-hook.ps1") {
		t.Fatalf("the old activity script is still registered:\n%s", joined)
	}
	// The permission hook is somebody else's and must be untouched.
	if !strings.Contains(joined, "atrium-perm-hook.ps1") {
		t.Fatalf("the permission hook was removed:\n%s", joined)
	}
	// The timeout the operator set on the entry survives.
	if !strings.Contains(string(mustJSON(t, doc)), `"timeout"`) {
		t.Fatal("a timeout the operator set was dropped")
	}
}

// A settings file that will not parse is the operator's, and atrium cannot
// merge into something it cannot read. Refusing is the only safe answer.
func TestInstallRefusesAnUnreadableFile(t *testing.T) {
	path := withHome(t, `{ "permissions": `)

	if _, _, err := Install("C:/tools/atrium.exe"); err == nil {
		t.Fatal("a truncated settings file was rewritten")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{ "permissions": ` {
		t.Fatalf("the file was changed anyway: %q", raw)
	}
}

// Anything replaced has to be recoverable, because this is the one file atrium
// edits that it does not own.
func TestInstallKeepsACopyOfWhatWasThere(t *testing.T) {
	original := `{ "model": "opus" }`
	path := withHome(t, original)

	_, res, err := Install("C:/tools/atrium.exe")
	if err != nil {
		t.Fatal(err)
	}
	if res.Backup == "" {
		t.Fatal("no copy was kept")
	}
	raw, err := os.ReadFile(res.Backup)
	if err != nil {
		t.Fatalf("the copy is not readable: %v", err)
	}
	if string(raw) != original {
		t.Fatalf("the copy is not what was there: %q", raw)
	}
	_ = path
}

// A fresh machine has no .claude directory at all, so the write has to create
// the path rather than failing on it.
func TestInstallCreatesTheFileWhenThereIsNone(t *testing.T) {
	path := withHome(t, "")

	_, res, err := Install("C:/tools/atrium.exe")
	if err != nil {
		t.Fatal(err)
	}
	if res.Backup != "" {
		t.Fatalf("a copy was kept of a file that did not exist: %q", res.Backup)
	}
	if !res.Changed {
		t.Fatal("creating the file was reported as changing nothing")
	}
	doc := readSettings(t, path)
	if _, ok := doc["hooks"]; !ok {
		t.Fatal("no hooks section was written")
	}
}

// A path with spaces is the normal case on Windows. Written unquoted, the
// shell reads the first word as the whole command.
func TestHookCommandQuotesAPathWithSpaces(t *testing.T) {
	got := HookCommandFor(`C:\Program Files\atrium\atrium.exe`, "tool-start")
	if !strings.HasPrefix(got, `"C:/Program Files/atrium/atrium.exe"`) {
		t.Fatalf("a spaced path was written unquoted: %s", got)
	}
	if plain := HookCommandFor("C:/tools/atrium.exe", "tool-start"); strings.Contains(plain, `"`) {
		t.Fatalf("a path with no spaces was quoted anyway: %s", plain)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

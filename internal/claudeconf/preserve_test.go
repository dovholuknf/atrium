package claudeconf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// settings.json is somebody's file that atrium happens to also write to. Three
// promises follow from that, and each is a way a real setup was reported
// broken rather than a hypothetical.

// A hook event usually runs several commands. Atrium owns one of them and must
// correct that one, leaving every sibling in place and in order.
func TestInstallKeepsSiblingCommands(t *testing.T) {
	withHome(t, `{
      "hooks": {
        "SessionStart": [
          {"matcher": "", "hooks": [
            {"type": "command", "command": "pwsh -File C:/dot/session-bootstrap.ps1 -Phase start"},
            {"type": "command", "command": "pwsh -File C:/old/atrium-session-hook.ps1 -Event start"},
            {"type": "command", "command": "pwsh -File C:/dot/set-session-state.ps1 -FromPayloadSource"}
          ]}
        ]
      }
    }`)

	if _, _, err := InstallOnly("C:/tools/atrium.exe", []string{"session-start"}); err != nil {
		t.Fatal(err)
	}

	cmds := commandsFor(t, "SessionStart")
	if len(cmds) != 3 {
		t.Fatalf("SessionStart runs %d commands, wanted 3: %v", len(cmds), cmds)
	}
	// Order matters. A bootstrap that registers the session ledger runs before
	// the thing that reads it.
	if !strings.Contains(cmds[0], "session-bootstrap.ps1") {
		t.Fatalf("the first command is now %q", cmds[0])
	}
	if !strings.Contains(cmds[1], "atrium.exe session --event start") {
		t.Fatalf("atrium's command was not corrected in place: %q", cmds[1])
	}
	if !strings.Contains(cmds[2], "set-session-state.ps1") {
		t.Fatalf("the last command is now %q", cmds[2])
	}
}

// Running install twice must not write the second time. A backup per run
// buries the one backup worth having, and a no-op has to be a no-op.
func TestInstallIsIdempotent(t *testing.T) {
	path := withHome(t, "")

	if _, res, err := Install("C:/tools/atrium.exe"); err != nil {
		t.Fatal(err)
	} else if !res.Changed {
		t.Fatal("the first install changed nothing")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	_, res, err := Install("C:/tools/atrium.exe")
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("a second install with nothing to do reported a change")
	}
	if res.Backup != "" {
		t.Fatalf("a second install kept a backup at %s", res.Backup)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("a second install rewrote the file")
	}
}

// settings.json is commonly a link into a dotfiles repo. Replacing the link
// with a plain file detaches the repo silently: both sides keep being edited
// and neither sees the other.
func TestInstallWritesThroughASymlink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	// The dotfiles repo, which is the file that has to end up changed.
	repo := t.TempDir()
	real := filepath.Join(repo, "settings.json")
	if err := os.WriteFile(real, []byte(`{"hooks":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		// Windows needs developer mode or an elevated shell to make one. The
		// promise still holds there, it just cannot be demonstrated here.
		t.Skipf("cannot create a symlink on this machine: %v", err)
	}

	if _, _, err := Install("C:/tools/atrium.exe"); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced with a plain file, detaching the dotfiles repo")
	}
	body, err := os.ReadFile(real)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "atrium.exe") {
		t.Fatal("the file in the repo was not the one written to")
	}
}

// commandsFor reads back what an event now runs, in order.
func commandsFor(t *testing.T, hook string) []string {
	t.Helper()
	path, err := UserSettingsPath()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, entry := range doc.Hooks[hook] {
		for _, h := range entry.Hooks {
			out = append(out, h.Command)
		}
	}
	return out
}

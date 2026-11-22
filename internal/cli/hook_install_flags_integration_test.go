//go:build integration

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runHookCmd(t *testing.T, args ...string) string {
	t.Helper()
	c := newHook()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(args)
	if err := c.Execute(); err != nil {
		t.Fatalf("atrium hook %v: %v\n%s", args, err, out.String())
	}
	return out.String()
}

func TestInstallDryRunWritesNothing(t *testing.T) {
	user := firstRunHome(t, "")
	project := t.TempDir()
	for _, args := range [][]string{
		{"install", "--dry-run"},
		{"install", "--report-only", "--project", project},
	} {
		out := runHookCmd(t, args...)
		if !strings.Contains(out, "nothing was written") || !strings.Contains(out, "+ SessionStart: ") {
			t.Fatalf("report does not say what would change:\n%s", out)
		}
	}
	if _, err := os.Stat(user); !os.IsNotExist(err) {
		t.Fatalf("a dry run created the user settings: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("a dry run created the project directory: %v", err)
	}
}

func TestInstallProjectTouchesOnlyThatProjectAndUninstallRestoresIt(t *testing.T) {
	const had = "{\n  \"model\": \"opus\"\n}\n"
	user := firstRunHome(t, had)
	project := t.TempDir()
	local := filepath.Join(project, ".claude", "settings.local.json")
	if err := os.MkdirAll(filepath.Dir(local), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local, []byte(had), 0o600); err != nil {
		t.Fatal(err)
	}

	runHookCmd(t, "install", "--project", project)
	if raw, _ := os.ReadFile(user); string(raw) != had {
		t.Fatalf("user settings were touched:\n%s", raw)
	}
	if raw, _ := os.ReadFile(local); !strings.Contains(string(raw), "atrium") || !strings.Contains(string(raw), "opus") {
		t.Fatalf("project settings lack the hooks or lost a key:\n%s", raw)
	}

	runHookCmd(t, "uninstall", "--project", project)
	if raw, _ := os.ReadFile(local); string(raw) != had {
		t.Fatalf("uninstall did not restore the project file:\n%s", raw)
	}
	if raw, _ := os.ReadFile(user); string(raw) != had {
		t.Fatalf("user settings were touched:\n%s", raw)
	}
}

func TestInstallWatchOnlyLeavesOutTheGate(t *testing.T) {
	user := firstRunHome(t, "")
	runHookCmd(t, "install", "--watch-only")
	raw, err := os.ReadFile(user)
	if err != nil {
		t.Fatal(err)
	}
	if s := string(raw); !strings.Contains(s, "SessionStart") || !strings.Contains(s, "tool-start") ||
		strings.Contains(s, "permission") {
		t.Fatalf("watch-only should write the reporting hooks and not the gate:\n%s", s)
	}
}

package claudeconf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTrustCodexProjectsAddsOnlyWhatIsMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	path := filepath.Join(home, "config.toml")
	a, b := filepath.Join(home, "a"), filepath.Join(home, "b")
	if err := os.WriteFile(path, []byte("[projects.'"+a+"']\ntrust_level = \"untrusted\""), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := TrustCodexProjects("", []string{a, b}); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := os.ReadFile(path)
	want := "[projects.'" + a + "']\ntrust_level = \"untrusted\"\n[projects.'" + b + "']\ntrust_level = \"trusted\"\n"
	if string(got) != want {
		t.Fatalf("config.toml:\n%s\nwant:\n%s", got, want)
	}
}

func TestTrustCodexProjectsWritesTheRowsCodexHome(t *testing.T) {
	daemon, row := t.TempDir(), t.TempDir()
	t.Setenv("CODEX_HOME", daemon)
	dir := filepath.Join(row, "work")
	if err := TrustCodexProjects(row, []string{dir}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(row, "config.toml")); string(got) != "\n[projects.'"+dir+"']\ntrust_level = \"trusted\"\n" {
		t.Fatalf("row config.toml:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(daemon, "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("the daemon's CODEX_HOME was written: %v", err)
	}
}

func TestAnswerCodexSandboxKeepsAnOwnChoice(t *testing.T) {
	cases := []struct{ name, before, after string }{
		{"empty", "", "\n[windows]\nsandbox = \"unelevated\"\n"},
		{"own table", "[windows]\nsandbox_private_desktop = true\n\n[tui]\nx = 1\n",
			"[windows]\nsandbox = \"unelevated\"\nsandbox_private_desktop = true\n\n[tui]\nx = 1\n"},
		{"own choice", "[windows]\nsandbox = \"elevated\"\n", "[windows]\nsandbox = \"elevated\"\n"},
		{"dotted", "windows.sandbox = \"elevated\"\n", "windows.sandbox = \"elevated\"\n"},
		{"other table's key", "[x]\nsandbox = 1\n", "[x]\nsandbox = 1\n\n[windows]\nsandbox = \"unelevated\"\n"},
	}
	for _, c := range cases {
		home := t.TempDir()
		path := filepath.Join(home, "config.toml")
		if c.before != "" {
			if err := os.WriteFile(path, []byte(c.before), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		for range 2 {
			if err := AnswerCodexSandbox(home); err != nil {
				t.Fatal(err)
			}
		}
		if got, _ := os.ReadFile(path); string(got) != c.after {
			t.Errorf("%s: config.toml:\n%s\nwant:\n%s", c.name, got, c.after)
		}
	}
}

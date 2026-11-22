package claudeconf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusLineIssues(t *testing.T) {
	dir := t.TempDir()
	spaced := filepath.ToSlash(filepath.Join(dir, "with space", "atrium.exe"))
	plain := filepath.ToSlash(filepath.Join(dir, "atrium.exe"))
	for _, p := range []string{spaced, plain} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	gone := filepath.ToSlash(filepath.Join(dir, "gone", "atrium.exe"))
	cases := []struct{ name, command, want string }{
		{"none", "", "no status line"},
		{"operator's own is accepted", "C:/tools/statusline.exe", ""},
		{"atrium statusline", plain + " statusline", ""},
		{"atrium statusline, quoted path with a space", `"` + spaced + `" statusline`, ""},
		{"atrium statusline of this room though absent", gone + " statusline", ""},
		{"atrium statusline that is not there", gone + " statusline", "not there"},
	}
	for _, c := range cases {
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
			t.Fatal(err)
		}
		doc, _ := json.Marshal(map[string]any{"statusLine": map[string]string{"type": "command", "command": c.command}})
		if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), doc, 0o644); err != nil {
			t.Fatal(err)
		}
		exe := plain
		if strings.Contains(c.name, "though absent") {
			exe = gone
		}
		got := strings.Join(StatusLineIssues(home, exe), "; ")
		if c.want == "" && got != "" || !strings.Contains(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	if got := StatusLineIssues(t.TempDir(), plain); len(got) != 1 || !strings.Contains(got[0], "no status line") {
		t.Errorf("no settings file: %v", got)
	}
}

func TestInstallStatusLineKeepsTheRestAndBacksUp(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := `{"model":"opus","statusLine":{"type":"command","command":"old.sh"}}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	backup, err := InstallStatusLine(home, "C:/Program Files/atrium/atrium.exe")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(backup); string(b) != old {
		t.Errorf("the backup holds %q", b)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var got struct {
		Model      string
		StatusLine struct{ Command string }
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Model != "opus" || got.StatusLine.Command != `"C:/Program Files/atrium/atrium.exe" statusline` {
		t.Errorf("settings after the fix: %s", b)
	}
	if issues := StatusLineIssues(home, "C:/Program Files/atrium/atrium.exe"); len(issues) != 0 {
		t.Errorf("still reported after the fix: %v", issues)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallStatusLine(home, "x"); err == nil {
		t.Error("a settings file that is not JSON was rewritten")
	}
}

package claudeconf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTranslateCommands(t *testing.T) {
	cases := []struct{ arg, want string }{
		// The colon marker means "and anything after", which is exactly what an
		// atrium plain pattern already does.
		{"go build:*", "go build"},
		{"ls:*", "ls"},
		{"gh api -X GET:*", "gh api -X GET"},
		// Already globs, carried across unchanged.
		{"go *", "go *"},
		{"git branch*", "git branch*"},
		{"ssh cdwsl *", "ssh cdwsl *"},
		// Exact commands work as prefixes.
		{"rm -rf ./build", "rm -rf ./build"},
		// A colon inside a path must not be mistaken for the marker.
		{"V:/work/tools/go/current/bin/go.exe build:*", "V:/work/tools/go/current/bin/go.exe build"},
	}
	for _, c := range cases {
		if got := translate("Bash", c.arg); got != c.want {
			t.Errorf("translate(Bash, %q) = %q, want %q", c.arg, got, c.want)
		}
	}
}

func TestTranslatePaths(t *testing.T) {
	cases := []struct{ arg, want string }{
		// The hook reports Windows paths, so the MSYS spelling has to be
		// converted or the rule can never match.
		{"//c/temp/**", "C:/temp/*"},
		{"//d/worktrees/**", "D:/worktrees/*"},
		{"/tmp/**", "/tmp/*"},
		{"//d/git/github/dovholuknf/dotagents/**", "D:/git/github/dovholuknf/dotagents/*"},
		{"./.env", "./.env"},
		{"~/.aws/**", "~/.aws/*"},
	}
	for _, c := range cases {
		if got := translate("Edit", c.arg); got != c.want {
			t.Errorf("translate(Edit, %q) = %q, want %q", c.arg, got, c.want)
		}
	}
}

func TestConvertClassifies(t *testing.T) {
	var entries []Entry
	var skipped []Skipped

	// A bare tool name means every use of it.
	convert("WebSearch", "approve", "test", &entries, &skipped)
	if len(entries) != 0 || len(skipped) != 1 {
		t.Fatalf("WebSearch should be skipped as ungated: %+v %+v", entries, skipped)
	}

	entries, skipped = nil, nil
	convert("Read", "approve", "test", &entries, &skipped)
	if len(entries) != 1 || !entries[0].Broad || entries[0].Pattern != "*" {
		t.Fatalf("a bare gated tool should become a broad rule: %+v", entries)
	}

	entries, skipped = nil, nil
	convert("mcp__ziti-mcp", "approve", "test", &entries, &skipped)
	if len(skipped) != 1 {
		t.Fatalf("mcp entries are not gated and should be skipped: %+v %+v", entries, skipped)
	}

	entries, skipped = nil, nil
	convert("Bash(git push*)", "block", "test", &entries, &skipped)
	if len(entries) != 1 || entries[0].Decision != "block" || entries[0].Pattern != "git push*" {
		t.Fatalf("deny entry did not convert: %+v", entries)
	}
	if entries[0].Broad {
		t.Error("a specific pattern was marked broad")
	}
}

// The real thing: parse a settings file shaped like the user's and check the
// conversion end to end.
func TestLoadFromSettingsFile(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	settings := map[string]any{
		"permissions": map[string]any{
			"allow": []string{
				"Bash(go build:*)", "Bash(go *)", "Bash(ls:*)",
				"Read(//c/temp/**)", "Edit(//d/worktrees/**)",
				"WebSearch", "mcp__ziti-mcp", "WebFetch(domain:github.com)",
			},
			"deny": []string{"Bash(git push*)", "Read(./.env)"},
		},
	}
	blob, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), blob, 0o644); err != nil {
		t.Fatal(err)
	}

	entries, skipped, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]string{}
	for _, e := range entries {
		got[e.Tool+"|"+e.Pattern] = e.Decision
	}
	for key, want := range map[string]string{
		"Bash|go build":       "approve",
		"Bash|go *":           "approve",
		"Bash|ls":             "approve",
		"Read|C:/temp/*":      "approve",
		"Edit|D:/worktrees/*": "approve",
		"Bash|git push*":      "block",
		"Read|./.env":         "block",
	} {
		if got[key] != want {
			t.Errorf("missing %s as %s, got %q", key, want, got[key])
		}
	}
	// Things atrium does not gate must be reported, not silently dropped.
	if len(skipped) < 3 {
		t.Errorf("expected WebSearch, mcp__ziti-mcp and WebFetch to be skipped, got %d: %+v",
			len(skipped), skipped)
	}
}

func TestMissingFilesAreNotAnError(t *testing.T) {
	entries, _, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("a project with no settings should import cleanly: %v", err)
	}
	_ = entries
}

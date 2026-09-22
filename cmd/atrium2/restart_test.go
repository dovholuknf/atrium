package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The restarter must start the SAME room. It once passed only --http, --agent
// and --db, so it read the default key directory instead of --dir, came up under
// a stale join record's name and hub, and never reattached.
func TestRestartArgsCarryTheWholeLaunch(t *testing.T) {
	l := roomLaunch{
		dir: `C:\rooms\a`, db: `C:\rooms\a.db`, human: "127.0.0.1:7781", agent: "127.0.0.1:7777",
		isolated: true, upgrades: true,
	}
	got := strings.Join(l.restartArgs(), " ")
	for _, want := range []string{
		"room", "--restart-after " + roomRestartDelay.String(), `--dir C:\rooms\a`, `--db C:\rooms\a.db`,
		"--http 127.0.0.1:7781", "--agent 127.0.0.1:7777", "--isolated", "--accept-upgrades",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("restart args %q are missing %q", got, want)
		}
	}

	plain := roomLaunch{dir: `C:\rooms\a`, human: "h", agent: "a"}.restartArgs()
	for _, a := range plain {
		switch a {
		case "--isolated", "--accept-upgrades", "--db":
			t.Errorf("restart args %v carry %s the room was not started with", plain, a)
		}
	}
}

// The restarter is detached and nobody reads its output, so what it did has to
// land in a file beside the room's keys.
func TestRestartLogAppends(t *testing.T) {
	dir := t.TempDir()
	restartLog(dir, "first %d", 1)
	restartLog(dir, "second")
	raw, err := os.ReadFile(filepath.Join(dir, restartLogName))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[0], "first 1") || !strings.HasSuffix(lines[1], "second") {
		t.Fatalf("restart.log is %q, want two appended lines", raw)
	}
}

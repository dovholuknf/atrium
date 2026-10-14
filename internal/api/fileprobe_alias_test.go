package api

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestProbeAliasSpellsHomeAndGitBashDrives(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got, want := probeAlias("~/notes/todo.md"), filepath.Join(home, "notes/todo.md"); got != want {
		t.Fatalf("~ path = %q, want %q", got, want)
	}
	if got := probeAlias("src/a.go"); got != "src/a.go" {
		t.Fatalf("a relative path changed: %q", got)
	}
	if got := probeAlias("/usr/x.go"); got != "/usr/x.go" {
		t.Fatalf("a posix path changed: %q", got)
	}
	want := "/d/work/a.go"
	if runtime.GOOS == "windows" {
		want = "D:/work/a.go"
	}
	if got := probeAlias("/d/work/a.go"); got != want {
		t.Fatalf("/d/ path = %q, want %q", got, want)
	}
}

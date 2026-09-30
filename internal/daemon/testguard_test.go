package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE GUARD IS ON FOR EVERY TEST HERE (TestMain). An agent becomes a shell, no
// ATRIUM_* pointer but the dead location is left, and home is not the user's.
func TestNoTestHereCanReachALiveRoom(t *testing.T) {
	for _, exe := range []string{"claude", "claude.exe", `C:\Users\x\AppData\Roaming\npm\claude.cmd`, "codex", "gemini"} {
		got, _ := agentSpawn(exe, []string{"--settings", "x"})
		if strings.Contains(strings.ToLower(got), "claude") || got == exe {
			t.Errorf("a test would start %s for real (%s)", exe, got)
		}
		if got, _ := agentFork(exe, nil); got == exe {
			t.Errorf("a test's keep-alive fork would run %s for real", exe)
		}
	}
	if got, _ := agentSpawn("cmd.exe", []string{"/k"}); got != "cmd.exe" {
		t.Errorf("a shell a test saved became %s", got)
	}
	for _, kv := range os.Environ() {
		k := kv[:strings.IndexByte(kv, '=')]
		if strings.HasPrefix(strings.ToUpper(k), "ATRIUM_") && !strings.EqualFold(k, "ATRIUM_LOCATION") && !strings.EqualFold(k, "ATRIUM_TESTGUARD") {
			t.Errorf("%s reached the tests", kv)
		}
	}
	loc := os.Getenv("ATRIUM_LOCATION")
	if loc == "" || !strings.Contains(filepath.ToSlash(loc), "atrium-testguard-") {
		t.Errorf("ATRIUM_LOCATION is %q, want the guard's dead file", loc)
	}
	if home, _ := os.UserHomeDir(); !strings.Contains(filepath.ToSlash(home), "atrium-testguard-") {
		t.Errorf("the tests' home is %q, the user's own", home)
	}
}

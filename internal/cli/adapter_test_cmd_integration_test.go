//go:build integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Today's claude hooks pass the kit unchanged. That is the standing proof that v1 describes what runners already do.
func TestTodaysClaudeHooksPassTheKit(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "atrium")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	build := exec.Command("go", "build", "-o", exe, "github.com/dovholuknf/atrium/cmd/atrium")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatal(err)
	}
	k := adapterKit{exe: exe, timeout: 60 * time.Second, out: os.Stderr}
	results := k.run()
	for _, r := range results {
		if len(r.faults) != 0 {
			t.Errorf("%s: %v", r.scenario, r.faults)
		}
	}
	for _, f := range []string{"session", "activity", "permission", "stop"} {
		if !results[0].features[f] {
			t.Errorf("claude's hooks never made the %s call", f)
		}
	}
}

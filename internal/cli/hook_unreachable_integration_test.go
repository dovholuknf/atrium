//go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/claudeconf"
)

// Every installed hook, run as the runner runs it, exits 0 inside the budget and says which hook, address and fix.
func TestEveryInstalledHookFailsOpenFastAndSaysWhy(t *testing.T) {
	const sessionWait = 2 * time.Second
	// Not t.TempDir: Windows may still hold a binary it just ran, and that would fail the test on cleanup.
	dir, err := os.MkdirTemp("", "hookunreachable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	exe := filepath.Join(dir, "atrium")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	build := exec.Command("go", "build", "-o", exe, "github.com/dovholuknf/atrium/cmd/atrium")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatal(err)
	}
	// The first starts of a new binary are scanned on Windows, which is not the hooks' time.
	closed := closedAddress(t)
	for _, w := range claudeconf.Claude.Wanted {
		warm := exec.Command(exe, w.Sub, "--event", w.Arg)
		warm.Env = append(os.Environ(), "ATRIUM_HUB_URL="+closed, "ATRIUM_AGENT_NAME=tester")
		if err := warm.Run(); err != nil {
			t.Fatal(err)
		}
	}

	targets := map[string]string{"nothing listening": closed, "silent": silentListener(t)}
	for _, w := range claudeconf.Claude.Wanted {
		for kind, url := range targets {
			func() {
				in, _ := json.Marshal(map[string]any{
					"hook_event_name": w.Hook, "tool_name": "Bash", "tool_input": map[string]string{"command": "ls"},
					"tool_use_id": "toolu_1", "session_id": "s1", "cwd": t.TempDir(), "reason": "prompt_input_exit",
					"notification_type": "idle_prompt",
				})
				cmd := exec.Command(exe, w.Sub, "--event", w.Arg)
				cmd.Env = append(os.Environ(), "ATRIUM_HUB_URL="+url, "ATRIUM_PERM_GATE=force",
					"ATRIUM_AGENT_NAME=tester")
				cmd.Stdin = bytes.NewReader(in)
				var out bytes.Buffer
				cmd.Stdout = &out
				start := time.Now()
				err := cmd.Run()
				took := time.Since(start)
				name := w.Hook + " " + w.Sub + " --event " + w.Arg + " against " + kind
				if err != nil {
					t.Errorf("%s: exited with %v", name, err)
				}
				// The whole process, startup included: hookBudget leaves that half second out.
				if took > sessionWait {
					t.Errorf("%s: took %s, budget %s", name, took, sessionWait)
				}
				var said struct {
					SystemMessage string         `json:"systemMessage"`
					Decision      string         `json:"decision"`
					Continue      *bool          `json:"continue"`
					Specific      map[string]any `json:"hookSpecificOutput"`
				}
				if err := json.Unmarshal(out.Bytes(), &said); err != nil {
					t.Errorf("%s: printed %q, want one json line", name, out.String())
					return
				}
				if said.Decision != "" || said.Continue != nil || said.Specific["permissionDecision"] != nil {
					t.Errorf("%s: decided something: %s", name, out.String())
				}
				for _, want := range []string{"atrium " + w.Sub + " --event " + w.Arg, url, "ATRIUM_PERM_GATE=off"} {
					if !strings.Contains(said.SystemMessage, want) {
						t.Errorf("%s: said %q, want it to name %q", name, said.SystemMessage, want)
					}
				}
			}()
		}
	}
}

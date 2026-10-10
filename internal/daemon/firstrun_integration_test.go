//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestAClaudePtyLaunchDeclinesTheFullscreenRenderer(t *testing.T) {
	claude := &store.Harness{ID: "claude", Cmd: "claude", LaunchMode: store.LaunchPTY}
	if v, ok := classicRendererDefault(claude, nil); !ok || v != "1" {
		t.Fatalf("a supervised claude was not kept on the classic renderer: %q %v", v, ok)
	}

	// The operator naming either variable is the operator choosing.
	for _, k := range []string{classicRendererEnv, "claude_code_no_flicker"} {
		if _, ok := classicRendererDefault(claude, map[string]string{k: "0"}); ok {
			t.Errorf("a launch env naming %s was overridden", k)
		}
	}

	// A window launch is in the operator's own terminal, and another runner
	// has no such variable.
	window := &store.Harness{ID: "claude", Cmd: "claude", LaunchMode: store.LaunchWindow}
	if _, ok := classicRendererDefault(window, nil); ok {
		t.Error("a window launch had its renderer chosen for it")
	}
	codex := &store.Harness{ID: "codex", Cmd: "codex", LaunchMode: store.LaunchPTY}
	if _, ok := classicRendererDefault(codex, nil); ok {
		t.Error("a codex launch got a claude variable")
	}
}

// Launched for real through the pty path, the runner starts with the variable.
func TestALaunchedClaudeStartsOnTheClassicRenderer(t *testing.T) {
	d := testDaemon(t)
	t.Setenv("ATRIUM_TEST_ENV_DUMP", "1")
	// An inherited choice is not the operator's for this runner: the room's own
	// CLAUDE_CODE_ variables are stripped, and the default goes on anyway.
	t.Setenv("CLAUDE_CODE_NO_FLICKER", "1")
	// ID "claude" makes it a claude runner to the daemon. The command is this
	// test binary, so the adapter, which goes by the command, writes no trust
	// into the real home directory.
	if _, err := d.st.SaveHarness(store.Harness{
		ID: "claude", Label: "claude code", Enabled: true, LaunchMode: store.LaunchPTY,
		Cmd: os.Args[0], Args: []string{"-test.run=^TestHelperEnvDump$"},
	}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	_, _ = d.Launch(LaunchRequest{Harness: "claude", Cwd: dir, SpawnedBy: store.HumanLauncher})

	var raw []byte
	deadline := time.Now().Add(10 * time.Second)
	for {
		b, err := os.ReadFile(filepath.Join(dir, "env.txt"))
		if err == nil {
			raw = b
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the runner never wrote its environment: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	seen := map[string]string{}
	for _, kv := range strings.Split(string(raw), "\n") {
		if i := strings.Index(kv, "="); i > 0 {
			seen[strings.ToUpper(kv[:i])] = kv[i+1:]
		}
	}
	if seen[classicRendererEnv] != "1" {
		t.Fatalf("the launched claude did not get %s=1", classicRendererEnv)
	}
	if v, ok := seen["CLAUDE_CODE_NO_FLICKER"]; ok {
		t.Fatalf("the room's own CLAUDE_CODE_NO_FLICKER=%s reached the runner", v)
	}
}

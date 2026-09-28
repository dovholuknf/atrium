package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/inputlag"
	"github.com/dovholuknf/atrium/internal/store"
)

// TestHelperEnvDump is the runner in TestALaunchedRunnerDoesNotInheritTheRoomsDebugSwitches.
// It writes its own environment into its working directory and exits.
func TestHelperEnvDump(t *testing.T) {
	if os.Getenv("ATRIUM_TEST_ENV_DUMP") == "" {
		t.Skip("not the helper")
	}
	body := strings.Join(os.Environ(), "\n")
	if err := os.WriteFile("env.txt.part", []byte(body), 0o644); err != nil {
		os.Exit(4)
	}
	if err := os.Rename("env.txt.part", "env.txt"); err != nil {
		os.Exit(5)
	}
	os.Exit(0)
}

// The live scripts start the room with ATRIUM_DEBUG_INPUTLAG=1 for the room's
// own lag log. A runner the room launched inherited it, so its `go test` failed
// in internal/link and every atrium binary it ran logged lag. Launched for real
// through the pty path, the runner's own environment must lack every inherited
// ATRIUM_DEBUG_ switch, and still carry one its harness names.
func TestALaunchedRunnerDoesNotInheritTheRoomsDebugSwitches(t *testing.T) {
	d := testDaemon(t)
	t.Setenv("ATRIUM_TEST_ENV_DUMP", "1")
	t.Setenv(inputlag.Env, "1")
	t.Setenv("ATRIUM_DEBUG_SOMETHING_ELSE", "1")
	if _, err := d.st.SaveHarness(store.Harness{
		ID: "envdump", Label: "env dump", Enabled: true, LaunchMode: store.LaunchPTY,
		Cmd: os.Args[0], Args: []string{"-test.run=^TestHelperEnvDump$"},
		Env: map[string]string{"ATRIUM_DEBUG_ASKED_FOR": "yes"},
	}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// The helper exits at once, so the launch may say it never settled. What
	// is checked is the environment it started with.
	_, _ = d.Launch(LaunchRequest{Harness: "envdump", Cwd: dir, SpawnedBy: store.HumanLauncher})

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
	for _, gone := range []string{inputlag.Env, "ATRIUM_DEBUG_SOMETHING_ELSE"} {
		if v, ok := seen[gone]; ok {
			t.Errorf("the launched runner inherited %s=%s from the room", gone, v)
		}
	}
	if seen["ATRIUM_DEBUG_ASKED_FOR"] != "yes" {
		t.Error("a debug switch the harness named itself did not reach the runner")
	}
	if seen["ATRIUM_TEST_ENV_DUMP"] != "1" {
		t.Error("an unrelated inherited variable was dropped")
	}
}

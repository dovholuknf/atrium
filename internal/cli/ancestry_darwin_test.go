//go:build darwin

package cli

import (
	"os"
	"strconv"
	"testing"
)

func TestProcInfoSelfAndParent(t *testing.T) {
	name, parent, ok := procInfo(os.Getpid())
	if !ok {
		t.Fatal("procInfo of this process failed")
	}
	if parent != os.Getppid() {
		t.Fatalf("parent = %d, want %d", parent, os.Getppid())
	}
	if name == "" {
		t.Fatal("empty name")
	}
	if _, _, ok := procInfo(parent); !ok {
		t.Fatalf("procInfo of the parent %d failed", parent)
	}
}

func TestProcInfoMissingPid(t *testing.T) {
	if _, _, ok := procInfo(1 << 30); ok {
		t.Fatal("a pid that does not exist reported ok")
	}
}

func TestArgv0BaseSelf(t *testing.T) {
	if got := argv0Base(os.Getpid()); got == "" {
		t.Fatal("argv0Base of this process is empty")
	}
	if got := argv0Base(1 << 30); got != "" {
		t.Fatalf("argv0Base of a missing pid = %q", got)
	}
}

// Set ATRIUM_EXPECT_RUNNER=1 when running this under a claude process, to see
// the walk find it. Skipped otherwise, since a test run has no runner above it.
func TestRunnerPIDUnderRunner(t *testing.T) {
	if os.Getenv("ATRIUM_EXPECT_RUNNER") == "" {
		t.Skip("not run under a runner")
	}
	for p, i := os.Getpid(), 0; p > 1 && i < maxHops; i++ {
		name, parent, _ := procInfo(p)
		t.Logf("hop %d pid %d name %q argv0 %q", i, p, name, argv0Base(p))
		p = parent
	}
	pid := runnerPID()
	t.Logf("runnerPID = %s", strconv.Itoa(pid))
	if pid == 0 {
		t.Fatal("no runner found above this process")
	}
}

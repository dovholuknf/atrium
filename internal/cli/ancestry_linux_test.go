//go:build linux

package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// THE NATIVE CLAUDE INSTALL IS A FILE NAMED FOR ITS VERSION, so the kernel's
// name for the process is `2.1.285` and only argv[0] says `claude`. This test
// binary plays all three parts: copied to a file named like a version and run
// with argv[0] `claude`, it is the runner, and the child it starts is the hook
// asking who its runner is.
func TestRunnerPIDFindsAClaudeNamedForItsVersion(t *testing.T) {
	const role = "ATRIUM_TEST_ANCESTRY"
	self := "-test.run=^TestRunnerPIDFindsAClaudeNamedForItsVersion$"
	switch os.Getenv(role) {
	case "hook":
		fmt.Printf("runner=%d\n", runnerPID())
		return
	case "runner":
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		hook := exec.Command(exe, self)
		hook.Env = append(os.Environ(), role+"=hook")
		hook.Stdout = os.Stdout
		if err := hook.Run(); err != nil {
			t.Fatal(err)
		}
		return
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	named := filepath.Join(t.TempDir(), "2.1.285")
	src, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenFile(named, os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	dst.Close()

	runner := exec.Command(named, self)
	runner.Args[0] = "claude"
	runner.Env = append(os.Environ(), role+"=runner")
	var out strings.Builder
	runner.Stdout = &out
	if err := runner.Start(); err != nil {
		t.Fatal(err)
	}
	pid := runner.Process.Pid
	if err := runner.Wait(); err != nil {
		t.Fatalf("the runner failed: %v %s", err, out.String())
	}
	want := "runner=" + strconv.Itoa(pid)
	if !strings.Contains(out.String(), want) {
		t.Fatalf("a hook under a claude named 2.1.285 (pid %d) found %q", pid, strings.TrimSpace(out.String()))
	}
}

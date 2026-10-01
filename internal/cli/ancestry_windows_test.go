//go:build windows

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

// OPENCODE RUNS ITS HOOKS FROM INSIDE ITSELF. The atrium plugin spawns the hook
// straight from opencode.exe, so the hook's parent is the runner. This test
// binary plays both parts: copied to opencode.exe it is the runner, and the
// child it starts is the hook asking who its runner is.
func TestRunnerPIDFindsOpencode(t *testing.T) {
	const role = "ATRIUM_TEST_ANCESTRY"
	self := "-test.run=^TestRunnerPIDFindsOpencode$"
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
	named := filepath.Join(t.TempDir(), "opencode.exe")
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
		t.Fatalf("a hook under opencode.exe (pid %d) found %q", pid, strings.TrimSpace(out.String()))
	}
}

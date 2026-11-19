//go:build integration

package daemon

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/store"
)

// processPathRunner looks only on this process PATH, not the user PATH or ~/.local/bin a real claude may sit on.
func processPathRunner(h *store.Harness) string {
	cmd := h.Cmd
	if cmd == "" {
		return ""
	}
	p, err := exec.LookPath(cmd)
	if err != nil {
		return ""
	}
	return p
}

func throwawayPath(t *testing.T, runners ...string) {
	t.Helper()
	shell := "sh"
	if runtime.GOOS == "windows" {
		shell = "cmd.exe"
	}
	src, err := exec.LookPath(shell)
	if err != nil {
		t.Skipf("no %s to stand in for a runner: %v", shell, err)
	}
	dir := t.TempDir()
	for _, name := range runners {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		in, err := os.Open(src)
		if err != nil {
			t.Fatal(err)
		}
		out, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY, 0o755)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(out, in)
		_ = in.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("Path", dir)
}

// A launch that names no runner is resolved on the room, so a hub that cannot see this PATH never has to guess.
func TestAnEmptyRunnerResolvesOnTheRoomToTheOnlyOneOnPath(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()
	throwawayPath(t, "codex")
	d.hk.runnerFound = processPathRunner
	if _, err := d.st.SaveHarness(store.Harness{ID: "codex", Label: "codex", Enabled: true, Cmd: "codex", LaunchMode: store.LaunchPTY}); err != nil {
		t.Fatal(err)
	}

	for name, req := range map[string]shared.LaunchRequest{
		"superlean true": {Cwd: t.TempDir(), Superlean: ptr(true)},
		"lean agents":    {Cwd: t.TempDir(), Lean: ptr(true), LeanDefaulted: true, LeanAgents: []string{"a"}},
		"lean skills":    {Cwd: t.TempDir(), Lean: ptr(true), LeanDefaulted: true, LeanSkills: []string{"s"}},
		"superlean+lean": {Cwd: t.TempDir(), Lean: ptr(true), LeanDefaulted: true, Superlean: ptr(true)},
	} {
		if _, err := d.Launch(req); err == nil || !strings.Contains(err.Error(), "claude launch option") {
			t.Fatalf("%s asked of a room that picked codex = %v, want the refusal", name, err)
		}
	}

	task, err := d.Launch(shared.LaunchRequest{Cwd: t.TempDir(), Lean: ptr(true), Superlean: ptr(true),
		LeanDefaulted: true, SuperleanDefaulted: true})
	if err != nil {
		if strings.Contains(err.Error(), "no runner") || strings.Contains(err.Error(), "unknown harness") ||
			strings.Contains(err.Error(), "lean") {
			t.Fatalf("an empty runner with only codex on PATH was refused: %v", err)
		}
		t.Skipf("could not spawn a stand-in codex on this machine: %v", err)
	}
	defer func() { _ = d.StopRunner(task.ID) }()
	if task.Runner != "codex" {
		t.Fatalf("the runner was %q, want codex", task.Runner)
	}
}

func TestAnEmptyRunnerWithNoRunnerOnPathIsRefusedByTheRoom(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()
	throwawayPath(t)
	d.hk.runnerFound = processPathRunner

	_, err := d.Launch(shared.LaunchRequest{Cwd: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "none of") {
		t.Fatalf("an empty runner with nothing on PATH = %v, want the refusal naming what was looked for", err)
	}
}

func ptr[T any](v T) *T { return &v }

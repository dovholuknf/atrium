//go:build integration

package roomspec

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestLinuxAdapterInDocker runs this package's real-filesystem and Linux tests inside ubuntu:24.04, so the Linux adapter is
// exercised on a real Linux filesystem and not only a fake. It is skipped when there is no docker, and inside the container.
func TestLinuxAdapterInDocker(t *testing.T) {
	if runtime.GOOS == Linux && os.Getenv("ROOMSPEC_IN_DOCKER") != "" {
		t.Skip("already in the container")
	}
	if testing.Short() {
		t.Skip("short")
	}
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("no docker")
	}
	run := func(name string, args ...string) string {
		cmd := exec.Command(name, args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Skipf("%s %v: %v: %s (docker is not usable here)", name, args[:1], err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run(docker, "version", "--format", "{{.Server.Version}}")

	bin := filepath.Join(t.TempDir(), "rs.test")
	build := exec.Command("go", "test", "-c", "-o", bin, ".")
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("cross-compile: %v\n%s", err, out)
	}
	// create, copy in, start, wait, read the log: a foreground `docker run` loses its output in some harnesses
	id := run(docker, "create", "-e", "ROOMSPEC_IN_DOCKER=1", "ubuntu:24.04", "/usr/local/bin/rs.test", "-test.v", "-test.run=RealFS|Linux|Darwin")
	t.Cleanup(func() { exec.Command(docker, "rm", "-f", id).Run() })
	run(docker, "cp", bin, id+":/usr/local/bin/rs.test")
	run(docker, "start", id)
	code := run(docker, "wait", id)
	logs, _ := exec.Command(docker, "logs", id).CombinedOutput()
	if code != "0" {
		t.Fatalf("the linux tests exited %s:\n%s", code, logs)
	}
	if !strings.Contains(string(logs), "PASS: TestRealFSApplyConverges") {
		t.Fatalf("the real-fs test did not run:\n%s", logs)
	}
}

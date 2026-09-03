package daemon

import (
	"runtime"
	"strings"
	"testing"
)

// A prepare command exists so a shell function that puts a toolchain on PATH
// can reach an agent. What it sets has to survive into the environment atrium
// hands the runner, or the agent starts without the tools it was prepared for
// and nothing says why.
func TestCaptureEnvKeepsWhatThePrepareCommandSet(t *testing.T) {
	set := `$env:ATRIUM_PREPARE_PROBE = 'yes'`
	if runtime.GOOS != "windows" {
		set = `export ATRIUM_PREPARE_PROBE=yes`
	}

	env, err := captureEnv(set, t.TempDir())
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if env["ATRIUM_PREPARE_PROBE"] != "yes" {
		t.Fatalf("the variable the command set did not come back: %q",
			env["ATRIUM_PREPARE_PROBE"])
	}
	// The rest of the environment comes with it, or the runner would start
	// with only what prepare happened to set and no PATH at all.
	if env["PATH"] == "" && env["Path"] == "" {
		t.Fatal("PATH did not survive, so the runner would have no commands")
	}
}

// Prepending to PATH is the actual use. Whatever was added has to be on the
// front, since that is what picking one toolchain over another means.
func TestCaptureEnvKeepsPathOrder(t *testing.T) {
	set := `$env:PATH = 'C:\atrium-probe;' + $env:PATH`
	if runtime.GOOS != "windows" {
		set = `export PATH=/atrium-probe:$PATH`
	}

	env, err := captureEnv(set, t.TempDir())
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	path := env["PATH"]
	if path == "" {
		path = env["Path"]
	}
	if !strings.HasPrefix(strings.ToLower(path), strings.ToLower("C:\\atrium-probe")) &&
		!strings.HasPrefix(path, "/atrium-probe") {
		t.Fatalf("what prepare put first is not first: %q", firstLine(path))
	}
}

// Nothing configured does nothing, rather than running a shell for no reason
// on every launch.
func TestCaptureEnvIsSkippedWhenEmpty(t *testing.T) {
	for _, in := range []string{"", "   ", "\n"} {
		env, err := captureEnv(in, t.TempDir())
		if err != nil {
			t.Fatalf("capture(%q): %v", in, err)
		}
		if env != nil {
			t.Fatalf("capture(%q) ran a shell anyway", in)
		}
	}
}

// A prepare command that fails has to say what the shell said. Launching
// without the tools it was supposed to set up produces an agent that cannot
// find them, which is a much worse thing to debug.
func TestCaptureEnvReportsWhatTheShellSaid(t *testing.T) {
	_, err := captureEnv("atrium-no-such-command-probe", t.TempDir())
	if err == nil {
		t.Fatal("a command that does not exist was reported as working")
	}
	if !strings.Contains(err.Error(), "prepare command") {
		t.Fatalf("the error does not say what failed: %v", err)
	}
}

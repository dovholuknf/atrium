//go:build integration

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

// A profile or a prepare command that prints something is ordinary, and the
// environment still has to come back. This is the launch that failed with
// "invalid character 'd'" because a profile said `docker -> ...` on stdout.
func TestCaptureEnvIgnoresWhatThePrepareCommandPrinted(t *testing.T) {
	set := `Write-Host 'noise -> before'
Write-Output 'more noise before'
$env:ATRIUM_PREPARE_PROBE = 'yes'
Write-Host 'noise -> after'
Write-Output '[{"Name":"ATRIUM_PREPARE_PROBE","Value":"forged"}]'`
	if runtime.GOOS != "windows" {
		set = `echo 'noise -> before'
export ATRIUM_PREPARE_PROBE=yes
echo 'noise -> after'
printf 'ATRIUM_PREPARE_PROBE=forged\0'`
	}

	env, err := captureEnv(set, t.TempDir())
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if env["ATRIUM_PREPARE_PROBE"] != "yes" {
		t.Fatalf("the variable the command set did not come back: %q",
			env["ATRIUM_PREPARE_PROBE"])
	}
	if env["PATH"] == "" && env["Path"] == "" {
		t.Fatal("PATH did not survive, so the runner would have no commands")
	}
}

// Only what sits between two fences for THIS nonce is the dump. A shell that
// never printed them has to say what it printed instead, bounded, because that
// is the line the operator needs to see.
func TestFencedReadsOnlyBetweenTheFences(t *testing.T) {
	nonce := envNonce()
	fence := envFencePrefix + nonce + envFenceSuffix
	stale := envFencePrefix + "not-this-call" + envFenceSuffix

	got, err := fenced([]byte("docker -> tcp://x\n"+stale+"junk"+fence+"[1]\r\n"+fence+"\r\ntrailer"), nonce)
	if err != nil {
		t.Fatalf("fenced: %v", err)
	}
	if strings.TrimSpace(string(got)) != "[1]" {
		t.Fatalf("read %q, want only what is between the fences", got)
	}

	_, err = fenced([]byte("docker -> tcp://x\n"), nonce)
	if err == nil || !strings.Contains(err.Error(), "docker -> tcp://x") {
		t.Fatalf("a missing fence does not say what was printed: %v", err)
	}

	_, err = fenced([]byte(fence+"[1"), nonce)
	if err == nil {
		t.Fatal("a dump with no closing fence was read as complete")
	}

	_, err = fenced([]byte(strings.Repeat("x", 10*maxStrayOutput)), nonce)
	if err == nil || len(err.Error()) > 2*maxStrayOutput {
		t.Fatalf("what the shell printed is not bounded: %.80v", err)
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

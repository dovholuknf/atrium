//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fakeVersionTool(t *testing.T, dir, name, out string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stub")
	}
	script := "#!/bin/sh\necho '" + out + "'\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestSetupProbesBinFileEnvAndVersions(t *testing.T) {
	bin := t.TempDir()
	fakeVersionTool(t, bin, "cmake", "cmake version 3.28.1")
	fakeVersionTool(t, bin, "ninja", "ninja")
	fakeVersionTool(t, bin, "gcc", "gcc 9.4.0")
	marker := filepath.Join(t.TempDir(), "marker")
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SETUP_TEST_SET", "x")
	t.Setenv("SETUP_TEST_EMPTY", "")
	repo := writeSetup(t, `
[cmake]
version = ">=3.25"
[ninja]
version = ">=1.0"
[gcc]
version = ">=10"
[nothere]
[viafile]
detect = { file = "`+filepath.ToSlash(marker)+`" }
[viaenv]
detect = { env = "SETUP_TEST_SET" }
[emptyenv]
detect = { env = "SETUP_TEST_EMPTY" }
[odd]
version = "~1.2"
detect = { bin = "cmake" }
[later]
detect = { docker = true }
[elsewhere]
os = ["plan9"]
`)
	got, err := checkSetup(repo, bin)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"cmake": needOK, "ninja": needUnknown, "gcc": needOld, "nothere": needMissing,
		"viafile": needOK, "viaenv": needOK, "emptyenv": needMissing, "odd": needUnknown, "later": needUnknown,
		"elsewhere": needNA}
	for name, state := range want {
		if stateOf(got, name) != state {
			t.Errorf("%s = %q, want %q", name, stateOf(got, name), state)
		}
	}
}

func TestSetupUnknownVersionNeverRefuses(t *testing.T) {
	bin := t.TempDir()
	fakeVersionTool(t, bin, "tool", "no digits here")
	repo := writeSetup(t, "[tool]\nversion = \">=1\"\nrequired = true\n")
	text, err := setupFor(repo, bin)
	if err != nil || !strings.Contains(text, "- tool: unknown") {
		t.Fatalf("text = %q, err = %v", text, err)
	}
}

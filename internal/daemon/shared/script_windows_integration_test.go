//go:build integration && windows

package shared

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func cardText() []string {
	return []string{`x" & echo PWNED & "`, `%ATRIUM_PROOF_VAR%`, `a ^& b | c > d`}
}

func TestBatchShimArgsAreNotLiveToCmd(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "fake.cmd")
	if err := os.WriteFile(shim, []byte("@echo off\r\necho ARGS=%*\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATRIUM_PROOF_VAR", "EXPANDED")
	for _, prompt := range cardText() {
		exe, args, err := ViaShellIfScript(shim, []string{"-p", prompt})
		if err == nil {
			out, _ := exec.Command(exe, args...).CombinedOutput()
			t.Errorf("cmd.exe was handed %q: %q", prompt, out)
		}
	}
	exe, args, err := ViaShellIfScript(shim, []string{"--model", "opus-4", "two words"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(exe, args...).CombinedOutput()
	if err != nil || !strings.Contains(string(out), `ARGS=--model opus-4 "two words"`) {
		t.Errorf("plain args through the shim: %q %v", out, err)
	}
}

// The shim cmd-shim writes for an npm global, but the package path.
const npmShim = `@ECHO off
GOTO start
:find_dp0
SET dp0=%~dp0
EXIT /b
:start
SETLOCAL
CALL :find_dp0

IF EXIST "%dp0%\node.exe" (
  SET "_prog=%dp0%\node.exe"
) ELSE (
  SET "_prog=node"
  SET PATHEXT=%PATHEXT:;.JS;=;%
)

endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & "%_prog%"  "%dp0%\node_modules\fake-runner\cli.js" %*
`

func TestNpmShimRunsNodeWithNoCmd(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil || !strings.EqualFold(filepath.Ext(node), ".exe") {
		t.Skip("no node.exe on PATH")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "node_modules", "fake-runner", "cli.js")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("console.log(JSON.stringify(process.argv.slice(2)))\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(dir, "fake-runner.cmd")
	if err := os.WriteFile(shim, []byte(strings.ReplaceAll(npmShim, "\n", "\r\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATRIUM_PROOF_VAR", "EXPANDED")
	want := append([]string{"-p"}, cardText()...)
	exe, args, err := ViaShellIfScript(shim, want)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(exe, node) || args[0] != script {
		t.Fatalf("want %s %s, got %s %q", node, script, exe, args)
	}
	out, err := exec.Command(exe, args...).Output()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil || strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("runner got %q, want %q verbatim", out, want)
	}
}

//go:build integration

package cli

import (
	"bytes"
	"strings"
	"testing"
)

func runSSH(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := newSSH()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(append(args, "--dir", dir))
	if err := c.Execute(); err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return out.String()
}

func TestSSHAddWithoutFromSaysNobody(t *testing.T) {
	out := runSSH(t, t.TempDir(), "add", "wsl", "127.0.0.1:22")
	if !strings.Contains(out, "wsl is offered to no room yet. add --from <room> or --from '*'") {
		t.Fatalf("out = %q", out)
	}
}

func TestSSHLsListsWhatAddWrote(t *testing.T) {
	dir := t.TempDir()
	runSSH(t, dir, "add", "wsl", "127.0.0.1:22", "--from", "room-a,room-d", "--user", "operator", "--note", "Ubuntu")
	runSSH(t, dir, "add", "win11", "10.0.0.9:22", "--from", "*")
	out := runSSH(t, dir, "ls")
	for _, want := range []string{"wsl\t127.0.0.1:22\troom-a,room-d\toperator\tUbuntu", "win11\t10.0.0.9:22\t*"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
	runSSH(t, dir, "rm", "wsl")
	if out := runSSH(t, dir, "ls"); strings.Contains(out, "wsl") {
		t.Fatalf("rm left it: %q", out)
	}
}

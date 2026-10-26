//go:build integration

package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/remoteaccess"
)

func runRemoteAccess(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	c := roomRemoteAccessCmd()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(append(args, "--dir", dir))
	err := c.Execute()
	return out.String(), err
}

// Off until a person turns it on, then on with the list they named, then off again.
func TestRemoteAccessOnOffAndState(t *testing.T) {
	t.Setenv("ATRIUM_TASK_ID", "")
	t.Setenv("ATRIUM_AGENT_NAME", "")
	t.Setenv(remoteaccess.EnvForce, "")
	dir := t.TempDir()
	writeRoomCert(t, dir)
	if out, err := runRemoteAccess(t, dir); err != nil || !strings.Contains(out, "remote access: off") {
		t.Fatalf("fresh state: %q %v", out, err)
	}
	if out, err := runRemoteAccess(t, dir, "on", "--from", "hub,m1mini", "--port", "2222"); err != nil ||
		!strings.Contains(out, "on, port 2222, from hub, m1mini") {
		t.Fatalf("on: %q %v", out, err)
	}
	s, err := remoteaccess.Load(dir)
	if err != nil || !s.On || s.Port != 2222 || !s.Allows("m1mini") || s.Allows("sg4") {
		t.Fatalf("saved: %+v %v", s, err)
	}
	if _, err := runRemoteAccess(t, dir, "off"); err != nil {
		t.Fatal(err)
	}
	if s, _ = remoteaccess.Load(dir); s.On {
		t.Fatalf("still on after off: %+v", s)
	}
	// Default from is *, and a bad list is refused.
	if _, err := runRemoteAccess(t, dir, "on"); err != nil {
		t.Fatal(err)
	}
	if s, _ = remoteaccess.Load(dir); !s.On || s.From[0] != "*" || s.Port != 22 {
		t.Fatalf("defaults: %+v", s)
	}
	if _, err := runRemoteAccess(t, dir, "on", "--from", "a b"); err == nil {
		t.Fatal("a bad from list was accepted")
	}
}

// A session atrium supervises cannot turn it on. It can turn it off.
func TestRemoteAccessOnRefusesInsideASupervisedSession(t *testing.T) {
	dir := t.TempDir()
	writeRoomCert(t, dir)
	for _, env := range []string{"ATRIUM_TASK_ID", "ATRIUM_AGENT_NAME"} {
		t.Setenv("ATRIUM_TASK_ID", "")
		t.Setenv("ATRIUM_AGENT_NAME", "")
		t.Setenv(env, "x")
		_, err := runRemoteAccess(t, dir, "on")
		if err == nil || !strings.Contains(err.Error(), "not by an agent") {
			t.Fatalf("%s: on was answered %v", env, err)
		}
		if _, statErr := remoteaccess.Load(dir); statErr != nil {
			t.Fatal(statErr)
		}
		if s, _ := remoteaccess.Load(dir); s.On {
			t.Fatalf("%s: the file says on", env)
		}
		if _, err := runRemoteAccess(t, dir, "off"); err != nil {
			t.Fatalf("%s: off was refused: %v", env, err)
		}
	}
}

func TestRemoteAccessEnvForcesOffInTheState(t *testing.T) {
	t.Setenv("ATRIUM_TASK_ID", "")
	t.Setenv("ATRIUM_AGENT_NAME", "")
	dir := t.TempDir()
	writeRoomCert(t, dir)
	if _, err := runRemoteAccess(t, dir, "on"); err != nil {
		t.Fatal(err)
	}
	t.Setenv(remoteaccess.EnvForce, "off")
	out, _ := runRemoteAccess(t, dir)
	if !strings.Contains(out, "off (forced by ATRIUM_REMOTE_ACCESS)") {
		t.Fatalf("state: %q", out)
	}
}

func TestRemoteAccessLogPrintsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	writeRoomCert(t, dir)
	remoteaccess.AppendLog(dir, remoteaccess.LogLine{Event: "open", ID: "s1", Caller: "hub", Via: "direct"})
	remoteaccess.AppendLog(dir, remoteaccess.LogLine{Event: "refused", ID: "s2", Caller: "m1mini", Error: "no"})
	out, err := runRemoteAccess(t, dir, "log", "--n", "5")
	if err != nil || strings.Index(out, "s2") > strings.Index(out, "s1") || !strings.Contains(out, "refused") {
		t.Fatalf("log: %q %v", out, err)
	}
	_ = filepath.Join
}

func TestRemoteAccessOnTakesAnAddr(t *testing.T) {
	t.Setenv("ATRIUM_TASK_ID", "")
	t.Setenv("ATRIUM_AGENT_NAME", "")
	t.Setenv(remoteaccess.EnvForce, "")
	dir := t.TempDir()
	writeRoomCert(t, dir)
	if out, err := runRemoteAccess(t, dir, "on", "--addr", "192.168.1.147", "--from", "hub"); err != nil ||
		!strings.Contains(out, "on, 192.168.1.147:22, from hub") {
		t.Fatalf("on --addr: %q %v", out, err)
	}
	if s, _ := remoteaccess.Load(dir); s.Addr != "192.168.1.147" {
		t.Fatalf("saved: %+v", s)
	}
	// The default address is stored as none, so the file stays as it was for every room that never set one.
	if _, err := runRemoteAccess(t, dir, "on", "--addr", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if s, _ := remoteaccess.Load(dir); s.Addr != "" {
		t.Fatalf("default addr stored: %+v", s)
	}
	if _, err := runRemoteAccess(t, dir, "on", "--addr", "a b"); err == nil {
		t.Fatal("a bad addr was accepted")
	}
}

func runSSHSetup(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	c := sshSetupCmd()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(append(args, "--dir", dir))
	err := c.Execute()
	return out.String(), err
}

// Only --check is built, and it changes nothing: not the switch, and no file in the key directory.
func TestSSHSetupCheckReadsAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	if _, err := runSSHSetup(t, dir); err == nil {
		t.Fatal("setup without --check ran")
	}
	// A directory that never joined: step 1 fails in words and the rest wait. Failed is a non zero exit.
	out, err := runSSHSetup(t, dir, "--check", "--no-self-test")
	if err == nil {
		t.Fatal("a failed step exited zero")
	}
	for _, sub := range []string{"1. ", "Where am I", "has not joined a hub yet", "11. ", "waiting", "waits for step 1"} {
		if !strings.Contains(out, sub) {
			t.Fatalf("missing %q in:\n%s", sub, out)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 0 {
		t.Fatalf("--check wrote %v", left)
	}

	js, _ := runSSHSetup(t, dir, "--check", "--json", "--no-self-test")
	if !strings.Contains(js, `"status": "failed"`) || !strings.Contains(js, `"ready": false`) || strings.Count(js, `"key":`) != 11 {
		t.Fatalf("json:\n%s", js)
	}
	if _, err := runSSHSetup(t, dir, "--check", "--from", "a b"); err == nil {
		t.Fatal("a bad --from was accepted")
	}
}

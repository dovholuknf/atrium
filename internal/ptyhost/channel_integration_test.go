//go:build integration

package ptyhost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAddressIsAHashOfTheStateDir(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	if Address(a) != Address(a) || Address(a) == Address(b) {
		t.Fatalf("the address is not a stable, distinct function of the state dir: %s %s", Address(a), Address(b))
	}
	// no path in it: the name is a hash, whatever the platform puts around it
	name := filepath.Base(Address(a))
	for _, part := range strings.Split(filepath.ToSlash(a), "/") {
		if len(part) > 3 && strings.Contains(strings.ToLower(name), strings.ToLower(part)) {
			t.Fatalf("the address %q reveals the path segment %q", name, part)
		}
	}
	if rel, err := filepath.Rel(".", a); err == nil && Address(a) != Address(filepath.Join(".", rel)) {
		t.Fatal("a relative spelling of the same dir got a different address")
	}
}

func TestStartRunsADetachedHostFromACopy(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	state, copies := t.TempDir(), t.TempDir()
	since := time.Now()
	p, err := Start(state, StartOptions{Exe: exe, CopyDir: copies,
		Args: []string{"__host", state, "3s"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("started after %s", time.Since(since))
	t.Cleanup(func() { _ = p.Kill() })
	c, err := DialTimeout(Address(state), 20*time.Second)
	if err != nil {
		data, _ := os.ReadFile(filepath.Join(state, "ptyhost.log"))
		t.Fatalf("dial: %v\nhost log:\n%s", err, data)
	}
	t.Logf("dialed after %s", time.Since(since))
	defer c.Close()
	pr, err := c.Probe()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("probed after %s, in a job: %v", time.Since(since), pr.InJob)
	if pr.Pid != p.Pid || pr.Pid == os.Getpid() || pr.Build != "test-host" {
		t.Fatalf("probe %+v, started pid %d", pr, p.Pid)
	}
	matches, _ := filepath.Glob(filepath.Join(copies, "atrium-pty*"))
	if len(matches) != 1 {
		t.Fatalf("the copy: %v", matches)
	}
	c.Close()
	done := make(chan struct{})
	go func() { _, _ = p.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the started host did not exit when idle")
	}
}

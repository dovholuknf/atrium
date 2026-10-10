//go:build integration

package ptyhost

import (
	"testing"
	"time"
)

// Closing a ConPTY does not end the process under it, so a host closed with a runner alive once left that runner
// running with nobody to collect it. A test run left twenty behind.
func TestHostCloseKillsItsRunners(t *testing.T) {
	dir := t.TempDir()
	h, err := Listen(dir, Options{Logf: func(f string, a ...any) { t.Logf("host: "+f, a...) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	c := daemonT(t, Address(dir))
	sp := spawnT(t, c, "card", KindRunner, 0, "echo")
	if !pidAlive(sp.Pid) {
		t.Fatalf("the runner (pid %d) was not running to begin with", sp.Pid)
	}
	h.Close()
	waitFor(t, 10*time.Second, "the runner to be gone after the host closed", func() bool { return !pidAlive(sp.Pid) })
}

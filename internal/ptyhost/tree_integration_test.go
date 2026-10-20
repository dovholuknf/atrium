//go:build integration

package ptyhost

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func pollUntil(limit time.Duration, cond func() bool) bool {
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	deadline := time.After(limit)
	for !cond() {
		select {
		case <-tick.C:
		case <-deadline:
			return cond()
		}
	}
	return true
}

func TestKillEndsTheRunnersChildren(t *testing.T) {
	for _, sig := range []string{SigKill, SigTerm} {
		t.Run(sig, func(t *testing.T) {
			_, addr := testHost(t, Options{})
			c := daemonT(t, addr)
			pidFile := filepath.Join(t.TempDir(), "child.pid")
			sp := spawnT(t, c, "tree-"+sig, KindRunner, 0, "parent", pidFile)
			var child int
			deadline := time.Now().Add(20 * time.Second)
			for child == 0 && time.Now().Before(deadline) {
				if b, err := os.ReadFile(pidFile); err == nil {
					child, _ = strconv.Atoi(strings.TrimSpace(string(b)))
				}
				time.Sleep(20 * time.Millisecond)
			}
			if child == 0 {
				t.Fatal("the runner never started its child")
			}
			t.Cleanup(func() {
				if p, err := os.FindProcess(child); err == nil {
					_ = p.Kill()
				}
			})
			if !pidAlive(child) {
				t.Fatalf("child %d is not running before the kill", child)
			}
			if err := c.Signal(sp.RunID, sig); err != nil {
				t.Fatal(err)
			}
			end := time.Now().Add(10 * time.Second)
			for pidAlive(child) && time.Now().Before(end) {
				time.Sleep(20 * time.Millisecond)
			}
			if pidAlive(child) {
				t.Fatalf("child %d outlived its runner's %s", child, sig)
			}
		})
	}
}

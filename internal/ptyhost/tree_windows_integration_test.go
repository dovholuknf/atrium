//go:build integration && windows

package ptyhost

import (
	"os/exec"
	"testing"
)

func TestTheJobIsNamedAfterTheLabelAndATakenNameGetsTheRunID(t *testing.T) {
	label := "atrium tree-name-test@" + t.Name()
	var trees []*tree
	for _, runID := range []string{"R1", "R2"} {
		cmd := exec.Command("ping", "-n", "30", "127.0.0.1")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		tr := newTree(cmd.Process.Pid, label, runID)
		trees = append(trees, tr)
		t.Cleanup(func() { tr.kill(); _ = cmd.Wait() })
		if tr.job == 0 {
			t.Fatalf("run %s is in no job", runID)
		}
	}
	if !jobExists(label) {
		t.Fatalf("no job is named %q", label)
	}
	if !jobExists(label + " R2") {
		t.Fatalf("the second run did not take %q", label+" R2")
	}
	for _, tr := range trees {
		tr.kill()
	}
	if jobExists(label) {
		t.Fatalf("%q outlived its runner", label)
	}
}

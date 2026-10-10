//go:build integration && darwin

package cli

import (
	"os"
	"testing"
)

func TestProcInfoSelfAndParent(t *testing.T) {
	name, parent, ok := procInfo(os.Getpid())
	if !ok {
		t.Fatal("procInfo of this process failed")
	}
	if parent != os.Getppid() {
		t.Fatalf("parent = %d, want %d", parent, os.Getppid())
	}
	if name == "" {
		t.Fatal("empty name")
	}
	if _, _, ok := procInfo(parent); !ok {
		t.Fatalf("procInfo of the parent %d failed", parent)
	}
}

//go:build integration && !windows

package ptyhost

import (
	"os"
	"testing"
)

func TestSocketIsOwnerOnly(t *testing.T) {
	_, addr := testHost(t, Options{})
	fi, err := os.Stat(addr)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode %v, want 0600", fi.Mode().Perm())
	}
}

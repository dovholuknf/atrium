//go:build integration

package dblock

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestASecondHolderIsRefusedWithThePidOfTheFirst(t *testing.T) {
	db := filepath.Join(t.TempDir(), "x.db")
	first, err := Acquire(db)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Acquire(db)
	var held *HeldError
	if !errors.Is(err, ErrHeld) || !errors.As(err, &held) || held.Pid != os.Getpid() {
		t.Fatalf("want held by pid %d, got %v", os.Getpid(), err)
	}
	if !strings.Contains(err.Error(), "store held by another process, pid "+strconv.Itoa(os.Getpid())) {
		t.Fatalf("message: %v", err)
	}
	first.Release()
	again, err := Acquire(db)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	again.Release()
}

// A holder that crashed leaves its sidecar, pid and all, and the operating system has already let go of the lock.
func TestALockFileLeftBehindByACrashDoesNotBlock(t *testing.T) {
	db := filepath.Join(t.TempDir(), "x.db")
	if err := os.WriteFile(Path(db), []byte("999999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := Acquire(db)
	if err != nil {
		t.Fatalf("a stale sidecar blocked a restart: %v", err)
	}
	l.Release()
}

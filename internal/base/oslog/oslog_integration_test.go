//go:build integration

package oslog

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupTeesTheStandardLoggerIntoABoundedFileAndTailFilters(t *testing.T) {
	dir := t.TempDir()
	prevFlags := log.Flags()
	log.SetFlags(0)
	defer log.SetFlags(prevFlags)
	undo := Setup(dir)
	Event("room", "start", 42, "boot", "launchd")
	log.Printf("[hub] up")
	undo()

	all, _ := Tail(dir, "", 0)
	if len(all) != 2 {
		t.Fatalf("lines = %q, want 2", all)
	}
	room, _ := Tail(dir, "room", 0)
	if len(room) != 1 || !strings.Contains(room[0], `start pid=42 reason="boot" by="launchd" level=info`) {
		t.Fatalf("room lines = %q", room)
	}
	if last, _ := Tail(dir, "", 1); len(last) != 1 || last[0] != "[hub] up" {
		t.Fatalf("last = %q", last)
	}
}

func TestRollingKeepsAtMostKeepFilesPlusTheCurrent(t *testing.T) {
	dir := t.TempDir()
	r := &rolling{path: filepath.Join(dir, FileName), max: 20}
	for i := 0; i < 10; i++ {
		_, _ = r.Write([]byte("0123456789abcdef\n"))
	}
	_ = r.Close()
	ents, _ := os.ReadDir(dir)
	if len(ents) != keepFiles+1 {
		t.Fatalf("%d files, want %d", len(ents), keepFiles+1)
	}
}

func TestLevelReadsTheLevelFieldAndDefaultsToInfo(t *testing.T) {
	if Level("[a] x level=warn") != "warn" || Level("[a] plain") != "info" || Level("level=bogus") != "info" {
		t.Fatal("level parse")
	}
}

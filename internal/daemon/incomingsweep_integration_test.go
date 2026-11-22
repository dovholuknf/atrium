//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSweepIncomingRemovesOnlyOldFilesAtriumWrote(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".atrium", "incoming")
	if err := os.MkdirAll(filepath.Join(dir, "20260101-000000-sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-10 * 24 * time.Hour)
	put := func(name string, age time.Time) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("abcd"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, age, age); err != nil {
			t.Fatal(err)
		}
	}
	put("20260101-000000-a.png", old)
	put("20260101-000000-2-b.png", old)
	put("20261120-000000-fresh.png", time.Now())
	put("mine.txt", old)

	n, bytes := sweepIncoming([]string{root}, 7*24*time.Hour, time.Now())
	if n != 2 || bytes != 8 {
		t.Fatalf("removed %d files, %d bytes, want 2 and 8", n, bytes)
	}
	for _, keep := range []string{"20261120-000000-fresh.png", "mine.txt", "20260101-000000-sub"} {
		if _, err := os.Lstat(filepath.Join(dir, keep)); err != nil {
			t.Fatalf("%s should have stayed: %v", keep, err)
		}
	}
}

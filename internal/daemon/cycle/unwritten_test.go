package cycle

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon/shared"
)

func TestUnwrittenDropsWhatTheHandoffAlreadyHolds(t *testing.T) {
	file := filepath.Join(t.TempDir(), "handoff.md")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(file)
	w := fi.ModTime()
	got := unwritten([]shared.DeliveredSay{
		{Text: "before", At: w.Add(-time.Second)},
		{Text: "after", At: w.Add(time.Second)},
	}, file)
	if len(got) != 1 || got[0].Text != "after" {
		t.Fatalf("want only the say delivered after the handoff, got %+v", got)
	}
}

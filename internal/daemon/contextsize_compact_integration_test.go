//go:build integration

package daemon

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A card that compacted reads its new size, never the reply before the compaction (public-ip on claude-room-e: 212k
// on the board, 55k on its status line).
func TestAContextReadFollowsACompaction(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	c := newContextSizes()
	c.transcript = func(cwd, id string) string {
		if id == "s" {
			return path
		}
		return ""
	}
	var heardUsed int64
	var heardAt time.Time
	c.heard = func(string) (int64, time.Time) { return heardUsed, heardAt }
	card := &store.Task{ID: "c", Worktree: "w", ResumeID: "s"}
	base := time.Now().Add(-time.Hour)
	at := func(m int) time.Time { return base.Add(time.Duration(m) * time.Minute) }
	want := func(n int64) {
		t.Helper()
		if got, _ := c.read(card); got != n {
			t.Fatalf("context %d, want %d", got, n)
		}
	}

	appendTo(t, path, replyLine("a", 212_000, at(0)), at(0))
	want(212_000)

	appendTo(t, path, `{"type":"system","subtype":"compact_boundary","isSidechain":false,`+
		`"compactMetadata":{"trigger":"auto","preTokens":212000}}`+"\n"+
		`{"type":"user","isCompactSummary":true,"message":{"role":"user","content":"summary"}}`+"\n", at(1))
	want(0)

	heardUsed, heardAt = 54_981, at(2)
	want(54_981)

	appendTo(t, path, replyLine("b", 56_000, at(3)), at(3))
	want(56_000)

	heardUsed, heardAt = 57_000, at(4)
	want(57_000)
}

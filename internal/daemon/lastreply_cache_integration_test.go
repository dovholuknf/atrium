//go:build integration

package daemon

import (
	"fmt"
	"github.com/dovholuknf/atrium/internal/daemon/cycle"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLastReplyUnchangedTranscriptScansOnce(t *testing.T) {
	var replies cycle.LastReplies
	p := filepath.Join(t.TempDir(), "a.jsonl")
	now := time.Now()
	appendTo(t, p, replyLine("1", 10, now), now)
	before := replies.Scans.Load()
	for range 3 {
		r, err := replies.ReadLastReply(p)
		if err != nil || r.Context != 10 || r.TTL != 5*time.Minute {
			t.Fatalf("got %+v, %v", r, err)
		}
	}
	if n := replies.Scans.Load() - before; n != 1 {
		t.Fatalf("scans = %d, want 1", n)
	}
}

func TestLastReplyAppendedReplyIncrementalMatchesWhole(t *testing.T) {
	var replies cycle.LastReplies
	t.Parallel()
	p := filepath.Join(t.TempDir(), "a.jsonl")
	now := time.Now()
	appendTo(t, p, replyLine("1", 10, now), now)
	replies.ReadLastReply(p)
	appendTo(t, p, replyLine("2", 20, now)+`{"type":"user"}`+"\n", now.Add(time.Second))
	got, err := replies.ReadLastReply(p)
	if err != nil || got.Context != 20 {
		t.Fatalf("got %+v, %v", got, err)
	}
	replies.LastReplyDrop(p)
	whole, err := replies.ReadLastReply(p)
	if err != nil || *whole != *got {
		t.Fatalf("incremental %+v, whole %+v, %v", got, whole, err)
	}
}

func TestLastReplyShrunkFileReadWhole(t *testing.T) {
	var replies cycle.LastReplies
	p := filepath.Join(t.TempDir(), "a.jsonl")
	now := time.Now()
	appendTo(t, p, replyLine("1", 10, now)+replyLine("2", 20, now), now)
	replies.ReadLastReply(p)
	os.Remove(p)
	appendTo(t, p, replyLine("3", 7, now), now.Add(time.Second))
	before := replies.Scans.Load()
	got, err := replies.ReadLastReply(p)
	if err != nil || got.Context != 7 || replies.Scans.Load()-before != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestLastReplyErrorNotCached(t *testing.T) {
	var replies cycle.LastReplies
	t.Parallel()
	p := filepath.Join(t.TempDir(), "a.jsonl")
	now := time.Now()
	appendTo(t, p, `{"type":"user"}`+"\n", now)
	if _, err := replies.ReadLastReply(p); err == nil {
		t.Fatal("want error")
	}
	appendTo(t, p, replyLine("1", 5, now), now.Add(time.Second))
	if r, err := replies.ReadLastReply(p); err != nil || r.Context != 5 {
		t.Fatalf("got %+v, %v", r, err)
	}
}

func TestLastReplyCacheBounded(t *testing.T) {
	var replies cycle.LastReplies
	dir := t.TempDir()
	now := time.Now()
	for i := range cycle.LastReplyCap + 20 {
		p := filepath.Join(dir, fmt.Sprintf("%d.jsonl", i))
		appendTo(t, p, replyLine("1", 1, now), now)
		replies.ReadLastReply(p)
	}
	replies.Mu.Lock()
	n := len(replies.M)
	replies.Mu.Unlock()
	if n > cycle.LastReplyCap {
		t.Fatalf("cache holds %d", n)
	}
}

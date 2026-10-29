//go:build integration

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLastReplyUnchangedTranscriptScansOnce(t *testing.T) {
	var replies lastReplies
	p := filepath.Join(t.TempDir(), "a.jsonl")
	now := time.Now()
	appendTo(t, p, replyLine("1", 10, now), now)
	before := replies.scans.Load()
	for range 3 {
		r, err := replies.read(p)
		if err != nil || r.Context != 10 || r.TTL != 5*time.Minute {
			t.Fatalf("got %+v, %v", r, err)
		}
	}
	if n := replies.scans.Load() - before; n != 1 {
		t.Fatalf("scans = %d, want 1", n)
	}
}

func TestLastReplyAppendedReplyIncrementalMatchesWhole(t *testing.T) {
	var replies lastReplies
	t.Parallel()
	p := filepath.Join(t.TempDir(), "a.jsonl")
	now := time.Now()
	appendTo(t, p, replyLine("1", 10, now), now)
	replies.read(p)
	appendTo(t, p, replyLine("2", 20, now)+`{"type":"user"}`+"\n", now.Add(time.Second))
	got, err := replies.read(p)
	if err != nil || got.Context != 20 {
		t.Fatalf("got %+v, %v", got, err)
	}
	replies.drop(p)
	whole, err := replies.read(p)
	if err != nil || *whole != *got {
		t.Fatalf("incremental %+v, whole %+v, %v", got, whole, err)
	}
}

func TestLastReplyShrunkFileReadWhole(t *testing.T) {
	var replies lastReplies
	p := filepath.Join(t.TempDir(), "a.jsonl")
	now := time.Now()
	appendTo(t, p, replyLine("1", 10, now)+replyLine("2", 20, now), now)
	replies.read(p)
	os.Remove(p)
	appendTo(t, p, replyLine("3", 7, now), now.Add(time.Second))
	before := replies.scans.Load()
	got, err := replies.read(p)
	if err != nil || got.Context != 7 || replies.scans.Load()-before != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestLastReplyErrorNotCached(t *testing.T) {
	var replies lastReplies
	t.Parallel()
	p := filepath.Join(t.TempDir(), "a.jsonl")
	now := time.Now()
	appendTo(t, p, `{"type":"user"}`+"\n", now)
	if _, err := replies.read(p); err == nil {
		t.Fatal("want error")
	}
	appendTo(t, p, replyLine("1", 5, now), now.Add(time.Second))
	if r, err := replies.read(p); err != nil || r.Context != 5 {
		t.Fatalf("got %+v, %v", r, err)
	}
}

func TestLastReplyCacheBounded(t *testing.T) {
	var replies lastReplies
	dir := t.TempDir()
	now := time.Now()
	for i := range lastReplyCap + 20 {
		p := filepath.Join(dir, fmt.Sprintf("%d.jsonl", i))
		appendTo(t, p, replyLine("1", 1, now), now)
		replies.read(p)
	}
	replies.mu.Lock()
	n := len(replies.m)
	replies.mu.Unlock()
	if n > lastReplyCap {
		t.Fatalf("cache holds %d", n)
	}
}

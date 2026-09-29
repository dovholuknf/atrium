package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func replyLine(id string, ctx int64, at time.Time) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"id":%q,"model":"m",`+
		`"usage":{"input_tokens":%d,"output_tokens":1,"cache_creation":{"ephemeral_5m_input_tokens":1}}}}`+"\n",
		at.Format(time.RFC3339Nano), id, ctx)
}

func appendTo(t testing.TB, path, s string, mtime time.Time) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(s)
	f.Close()
	os.Chtimes(path, mtime, mtime)
}

func TestLastReplyUnchangedTranscriptScansOnce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	now := time.Now()
	appendTo(t, p, replyLine("1", 10, now), now)
	before := lastReplyScans.Load()
	for range 3 {
		r, err := readLastReply(p)
		if err != nil || r.Context != 10 || r.TTL != 5*time.Minute {
			t.Fatalf("got %+v, %v", r, err)
		}
	}
	if n := lastReplyScans.Load() - before; n != 1 {
		t.Fatalf("scans = %d, want 1", n)
	}
}

func TestLastReplyAppendedReplyIncrementalMatchesWhole(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	now := time.Now()
	appendTo(t, p, replyLine("1", 10, now), now)
	readLastReply(p)
	appendTo(t, p, replyLine("2", 20, now)+`{"type":"user"}`+"\n", now.Add(time.Second))
	got, err := readLastReply(p)
	if err != nil || got.Context != 20 {
		t.Fatalf("got %+v, %v", got, err)
	}
	lastReplyDrop(p)
	whole, err := readLastReply(p)
	if err != nil || *whole != *got {
		t.Fatalf("incremental %+v, whole %+v, %v", got, whole, err)
	}
}

func TestLastReplyShrunkFileReadWhole(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	now := time.Now()
	appendTo(t, p, replyLine("1", 10, now)+replyLine("2", 20, now), now)
	readLastReply(p)
	os.Remove(p)
	appendTo(t, p, replyLine("3", 7, now), now.Add(time.Second))
	before := lastReplyScans.Load()
	got, err := readLastReply(p)
	if err != nil || got.Context != 7 || lastReplyScans.Load()-before != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestLastReplyErrorNotCached(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	now := time.Now()
	appendTo(t, p, `{"type":"user"}`+"\n", now)
	if _, err := readLastReply(p); err == nil {
		t.Fatal("want error")
	}
	appendTo(t, p, replyLine("1", 5, now), now.Add(time.Second))
	if r, err := readLastReply(p); err != nil || r.Context != 5 {
		t.Fatalf("got %+v, %v", r, err)
	}
}

func TestLastReplyCacheBounded(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for i := range lastReplyCap + 20 {
		p := filepath.Join(dir, fmt.Sprintf("%d.jsonl", i))
		appendTo(t, p, replyLine("1", 1, now), now)
		readLastReply(p)
	}
	lastReplyCache.mu.Lock()
	n := len(lastReplyCache.m)
	lastReplyCache.mu.Unlock()
	if n > lastReplyCap {
		t.Fatalf("cache holds %d", n)
	}
}

// BenchmarkLastReply30Cards reads 30 cards' 2 MB transcripts, as one board
// poll does. Cached, the run is 30 stats. Uncached, 30 decodes of 2 MB.
func BenchmarkLastReply30Cards(b *testing.B) {
	dir := b.TempDir()
	now := time.Now()
	pad := `{"type":"user","message":{"content":"` + strings.Repeat("x", 800) + `"}}` + "\n"
	var paths []string
	for i := range 30 {
		p := filepath.Join(dir, fmt.Sprintf("%d.jsonl", i))
		var sb strings.Builder
		for j := 0; sb.Len() < transcriptTail-2000; j++ {
			if j%4 == 0 {
				sb.WriteString(replyLine(fmt.Sprint(j), int64(j), now))
			} else {
				sb.WriteString(pad)
			}
		}
		appendTo(b, p, sb.String(), now)
		paths = append(paths, p)
	}
	b.ResetTimer()
	for range b.N {
		for _, p := range paths {
			if _, err := readLastReply(p); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkLastReply30CardsUncached is the same poll with the cache emptied
// first, which is what every poll cost before the cache.
func BenchmarkLastReply30CardsUncached(b *testing.B) {
	dir := b.TempDir()
	now := time.Now()
	pad := `{"type":"user","message":{"content":"` + strings.Repeat("x", 800) + `"}}` + "\n"
	var paths []string
	for i := range 30 {
		p := filepath.Join(dir, fmt.Sprintf("%d.jsonl", i))
		var sb strings.Builder
		for j := 0; sb.Len() < transcriptTail-2000; j++ {
			if j%4 == 0 {
				sb.WriteString(replyLine(fmt.Sprint(j), int64(j), now))
			} else {
				sb.WriteString(pad)
			}
		}
		appendTo(b, p, sb.String(), now)
		paths = append(paths, p)
	}
	b.ResetTimer()
	for range b.N {
		for _, p := range paths {
			lastReplyDrop(p)
			if _, err := readLastReply(p); err != nil {
				b.Fatal(err)
			}
		}
	}
}

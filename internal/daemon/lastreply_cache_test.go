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

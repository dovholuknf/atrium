//go:build integration

package api

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// manyCards is a room like claude-sg4: n cards, each in its own directory with a file two folders down.
func manyCards(t *testing.T, n int) (*Server, []*store.Task) {
	st := openMigrated(t)
	base := t.TempDir()
	var tasks []*store.Task
	for i := 0; i < n; i++ {
		dir := filepath.Join(base, "cards", "c"+strconv.Itoa(i), "work")
		if err := os.MkdirAll(filepath.Join(dir, "internal", "api"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "internal", "api", "x.go"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		task, _, err := st.Register(store.Observed{
			WireName: "c" + strconv.Itoa(i), Worktree: filepath.ToSlash(dir), Runner: "claude",
		})
		if err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, task)
	}
	return New(st), tasks
}

// A row holding one absolute path sends the path and one token per folder above its file. It must be answered in
// well under the 250 ms it took on a room with 35 cards, where every token resolved every open root again.
func TestAProbeIsFastOnARoomWithManyCards(t *testing.T) {
	s, tasks := manyCards(t, 40)
	p := tasks[5].Worktree + "/internal/api/x.go"
	toks := []string{p}
	for i := 3; i < len(p); i++ {
		if p[i] == '/' && p[i-1] != '/' {
			toks = append(toks, p[:i])
		}
	}
	want := len(probed(t, s, tasks[5].ID, toks))
	if want < 2 {
		t.Fatalf("only %d of %d tokens came back", want, len(toks))
	}

	// The fastest run inside a few seconds, so a stalled merge room (52ms best of 50 once, 1ms idle) does not stand in for
	// the 250ms regression, which stays over 50ms on every run.
	per := time.Hour
	for end := time.Now().Add(5 * time.Second); per > 50*time.Millisecond && time.Now().Before(end); {
		at := time.Now()
		if got := probed(t, s, tasks[5].ID, toks); len(got) != want {
			t.Fatalf("%d tokens came back, then %d", want, len(got))
		}
		if d := time.Since(at); d < per {
			per = d
		}
	}
	if per > 50*time.Millisecond {
		t.Fatalf("a probe of %d tokens took %v with 40 cards, want under 50ms", len(toks), per)
	}
}

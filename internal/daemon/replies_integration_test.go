//go:build integration

package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/dovholuknf/atrium/internal/store"
)

// r-024: main-chain assistant text only, one reply per message id, oldest first,
// the last n, and each reply cut at replyTextMax.
func TestRepliesAreTheLastMainChainTextOldestFirst(t *testing.T) {
	f := newUsageFix(t)
	path := f.path
	base := f.base
	transcriptReply(t, path, "m1", base, false, textBlock("first answer"))
	transcriptReply(t, path, "m2", base.Add(time.Second), false,
		map[string]any{"type": "thinking", "thinking": "secret"},
		map[string]any{"type": "tool_use", "name": "Bash", "input": map[string]any{"command": "ls"}})
	transcriptReply(t, path, "m3", base.Add(2*time.Second), true, textBlock("a subagent's words"))
	transcriptReply(t, path, "m4", base.Add(3*time.Second), false, textBlock("second, part one"))
	transcriptReply(t, path, "m4", base.Add(3*time.Second), false, textBlock("second, part two"))
	transcriptReply(t, path, "m5", base.Add(4*time.Second), false, textBlock(strings.Repeat("x", replyTextMax+500)))

	d := &Daemon{st: f.st, usage: f.u, ctx: newContextSizes()}

	v, err := d.repliesFor(f.task.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if v.Source != "transcript" || len(v.Replies) != 3 {
		t.Fatalf("got %+v", v)
	}
	if v.Replies[0].Text != "first answer" {
		t.Fatalf("oldest first, got %q", v.Replies[0].Text)
	}
	if v.Replies[1].Text != "second, part one\n\nsecond, part two" {
		t.Fatalf("one reply per message id, got %q", v.Replies[1].Text)
	}
	if !v.Replies[2].Truncated || len(v.Replies[2].Text) > replyTextMax {
		t.Fatalf("a long reply is not cut: %d bytes, truncated=%v", len(v.Replies[2].Text), v.Replies[2].Truncated)
	}
	for _, r := range v.Replies {
		if strings.Contains(r.Text, "secret") || strings.Contains(r.Text, "subagent") || strings.Contains(r.Text, "Bash") {
			t.Fatalf("thinking, a tool call or a sidechain leaked: %q", r.Text)
		}
	}

	// n=1 is the last one. n over the cap is held to it.
	if v, _ := d.repliesFor(f.task.ID, 1); len(v.Replies) != 1 || !v.Replies[0].Truncated {
		t.Fatalf("n=1 gave %+v", v)
	}
	if v, _ := d.repliesFor(f.task.ID, 99); len(v.Replies) != 3 {
		t.Fatalf("there are 3 text replies, got %d", len(v.Replies))
	}
}

// The conversation the runner last started, not the stored resume id (r-021).
func TestRepliesFollowTheStartedSession(t *testing.T) {
	f := newUsageFix(t)
	dir := filepath.Dir(f.path)
	fresh := filepath.Join(dir, "s2.jsonl")
	f.u.transcript = func(cwd, id string) string {
		switch id {
		case "s1":
			return f.path
		case "s2":
			return fresh
		}
		return ""
	}
	transcriptReply(t, f.path, "old", f.base, false, textBlock("from before the clear"))
	transcriptReply(t, fresh, "new", f.base.Add(time.Second), false, textBlock("after the clear"))
	d := &Daemon{st: f.st, usage: f.u, ctx: newContextSizes()}
	d.ctx.started(f.task.ID, "s2")

	v, err := d.repliesFor(f.task.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Replies) != 1 || v.Replies[0].Text != "after the clear" {
		t.Fatalf("read the old conversation: %+v", v)
	}
}

// A card with no readable transcript (codex here) answers from the screen, and
// an unknown card is 404 on the route.
func TestRepliesFallBackToTheScreenAndTheRouteAnswers404(t *testing.T) {
	d := testDaemon(t)
	task, _, err := d.st.Register(store.Observed{WireName: "codexer", Worktree: "/tmp/codexer", Runner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	v, err := d.repliesFor(task.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if v.Source != "screen" || v.Replies == nil {
		t.Fatalf("got %+v, want source screen with a (possibly empty) list", v)
	}

	ts := httptest.NewServer(d.ap.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/v1/tasks/01a0nosuchcard/replies")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("an unknown card answered %d", resp.StatusCode)
	}
	resp, err = http.Get(ts.URL + "/v1/tasks/" + task.ID + "/replies?n=2")
	if err != nil {
		t.Fatal(err)
	}
	var body RepliesView
	_ = json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || body.Source != "screen" {
		t.Fatalf("the route answered %d %+v", resp.StatusCode, body)
	}
}

// r-042: only a missing row is "no such card". A store error is an error.
func TestRepliesForSurfacesAStoreError(t *testing.T) {
	f := newUsageFix(t)
	d := &Daemon{st: f.st, usage: f.u, ctx: newContextSizes()}
	if _, err := d.repliesFor("01a0nosuchcard", 3); !errors.Is(err, errNoSuchCard) {
		t.Fatalf("an unknown card gave %v", err)
	}
	f.st.Close()
	_, err := d.repliesFor(f.task.ID, 3)
	if err == nil || errors.Is(err, errNoSuchCard) {
		t.Fatalf("a store error was answered as no such card: %v", err)
	}
}

// r-042: a transcript over the tail window is read from its tail, so what lies
// before it is gone and the cut line is skipped, not an error.
func TestReadRepliesSeeksTheTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.jsonl")
	base := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	transcriptReply(t, path, "old", base, false, textBlock("ancient"))
	pad := `{"type":"user","pad":"` + strings.Repeat("p", 100<<10) + `"}` + "\n"
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	for written := 0; written < transcriptTail+len(pad); written += len(pad) {
		fh.WriteString(pad)
	}
	fh.Close()
	transcriptReply(t, path, "new", base.Add(time.Minute), false, textBlock("recent"))
	if info, _ := os.Stat(path); info.Size() <= transcriptTail {
		t.Fatalf("the fixture is %d bytes, not over the tail", info.Size())
	}
	got, err := readReplies(path, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Text != "recent" {
		t.Fatalf("want only the recent reply, got %+v", got)
	}
}

// r-042: a changed size or modification time is read again, an unchanged file is not.
func TestReadRepliesCacheInvalidates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.jsonl")
	base := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	transcriptReply(t, path, "m1", base, false, textBlock("aaaa"))
	got, err := readReplies(path, 5)
	if err != nil || len(got) != 1 || got[0].Text != "aaaa" {
		t.Fatalf("first read: %+v %v", got, err)
	}
	// Size change.
	transcriptReply(t, path, "m2", base.Add(time.Second), false, textBlock("bbbb"))
	if got, _ = readReplies(path, 5); len(got) != 2 {
		t.Fatalf("a grown file was served from the cache: %+v", got)
	}
	// Same size, later mtime: rewrite the last text in place.
	info, _ := os.Stat(path)
	b, _ := os.ReadFile(path)
	b = []byte(strings.Replace(string(b), "bbbb", "cccc", 1))
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, time.Now(), info.ModTime().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, _ = readReplies(path, 5); len(got) != 2 || got[1].Text != "cccc" {
		t.Fatalf("a touched file was served from the cache: %+v", got)
	}
	// Untouched: the cache answers, so a change made behind its back is not seen.
	info, _ = os.Stat(path)
	b = []byte(strings.Replace(string(b), "cccc", "dddd", 1))
	os.WriteFile(path, b, 0o600)
	os.Chtimes(path, time.Now(), info.ModTime())
	if got, _ = readReplies(path, 5); got[1].Text != "cccc" {
		t.Fatalf("an unchanged size and mtime was read again: %+v", got)
	}
}

// r-042: a reply cut at replyTextMax through a multi-byte rune stays valid UTF-8.
func TestRepliesCutOnARuneBoundary(t *testing.T) {
	for _, r := range []string{"é", "€", "😀"} {
		text := "a" + strings.Repeat(r, replyTextMax) // the first rune starts at an odd offset
		path := filepath.Join(t.TempDir(), "m.jsonl")
		transcriptReply(t, path, "m1", time.Now(), false, textBlock(text))
		got, err := readReplies(path, 1)
		if err != nil || len(got) != 1 {
			t.Fatalf("%q: %+v %v", r, got, err)
		}
		if !got[0].Truncated || len(got[0].Text) > replyTextMax || !utf8.ValidString(got[0].Text) {
			t.Fatalf("%q: truncated=%v len=%d valid=%v", r, got[0].Truncated, len(got[0].Text), utf8.ValidString(got[0].Text))
		}
	}
	if s, cut := keepTail("é"+strings.Repeat("a", 9), 10); !cut || !utf8.ValidString(s) || s != strings.Repeat("a", 9) {
		t.Fatalf("keepTail through a rune: %q %v", s, cut)
	}
}

// r-042: a reply whose timestamp is missing or unparseable is still a reply,
// with a zero time.
func TestRepliesWithBadTimestamps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ts.jsonl")
	for i, ts := range []string{"", "not a time", "2026-09-28 10:00:00"} {
		b, _ := json.Marshal(map[string]any{
			"type": "assistant", "timestamp": ts,
			"message": map[string]any{"id": fmt.Sprint("m", i), "content": []map[string]any{textBlock(fmt.Sprint("r", i))}},
		})
		fh, _ := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		fmt.Fprintln(fh, string(b))
		fh.Close()
	}
	got, err := readReplies(path, 5)
	if err != nil || len(got) != 3 {
		t.Fatalf("%+v %v", got, err)
	}
	for _, r := range got {
		if !r.At.IsZero() && r.At.Year() > 1 {
			t.Fatalf("a bad timestamp gave %v", r.At)
		}
	}
}

// r-042: an unsupervised card owns a session when it has no pid, no live pid, or
// the same pid.
func TestOwnsSessionUnsupervised(t *testing.T) {
	d := testDaemon(t)
	alive := os.Getpid()
	dead := 2147483000
	cases := []struct {
		name string
		card int
		pid  int
		want bool
	}{
		{"no pid from the hook is believed", alive, 0, true},
		{"card has no pid", 0, 555, true},
		{"same process", alive, alive, true},
		{"card's process is gone", dead, 555, true},
		{"card's process is alive and another is asking", alive, alive + 1, false},
	}
	for _, c := range cases {
		if got := d.ownsSession(&store.Task{ID: "unsupervised", PID: c.card}, c.pid); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}
